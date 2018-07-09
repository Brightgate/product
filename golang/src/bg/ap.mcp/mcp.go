/*
 * COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
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
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"bg/ap_common/aputil"
	"bg/ap_common/mcp"
	"bg/ap_common/platform"
	"bg/base_def"
	"bg/base_msg"

	"github.com/golang/protobuf/proto"
	zmq "github.com/pebbe/zmq4"
)

type daemon struct {
	Name       string
	Binary     string
	Modes      []string `json:"Modes,omitempty"`
	Options    []string `json:"Options,omitempty"`
	DependsOn  *string  `json:"DependsOn,omitempty"`
	ThirdParty bool     `json:"ThirdParty,omitempty"`
	Privileged bool

	execpath     string
	args         []string
	dependencies []*daemon
	dependents   []*daemon

	evaluate chan bool
	goal     chan int
	state    int

	setTime time.Time
	child   *aputil.Child

	sync.Mutex
}

type daemonSet map[string]*daemon

type remoteDaemonState struct {
	eol   time.Time
	state []*mcp.DaemonState
}

const (
	pname = "ap.mcp"

	// Allow up to 4 failures in a 1 minute period before giving up
	failuresAllowed = 4
	restartPeriod   = time.Duration(time.Minute)

	remoteUpdatePeriod = time.Second
	onlineTimeout      = time.Duration(15 * time.Second)
	offlineTimeout     = time.Duration(15 * time.Second)

	nobodyUID = 65534 // uid for 'nobody'
	rootUID   = 0     // uid for 'root'
	pidfile   = "/var/tmp/ap.mcp.pid"
)

var (
	aproot   = flag.String("root", "", "Root of AP installation")
	apmode   = flag.String("mode", "", "Mode in which this AP should operate")
	cfgfile  = flag.String("c", "", "Alternate daemon config file")
	logname  = flag.String("l", "", "where to send log messages")
	nodeFlag = flag.String("nodeid", "", "new value for device nodeID")
	platFlag = flag.String("platform", "", "hardware platform name")
	verbose  = flag.Bool("v", false, "more verbose logging")

	logfile *os.File

	localDaemons  = make(daemonSet)
	remoteDaemons = make(map[string]remoteDaemonState)
	daemonLock    sync.RWMutex // Protects the map - not daemon state

	self *daemon
	plat *platform.Platform

	nodeName        string
	stateReverseMap map[string]int
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
		if *verbose {
			log.Printf("%s transitioning from %s to %s\n", d.Name,
				mcp.States[d.state], mcp.States[state])
		}

		d.state = state
		d.setTime = time.Now()
		d.evaluate <- true
		for _, dep := range d.dependents {
			dep.evaluate <- true
		}
	}
}

//
// Given a name, select the daemons that will be affected.  Currently the
// choices are all, one, or none.  Eventually, this could be expanded to
// identify daemons that should be acted on together.
//
func selectTargets(name *string) daemonSet {
	set := make(daemonSet)

	for _, d := range localDaemons {
		if *name == "all" || *name == d.Name {
			set[d.Name] = d
		}
	}
	return set
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
	log.Printf("%s exited %s after %s\n", d.Name, msg,
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

	log.Printf("starting %s\n", d.Name)
	if d.child != nil {
		log.Printf("%s already running as pid %d\n", d.Name,
			d.child.Process.Pid)
		return
	}

	out := os.Stderr
	if logfile != nil {
		out = logfile
	}

	child := aputil.NewChild(d.execpath, d.args...)
	child.LogOutputTo("", 0, out)

	if !d.Privileged {
		child.SetUID(nobodyUID, nobodyUID)
	}

	// Put each daemon into its own process group
	child.SetPgid(true)

	d.setState(mcp.STARTING)
	if err = child.Start(); err != nil {
		log.Printf("%s unable to launch: %v", d.Name, err)
		d.setState(mcp.OFFLINE)
		return
	}
	d.child = child
	if d.ThirdParty {
		// A third party daemon doesn't participate in the ZMQ updates,
		// so we won't get an online notification.  Just set it here.
		d.setState(mcp.ONLINE)
	}

	go d.wait()
}

func (d *daemon) stop() {
	if d.state == mcp.BLOCKED {
		d.setState(mcp.OFFLINE)
	} else if !d.offline() && d.state != mcp.STOPPING {
		d.setState(mcp.STOPPING)
		if d.child != nil {
			log.Printf("Stopping %s (%d)\n", d.Name,
				d.child.Process.Pid)
			d.child.Stop()
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
				if *verbose {
					log.Printf("%s goal: %s\n", d.Name,
						mcp.States[goal])
				}
			case <-d.evaluate:
			}
			// If we have more signals pending, consume them now
			spin = (len(d.evaluate) + len(d.goal)) > 0
		}
		d.Lock()

		if timedout && (d.state == mcp.INITING || d.state == mcp.STARTING) {
			log.Printf("%s took more than %v to come online.  Giving up.",
				d.Name, onlineTimeout)
			d.stop()
			d.setState(mcp.BROKEN)
		}
		if (d.state != mcp.BROKEN) &&
			(time.Since(startTimes[0]) < restartPeriod) {
			log.Printf("%s is dying too quickly", d.Name)
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

func handleGetState(set daemonSet, includeRemote bool) *string {
	var rval *string

	// If any node's data has expired, mark everything offline
	now := time.Now()
	for _, remoteList := range remoteDaemons {
		if remoteList.eol.Before(now) {
			remoteList.eol = now.AddDate(1, 0, 0)
			for _, d := range remoteList.state {
				if d.State > mcp.OFFLINE {
					d.State = mcp.OFFLINE
					d.Pid = -1
					d.Since = now
				}
			}
		}
	}

	list := getCurrentState(set)
	if includeRemote {
		for _, remoteList := range remoteDaemons {
			list = append(list, remoteList.state...)
		}
	}

	b, err := json.MarshalIndent(list, "", "  ")
	if err == nil {
		s := string(b)
		rval = &s
	}
	return rval
}

func handleSetState(set daemonSet, state *string) base_msg.MCPResponse_OpResponse {
	// A daemon can only update its own state, so we should never have more
	// than one in the set.
	if state != nil && len(set) == 1 {
		if s, ok := stateReverseMap[*state]; ok {
			for _, d := range set {
				d.Lock()
				d.setState(s)
				d.Unlock()
			}
			return mcp.OK
		}
	}
	return mcp.INVALID
}

func handlePeerUpdate(node, in *string, lifetime int32) (*string,
	base_msg.MCPResponse_OpResponse) {
	var (
		state mcp.DaemonList
		rval  *string
		code  base_msg.MCPResponse_OpResponse
	)

	b := []byte(*in)
	if err := json.Unmarshal(b, &state); err != nil {
		log.Printf("failed to unmarshal state from %s: %v", *node, err)
		code = mcp.INVALID
	} else {
		// The remote node tells us how long we should consider this
		// data to be valid.
		lifeDuration := time.Duration(lifetime) * time.Second
		remoteDaemons[*node] = remoteDaemonState{
			eol:   time.Now().Add(lifeDuration),
			state: state,
		}
		rval = handleGetState(localDaemons, false)
		code = mcp.OK
	}
	return rval, code
}

func handleStart(set daemonSet) {
	for _, d := range set {
		d.Lock()
		if d.state == mcp.BROKEN {
			d.setState(mcp.OFFLINE)
		}
		d.Unlock()
		log.Printf("Tell %s to come online\n", d.Name)
		d.goal <- mcp.ONLINE
	}
}

func handleStop(set daemonSet) int {
	running := make(daemonSet)
	for n, d := range set {
		if !d.offline() {
			running[n] = d
			d.goal <- mcp.OFFLINE
		}
	}

	// Wait for the daemons to die
	deadline := time.Now().Add(offlineTimeout)
	for len(running) > 0 && time.Now().Before(deadline) {
		for n, d := range running {
			if d.offline() {
				delete(running, n)
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if len(running) > 0 {
		msg := "failed to shut down: "
		for n := range running {
			msg += n + " "
		}
		log.Printf("%s\n", msg)
	}
	return len(running)
}

func handleRestart(set daemonSet) {
	if handleStop(set) == 0 {
		handleStart(set)
	}
}

func handleDoCmd(set daemonSet, cmd string) base_msg.MCPResponse_OpResponse {
	code := mcp.OK

	switch cmd {
	case "start":
		handleStart(set)

	case "restart":
		handleRestart(set)

	case "stop":
		handleStop(set)

	default:
		code = mcp.INVALID
	}

	return code
}

func getDaemonSet(req *base_msg.MCPRequest) (daemonSet,
	base_msg.MCPResponse_OpResponse) {
	if req.Daemon == nil {
		return nil, mcp.INVALID
	}

	set := selectTargets(req.Daemon)
	if len(set) == 0 {
		return nil, mcp.NODAEMON
	}

	return set, mcp.OK
}

//
// Parse and execute a single client request
//
func handleRequest(req *base_msg.MCPRequest) (*string,
	base_msg.MCPResponse_OpResponse) {
	var (
		set  daemonSet
		rval *string
		code base_msg.MCPResponse_OpResponse
	)

	daemonLock.RLock()
	defer daemonLock.RUnlock()

	if *req.Version.Major != mcp.Version {
		return nil, mcp.BADVER
	}

	switch *req.Operation {
	case mcp.PING:

	case mcp.GET:
		all := (req.Daemon) != nil && (*req.Daemon == "all")

		if set, code = getDaemonSet(req); code == mcp.OK {
			if rval = handleGetState(set, all); rval == nil {
				code = mcp.INVALID
			}
		}

	case mcp.SET:
		if req.State == nil {
			code = mcp.INVALID
		} else if set, code = getDaemonSet(req); code == mcp.OK {
			code = handleSetState(set, req.State)
		}

	case mcp.DO:
		if req.Command == nil {
			code = mcp.INVALID
		} else if set, code = getDaemonSet(req); code == mcp.OK {
			code = handleDoCmd(set, *req.Command)
		}

	case mcp.UPDATE:
		if req.State == nil || req.Node == nil {
			code = mcp.INVALID
		} else {
			rval, code = handlePeerUpdate(req.Node, req.State,
				*req.Lifetime)
		}
	default:
		code = mcp.INVALID
	}

	return rval, code
}

func signalHandler() {
	sig := make(chan os.Signal, 3)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	for {
		s := <-sig
		switch s {

		case syscall.SIGHUP:
			reopenLogfile()
			log.Printf("Reloading mcp.json\n")
			loadDefinitions()

		default:
			log.Printf("Signal %v received, stopping childen\n", s)
			all := "all"
			handleStop(selectTargets(&all))
			log.Printf("Exiting\n")
			if logfile != nil {
				logfile.Close()
			}
			os.Remove(pidfile)
			os.Exit(0)
		}
	}
}

func connectToGateway() *mcp.MCP {
	warnAt := time.Now()
	warnWait := time.Second
	for {
		if mcpd, err := mcp.NewPeer(pname); err == nil {
			return mcpd
		}

		now := time.Now()
		if now.After(warnAt) {
			log.Printf("failed to connect to mcp on gateway\n")
			if warnWait < time.Hour {
				warnWait *= 2
			}
			warnAt = now.Add(warnWait)
		}
		time.Sleep(time.Second)
	}
}

func satelliteLoop() {
	var (
		mcpd   *mcp.MCP
		ticker *time.Ticker
	)

	// If we intend to update every second, guarantee the upstream gateway
	// that we will check in every 5 seconds.  This gives us plenty of
	// wiggle room to allow for high system load or network congestion.
	lifeDuration := remoteUpdatePeriod * 5
	for {
		if mcpd == nil {
			mcpd = connectToGateway()
			log.Printf("Connected to gateway\n")

			// Any daemon currently running should be restarted, so
			// it will pull the freshest state from the gateway.
			list := make(daemonSet)
			for n, d := range localDaemons {
				if !d.offline() {
					list[n] = d
				}
			}
			handleRestart(list)

			ticker = time.NewTicker(remoteUpdatePeriod)
		}

		daemonLock.RLock()
		state := getCurrentState(localDaemons)
		daemonLock.RUnlock()

		state, err := mcpd.PeerUpdate(lifeDuration, state)
		if err != nil {
			log.Printf("Lost connection to gateway\n")
			mcpd.Close()
			mcpd = nil
		} else {
			eol := time.Now().Add(lifeDuration)
			daemonLock.Lock()
			remoteDaemons["gateway"] = remoteDaemonState{
				eol:   eol,
				state: state,
			}
			daemonLock.Unlock()
		}

		<-ticker.C
	}
}

//
// Spin waiting for commands from ap-ctl and status updates from spawned daemons
//
func mainLoop() {
	err := exec.Command(plat.IPCmd, "link", "set", "up", "lo").Run()
	if err != nil {
		log.Printf("Failed to enable loopback: %v\n", err)
	}

	incoming, err := zmq.NewSocket(zmq.REP)
	if err != nil {
		log.Fatalf("failed to get ZMQ socket: %v\n", err)
	}
	port := base_def.INCOMING_ZMQ_URL + base_def.MCP_ZMQ_REP_PORT
	if err := incoming.Bind(port); err != nil {
		log.Fatalf("failed to bind incoming port %s: %v\n", port, err)
	}
	me := "mcp." + strconv.Itoa(os.Getpid()) + ")"

	log.Println("MCP online")
	for {
		msg, err := incoming.RecvMessageBytes(0)
		if err != nil {
			log.Printf("err: %v\n", err)
			continue
		}

		req := &base_msg.MCPRequest{}
		proto.Unmarshal(msg[0], req)
		rval, rc := handleRequest(req)

		version := base_msg.Version{Major: proto.Int32(mcp.Version)}
		response := &base_msg.MCPResponse{
			Timestamp: aputil.NowToProtobuf(),
			Sender:    proto.String(me),
			Version:   &version,
			Debug:     proto.String("-"),
			Response:  &rc,
		}
		if rval != nil {
			response.State = proto.String(*rval)
		}

		data, err := proto.Marshal(response)
		if err != nil {
			log.Printf("Failed to marshal response: %v\n", err)
		} else {
			incoming.SendBytes(data, 0)
		}
	}
}

// Build the lists of dependents and dependencies
func recomputeDependencies() {
	for _, d := range localDaemons {
		d.Lock()
		d.dependencies = make([]*daemon, 0)
		d.dependents = make([]*daemon, 0)
	}

	for _, d := range localDaemons {
		if d.DependsOn != nil {
			if x := localDaemons[*d.DependsOn]; x != nil {
				x.dependents = append(x.dependents, d)
				d.dependencies = append(d.dependencies, x)
			}
		}
	}

	for _, d := range localDaemons {
		d.Unlock()
		d.evaluate <- true
	}
}

// Given a daemon definition loaded from the json file, initialize all of the
// run-time state for the daemon and launch the goroutine that monitors it.  If
// we are reloading the json file, refresh those fields of the run-time state
// that may have been changed by modifications to the file.
func daemonInit(def *daemon) {
	re := regexp.MustCompile(`\$APROOT`)

	d := localDaemons[def.Name]
	if d == nil {
		d = def
		d.state = mcp.OFFLINE
		d.setTime = time.Unix(0, 0)
		d.evaluate = make(chan bool, 20)
		d.goal = make(chan int, 20)
		localDaemons[d.Name] = d
	} else {
		// Replace any fields that might reasonably have changed
		d.Binary = def.Binary
		d.Options = def.Options
		d.DependsOn = def.DependsOn
		d.Privileged = def.Privileged
	}

	d.args = make([]string, 0)
	for _, o := range d.Options {
		// replace any instance of $APROOT with the real path
		o = re.ReplaceAllString(o, *aproot)
		options := strings.Split(o, " ")
		d.args = append(d.args, options...)
	}
	if d.Binary[0] == byte('/') {
		d.execpath = d.Binary
	} else {
		d.execpath = *aproot + "/bin/" + d.Binary
	}
	if d == def {
		go d.daemonLoop()
	}
}

func loadDefinitions() error {
	daemonLock.Lock()
	defer daemonLock.Unlock()

	fn := *cfgfile
	if len(fn) == 0 {
		fn = *aproot + "/etc/mcp.json"
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

	nodeMode := aputil.GetNodeMode()
	for _, def := range set {
		for _, mode := range def.Modes {
			if mode == nodeMode {
				daemonInit(def)
				break
			}
		}
	}

	if len(localDaemons) == 0 {
		err = fmt.Errorf("no daemons for '%s' mode", nodeMode)
	} else {
		recomputeDependencies()
	}

	return err
}

// Check for the existence of /var/tmp/ap.mcp.pid.  If the file exists, check to
// see whether the pid it contains is still running as ap.mcp.  If it is,
// decline to start.  Otherwise, create the file with our PID.
func pidLock() error {
	var err error
	var data []byte

	if data, err = ioutil.ReadFile(pidfile); err == nil {
		pid := string(data)
		data, err = ioutil.ReadFile("/proc/" + pid + "/stat")
		if err == nil {
			fields := strings.Split(string(data), " ")
			if len(fields) > 2 && fields[1] == "(ap.mcp)" {
				return fmt.Errorf("another instance of mcp "+
					"appears to be running as pid %s", pid)
			}
		}
	}

	pid := strconv.Itoa(os.Getpid())
	err = ioutil.WriteFile(pidfile, []byte(pid), 0666)
	if err != nil {
		err = fmt.Errorf("unable to create %s: %v", pidfile, err)
	}
	return err
}

func reopenLogfile() {
	if *logname == "" {
		return
	}

	path := aputil.ExpandDirPath(*logname)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		log.Printf("Unable to redirect logging to %s: %v", path, err)
		return
	}

	os.Stdout = f
	os.Stderr = f
	for _, d := range localDaemons {
		d.Lock()
		if d.child != nil {
			d.child.SetOutput(f)
		}
	}

	if logfile == nil {
		os.Stdin, err = os.OpenFile("/dev/null", os.O_RDONLY, 0)
		if err != nil {
			log.Printf("Couldn't close stdin\n")
		}
	} else {
		log.Printf("Closing log\n")
		logfile.Close()
	}
	log.SetOutput(f)
	log.Printf("Opened %s\n", path)
	logfile = f
}

func setEnvironment() {

	if *platFlag != "" {
		os.Setenv("APPLATFORM", *platFlag)
	}
	plat = platform.NewPlatform()
	if err := verifyNodeID(); err != nil {
		log.Printf("%v\n", err)
		os.Exit(1)
	}

	if *aproot == "" {
		if strings.HasSuffix(self.Binary, "/bin/ap.mcp") {
			*aproot = strings.TrimSuffix(self.Binary, "/bin/ap.mcp")
		} else {
			wd, _ := os.Getwd()
			*aproot = wd
		}
		fmt.Printf("aproot not set - using '%s'\n", *aproot)
	}
	os.Setenv("APROOT", *aproot)

	if *apmode == "" {
		*apmode = aputil.GetNodeMode()
	}
	os.Setenv("APMODE", *apmode)
	if aputil.IsSatelliteMode() {
		nodeName, _ = plat.GetNodeID()
	} else {
		nodeName = "gateway"
	}
}

func verifyNodeID() error {
	nodeID, err := plat.GetNodeID()

	if err == nil {
		var current, proposed string

		if *nodeFlag != "" {
			current = strings.ToLower(nodeID)
			proposed = strings.ToLower(*nodeFlag)
		}
		if current != proposed {
			log.Printf("Not overriding existing nodeid: %s\n",
				current)
		}
		return nil
	}
	log.Printf("Unable to get a device nodeID: %v\n", err)

	if *nodeFlag == "" {
		err = fmt.Errorf("must provide a device nodeID")

	} else if err = plat.SetNodeID(*nodeFlag); err != nil {
		err = fmt.Errorf("unable to set device nodeID: %v", err)
	} else {
		log.Printf("Set new device nodeID: %s\n", *nodeFlag)
	}

	return err
}

func main() {
	flag.Parse()
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	if os.Geteuid() != rootUID {
		log.Printf("mcp must be run as root\n")
		os.Exit(1)
	}

	if err := pidLock(); err != nil {
		log.Printf("%v\n", err)
		os.Exit(1)
	}

	stateReverseMap = make(map[string]int)
	for i, s := range mcp.States {
		stateReverseMap[s] = i
	}

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
	}

	setEnvironment()
	reopenLogfile()

	log.Printf("ap.mcp (%d) coming online...\n", os.Getpid())

	if err := loadDefinitions(); err != nil {
		log.Fatalf("Failed to load daemon config: %v\n", err)
	}

	go signalHandler()

	if aputil.IsSatelliteMode() {
		go satelliteLoop()
	}

	mainLoop()
}
