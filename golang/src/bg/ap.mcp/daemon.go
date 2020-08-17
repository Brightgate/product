/*
 * Copyright 2019 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"bg/ap_common/aputil"
	"bg/ap_common/mcp"
)

const (
	// If a daemon fails 10 times in a row, we will stop trying to restart
	// it.
	failuresAllowed = 10
	successTime     = time.Duration(time.Minute)
	warnPeriod      = time.Duration(5 * time.Minute)

	onlineTimeout  = time.Duration(10 * time.Second)
	offlineTimeout = time.Duration(15 * time.Second)

	onlineFile = "/tmp/mcp.online"
)

type daemon struct {
	// State exported through the GetState API
	Name        string
	Binary      string
	Modes       []string `json:"Modes,omitempty"`
	Options     []string `json:"Options,omitempty"`
	DependsOn   *string  `json:"DependsOn,omitempty"`
	ThirdParty  bool     `json:"ThirdParty,omitempty"`
	MemWarn     uint64   `json:"MemWarn,omitempty"`
	MemKill     uint64   `json:"MemKill,omitempty"`
	SoftTimeout uint64   `json:"SoftTimeout,omitempty"`
	Privileged  bool

	// Daemon definition from mcp.json
	execpath     string
	args         []string
	dependencies []*daemon // which daemons does this depend on
	dependents   []*daemon // which daemons depend on this one

	// Channels used to control a daemon's behavior
	evaluate chan bool // change in state for daemon or dependency
	goal     chan int  // change in the goal state for this daemon

	// State machine
	goalState int       // desired state
	state     int       // current state
	setTime   time.Time // when the current state was entered

	// The process corresponding to the current instance of the daemon
	child        *aputil.Child
	childState   *aputil.ChildState
	startTime    time.Time
	stopTime     time.Time
	exitDetected bool

	// Tracking daemon health
	failures    int // number of consecutive failures
	memWarnTime time.Time

	sync.Mutex
}

type daemonSet map[string]*daemon

type remoteState struct {
	eol   time.Time
	state []*mcp.DaemonState
}

var (
	daemons struct {
		local  daemonSet              // map of daemons on this node
		remote map[string]remoteState // per-node daemon sets

		// The lock protects the contents of the maps, but not the
		// contents of the daemons within the maps.
		sync.Mutex
	}

	onlineState struct {
		daemons map[string]bool
		track   bool
		sync.Mutex
	}

	self *daemon

	stateEvalFns = map[int]func(*daemon){
		mcp.BROKEN:   stateBroken,
		mcp.OFFLINE:  stateOffline,
		mcp.BLOCKED:  stateBlocked,
		mcp.STARTING: stateStarting,
		mcp.INITING:  stateStarting,
		mcp.FAILSAFE: stateOnline,
		mcp.ONLINE:   stateOnline,
		mcp.STOPPING: stateStopping,
	}
)

func activeDaemons() daemonSet {
	list := make(daemonSet)
	for n, d := range daemons.local {
		if !d.offline() {
			list[n] = d
		}
	}
	return list
}

func (d *daemon) online() bool {
	return d.state == mcp.FAILSAFE || d.state == mcp.ONLINE
}

func (d *daemon) offline() bool {
	return d.state == mcp.OFFLINE || d.state == mcp.BROKEN
}

// A daemon is blocked from running if any of its dependencies are
// not running.
func (d *daemon) blocked() bool {
	for _, dep := range d.dependencies {
		if !dep.online() {
			return true
		}
	}

	return false
}

func (d *daemon) setState(state int) {
	if d.state != state {
		logDebug("%s transitioning from %s to %s", d.Name,
			mcp.States[d.state], mcp.States[state])

		d.state = state
		d.setTime = time.Now()
		d.evaluate <- true
		for _, dep := range d.dependents {
			dep.evaluate <- true
		}
	}
}

// Wait for the child process to exit.
func (d *daemon) wait() {
	var msg string

	err := d.child.Wait()
	d.stopTime = time.Now()

	if err == nil {
		msg = "exited cleanly"
	} else {
		msg = fmt.Sprintf("exited with '%v'", err)
	}
	logInfo("%s exited %s after %s", d.Name, msg, time.Since(d.startTime))

	if err != nil && d.goalState != mcp.OFFLINE && !d.blocked() {
		// XXX: there are certainly times we would like to know that a
		// daemon crashed while trying to shut down.  However, because
		// we don't yet support the cancellation of cfgapi operations,
		// this would result in far too many false positives as daemons
		// get wedged waiting for a dead ap.configd to respond to them.
		err = aputil.ReportCrash(d.Name, msg, d.child.LogContents())
		if err != nil {
			logWarn("unable to record crash: %v", err)
		}
	}
	d.Lock()
	if d.state != mcp.BROKEN {
		d.setState(mcp.OFFLINE)
	}
	d.child = nil
	d.Unlock()
	d.evaluate <- true
}

// Attempt to start the daemon as a child process
func (d *daemon) start() {
	var err error

	logInfo("starting %s", d.Name)
	if d.child != nil {
		logWarn("%s already running as pid %d", d.Name,
			d.child.Process.Pid)
		return
	}

	out := os.Stderr
	if logfile != nil {
		out = logfile
	}

	// If any dependent daemons were marked as BROKEN, move them to OFFLINE
	// and try starting them now.  Perhaps whatever broke them has been fixed
	// by restarting this daemon.
	for _, x := range daemons.local {
		if x.DependsOn != nil && *x.DependsOn == d.Name {
			x.Lock()
			if x.state == mcp.BROKEN {
				x.setState(mcp.OFFLINE)
				x.goal <- mcp.ONLINE
			}
			x.Unlock()
		}
	}
	os.Setenv("APMODE", nodeMode)
	os.Setenv("APGATEWAY", gatewayAddr.String())
	child := aputil.NewChild(d.execpath, d.args...)
	child.UseStdLog("", 0, out)

	if d.failures >= (failuresAllowed / 2) {
		logInfo("starting %s in failsafe", d.Name)
		child.SetEnv("BG_FAILSAFE", "true")
	}

	child.SetEnv(aputil.MemFaultWarnMB, strconv.Itoa(int(d.MemWarn)))
	child.SetEnv(aputil.MemFaultKillMB, strconv.Itoa(int(d.MemKill)))

	if !d.Privileged {
		child.SetUID(nobodyUID, nobodyUID)
	}
	if d.SoftTimeout != 0 {
		ms := time.Duration(d.SoftTimeout) * time.Millisecond
		child.SetSoftTimeout(ms)
	}

	// Put each daemon into its own process group
	child.SetPgid(true)

	// Keep the last 32k of log data in memory, so we can dump it on failure
	child.LogPreserve(32 * 1024)

	d.setState(mcp.STARTING)
	d.startTime = time.Now()
	if err = child.Start(); err != nil {
		logWarn("%s unable to launch: %v", d.Name, err)
		d.setState(mcp.OFFLINE)
		return
	}
	d.child = child
	if d.ThirdParty {
		// A third party daemon doesn't know how to talk to us, so
		// we won't get an online notification.  Just set it here.
		d.setState(mcp.ONLINE)
	}

	go d.wait()
}

// If the daemon is running, kill the process with SIGABRT to try to collect
// stacktraces in the log.
func (d *daemon) crash() {
	if !d.offline() {
		d.setState(mcp.STOPPING)

		if c := d.child; c != nil {
			pid := -1
			if c.Process != nil {
				pid = c.Process.Pid
			}

			logInfo("Crashing %s (%d)", d.Name, pid)
			c.Signal(syscall.SIGABRT)
			time.Sleep(time.Millisecond)
		}
		d.goal <- mcp.OFFLINE
	}
}

// If the daemon is running, kill its process.
func (d *daemon) stop() {
	if !d.offline() && d.state != mcp.STOPPING {
		d.setState(mcp.STOPPING)
		if c := d.child; c != nil {
			if len(d.dependents) > 0 {
				// Any dependents should react to our change in
				// state by shutting themselves down.  Give
				// them a second to exit cleanly before we tear
				// down any infrastructure they rely on.
				time.Sleep(time.Second)
			}
			pid := -1
			if c.Process != nil {
				pid = c.Process.Pid
			}

			logInfo("Stopping %s (%d)", d.Name, pid)
			c.Stop()
		}
	}
}

func stateBlocked(d *daemon) {
	if d.goalState == mcp.OFFLINE || !d.blocked() {
		// The daemon's goal has changed to OFFLINE or its dependencies
		// are now running.  Drop to OFFLINE so we can try starting
		// again.
		d.setState(mcp.OFFLINE)
		d.failures = 0
	}
}

func stateStarting(d *daemon) {
	if d.goalState == mcp.OFFLINE || d.blocked() {
		// The daemon's goal has changed to OFFLINE or one if its
		// dependencies stopped, so this daemon should be stopped.
		d.stop()
		d.failures = 0

	} else if time.Since(d.startTime) > onlineTimeout {
		// The daemon has been stuck in STARTING/INITING for too long.
		// Kill it and try again.
		logWarn("%s took more than %v to come online. "+
			"Giving up.", d.Name, onlineTimeout)
		d.crash()
		d.failures++
	}
}

func stateOnline(d *daemon) {
	if d.goalState == mcp.OFFLINE || d.blocked() {
		// The daemon's goal has changed to OFFLINE or one if its
		// dependencies stopped, so this daemon should be stopped.
		d.stop()
		d.failures = 0

	} else if time.Since(d.startTime) > successTime {
		// The daemon has been running for long enough for us to assume
		// it's a success.
		d.failures = 0
	}
}

func stateOffline(d *daemon) {
	if !d.exitDetected {
		d.exitDetected = true
		if time.Since(d.startTime) < successTime {
			// The daemon died quickly enough that we assume it
			// failed or crashed.
			d.failures++
		}
	}

	restartDelay := time.Second * time.Duration(d.failures)
	ready := time.Since(d.stopTime) > restartDelay
	if d.goalState == mcp.ONLINE {
		if d.blocked() {
			d.setState(mcp.BLOCKED)
			d.failures = 0

		} else if d.failures > failuresAllowed {
			logWarn("%s is dying too quickly", d.Name)
			d.setState(mcp.BROKEN)

		} else if ready {
			d.exitDetected = false
			d.start()
		}
	}
}

func stateStopping(d *daemon) {
}

func stateBroken(d *daemon) {
}

func (d *daemon) daemonLoop() {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	d.Lock()
	for {
		// Evaluate whether we should move to a different position in
		// the daemon state machine.
		oldState := d.state
		stateEvalFns[oldState](d)
		newState := d.state

		if newState != oldState {
			continue
		}

		d.Unlock()
		for spin := true; spin; {
			select {
			case <-ticker.C:
			case <-d.evaluate:
			case goal := <-d.goal:
				if goal != d.goalState {
					logDebug("%s has new goal: %s", d.Name,
						mcp.States[goal])
					d.goalState = goal
					daemonSaveOnline()
				}
			}

			// If we have more signals pending, consume them now
			spin = (len(d.evaluate) + len(d.goal)) > 0
		}
		d.Lock()
	}
}

func daemonToState(d *daemon) *mcp.DaemonState {
	state := mcp.DaemonState{
		Name:  d.Name,
		State: d.state,
		Since: d.setTime,
		Node:  nodeName,
		Pid:   d.child.GetPID(),
	}
	if s, _ := d.child.GetState(); s != nil {
		state.VMSize = s.VMSize
		state.RssSize = s.RssSize
		state.VMSwap = s.VMSwap
		state.Utime = s.Utime
		state.Stime = s.Stime
	}
	return &state
}

func getCurrentState(set daemonSet, includeSelf bool) mcp.DaemonList {
	list := make(mcp.DaemonList, 0)

	if includeSelf {
		list = append(list, daemonToState(self))
	}

	for _, d := range set {
		list = append(list, daemonToState(d))
	}
	list.Sort()

	return list
}

// Shut down any daemons that may be running.  Return a set containing those
// daemons.
func daemonStopAll() daemonSet {
	daemons.Lock()
	defer daemons.Unlock()

	running := activeDaemons()
	active := make(daemonSet)
	for n, d := range running {
		active[n] = d
	}

	deadline := time.Now().Add(offlineTimeout)
	handleStop(running)
	for len(running) > 0 && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
		running = activeDaemons()
	}

	if len(running) > 0 {
		msg := "failed to shut down: "
		for n := range running {
			msg += n + " "
		}
		logWarn("%s", msg)
		shutdown(1)
	}

	return active
}

// Build the lists of dependents and dependencies
func recomputeDependencies() {
	for _, d := range daemons.local {
		d.Lock()
		d.dependencies = make([]*daemon, 0)
		d.dependents = make([]*daemon, 0)
	}

	for _, d := range daemons.local {
		if d.DependsOn != nil {
			if x := daemons.local[*d.DependsOn]; x != nil {
				x.dependents = append(x.dependents, d)
				d.dependencies = append(d.dependencies, x)
			}
		}
	}

	for _, d := range daemons.local {
		d.Unlock()
		d.evaluate <- true
	}
}

// Given a daemon definition loaded from the json file, initialize all of the
// run-time state for the daemon and launch the goroutine that monitors it.  If
// we are reloading the json file, refresh those fields of the run-time state
// that may have been changed by modifications to the file.
func daemonDefine(def *daemon) {
	re := regexp.MustCompile(`\$APROOT`)

	d := daemons.local[def.Name]
	if d == nil {
		d = def
		d.state = mcp.OFFLINE
		d.goalState = mcp.OFFLINE
		d.setTime = time.Unix(0, 0)
		d.evaluate = make(chan bool, 20)
		d.goal = make(chan int, 20)
		daemons.local[d.Name] = d
	} else {
		// Replace any fields that might reasonably have changed
		d.Binary = def.Binary
		d.Options = def.Options
		d.DependsOn = def.DependsOn
		d.Privileged = def.Privileged
		d.MemWarn = def.MemWarn
		d.MemKill = def.MemKill
	}

	d.args = make([]string, 0)
	for _, o := range d.Options {
		// replace any instance of $APROOT with the real path
		o = re.ReplaceAllString(o, *aproot)
		options := strings.Split(o, " ")
		d.args = append(d.args, options...)
	}
	if d.Binary[0] == byte('/') {
		d.execpath = plat.ExpandDirPath("__APROOT__", d.Binary)
	} else {
		d.execpath = plat.ExpandDirPath("__APPACKAGE__", "bin", d.Binary)
	}
	logDebug("%s execpath is %s", d.Binary, d.execpath)

	if d == def {
		go d.daemonLoop()
	}
}

func loadDefinitions() error {
	daemons.Lock()
	defer daemons.Unlock()

	fn := *cfgfile
	if len(fn) == 0 {
		fn = plat.ExpandDirPath("__APPACKAGE__", "/etc/mcp.json")
	}

	file, err := ioutil.ReadFile(fn)
	if err != nil {
		return fmt.Errorf("failed to load daemon configs from %s: %v",
			fn, err)
	}

	set := make(daemonSet)
	err = json.Unmarshal(file, &set)
	if err != nil {
		return fmt.Errorf("failed to import daemon configs from %s: %v",
			fn, err)
	}

	for _, def := range set {
		for _, mode := range def.Modes {
			if mode == nodeMode {
				daemonDefine(def)
				break
			}
		}
	}

	if len(daemons.local) == 0 {
		err = fmt.Errorf("no daemons for '%s' mode", nodeMode)
	} else {
		recomputeDependencies()
	}

	return err
}

func updateDaemonResources(d *daemon) {
	d.Lock()
	defer d.Unlock()

	mem := uint64(0)
	if d.child != nil {
		d.childState, _ = d.child.GetState()

		if c := d.childState; c != nil {
			mem = (c.RssSize + c.VMSwap) / (1024 * 1024)
		}
	}

	if d.MemKill > 0 && mem > d.MemKill {
		logWarn("%s using %dMB of memory - killing it", d.Name, mem)
		if d == self {
			shutdown(1)
		} else {
			// Before going through the normal child shutdown, send
			// it a SIGABRT.  Hopefully this will leave some
			// breadcrumbs to help figure out why it's using
			// excessive memory.
			if pid := d.child.GetPID(); pid > 0 {
				syscall.Kill(pid, syscall.SIGABRT)
				time.Sleep(10 * time.Millisecond)
			}
			d.stop()
		}
	} else if d.MemWarn > 0 && mem > d.MemWarn {
		now := time.Now()
		nextWarn := d.memWarnTime.Add(warnPeriod)
		if nextWarn.Before(now) {
			logWarn("%s using %dMB of memory", d.Name, mem)
			aputil.ReportMem(d.Name, mem, nil)
			d.memWarnTime = now

			if d == self {
				debug.FreeOSMemory()
			}
		}
	}
}

func resourceLoop() {
	t := time.NewTicker(5 * time.Second)

	for {
		if self != nil {
			updateDaemonResources(self)
		}
		for _, d := range daemons.local {
			updateDaemonResources(d)
		}

		<-t.C
	}
}

func daemonChooseAutostart() []*daemon {
	var data []byte
	var err error

	online := make(map[string]bool)
	if aputil.FileExists(onlineFile) {
		if data, err = ioutil.ReadFile(onlineFile); err != nil {
			logWarn("loading daemon list: %v", err)

		} else if err = json.Unmarshal(data, &online); err != nil {
			logWarn("importing daemon list: %v", err)
		}
	}

	if len(online) == 0 {
		for name := range daemons.local {
			online[name] = true
		}
	}
	onlineState.track = true
	onlineState.daemons = online

	o := make([]*daemon, 0)
	for name, d := range daemons.local {
		if online[name] {
			o = append(o, d)
		}
	}

	return o
}

func daemonSaveOnline() error {
	if !onlineState.track {
		return nil
	}

	onlineState.Lock()
	defer onlineState.Unlock()

	rewriteNeeded := false
	for name, x := range daemons.local {
		new := (x.goalState == mcp.ONLINE)
		old := onlineState.daemons[name]
		if old != new {
			onlineState.daemons[name] = new
			rewriteNeeded = true
		}
	}
	if !rewriteNeeded {
		return nil
	}

	data, err := json.Marshal(&onlineState.daemons)
	if err != nil {
		return fmt.Errorf("exporting daemon list: %v", err)
	}

	tmpfile, err := ioutil.TempFile("/tmp", "")
	if err != nil {
		return fmt.Errorf("creating temp file: %v", err)
	}

	name := tmpfile.Name()
	if _, err = tmpfile.Write(data); err != nil {
		err = fmt.Errorf("writing to temp file: %v", err)
		os.Remove(name)
	} else {
		tmpfile.Close()
		err = os.Rename(name, onlineFile)
	}

	return err
}

func daemonReinit() {
	daemons.local = make(daemonSet)
	daemons.remote = make(map[string]remoteState)

	if err := loadDefinitions(); err != nil {
		log.Fatalf("Failed to load daemon config: %v", err)
	}
}

func daemonInit() {
	daemonReinit()
	process, _ := os.FindProcess(os.Getpid())
	binary, _ := os.Executable()

	self = &daemon{
		Name:      pname,
		Binary:    binary,
		goalState: mcp.ONLINE,
		state:     mcp.ONLINE,
		setTime:   time.Now(),
		child: &aputil.Child{
			Process: process,
		},
		MemWarn:     20,
		MemKill:     40,
		SoftTimeout: 100,
	}

	if !*nostart {
		autostart := daemonChooseAutostart()
		for _, d := range autostart {
			d.goal <- mcp.ONLINE
		}
	}

	go resourceLoop()
}

