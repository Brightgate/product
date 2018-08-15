/*
 * COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 *
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
		allvalid := wifi(os.Args[2:])
		if allvalid {
			os.Exit(0)
		}
		os.Exit(1)

	default:
		fmt.Printf("Unknown subcommand '%s'\n", os.Args[1])
		os.Exit(2)
	}
}
