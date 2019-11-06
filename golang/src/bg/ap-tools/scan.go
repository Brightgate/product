/*
 * COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
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

	"bg/ap_common/apscan"
)

func scan() {
	if len(os.Args) != 2 {
		fmt.Printf("Usage: %s <iface>\n", pname)
		os.Exit(1)
	}
	if os.Geteuid() != 0 {
		fmt.Printf("must be root to run %s\n", pname)
		os.Exit(1)
	}

	aps := apscan.ScanIface(os.Args[1])
	fmt.Printf("%-17s %5s %7s %5s %8s %8s  %s\n",
		"MacAddr", "Mode", "Channel", "Width", "Strength",
		"LastSeen", "SSID")
	for _, ap := range aps {
		fmt.Printf("%-17s %5s %7d %5d %8d %8v  %s\n",
			ap.Mac, ap.Mode, ap.Channel, ap.Width, ap.Strength,
			ap.LastSeen, ap.SSID)
	}
}

func init() {
	addTool("ap-scan", scan)
}
