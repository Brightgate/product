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

// Wireless device information
//
// Usage:
//
//   ap-diag wifi [-q] [-v] [interface]
//
// The wifi subcommand collects information about the wireless interfaces on the
// system, and classifies them as valid or not for use with the Brightgate AP
// stack.  If an argument is provided naming a wireless interface, only that
// interface will be considered.
//
// The exit code is 0 if all of the interfaces support the stack, or 1
// otherwise, or if an error occurred.
//
// For an interface to be considered valid, it must support VLANs (for the
// different security rings), multiple simultaneous SSIDs (we need one each for
// the PSK and EAP networks), and at least one channel in either the 2.4GHz or
// the 5GHz bands.
//
// By default, the output is a list of the interfaces and whether or not they
// are valid.  With the -v flag, more information is provided: what 802.11 modes
// are supported, the validity criteria, and a list of channels collated by
// channel width and frequency band.  With the -q flag, nothing is output; the
// exit code indicates validity.
package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"strings"

	"bg/ap_common/platform"
	"bg/ap_common/wificaps"
)

var wifiCmd *flag.FlagSet

func wPrintf(msg string, a ...interface{}) {
	fmt.Fprintf(wifiCmd.Output(), msg, a...)
}

func wifiUsage() {
	wPrintf("Usage: %s wifi [-q] [-v] [interface]\n", os.Args[0])
	wPrintf("\n")
	wifiCmd.PrintDefaults()
	wPrintf("\n")
	wPrintf("The wifi subcommand collects information about the wireless interfaces on the\n")
	wPrintf("system, and classifies them as valid or not for use with the Brightgate AP\n")
	wPrintf("stack.  If an argument is provided naming a wireless interface, only that\n")
	wPrintf("interface will be considered.\n")
	wPrintf("\n")
	wPrintf("The exit code is 0 if all of the interfaces support the stack, or 1\n")
	wPrintf("otherwise, or if an error occurred.\n")
	wPrintf("\n")
	wPrintf("For an interface to be considered valid, it must support VLANs (for the\n")
	wPrintf("different security rings), multiple simultaneous SSIDs (we need one each for\n")
	wPrintf("the PSK and EAP networks), and at least one channel in either the 2.4GHz or\n")
	wPrintf("the 5GHz bands.\n")
	wPrintf("\n")
	wPrintf("By default, the output is a list of the interfaces and whether or not they\n")
	wPrintf("are valid.  With the -v flag, more information is provided: what 802.11 modes\n")
	wPrintf("are supported, the validity criteria, and a list of channels collated by\n")
	wPrintf("channel width and frequency band.  With the -q flag, nothing is output; the\n")
	wPrintf("exit code indicates validity.\n")
}

func wifi(args []string) bool {
	wifiCmd = flag.NewFlagSet("wifi", flag.ExitOnError)
	wifiCmd.Usage = wifiUsage
	vFlag := wifiCmd.Bool("v", false, "verbose output")
	qFlag := wifiCmd.Bool("q", false, "no output")

	wifiCmd.Parse(args)

	verbose := 1
	if *vFlag == true {
		verbose = 2
	} else if *qFlag == true {
		verbose = 0
	}

	var ifaces []net.Interface
	if wifiCmd.NArg() > 0 {
		iface, err := net.InterfaceByName(wifiCmd.Arg(0))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Unable to find network device '%s': %v\n",
				wifiCmd.Arg(0), err)
			return false
		}
		ifaces = []net.Interface{*iface}
	} else {
		var err error
		ifaces, err = net.Interfaces()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Unable to inventory network devices: %v\n", err)
			return false
		}
	}

	plat := platform.NewPlatform()
	i := 0
	allvalid := true
	for _, iface := range ifaces {
		if !plat.NicIsWireless(iface.Name) {
			continue
		}
		cap, err := wificaps.GetCapabilities(iface.Name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Couldn't determine wifi capabilities of %s: %v\n",
				iface.Name, err)
			return false
		}

		reasons := make([]string, 0)
		if !cap.SupportVLANs {
			reasons = append(reasons, "doesn't support VLANs")
		}
		if cap.Interfaces < 2 {
			reasons = append(reasons, "supports at most one SSID")
		}
		if !cap.WifiBands[wificaps.LoBand] && cap.WifiBands[wificaps.HiBand] {
			reasons = append(reasons, "no supported channels")
		}
		valid := "INVALID"
		reasonStr := strings.Join(reasons, ", ")
		if len(reasons) == 0 {
			valid = "VALID"
		} else {
			allvalid = false
			reasonStr = fmt.Sprintf(" (%s)", reasonStr)
		}

		if verbose > 0 {
			if i > 0 && verbose > 1 {
				fmt.Printf("\n")
			}
			fmt.Printf("device: %s is %s%s\n", iface.Name, valid, reasonStr)
			if verbose > 1 {
				fmt.Println("   Location:", plat.NicLocation(iface.Name))
				fmt.Print(cap)
			}
		}
		i++
	}

	return allvalid
}
