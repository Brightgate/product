/*
 * COPYRIGHT 2017 Brightgate Inc. All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"bg/ap_common/aputil"
	"bg/ap_common/mcp"
	"bg/ap_common/platform"

	"golang.org/x/crypto/ssh/terminal"
)

type daemonMap map[string]*mcp.DaemonState

var (
	dateFormat  = time.Stamp
	colFormat   string
	verboseWait bool
)

var validCmds = map[string]int{
	"ip":      0,
	"status":  1,
	"stop":    1,
	"start":   1,
	"restart": 1,
	"crash":   1,
}

func ctlUsage() {
	fmt.Printf("usage:\t%s ip\n", pname)
	fmt.Printf("      \t%s <status | stop | start | restart | crash >"+
		" <daemon> | all\n", pname)
	os.Exit(2)
}

func stateString(s int) string {
	state, ok := mcp.States[s]
	if !ok {
		state = "invalid_state"
	}
	return state
}

func printLine(line string) {
	termWidth, _, _ := terminal.GetSize(1)
	if 0 < termWidth && termWidth < len(line) {
		line = line[:termWidth]
	}
	fmt.Printf("%s\n", line)
}

func printNode(states mcp.DaemonList, node string) {
	for _, s := range states {
		if s.Node != node {
			continue
		}
		var rss, swap, tm, pid, since string

		if s.Pid != -1 {
			pid = strconv.Itoa(s.Pid)
		} else {
			pid = "-"
		}
		if s.Since == time.Unix(0, 0) {
			since = "forever"
		} else {
			since = s.Since.Format(dateFormat)
		}
		if s.RssSize == 0 {
			rss, swap, tm = "-", "-", "-"
		} else {
			rss = fmt.Sprintf("%d MB", s.RssSize/(1024*1024))
			swap = fmt.Sprintf("%d MB", s.VMSwap/(1024*1024))

			// The correct way to find the scaling factor is to call
			// sysconf(_SC_CLK_TCK).  Since there doesn't seem to be
			// a simple way to do that in Go, I've just hardcoded
			// the value instead.
			ticks := s.Utime + s.Stime
			secs := ticks / 100

			tm = (time.Second * time.Duration(secs)).String()
		}

		printLine(fmt.Sprintf(colFormat, s.Name, pid, rss, swap, tm,
			stateString(s.State), since, s.Node))
	}
}

func printState(states mcp.DaemonList) {
	var localID string

	if len(states) == 0 {
		return
	} else if len(states) == 1 {
		fmt.Println(stateString(states[0].State))
		return
	}

	if aputil.IsSatelliteMode() {
		p := platform.NewPlatform()
		localID, _ = p.GetNodeID()
	} else {
		localID = "gateway"
	}

	// Build a list of the daemon states to display.  Daemons running on the
	// local node get shown first, then those running on remote nodes.
	nodes := make(map[string]bool)
	for _, s := range states {
		nodes[s.Node] = true
	}
	remoteList := make([]string, 0)
	for node := range nodes {
		if node != localID {
			remoteList = append(remoteList, node)
		}
	}

	printLine(fmt.Sprintf(colFormat, "DAEMON", "PID", "RSS", "SWAP",
		"TIME", "STATE", "SINCE", "NODE"))

	printNode(states, localID)
	for _, node := range remoteList {
		printNode(states, node)
	}
}

func getState(c *mcp.MCP, daemon string) (mcp.DaemonList, error) {
	var states mcp.DaemonList

	raw, err := c.GetState(daemon)
	if err == nil {
		if err = json.Unmarshal([]byte(raw), &states); err != nil {
			err = fmt.Errorf("unpacking daemon state: %v", err)
		}
	}

	return states, err
}

// Ask ap.mcp for the state of one or more daemons in the cluster.  Use the
// returned slice to build a node:daemon indexed map, which will be returned to
// the caller.
func getStateMap(c *mcp.MCP, daemon string) (daemonMap, error) {
	dmap := make(daemonMap)
	states, err := getState(c, daemon)
	if err == nil {
		for _, d := range states {
			key := d.Node + ":" + d.Name
			dmap[key] = d
		}
	}
	return dmap, err
}

// Periodically poll ap.mcp for the current states, printing any that have
// changed.  Repeat until all of the daemons have reached a stable state.
// This acts as a 'wait' until the current operation, and all its triggered
// state changes, have completed.
func wait(c *mcp.MCP, old daemonMap, timeout time.Duration) error {
	const maxDelay = time.Second / 2
	var done bool
	var err error

	transitionStates := map[int]bool{
		mcp.STARTING: true,
		mcp.INITING:  true,
		mcp.STOPPING: true,
	}

	giveUp := time.Now().Add(timeout)
	delay := 10 * time.Millisecond

	for !done && err == nil {
		var current daemonMap
		done = true

		time.Sleep(delay)
		if delay *= 2; delay > maxDelay {
			delay = maxDelay
		}

		current, err = getStateMap(c, "all")
		for name, c := range current {
			if transitionStates[c.State] {
				// If any daemon is still transitioning, we keep
				// waiting.
				done = false
			}

			if verboseWait && c.State != old[name].State {
				fmt.Printf("%s %s %v\n",
					c.Since.Format(time.Stamp), c.Name,
					mcp.States[c.State])
			}
		}
		if !done && time.Now().After(giveUp) {
			err = fmt.Errorf("timed out after %v", timeout)
		}
		old = current
	}

	return err
}

func do(c *mcp.MCP, cmd string, daemons []string) error {
	state, err := getStateMap(c, "all")
	if err == nil {
		for _, daemon := range daemons {
			if err = c.Do(daemon, cmd); err != nil {
				break
			}
		}
		if err == nil && (cmd == "stop" || verboseWait) {
			err = wait(c, state, 10*time.Second)
		}
	}
	return err
}

func ctl() {
	var cmd string
	var err error

	args := os.Args[1:]
	if len(args) > 0 && args[0] == "-v" {
		args = args[1:]
		verboseWait = true
	}
	if len(args) > 0 {
		cmd = args[0]
		args = args[1:]
	}

	minArgs, ok := validCmds[cmd]
	if !ok || len(args) < minArgs {
		ctlUsage()
	}

	mcp, err := mcp.New("ap-ctl")
	if err != nil {
		fmt.Printf("%s: unable to connect to mcp: %v\n", pname, err)
		os.Exit(1)
	}

	switch cmd {
	case "ip":
		ip, xerr := mcp.Gateway()
		if xerr != nil {
			err = fmt.Errorf("failed to get gateway: %v", xerr)
		} else {
			fmt.Printf("%v\n", ip)
		}

	case "status":
		for _, d := range args {
			states, xerr := getState(mcp, d)
			if xerr == nil {
				printState(states)
			} else {
				err = xerr
				break
			}
		}

	case "restart":
		if err = do(mcp, "stop", args); err == nil {
			err = do(mcp, "start", args)
		}
	case "stop", "start", "crash":
		err = do(mcp, cmd, args)
	}

	if err != nil {
		fmt.Printf("%s: %v\n", pname, err)
		os.Exit(1)
	}
}

func init() {
	var stateLen int

	for _, state := range mcp.States {
		if len(state) > stateLen {
			stateLen = len(state)
		}
	}

	dateField := strconv.Itoa(len(dateFormat) + 1)
	stateField := strconv.Itoa(stateLen + 1)
	colFormat = "%12s%6s%8s%7s%8s%" + stateField + "s%" + dateField + "s %s"

	addTool("ap-ctl", ctl)
}
