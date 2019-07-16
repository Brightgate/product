/*
 * COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 *
 */

package main

import (
	"net"
	"os"
	"syscall"
	"time"
	"unsafe"

	"bg/base_def"

	"github.com/sparrc/go-ping"
)

var (
	wanName  string
	wanIface *net.Interface
)

var (
	// XXX: These should come from a config property, so we can tweak it
	// over time and for geographical suitability
	pingAddresses = []string{"8.8.8.8", "1.1.1.1"}
	dnsNames      = []string{"www.google.com", "rpc0.b10e.net"}
)

// ap.networkd selects the wan device, which we learn of via configd.  If we
// can't get to configd, we make our best guess at what the wan device might
// be.  Without this, we may incorrectly diagnose a configd failure as a lack of
// network connectivity.
func chooseWanFallback() string {
	all, _ := net.Interfaces()
	for _, i := range all {
		name := i.Name
		hwaddr := i.HardwareAddr.String()

		if plat.NicIsWired(name) && plat.NicIsWan(name, hwaddr) {
			return name
		}
	}

	return ""
}

// Given the gateway's @/nodes/<uuid>/nics hierarchy, find the nic that has been
// configured as our wan-facing device.
func getWanName(t *hTest) bool {
	var nicName string

	if t.data != nil && t.data.Children != nil {
		for name, nic := range t.data.Children {
			if nic.Children != nil {
				node, ok := nic.Children["ring"]
				if ok && node.Value == base_def.RING_WAN {
					nicName = name
					break
				}
			}
		}
	}

	if nicName == "" {
		nicName = chooseWanFallback()
	}

	if wanName != nicName {
		logDebug("wan nic has changed from %s to %s", wanName, nicName)
		wanName = nicName
		wanIface = nil
	}

	if wanIface == nil && wanName != "" {
		wanIface, _ = net.InterfaceByName(nicName)
	}

	return (wanIface != nil)
}

// Use the netlink() system call to fetch a specific set of attributes for a
// single interface
func getAttrs(idx, call int, hdr uint16) []syscall.NetlinkRouteAttr {
	msg, err := syscall.NetlinkRIB(call, syscall.AF_UNSPEC)
	if err != nil {
		return nil
	}

	msgs, err := syscall.ParseNetlinkMessage(msg)
	if err != nil {
		return nil
	}

	for _, m := range msgs {
		if m.Header.Type == hdr {
			ifim := (*syscall.IfInfomsg)(unsafe.Pointer(&m.Data[0]))
			if int(ifim.Index) == idx {
				attrs, _ := syscall.ParseNetlinkRouteAttr(&m)
				return attrs
			}
		}
	}

	return nil
}

// Determine whether we have a live upstream link
func getCarrierState(t *hTest) bool {
	const IflaCarrier = 33

	if wanIface == nil {
		return false
	}

	attrs := getAttrs(wanIface.Index, syscall.RTM_GETLINK,
		syscall.RTM_NEWLINK)

	for _, a := range attrs {
		if a.Attr.Type == IflaCarrier {
			if a.Value[0] == 1 {
				return true
			}
		}
	}

	return false
}

// Determine the scope of our WAN address (if any)
func getAddressState(t *hTest) bool {
	t.state = "none"

	if wanIface == nil {
		return false
	}

	attrs := getAttrs(wanIface.Index, syscall.RTM_GETADDR,
		syscall.RTM_NEWADDR)

	for _, a := range attrs {
		if a.Attr.Type == syscall.IFLA_ADDRESS &&
			len(a.Value) == net.IPv4len {

			ip := net.IP(a.Value)
			if ip.IsLinkLocalUnicast() {
				t.state = "self-assigned"
			} else {
				t.state = "valid"
				break
			}
		}
	}

	return (t.state == "valid")
}

// Check to see whether we can ping remote sites
func connCheck(t *hTest) bool {
	const count = 3

	// We can't use the ICMP-based connection test unless we're running as
	// root.  We pretend that the connection succeeded, so we continue on to
	// perform the DNS check.
	if os.Geteuid() != 0 {
		t.state = ""
		return true
	}

	hits := 0
	for _, addr := range pingAddresses {
		pinger, err := ping.NewPinger(addr)
		if err == nil {
			pinger.Count = count
			pinger.Timeout = time.Second
			pinger.SetPrivileged(true)
			pinger.Run()
			stats := pinger.Statistics()

			if stats.PacketsRecv > 0 {
				hits++
			} else {
				logDebug("failed to ping %s", addr)
			}
		} else {
			logInfo("failed to create pinger: %v", err)
		}
	}

	if (hits == 0) || (*strict && hits < len(pingAddresses)) {
		t.state = "fail"
		return false
	}

	t.state = "success"
	return true
}

// Check to see whether we can resolve some well-known addresses
func dnsCheck(t *hTest) bool {
	hits := 0

	for _, name := range dnsNames {
		if _, err := net.LookupHost(name); err != nil {
			logDebug("Failed to lookup %s: %v", name, err)
		} else {
			logDebug("Found %s", name)
			hits++
		}
	}

	if (hits == 0) || (*strict && hits < len(dnsNames)) {
		t.state = "fail"
		return false
	}

	t.state = "success"
	return true
}
