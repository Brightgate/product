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

	run        bool
	statusTime time.Time
	process    *os.Process
	status     string
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
	aproot = flag.String("root", "proto.armv7l/opt/com.brightgate",
		"Root of AP installation")
	cfgfile = flag.String("c", "", "Alternate daemon config file")
	debug   = flag.Bool("d", false, "Extra debug logging")

	daemons = make(daemonSet)
)

func setStatus(d *daemon, status string) {
	d.status = status
	d.statusTime = time.Now()
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

	setStatus(d, "starting")

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
		setStatus(d, "online")
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

	// Don't attempt to start a daemon that isn't in a terminal state
	d.Lock()
	if d.status == "offline" || d.status == "broken" {
		d.run = true
		setStatus(d, "preparing")
	} else {
		log.Printf("%s is already %s\n", d.Name, d.status)
		d.run = false
	}

	for d.run {
		start_time := time.Now()
		start_times = append(start_times[1:failures_allowed], start_time)

		err := singleInstance(d)

		if err == nil {
			log.Printf("%s exited after %s\n", d.Name, time.Since(start_time))
			if time.Since(start_times[0]) < period {
				log.Printf("%s is dying too quickly")
				setStatus(d, "broken")
				d.run = false
			}
		} else {
			log.Printf("%s wouldn't start: %v\n", d.Name, err)
			setStatus(d, "broken")
			d.run = false
		}
		if d.run {
			setStatus(d, "restarting")
		}
	}
	d.Unlock()
}

func handleGetStatus(set daemonSet) *string {
	var rval *string
	status := make(map[string]mcp.DaemonStatus)

	for _, d := range set {
		pid := -1
		if d.process != nil {
			pid = d.process.Pid
		}

		status[d.Name] = mcp.DaemonStatus{
			Name:   d.Name,
			Status: d.status,
			Since:  d.statusTime,
			Pid:    pid,
		}
	}
	b, err := json.MarshalIndent(status, "", "  ")
	if err == nil {
		s := string(b)
		rval = &s
	}
	return rval
}

func handleSetStatus(set daemonSet, status *string) {
	// A daemon can only set its own status, so it's illegal for any 'set'
	// command to target more than a single daemon
	if len(set) == 1 {
		for _, d := range set {
			setStatus(d, *status)
		}
	}
}

//
// Repeatedly iterate over all daemons in the set.  On each iteration, we select
// one to launch.  It then gets launched and removed from the set.  When all the
// daemons have been removed from the set, we're done.
//
func handleStart(set daemonSet) {
	broken := make(daemonSet)
	for len(set) > 0 {
		var next *daemon

		for _, d := range set {
			dep := d.DependsOn

			// If the daemon has no dependencies, start it
			if dep == nil {
				next = d
				break
			}

			if _, ok := broken[*dep]; ok {
				log.Printf("%s depends on %s.  Skipping.\n",
					d.Name, *dep)
				delete(set, d.Name)
			}

			// If the daemon has no dependencies left for us to
			// start, start it.  XXX: we really want to add its
			// dependencies to the start list or fail the operation,
			// but we can save that until we support starting
			// arbitrary sets of daemons
			if _, ok := set[*dep]; !ok {
				next = d
				break
			}
		}
		if next == nil {
			// Shouldn't be possible unless we create a circular
			// dependency
			log.Printf("No daemons eligible to run")
			break
		}

		go runDaemon(next)

		// Wait for the freshly launched daemon to come online
		wait := true
		last := "offline"
		started := time.Now()
		for wait {
			next.Lock()
			if next.status == "online" {
				wait = false
			} else if time.Since(started) > online_timeout {
				log.Printf("%s took more than %v to come "+
					"online.  Giving up.",
					next.Name, online_timeout)
				setStatus(next, "broken")
			} else if *debug && next.status != last {
				last = next.status
				if *debug {
					log.Printf("Waiting for %s (currently %s)\n",
						next.Name, last)
				}
			}

			if next.status == "broken" {
				broken[next.Name] = next
				wait = false
			}

			next.Unlock()
			time.Sleep(time.Millisecond * 100)
		}
		delete(set, next.Name)
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
		if d.status != "offline" {
			log.Printf("Stopping %s\n", d.Name)
			procs[n] = d.process
			setStatus(d, "stopping")
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
					setStatus(d, "offline")
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
		return nil, mcp.MCP_INVALID
	}

	set := selectTargets(req.Daemon)
	if set == nil {
		if *debug {
			log.Printf("Bad req from %s: unknown daemon: %s\n",
				*req.Sender, *req.Daemon)
		}
		return nil, mcp.MCP_NO_DAEMON
	}

	switch *req.Operation {
	case mcp.MCP_OP_GET:
		if *debug {
			log.Printf("%s: Get(%s)\n", *req.Sender, *req.Daemon)
		}
		s := handleGetStatus(set)
		if s == nil {
			return nil, mcp.MCP_INVALID
		} else {
			return s, mcp.MCP_OK
		}

	case mcp.MCP_OP_SET:
		if *debug {
			log.Printf("%s: Set(%s, %s)\n", *req.Sender,
				*req.Daemon, *req.Status)
		}
		if req.Status == nil {
			return nil, mcp.MCP_INVALID
		}
		handleSetStatus(set, req.Status)
		return nil, mcp.MCP_OK

	case mcp.MCP_OP_DO:
		if req.Command == nil {
			if *debug {
				log.Printf("Bad DO from %s: no cmd for %s\n",
					*req.Daemon, *req.Daemon)
			}
			return nil, mcp.MCP_INVALID
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
			return nil, mcp.MCP_OK

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
			return nil, mcp.MCP_OK

		case "stop":
			if *debug {
				log.Printf("%s: STOP(%s)\n", *req.Daemon,
					*req.Daemon)
			}
			handleStop(set)
			return nil, mcp.MCP_OK
		}
	}

	return nil, mcp.MCP_INVALID
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
			response.Status = proto.String(*rval)
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
		log.Printf("Failed to load daemon configs from %s: %v\n",
			fn, err)
		return err
	}

	err = json.Unmarshal(file, &daemons)
	if err != nil {
		log.Printf("Failed to import daemon configs from %s: %v\n",
			fn, err)
		return err
	}

	//
	// Set the initial status for each daemon to 'offline'
	// For those daemons that have command line options, replace any
	// instance of $APROOT with the real path
	//
	re := regexp.MustCompile(`\$APROOT`)
	for n, d := range daemons {
		if d.Arch != nil && *d.Arch != runtime.GOARCH {
			log.Printf("Dropping %s - wrong architecture\n", n)
			delete(daemons, n)
			continue
		}
		d.status = "offline"
		d.statusTime = time.Unix(0, 0)
		if d.Options != nil {
			r := re.ReplaceAllString(*d.Options, *aproot)
			d.Options = &r
		}
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

	if err := loadDefinitions(); err != nil {
		os.Exit(1)
	}

	mainLoop()
}
