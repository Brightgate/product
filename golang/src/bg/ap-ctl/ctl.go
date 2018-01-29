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

	"bg/ap_common/mcp"
)

const (
	pname = "ap-ctl"
)

var validCmds = map[string]bool{
	"status":  true,
	"stop":    true,
	"start":   true,
	"restart": true,
}

func usage() {
	fmt.Printf("usage:\t%s <status | stop | start | restart> daemon...\n"+
		"\t%s <status | stop | start | restart> all\n",
		pname, pname)
	os.Exit(2)
}

func stateString(s int) string {
	state, ok := mcp.States[s]
	if !ok {
		state = "invalid_state"
	}
	return state
}

func printState(incoming string) {
	var states mcp.DaemonList

	if err := json.Unmarshal([]byte(incoming), &states); err != nil {
		fmt.Printf("Unable to unpack result from ap.mcp\n")
		return
	}

	if len(states) == 0 {
		return
	}
	if len(states) == 1 {
		fmt.Println(stateString(states[0].State))
		return
	}

	const format = "%12s\t%5s\t%7s\t%7s\t%7s\t%12s\t%s\n"
	fmt.Printf(format, "DAEMON", "PID", "RSS", "SWAP", "TIME",
		"STATE", "SINCE")
	for _, s := range states {
		var rss, swap, tm, pid, since string

		if s.Pid != -1 {
			pid = strconv.Itoa(s.Pid)
		} else {
			pid = "-"
		}
		if s.Since == time.Unix(0, 0) {
			since = "forever"
		} else {
			since = s.Since.Format("Mon Jan 2 15:04:05")
		}
		if s.State == mcp.ONLINE {
			rss = fmt.Sprintf("%d MB", s.RssSize/(1024*1024))
			swap = fmt.Sprintf("%d MB", s.VMSwap/(1024*1024))

			// The correct way to find the scaling factor is to call
			// sysconf(_SC_CLK_TCK).  Since there doesn't seem to be
			// a simple way to do that in Go, I've just hardcoded
			// the value instead.
			ticks := s.Utime + s.Stime
			secs := ticks / 100

			tm = (time.Second * time.Duration(secs)).String()

		} else {
			rss, swap, tm = "-", "-", "-"
		}

		fmt.Printf(format, s.Name, pid, rss, swap, tm,
			stateString(s.State), since)
	}
}

func main() {
	var cmd, daemon string
	var err error

	if len(os.Args) < 3 {
		usage()
	}

	cmd = os.Args[1]
	if _, ok := validCmds[cmd]; !ok {
		usage()
	}

	mcp, err := mcp.New("ap-ctl")
	if err != nil {
		fmt.Printf("%s: unable to connect to mcp: %v\n", pname, err)
		os.Exit(1)
	}

	for _, daemon = range os.Args[2:] {
		if cmd == "status" {
			var rval string
			rval, err = mcp.GetState(daemon)
			if err != nil {
				fmt.Printf("%s: failed get state for '%s': %v\n",
					pname, daemon, err)
			} else {
				printState(rval)
			}
		} else {
			err = mcp.Do(daemon, cmd)
			if err != nil {
				fmt.Printf("%s: failed to %s %s: %v\n", pname, cmd,
					daemon, err)
			}
		}
		if err != nil {
			os.Exit(1)
		}
	}
	os.Exit(0)
}
