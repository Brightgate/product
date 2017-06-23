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
	"fmt"
	"os"

	"ap_common/mcp"
)

var valid_cmds = map[string]bool{
	"status":  true,
	"stop":    true,
	"start":   true,
	"restart": true,
}

func usage() {
	fmt.Printf("usage: ap-ctl <status | stop | start | restart> <daemon | all>\n")
	os.Exit(1)
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
		fmt.Printf("Unable to connect to mcp: %v\n", err)
		os.Exit(1)
	}

	if cmd == "status" {
		rval, err := mcp.GetStatus(daemon)
		if err != nil {
			fmt.Printf("Failed get status for %s: %v\n",
				daemon, err)
		}
		fmt.Printf("%v\n", rval)
	} else {
		err := mcp.Do(daemon, cmd)
		if err != nil {
			fmt.Printf("Failed to %s %s: %v\n", cmd, daemon, err)
		}
	}
	if err != nil {
		os.Exit(1)
	}
	os.Exit(0)
}
