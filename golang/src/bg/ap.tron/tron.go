/*
 * Copyright 2020 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
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
	pname = "ap.tron"
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

func (t *hTest) setValue(key, newVal string) {
	prop := t.name + "/" + key
	states.Lock()
	defer states.Unlock()

	old := states.current[prop]
	if old != newVal {
		states.current[prop] = newVal
		states.updated <- true
	}
}

func (t *hTest) setState(newState string) {
	if t.state != newState {
		t.state = newState
		t.setValue(newState, time.Now().Format(time.RFC3339))
	}
}

// Periodically execute a single health check.
func (t *hTest) monitor(wg *sync.WaitGroup) {
	logDebug("%s monitor started", t.name)

	defer wg.Done()
	for running {
		oldPass := t.pass
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
		go t.monitor(&wg)
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
	allTests = append(allTests, sysTests...)
	allTests = append(allTests, networkTests...)
	allTests = append(allTests, stackTests...)

	healthMonitor()
}

