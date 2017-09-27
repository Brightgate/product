/*
 * COPYRIGHT 2017 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 *
 */

/*
 * Todo:
 *    - Define real states and a state machine
 *    - Support more fine-grained capabilities / privileges
 */
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"ap_common/mcp"
	"base_def"
	"base_msg"

	"github.com/golang/protobuf/proto"
	zmq "github.com/pebbe/zmq4"
)

type daemon struct {
	Name       string
	Binary     string
	Options    *string `json:"Options,omitempty"`
	DependsOn  *string `json:"DependsOn,omitempty"`
	Arch       *string `json:"Arch,omitempty"`
	ThirdParty bool    `json:"ThirdParty,omitempty"`
	Privileged bool

	run       bool
	going     bool // is being maintained by a goroutine
	startTime time.Time
	setTime   time.Time
	process   *os.Process
	state     int
	sync.Mutex
}

type daemonSet map[string]*daemon

// Allow up to 4 failures in a 1 minute period before giving up
const (
	failures_allowed = 4
	period           = time.Duration(time.Minute)
	online_timeout   = time.Duration(15 * time.Second)
	NOBODY_UID       = 65534 // uid for 'nobody'
	ROOT_UID         = 0     // uid for 'root'
)

var (
	aproot = flag.String("root", "proto.armv7l/appliance/opt/com.brightgate",
		"Root of AP installation")
	cfgfile = flag.String("c", "", "Alternate daemon config file")
	debug   = flag.Bool("d", false, "Extra debug logging")

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
// Wait for stdout/stderr from a process, and print whatever it sends.  When the
// pipe is closed, notify our caller.
//
func handlePipe(name string, r io.ReadCloser, done chan string) {
	var err error
	var n int

	buf := make([]byte, 1024)
	for err == nil {
		if n, err = r.Read(buf); err == nil {
			fmt.Printf("%s", string(buf[:n]))
		}
	}

	done <- name
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

	setState(d, mcp.STARTING)

	if d.Options != nil {
		args = strings.Split(*d.Options, " ")
	}

	if d.Binary[0] == byte('/') {
		execpath = d.Binary
	} else {
		execpath = *aproot + "/bin/" + d.Binary
	}
	cmd := exec.Command(execpath, args...)

	// Set up pipes for the child's stderr and stdout, so we can get
	// the output while the child is still running
	pipes := 0
	pipe_closed := make(chan string)
	if stdout, err := cmd.StdoutPipe(); err == nil {
		pipes++
		go handlePipe("stdout", stdout, pipe_closed)
	}
	if stderr, err := cmd.StderrPipe(); err == nil {
		pipes++
		go handlePipe("stderr", stderr, pipe_closed)
	}

	if *debug {
		log.Printf("Starting %s\n", execpath)
	}

	if d.Privileged {
		err = cmd.Start()
	} else {
		syscall.Setreuid(ROOT_UID, NOBODY_UID)
		err = cmd.Start()
		syscall.Setreuid(ROOT_UID, ROOT_UID)
	}
	if d.ThirdParty {
		// A third party daemon doesn't participate in the ZMQ updates,
		// so we won't get an online notification.  Just set it here.
		setState(d, mcp.ONLINE)
	}

	if err == nil {
		d.process = cmd.Process
		d.Unlock()

		// Wait for the stdout/stderr pipes to close and for the child
		// process to exit
		for pipes > 0 {
			<-pipe_closed
			pipes--
		}
		err = cmd.Wait()
		if err != nil {
			log.Printf("%s failed: %v\n", d.Name, err)
		}
		d.Lock()
		d.process = nil
	}

	return err
}

//
// launch, monitor, and restart a single daemon
//
func runDaemon(d *daemon) {
	start_times := make([]time.Time, failures_allowed)

	d.Lock()
	if d.going {
		d.Unlock()
		return
	}
	d.going = true
	d.run = true

	for d.run {
		var msg string

		start_time := time.Now()
		start_times = append(start_times[1:failures_allowed], start_time)

		err := singleInstance(d)
		if err == nil {
			msg = "cleanly"
		} else {
			msg = fmt.Sprintf("with '%v'", err)
		}

		log.Printf("%s exited %s after %s\n", d.Name, msg,
			time.Since(start_time))
		if time.Since(start_times[0]) < period {
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

func handleGetState(set daemonSet) *string {
	var rval *string
	state := make(map[string]mcp.DaemonState)

	for _, d := range set {
		pid := -1
		if d.process != nil {
			pid = d.process.Pid
		}

		state[d.Name] = mcp.DaemonState{
			Name:  d.Name,
			State: d.state,
			Since: d.setTime,
			Pid:   pid,
		}
	}
	b, err := json.MarshalIndent(state, "", "  ")
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
			dep, ok := daemons[*d.DependsOn]
			if !ok {
				// This should never happen.  If it does, launch the new
				// daemon anyway.
				log.Printf("%s depends on non-existent daemon %s.\n",
					d.Name, *d.DependsOn)
				add = true
			} else if dep.state == mcp.ONLINE {
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
	log.Printf("Starting %v\n", set)
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
			if time.Since(d.startTime) > online_timeout {
				log.Printf("%s took more than %v to come "+
					"online.  Giving up.",
					n, online_timeout)
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

//
// Repeatedly iterate over all daemons in the set.  On each iteration we ask
// all daemons in the set to exit.  We start by asking nicely (SIGINT) and then
// less nicely (SIGKILL).  When a daemon has stopped, we remove it from the set.
// We're done when the set is empty.
//
func handleStop(set daemonSet) {
	const nice_tries = 10

	tries := 0
	procs := make(map[string]*os.Process)
	for n, d := range set {
		d.Lock()
		if d.state != mcp.OFFLINE {
			log.Printf("Stopping %s\n", d.Name)
			procs[n] = d.process
			setState(d, mcp.STOPPING)
			d.run = false
		}
		d.Unlock()
	}

	for len(set) > 0 {
		for n, d := range set {
			d.Lock()
			p := d.process
			if p == nil || p != procs[n] {
				if p == nil {
					// if the process has changed, it means
					// the daemon has already been
					// restarted.
					setState(d, mcp.OFFLINE)
					log.Printf("%s stopped\n", d.Name)
				}
				delete(set, n)
			} else if tries < nice_tries {
				p.Signal(os.Interrupt)
			} else {
				p.Signal(os.Kill)
			}
			d.Unlock()
		}
		tries++
		time.Sleep(time.Millisecond * 250)
	}
}

//
// Parse and execute a single client request
//
func handleRequest(req *base_msg.MCPRequest) (*string,
	base_msg.MCPResponse_OpResponse) {

	if req.Daemon == nil {
		if *debug {
			log.Printf("Bad req from %s: no daemon\n", *req.Sender)
		}
		return nil, mcp.INVALID
	}

	set := selectTargets(req.Daemon)
	if len(set) == 0 {
		if *debug {
			log.Printf("Bad req from %s: unknown daemon: %s\n",
				*req.Sender, *req.Daemon)
		}
		return nil, mcp.NO_DAEMON
	}

	switch *req.Operation {
	case mcp.OP_GET:
		if *debug {
			log.Printf("%s: Get(%s)\n", *req.Sender, *req.Daemon)
		}
		s := handleGetState(set)
		if s == nil {
			return nil, mcp.INVALID
		} else {
			return s, mcp.OK
		}

	case mcp.OP_SET:
		if *debug {
			log.Printf("%s: Set(%s, %s)\n", *req.Sender,
				*req.Daemon, *req.State)
		}
		if req.State == nil {
			return nil, mcp.INVALID
		}
		rval := handleSetState(set, int(*req.State))
		return nil, rval

	case mcp.OP_DO:
		if req.Command == nil {
			if *debug {
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
			if *debug {
				log.Printf("%s: START(%s)\n", *req.Daemon,
					*req.Daemon)
			}
			go handleStart(set)
			return nil, mcp.OK

		case "restart":
			if *debug {
				log.Printf("%s: RESTART(%s)\n", *req.Daemon,
					*req.Daemon)
			}
			// The set is emptied as a side effect of the
			// handleStop, so we make a copy to use during the
			// subsequent handleStart()
			restart_set := make(daemonSet)
			for k, v := range set {
				restart_set[k] = v
			}
			handleStop(set)
			go handleStart(restart_set)
			return nil, mcp.OK

		case "stop":
			if *debug {
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
	sig := make(chan os.Signal)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	for {
		s := <-sig
		if s == syscall.SIGHUP {
			log.Printf("Reloading mcp.json\n")
			loadDefinitions()
		} else {
			log.Printf("Signal (%v) received, stopping\n", s)
			os.Exit(0)
		}
	}
}

//
// Spin waiting for commands from ap-ctl and status updates from spawned daemons
//
func mainLoop() {
	incoming, _ := zmq.NewSocket(zmq.REP)
	incoming.Bind(base_def.MCP_ZMQ_REP_URL)
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

		t := time.Now()
		response := &base_msg.MCPResponse{
			Timestamp: &base_msg.Timestamp{
				Seconds: proto.Int64(t.Unix()),
				Nanos:   proto.Int32(int32(t.Nanosecond())),
			},
			Sender:   proto.String(me),
			Debug:    proto.String("-"),
			Response: &rc,
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
	re := regexp.MustCompile(`\$APROOT`)
	set := make(daemonSet)

	fn := *cfgfile
	if len(fn) == 0 {
		fn = *aproot + "/etc/mcp.json"
	}

	file, err := ioutil.ReadFile(fn)
	if err != nil {
		log.Printf("Failed to load daemon configs from %s: %v\n",
			fn, err)
		return err
	}

	err = json.Unmarshal(file, &set)
	if err != nil {
		log.Printf("Failed to import daemon configs from %s: %v\n",
			fn, err)
		return err
	}

	for name, new := range set {
		if new.Arch != nil && *new.Arch != runtime.GOARCH {
			log.Printf("Dropping %s - wrong architecture\n", name)
			continue
		}

		d, ok := daemons[name]
		if !ok {
			// This is the first time we've seen this daemon, so
			// keep the entire record
			d = new
			d.Lock()
			d.state = mcp.OFFLINE
			d.setTime = time.Unix(0, 0)
			daemons[name] = d
		} else {
			// Replace any fields the might reasonably have changed
			d.Lock()
			d.Binary = new.Binary
			d.Options = new.Options
			d.DependsOn = new.DependsOn
			d.Privileged = new.Privileged
		}
		if d.Options != nil {
			// replace any instance of $APROOT with the real path
			r := re.ReplaceAllString(*d.Options, *aproot)
			d.Options = &r
		}
		d.Unlock()
	}

	return nil
}

func main() {
	flag.Parse()
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	if os.Geteuid() != ROOT_UID {
		log.Printf("mcp must be run as root\n")
		os.Exit(1)
	}

	go signalHandler()

	if err := loadDefinitions(); err != nil {
		log.Fatal("Failed to read daemon definitions\n")
	}
	mainLoop()
}
