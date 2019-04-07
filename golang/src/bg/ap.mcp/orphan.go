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
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"bg/ap_common/aputil"
)

func isAlive(pid int) bool {
	procName := fmt.Sprintf("/proc/%d", pid)

	if f, err := os.Open(procName); err == nil {
		f.Close()
		return true
	}

	return false
}

// given a pid, return the name of the binary associated with that pid
func getBinaryName(pid int) (string, error) {
	var name string

	link := fmt.Sprintf("/proc/%d/exe", pid)
	name, err := os.Readlink(link)
	if err == nil {
		// Trim off commentary, like "(deleted)"
		if idx := strings.Index(name, " ("); idx > 0 {
			name = name[:idx]
		}
		name = filepath.Base(name)
	} else {
		err = fmt.Errorf("following %s: %v", link, err)
	}

	return name, err
}

// Return all of the pids for processes running any of the binaries in the
// 'names' parameter.
func getPids(names []string) map[int]string {
	inSet := make(map[string]bool)
	for _, n := range names {
		inSet[n] = true
	}

	f, err := os.Open("/proc")
	if err != nil {
		logWarn("opening /proc: %v", err)
		return nil
	}
	defer f.Close()

	all, err := f.Readdirnames(0)
	if err != nil {
		logWarn("reading /proc: %v", err)
		return nil
	}

	// Iterate over all the entries in /proc.  For any that look like pids,
	// extract the binary name associated with the pid, and compare that
	// name to those in the provided list.
	pids := make(map[int]string)
	for _, name := range all {
		if pid, err := strconv.Atoi(name); err == nil {
			binary, _ := getBinaryName(pid)
			if inSet[binary] {
				pids[pid] = binary
			}
		}
	}

	return pids
}

// Given a set of binary names, find all the processes running any of those
// binaries, and kill them.
func killSet(binaries []string) {

	pids := getPids(binaries)

	for pid, binary := range pids {
		logInfo("killing pid %d (%s)", pid, binary)
		p, err := os.FindProcess(pid)
		if err != nil {
			logWarn("constructing Process struct:  %v", err)
			continue
		}

		kill := func(sig syscall.Signal) error { return p.Signal(sig) }
		alive := func() bool { return isAlive(pid) }
		aputil.RetryKill(kill, alive, time.Second)
	}
}

// Track down and kill any processes that may have been left behind if a
// previous instance of mcp crashed rather than exiting cleanly.
func orphanCleanup() {
	logInfo("Cleaning up any orphaned processes")

	// First we kill any top-level daemons that may have survived
	daemonNames := make([]string, 0)
	for _, d := range daemons.local {
		daemonNames = append(daemonNames, d.Binary)
	}
	killSet(daemonNames)

	// Then kill any tools invoked by our daemons that somehow escaped the
	// first round of reaping
	tools := []string{"ap-defaultpass", "ap-vuln-aggregate", "nmap",
		"hostapd"}
	killSet(tools)
}
