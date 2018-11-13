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
// the PSK and EAP networks, and we expect to need two PSK and one Open
// networks), and at least one channel in either the 2.4GHz or the 5GHz bands.
//
// By default, the output is a list of the interfaces and whether or not they
// are valid.  With the -v flag, more information is provided: the physical
// location of the device, if known; the permanent MAC address, if known; what
// 802.11 modes are supported; the validity criteria; and a list of channels
// collated by channel width and frequency band.  With the -q flag, nothing is
// output; the exit code indicates validity.
package main

// #include <stdlib.h>
// #include <string.h>
// #include <unistd.h>
// #include <net/if.h>
// #include <sys/ioctl.h>
// #include <sys/socket.h>
// #include <sys/types.h>
// #include <linux/ethtool.h>
// #include <linux/netdevice.h>
// #include <linux/sockios.h>
//
// void *
// get_perm_addr(_GoString_ name, int *addrlen) {
//     struct ifreq ifr;
//     struct ethtool_perm_addr *cmd = NULL;
//     void *ret = NULL;
//     int fd = -1;
//
//     // If we would overflow ifr.ifr_name, we won't get a well-defined answer.
//     if (_GoStringLen(name) > sizeof(ifr.ifr_name)) {
//         goto cleanup;
//     }
//     memset(&ifr, 0, sizeof(ifr));
//     // ifr.ifr_name is not a null-terminated string
//     strncpy(ifr.ifr_name, _GoStringPtr(name), sizeof(ifr.ifr_name));
//     ifr.ifr_name[_GoStringLen(name)] = '\0';
//
//     if ((cmd = malloc(sizeof(*cmd) + MAX_ADDR_LEN)) == NULL) {
//         goto cleanup;
//     }
//     cmd->cmd = ETHTOOL_GPERMADDR;
//     cmd->size = MAX_ADDR_LEN;
//     ifr.ifr_data = (void *)cmd;
//
//     if ((fd = socket(AF_INET, SOCK_DGRAM, 0)) < 0) {
//         goto cleanup;
//     }
//     if (ioctl(fd, SIOCETHTOOL, &ifr) == -1) {
//         goto cleanup;
//     }
//     if ((ret = malloc(cmd->size)) == NULL) {
//         goto cleanup;
//     }
//     memcpy(ret, cmd->data, cmd->size);
//     *addrlen = cmd->size;
//
// cleanup:
//     if (cmd != NULL) {
//         free(cmd);
//     }
//     if (fd != -1) {
//         close(fd);
//     }
//
//     return ret;
// }
import "C"

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
	wPrintf(`
The wifi subcommand collects information about the wireless interfaces on the
system, and classifies them as valid or not for use with the Brightgate AP
stack.  If an argument is provided naming a wireless interface, only that
interface will be considered.

The exit code is 0 if all of the interfaces support the stack, or 1
otherwise, or if an error occurred.

For an interface to be considered valid, it must support VLANs (for the
different security rings), multiple simultaneous SSIDs (we need one each for
the PSK and EAP networks, and we expect to need two PSK and one Open
networks), and at least one channel in either the 2.4GHz or the 5GHz bands.

By default, the output is a list of the interfaces and whether or not they
are valid.  With the -v flag, more information is provided: the physical
location of the device, if known; the permanent MAC address, if known; what
802.11 modes are supported; the validity criteria; and a list of channels
collated by channel width and frequency band.  With the -q flag, nothing is
output; the exit code indicates validity.
`)
}

// getPermAddr returns the MAC address the kernel considers the "permanent" MAC
// address for a given device.  This may or may not be the address the hardware
// had programmed into it when it left the factory.
func getPermAddr(devName string) net.HardwareAddr {
	var addrLen C.int
	addr := C.get_perm_addr(devName, &addrLen)
	if addr == nil {
		return net.HardwareAddr{}
	}
	defer C.free(addr)
	return net.HardwareAddr(C.GoBytes(addr, addrLen))
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
		if cap.Interfaces < 4 {
			reasons = append(reasons,
				fmt.Sprintf("supports only %d SSIDs (need 4 minimum)",
					cap.Interfaces))
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
				fmt.Println("   Permanent MAC Address:", getPermAddr(iface.Name))
				fmt.Print(cap)
			}
		}
		i++
	}

	return allvalid
}
