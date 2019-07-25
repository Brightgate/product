/*
 * COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 *
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
	"strings"
	"sync"
	"syscall"
	"time"

	"bg/ap_common/aputil"
	"bg/ap_common/mcp"
)

const (
	// Allow up to 4 failures in a 1 minute period before giving up
	failuresAllowed = 4
	restartPeriod   = time.Duration(time.Minute)
	warnPeriod      = time.Duration(5 * time.Minute)

	onlineTimeout  = time.Duration(15 * time.Second)
	offlineTimeout = time.Duration(15 * time.Second)
)

type daemon struct {
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

	execpath     string
	args         []string
	dependencies []*daemon
	dependents   []*daemon

	evaluate chan bool
	goal     chan int
	state    int

	memWarnTime time.Time
	setTime     time.Time
	child       *aputil.Child
	childState  *aputil.ChildState

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

	self *daemon
)

func (d *daemon) offline() bool {
	return d.state == mcp.OFFLINE || d.state == mcp.BROKEN
}

func (d *daemon) blocked() bool {
	for _, dep := range d.dependencies {
		if dep.state != mcp.ONLINE {
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

	startTime := time.Now()
	err := d.child.Wait()

	if err == nil {
		msg = "exited cleanly"
	} else {
		msg = fmt.Sprintf("exited with '%v'", err)
	}
	logInfo("%s exited %s after %s", d.Name, msg,
		time.Since(startTime))

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
	child := aputil.NewChild(d.execpath, d.args...)
	child.UseStdLog("", 0, out)

	if !d.Privileged {
		child.SetUID(nobodyUID, nobodyUID)
	}
	if d.SoftTimeout != 0 {
		ms := time.Duration(d.SoftTimeout) * time.Millisecond
		child.SetSoftTimeout(ms)
	}

	// Put each daemon into its own process group
	child.SetPgid(true)

	d.setState(mcp.STARTING)
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

func (d *daemon) stop() {
	if d.state == mcp.BLOCKED {
		d.setState(mcp.OFFLINE)
	} else if !d.offline() && d.state != mcp.STOPPING {
		d.setState(mcp.STOPPING)
		if c := d.child; c != nil {
			pid := -1
			if c.Process != nil {
				pid = c.Process.Pid
			}

			logInfo("Stopping %s (%d)", d.Name, pid)
			c.Stop()
		}
	}
}

func (d *daemon) daemonLoop() {
	timeout := time.NewTimer(0)
	startTimes := make([]time.Time, failuresAllowed)
	goal := mcp.OFFLINE

	d.Lock()
	for {
		if d.state == mcp.BROKEN {
			goal = mcp.OFFLINE
		}

		// Check to see whether our dependencies have changed state
		if d.state > mcp.BLOCKED && d.blocked() {
			// A dependency stopped, so we need to as well
			d.stop()
		} else if d.state == mcp.BLOCKED && !d.blocked() {
			// We're no longer blocked.  Drop to OFFLINE so we can
			// try starting again.
			d.setState(mcp.OFFLINE)
		}

		// Check to see whether we are currently in our intended state
		if goal == mcp.ONLINE && d.state == mcp.OFFLINE {
			if d.blocked() {
				d.setState(mcp.BLOCKED)
			} else {
				startTimes = append(startTimes[1:failuresAllowed],
					time.Now())
				timeout.Reset(onlineTimeout)
				d.start()
			}
		} else if goal == mcp.OFFLINE && !d.offline() {
			timeout.Stop()
			d.stop()
		}

		// We've taken any actions we can.  Now wait for our state to
		// change, our goal to change, or for an action to timeout
		d.Unlock()
		timedout := false
		for spin := true; spin; {
			select {
			case <-timeout.C:
				timedout = true
			case goal = <-d.goal:
				startTimes = make([]time.Time, failuresAllowed)
				logDebug("%s goal: %s", d.Name,
					mcp.States[goal])
			case <-d.evaluate:
			}
			// If we have more signals pending, consume them now
			spin = (len(d.evaluate) + len(d.goal)) > 0
		}
		d.Lock()

		if timedout && (d.state == mcp.INITING || d.state == mcp.STARTING) {
			logWarn("%s took more than %v to come online.  Giving up.",
				d.Name, onlineTimeout)
			d.stop()
			d.setState(mcp.BROKEN)
		}
		if (d.state != mcp.BROKEN) &&
			(time.Since(startTimes[0]) < restartPeriod) {
			logWarn("%s is dying too quickly", d.Name)
			d.stop()
			d.setState(mcp.BROKEN)
		}
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

func getCurrentState(set daemonSet) mcp.DaemonList {
	list := make(mcp.DaemonList, 0)

	list = append(list, daemonToState(self))

	for _, d := range set {
		list = append(list, daemonToState(d))
	}
	list.Sort()

	return list
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
		Name:    pname,
		Binary:  binary,
		state:   mcp.ONLINE,
		setTime: time.Now(),
		child: &aputil.Child{
			Process: process,
		},
		MemWarn:     20,
		MemKill:     40,
		SoftTimeout: 100,
	}

	go resourceLoop()
}
