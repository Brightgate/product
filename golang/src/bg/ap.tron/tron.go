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
	"flag"
	"fmt"
	"golang.org/x/crypto/ssh/terminal"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"bg/ap_common/aputil"
	"bg/ap_common/platform"
	"bg/common/cfgapi"
)

const (
	pname = "ap.healthd"
)

var (
	strict = flag.Bool("strict", false,
		"all tests must pass to declare success")
	verbose = flag.Bool("v", false, "verbose output")

	plat *platform.Platform

	states struct {
		current map[string]string
		old     map[string]string
		updated chan bool
		sync.Mutex
	}

	running    bool
	isTerminal bool

	nodeUUID string
	nodeName string

	configMtx sync.Mutex
)

type hTest struct {
	name     string            // name of the test
	testFn   func(*hTest) bool // function that performs the test
	period   time.Duration     // how frequently to perform the test
	ledValue int               // % of time led is lit
	triggers []*hTest          // tests triggered if this result changes

	signal chan bool // used to trigger this test

	pass  bool   // did the last test check pass?
	state string // value to push into config tree

	// tests may rely on data in the config tree.  'source' is the
	// property that should be fetched and 'data' is the result.
	source string
	data   *cfgapi.PropertyNode
}

var (
	networkTests = []*hTest{wanTest, carrierTest, addrTest,
		connectTest, dnsTest}

	stackTests = []*hTest{selfTest, mcpTest, configTest, rpcdTest}

	// The following 2 tests are used to determine whether the wan link is
	// alive.  The wan_carrier state is displayed on a dedicated LED and is
	// controlled by the hardware.  We only track it internally so it can be
	// used to trigger higher-level tests.
	wanTest = &hTest{
		name:     "wan_discover",
		testFn:   getWanName,
		period:   10 * time.Second,
		source:   "",                    // initialized at runtime
		triggers: []*hTest{carrierTest}, // new wan -> check carrier
	}
	carrierTest = &hTest{
		name:     "wan_carrier",
		testFn:   getCarrierState,
		period:   time.Second,
		triggers: []*hTest{addrTest},
	}

	// The following 3 tests attempt to determine how much basic network
	// functionality we currently have.  These are used to determine the
	// blink pattern displayed on LED 3.
	addrTest = &hTest{
		name:     "wan_address",
		testFn:   getAddressState,
		period:   5 * time.Second,
		triggers: []*hTest{connectTest},
		ledValue: 10,
	}
	connectTest = &hTest{
		name:     "net_connect",
		testFn:   connCheck,
		period:   30 * time.Second,
		triggers: []*hTest{dnsTest, rpcdTest},
		ledValue: 90,
	}
	dnsTest = &hTest{
		name:     "dns_lookup",
		testFn:   dnsCheck,
		period:   60 * time.Second,
		ledValue: 100,
	}

	// The remaining tests attempt to determine the health of the brightgate
	// software stack.  In particular, we want to know whether enough of our
	// stack is working to support the creation of service tunnels from the
	// cloud.  This is communicated through the blink pattern on LED 4.
	selfTest = &hTest{
		name:     "self",
		testFn:   selfCheck,
		period:   time.Second,
		ledValue: 10,
	}
	mcpTest = &hTest{
		name:     "mcp",
		testFn:   mcpCheck,
		period:   5 * time.Second,
		triggers: []*hTest{configTest},
		ledValue: 50,
	}
	configTest = &hTest{
		name:     "configd",
		testFn:   configCheck,
		period:   5 * time.Second,
		source:   "@/apversion",
		triggers: []*hTest{wanTest, rpcdTest},
		ledValue: 90,
	}
	rpcdTest = &hTest{
		name:     "cloud_rpc",
		testFn:   rpcCheck,
		period:   5 * time.Second,
		source:   "", // initialized at runtime
		ledValue: 100,
	}

	// Two different ways of iterating over all of the tests
	allTests    []*hTest
	perLedTests map[string][]*hTest
)

func logMsg(level, msg string) {
	file := "???"
	line := 0
	if _, path, l, ok := runtime.Caller(2); ok {
		pathFields := strings.Split(path, "/")
		if n := len(pathFields); n >= 2 {
			file = strings.Join(pathFields[n-2:], "/")
		} else {
			file = path
		}
		line = l
	}

	if isTerminal {
		fmt.Printf("%s %5s %s:%d\t%s\n", time.Now().Format(time.Stamp),
			level, file, line, msg)
	} else {
		fmt.Printf("%5s %s:%d %s\n", level, file, line, msg)
	}
}

func logInfo(format string, v ...interface{}) {
	logMsg("INFO", fmt.Sprintf(format, v...))
}

func logWarn(format string, v ...interface{}) {
	logMsg("WARN", fmt.Sprintf(format, v...))
}

func logDebug(format string, v ...interface{}) {
	if *verbose {
		logMsg("DEBUG", fmt.Sprintf(format, v...))
	}
}

// Periodically execute a single health check.
func monitorOne(t *hTest, wg *sync.WaitGroup) {
	logDebug("%s monitor started", t.name)

	defer wg.Done()
	for running {
		oldPass := t.pass
		oldState := t.state

		t.pass = t.testFn(t)

		if t.pass != oldPass {
			logInfo("%s changes from %v -> %v", t.name,
				oldPass, t.pass)

			// a change in this condition may be a trigger for us to
			// examine other conditions as well
			for _, trigger := range t.triggers {
				logDebug("    change of %s triggers check "+
					"of %s", t.name, trigger.name)
				trigger.signal <- true
			}
		}

		if t.state != oldState {
			states.Lock()
			states.current[t.name] = t.state
			states.Unlock()
			states.updated <- true
		}

		select {
		case <-t.signal:
		case <-time.After(t.period):
		}
	}
	logDebug("%s monitor finished", t.name)
}

// Fire off goroutines to execute all of the defined tests, periodically push
// the test results into the config tree, and update the status displayed on the
// LEDs.
func healthMonitor() {
	var wg sync.WaitGroup

	running = true

	wg.Add(1)
	go configUpdater(&wg)

	wg.Add(1)
	go ledDriver(&wg)

	for _, t := range allTests {
		t.signal = make(chan bool, 1)
		wg.Add(1)
		go monitorOne(t, &wg)
	}

	sig := make(chan os.Signal, 2)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	s := <-sig
	logInfo("Received signal %v", s)
	running = false

	// kick all the test routines, so they will notice the change in
	// 'running' and exit.
	for _, t := range allTests {
		t.signal <- true
	}
	states.updated <- true

	wg.Wait()
}

func main() {
	var err error

	flag.Parse()

	isTerminal = terminal.IsTerminal(int(os.Stdout.Fd()))

	plat = platform.NewPlatform()
	if nodeUUID, err = plat.GetNodeID(); err != nil {
		logWarn("Failed to get nodeUUID: %v", err)
	} else {
		wanTest.source = "@/nodes/" + nodeUUID + "/nics"
		rpcdTest.source = "@/metrics/health/" + nodeUUID + "/cloud_rpc"
	}
	if aputil.IsSatelliteMode() {
		nodeName = nodeUUID
	} else {
		nodeName = "gateway"
	}

	states.current = make(map[string]string)
	states.old = make(map[string]string)
	states.updated = make(chan bool, 32)

	perLedTests = map[string][]*hTest{
		"3": networkTests,
		"4": stackTests,
	}
	allTests = make([]*hTest, 0)
	allTests = append(allTests, networkTests...)
	allTests = append(allTests, stackTests...)

	healthMonitor()
}
