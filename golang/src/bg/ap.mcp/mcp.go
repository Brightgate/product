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
	"os/signal"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"bg/ap_common/aputil"
	"bg/ap_common/mcp"
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

	run         bool
	going       bool // is being maintained by a goroutine
	startTime   time.Time
	setTime     time.Time
	child       *aputil.Child
	state       int
	launchOrder int
	sync.Mutex
}

type daemonSet map[string]*daemon

// Allow up to 4 failures in a 1 minute period before giving up
const (
	failuresAllowed = 4
	period          = time.Duration(time.Minute)
	onlineTimeout   = time.Duration(15 * time.Second)
	nobodyUID       = 65534 // uid for 'nobody'
	rootUID         = 0     // uid for 'root'
	pidfile         = "/var/tmp/ap.mcp.pid"
)

var (
	aproot  = flag.String("root", "", "Root of AP installation")
	apmode  = flag.String("mode", "", "Mode in which this AP should operate")
	cfgfile = flag.String("c", "", "Alternate daemon config file")
	logname = flag.String("l", "", "where to send log messages")
	verbose = flag.Bool("v", false, "more verbose logging")

	logfile *os.File

	daemons = make(daemonSet)
)

var stableStates = map[int]bool{
	mcp.OFFLINE:  true,
	mcp.ONLINE:   true,
	mcp.INACTIVE: true,
	mcp.BROKEN:   true,
}

var terminalStates = map[int]bool{
	mcp.OFFLINE:  true,
	mcp.INACTIVE: true,
	mcp.BROKEN:   true,
}

func setState(d *daemon, state int) {
	d.state = state
	d.setTime = time.Now()
}

//
// Given a name, select the daemons that will be affected.  Currently the
// choices are all, one, or none.  Eventually, this could be expanded to
// identify daemons that should be acted on together.
//
func selectTargets(name *string) daemonSet {
	set := make(daemonSet)

	for _, d := range daemons {
		if *name == "all" || *name == d.Name {
			set[d.Name] = d
		}
	}
	return set
}

//
// Attempt to launch a child process.  If that fails, return an error.  If it
// succeeds, return nil when the child process exits
//
// Note: we enter and exit this routine with the daemon's mutex held.
//
func singleInstance(d *daemon) error {
	var err error
	var args []string
	var execpath string

	out := os.Stderr
	if logfile != nil {
		out = logfile
	}

	setState(d, mcp.STARTING)

	for _, o := range d.Options {
		args = append(args, strings.Split(o, " ")...)
	}

	if d.Binary[0] == byte('/') {
		execpath = d.Binary
	} else {
		execpath = *aproot + "/bin/" + d.Binary
	}

	child := aputil.NewChild(execpath, args...)
	child.LogOutputTo("", 0, out)

	if *verbose {
		log.Printf("Starting %s\n", execpath)
	}

	if !d.Privileged {
		child.SetUID(nobodyUID, nobodyUID)
	}

	// Put each daemon into its own process group
	child.SetPgid(true)

	if err = child.Start(); err != nil {
		return err
	}

	if d.ThirdParty {
		// A third party daemon doesn't participate in the ZMQ updates,
		// so we won't get an online notification.  Just set it here.
		setState(d, mcp.ONLINE)
	}

	d.child = child
	d.Unlock()

	err = child.Wait()

	if d.state != mcp.STOPPING && err != nil {
		log.Printf("%s failed: %v\n", d.Name, err)
	}

	d.Lock()
	d.child = nil

	return err
}

//
// launch, monitor, and restart a single daemon
//
func runDaemon(d *daemon) {
	startTimes := make([]time.Time, failuresAllowed)

	d.Lock()
	if d.going {
		d.Unlock()
		return
	}
	d.going = true
	d.run = true

	for d.run {
		var msg string

		startTime := time.Now()
		startTimes = append(startTimes[1:failuresAllowed], startTime)

		err := singleInstance(d)
		if err == nil {
			msg = "cleanly"
		} else {
			msg = fmt.Sprintf("with '%v'", err)
		}

		log.Printf("%s exited %s after %s\n", d.Name, msg,
			time.Since(startTime))
		if time.Since(startTimes[0]) < period {
			log.Printf("%s is dying too quickly", d.Name)
			setState(d, mcp.BROKEN)
		}
		if d.state == mcp.BROKEN || d.state == mcp.INACTIVE {
			d.run = false
		}
	}
	d.going = false
	d.Unlock()
}

type sortList mcp.DaemonList

func (l sortList) Len() int {
	return len(l)
}

func (l sortList) Less(a, b int) bool {
	A := daemons[l[a].Name]
	B := daemons[l[b].Name]

	return A.launchOrder < B.launchOrder
}

func (l sortList) Swap(a, b int) {
	l[a], l[b] = l[b], l[a]
}

func handleGetState(set daemonSet) *string {
	var rval *string
	list := make(sortList, 0)

	for _, d := range set {
		state := mcp.DaemonState{
			Name:  d.Name,
			State: d.state,
			Since: d.setTime,
			Pid:   d.child.GetPID(),
		}
		if s, _ := d.child.GetState(); s != nil {
			state.VMSize = s.VMSize
			state.RssSize = s.RssSize
			state.VMSwap = s.VMSwap
			state.Utime = s.Utime
			state.Stime = s.Stime
		}
		list = append(list, &state)
	}
	sort.Sort(list)

	b, err := json.MarshalIndent(list, "", "  ")
	if err == nil {
		s := string(b)
		rval = &s
	}
	return rval
}

func handleSetState(set daemonSet, state int) base_msg.MCPResponse_OpResponse {
	// A daemon can only set its own state, so it's illegal for any 'set'
	// command to target more than a single daemon
	_, ok := mcp.States[state]
	if !ok || len(set) != 1 {
		return mcp.INVALID
	}

	for _, d := range set {
		setState(d, state)
	}
	return mcp.OK
}

//
// Scan the list of candidate daemons, and identify those that are ready to run.
//
func readySet(candidates daemonSet) daemonSet {
	ready := make(daemonSet)

	for n, d := range candidates {
		add := false
		if d.DependsOn == nil {
			// This daemon has no dependencies, so can run any time
			add = true
		} else {
			// If the dependency isn't in the daemons map, then it's
			// not expected to be running in this mode.
			dep, ok := daemons[*d.DependsOn]
			if !ok || dep.state == mcp.ONLINE {
				add = true
			}
		}
		if add {
			delete(candidates, n)
			ready[n] = d
		}
	}
	return ready
}

//
// Repeatedly iterate over all daemons in the set.  On each iteration, we
// identify all the daemons that are eligble to run, launch them, and remove
// them from the set.  Repeat until all the daemons are running or broken.
//
func handleStart(set daemonSet) {
	launching := make(daemonSet)
	for {
		next := readySet(set)
		if len(next) == 0 && len(launching) == 0 {
			break
		}

		for n, d := range next {
			d.Lock()
			delete(next, n)
			if !d.going {
				log.Printf("Launching %s\n", n)
				setState(d, mcp.STARTING)
				d.startTime = time.Now()
				launching[n] = d
				go runDaemon(d)
			}
			d.Unlock()
		}

		for n, d := range launching {
			d.Lock()
			if time.Since(d.startTime) > onlineTimeout {
				log.Printf("%s took more than %v to come "+
					"online.  Giving up.",
					n, onlineTimeout)
				setState(d, mcp.BROKEN)
			}
			if stableStates[d.state] {
				delete(launching, n)
			}
			d.Unlock()
		}
	}

	if len(set) > 0 {
		log.Printf("The following daemons weren't started:\n")
		for n, d := range set {
			dep, _ := daemons[*d.DependsOn]
			log.Printf("   %s: depends on %s (%s)\n", n, dep.Name,
				mcp.States[dep.state])
		}
	}
}

// killSet is the killer helper function for handleStop, for use with RetryKill
func killSet(sig syscall.Signal, set daemonSet,
	children map[string]*aputil.Child) error {
	errs := make(map[string]error)
	for n, d := range set {
		d.Lock()
		c := d.child
		if c == nil || c != children[n] {
			if c == nil {
				// if the child has changed, it means the daemon
				// has already been restarted.
				setState(d, mcp.OFFLINE)
				log.Printf("%s stopped\n", d.Name)
			}
			delete(set, n)
			delete(errs, n)
		} else {
			errs[n] = c.Signal(sig)
		}
		d.Unlock()
	}

	// There's no such thing as partial success: the calling loop will break
	// if we return failure because it thinks there's no more useful work to
	// be done.  Thus, only return non-nil if everything is failing.
	var realerr error
	for _, e := range errs {
		if e == nil {
			return nil
		}
		realerr = e
	}
	return realerr
}

//
// Kill all daemons in the set using the retryKill() behavior.  When a daemon
// has stopped, we remove it from the set.  We're done when the set is empty.
//
func handleStop(set daemonSet) {
	children := make(map[string]*aputil.Child)
	for n, d := range set {
		d.Lock()
		if d.state != mcp.OFFLINE {
			if d.child != nil {
				log.Printf("Stopping %s\n", d.Name)
				children[n] = d.child
			}
			setState(d, mcp.STOPPING)
			d.run = false
		}
		d.Unlock()
	}

	kill := func(sig syscall.Signal) error {
		return killSet(sig, set, children)
	}
	alive := func() bool { return len(set) > 0 }
	aputil.RetryKill(kill, alive)
}

//
// Parse and execute a single client request
//
func handleRequest(req *base_msg.MCPRequest) (*string,
	base_msg.MCPResponse_OpResponse) {

	if req.Daemon == nil {
		if *verbose {
			log.Printf("Bad req from %s: no daemon\n", *req.Sender)
		}
		return nil, mcp.INVALID
	}

	set := selectTargets(req.Daemon)
	if len(set) == 0 {
		if *verbose {
			log.Printf("Bad req from %s: unknown daemon: %s\n",
				*req.Sender, *req.Daemon)
		}
		return nil, mcp.NODAEMON
	}

	switch *req.Operation {
	case mcp.GET:
		if *verbose {
			log.Printf("%s: Get(%s)\n", *req.Sender, *req.Daemon)
		}
		s := handleGetState(set)
		rval := mcp.OK
		if s == nil {
			rval = mcp.INVALID
		}
		return s, rval

	case mcp.SET:
		if *verbose {
			log.Printf("%s: Set(%s, %d)\n", *req.Sender,
				*req.Daemon, *req.State)
		}
		if req.State == nil {
			return nil, mcp.INVALID
		}
		rval := handleSetState(set, int(*req.State))
		return nil, rval

	case mcp.DO:
		if req.Command == nil {
			if *verbose {
				log.Printf("Bad DO from %s: no cmd for %s\n",
					*req.Daemon, *req.Daemon)
			}
			return nil, mcp.INVALID
		}

		switch *req.Command {
		case "start":
			// start/restart needs to go asynchronous so this
			// thread is available to process status updates from
			// the launched daemons.  XXX: We immediately return OK
			// to the caller, but that should really be done by the
			// go routine when the daemons are all back online
			if *verbose {
				log.Printf("%s: START(%s)\n", *req.Daemon,
					*req.Daemon)
			}
			go handleStart(set)
			return nil, mcp.OK

		case "restart":
			if *verbose {
				log.Printf("%s: RESTART(%s)\n", *req.Daemon,
					*req.Daemon)
			}
			// The set is emptied as a side effect of the
			// handleStop, so we make a copy to use during the
			// subsequent handleStart()
			restartSet := make(daemonSet)
			for k, v := range set {
				restartSet[k] = v
			}
			handleStop(set)
			go handleStart(restartSet)
			return nil, mcp.OK

		case "stop":
			if *verbose {
				log.Printf("%s: STOP(%s)\n", *req.Daemon,
					*req.Daemon)
			}
			handleStop(set)
			return nil, mcp.OK
		}
	}

	return nil, mcp.INVALID
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

//
// Spin waiting for commands from ap-ctl and status updates from spawned daemons
//
func mainLoop() {
	incoming, _ := zmq.NewSocket(zmq.REP)
	port := base_def.LOCAL_ZMQ_URL + base_def.MCP_ZMQ_REP_PORT
	if err := incoming.Bind(port); err != nil {
		log.Fatalf("failed to bind incoming port %s: %v\n", port, err)
	}
	me := "mcp." + strconv.Itoa(os.Getpid()) + ")"

	log.Println("MCP online")
	for {
		msg, err := incoming.RecvMessageBytes(0)
		if err != nil {
			continue
		}

		req := &base_msg.MCPRequest{}
		proto.Unmarshal(msg[0], req)
		rval, rc := handleRequest(req)

		response := &base_msg.MCPResponse{
			Timestamp: aputil.NowToProtobuf(),
			Sender:    proto.String(me),
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

func loadDefinitions() error {
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

	re := regexp.MustCompile(`\$APROOT`)
	for name, newDaemon := range set {
		included := false

		for _, mode := range newDaemon.Modes {
			if aputil.IsNodeMode(mode) {
				included = true
				break
			}
		}
		if !included {
			continue
		}

		d, ok := daemons[name]
		if !ok {
			// This is the first time we've seen this daemon, so
			// keep the entire record
			d = newDaemon
			d.Lock()
			d.state = mcp.OFFLINE
			d.setTime = time.Unix(0, 0)
			daemons[name] = d
		} else {
			// Replace any fields the might reasonably have changed
			d.Lock()
			d.Binary = newDaemon.Binary
			d.Options = newDaemon.Options
			d.DependsOn = newDaemon.DependsOn
			d.Privileged = newDaemon.Privileged
		}
		options := make([]string, 0)
		for _, o := range d.Options {
			// replace any instance of $APROOT with the real path
			o = re.ReplaceAllString(o, *aproot)
			options = append(options, o)
		}
		d.Options = options
		d.Unlock()
	}
	if len(daemons) == 0 {
		return fmt.Errorf("no daemons for '%s' mode",
			aputil.GetNodeMode())
	}

	for _, d := range daemons {
		d.launchOrder = 0
	}
	ordered := 0
	for ordered < len(daemons) {
		for _, d := range daemons {
			if d.launchOrder > 0 {
				continue
			}
			launchable := true
			if d.DependsOn != nil {
				daemon, ok := daemons[*d.DependsOn]
				if ok && daemon.launchOrder == 0 {
					launchable = false
				}
			}
			if launchable {
				ordered++
				d.launchOrder = ordered
			}
		}
	}

	return nil
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
	for _, d := range daemons {
		d.Lock()
		if d.child != nil {
			d.child.SetOutput(f)
		}
		d.Unlock()
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
	if *aproot == "" {
		p, _ := os.Executable()
		if strings.HasSuffix(p, "/bin/ap.mcp") {
			*aproot = strings.TrimSuffix(p, "/bin/ap.mcp")
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

	setEnvironment()
	reopenLogfile()

	log.Printf("ap.mcp (%d) coming online...\n", os.Getpid())

	if err := loadDefinitions(); err != nil {
		log.Fatalf("Failed to load daemon config: %v\n", err)
	}

	go signalHandler()

	mainLoop()
}
