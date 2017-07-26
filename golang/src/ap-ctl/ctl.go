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

	"ap_common/mcp"
)

const (
	pname = "ap-ctl"
)

var valid_cmds = map[string]bool{
	"status":  true,
	"stop":    true,
	"start":   true,
	"restart": true,
}

func usage() {
	fmt.Printf("usage: %s <status | stop | start | restart> <daemon | all>\n",
		pname)
	os.Exit(2)
}

func printStatus(incoming string) {
	var status map[string]mcp.DaemonStatus

	if err := json.Unmarshal([]byte(incoming), &status); err != nil {
		fmt.Printf("Unable to unpack result from ap.mcp\n")
		return
	}

	if len(status) == 0 {
		return
	}
	if len(status) == 1 {
		for _, s := range status {
			fmt.Println(s.Status)
		}
		return
	}

	format := "%12s\t%5s\t%12s\t%s\n"
	fmt.Printf(format, "DAEMON", "PID", "STATUS", "SINCE")
	for _, s := range status {
		var pid, since string

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

		fmt.Printf(format, s.Name, pid, s.Status, since)
	}
}

func main() {
	var cmd, daemon string
	var err error

	if len(os.Args) != 3 {
		usage()
	}

	cmd = os.Args[1]
	daemon = os.Args[2]
	if _, ok := valid_cmds[cmd]; !ok {
		usage()
	}

	mcp, err := mcp.New("ap-ctl")
	if err != nil {
		fmt.Printf("%s: unable to connect to mcp: %v\n", pname, err)
		os.Exit(1)
	}

	if cmd == "status" {
		var rval string
		rval, err = mcp.GetStatus(daemon)
		if err != nil {
			fmt.Printf("%s: failed get status for '%s': %v\n",
				pname, daemon, err)
		} else {
			printStatus(rval)
		}
	} else {
		err := mcp.Do(daemon, cmd)
		if err != nil {
			fmt.Printf("%s: failed to %s %s: %v\n", pname, cmd,
				daemon, err)
		}
	}
	if err != nil {
		os.Exit(1)
	}
	os.Exit(0)
}
