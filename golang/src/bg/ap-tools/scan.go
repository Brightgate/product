/*
 * Copyright 2019 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
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

