/*
 * Copyright 2018 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


// Run diagnostics on the AP device.
//
// The ap-diag command provides a subcommand CLI.  The only subcommand currently
// is "wifi".
package main

import (
	"flag"
	"fmt"
	"os"
)

func usage(exitcode int) {
	fmt.Fprintf(flag.CommandLine.Output(), "Usage: %s <cmd> [<args>]\n", os.Args[0])
	fmt.Fprintf(flag.CommandLine.Output(), "  subcommands: wifi\n")
	os.Exit(exitcode)
}

func main() {
	if len(os.Args) == 1 {
		usage(2)
	}

	switch os.Args[1] {
	case "wifi":
		allvalid := execWifi(os.Args[2:])
		if allvalid {
			os.Exit(0)
		}
		os.Exit(1)

	default:
		fmt.Printf("Unknown subcommand '%s'\n", os.Args[1])
		os.Exit(2)
	}
}

