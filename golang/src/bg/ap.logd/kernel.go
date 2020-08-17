/*
 * Copyright 2019 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package main

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"sync"
	"syscall"
	"time"

	"bg/ap_common/apcfg"
	"bg/ap_common/aputil"
	"bg/ap_common/platform"
)

const (
	switchReset = `NETDEV WATCHDOG: .*: transmit queue \d+ timed out`
	testCase    = `restart mcp daemons`
)

type logState struct {
	pipe *os.File
	wg   sync.WaitGroup
	done bool

	sync.Mutex
}

var (
	pipeName = apcfg.String("pipe", "/var/tmp/kernel_pipe", false, nil)

	plat   *platform.Platform
	kstate logState

	callbacks = map[*regexp.Regexp]func(string){
		regexp.MustCompile(switchReset): handleReset,
		regexp.MustCompile(testCase):    handleTestCase,
	}
)

func handleReset(msg string) {
	slog.Infof("hit '%s' - rebooting", msg)
	mcpd.Reboot()
}

func handleTestCase(msg string) {
	slog.Infof("found magic testcase string.  Restarting daemons.")
	mcpd.Do("all", "restart")
}

func openPipe() {
	var warned bool

	slog.Infof("Opening kernel log pipe: %s", *pipeName)
	for kstate.pipe == nil && !kstate.done {
		pipe, err := os.OpenFile(*pipeName, os.O_RDONLY,
			os.ModeNamedPipe)

		if err == nil {
			kstate.pipe = pipe
			slog.Infof("Opened kernel log pipe: %s", *pipeName)
		} else {
			if !warned {
				slog.Warnf("Failed to open kernel log pipe %s: %v",
					*pipeName, err)
				warned = true
			}
			kstate.Unlock()
			time.Sleep(time.Second)
			kstate.Lock()
		}
	}
}

func createPipe() error {
	slog.Infof("Creating named pipe %s for log input", *pipeName)
	if err := syscall.Mkfifo(*pipeName, 0600); err != nil {
		return fmt.Errorf("failed to create %s: %v", *pipeName, err)
	}

	slog.Infof("Restarting rsyslogd")
	if err := plat.RestartService("rsyslog"); err != nil {
		return fmt.Errorf("failed to restart rsyslogd: %v", err)
	}

	return nil
}

func kernelMonitor() {
	defer kstate.wg.Done()

	plat = platform.NewPlatform()

	if !aputil.FileExists(*pipeName) {
		if err := createPipe(); err != nil {
			slog.Errorf("creating %s: %v", *pipeName, err)
			return
		}
	}

	kstate.Lock()
	for !kstate.done {
		if kstate.pipe == nil {
			openPipe()
			continue
		}

		scanner := bufio.NewScanner(kstate.pipe)
		kstate.Unlock()
		for scanner.Scan() {
			line := scanner.Text()
			for re, f := range callbacks {
				if re.MatchString(line) {
					f(line)
				}
			}
		}

		kstate.Lock()
		err := scanner.Err()
		if err != nil && (err != os.ErrClosed || !kstate.done) {
			slog.Errorf("error processing log pipe: %v", err)
		}
		if kstate.pipe != nil {
			kstate.pipe.Close()
			kstate.pipe = nil
		}
	}
	kstate.Unlock()
}

func kernelMonitorStart() {
	kstate.wg.Add(1)
	go kernelMonitor()
}

func kernelMonitorStop() {
	slog.Infof("stopping kernel log monitor")

	kstate.Lock()
	if kstate.pipe != nil {
		kstate.pipe.Close()
		kstate.pipe = nil
	}
	kstate.done = true
	kstate.Unlock()

	kstate.wg.Wait()
	slog.Infof("stopped kernel log monitor")
}

