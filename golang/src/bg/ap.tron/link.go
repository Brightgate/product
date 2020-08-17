/*
 * Copyright 2020 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package main

import (
	"net"
	"os"
	"time"

	"bg/base_def"

	"github.com/sparrc/go-ping"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

var (
	wanName  string
	wanIface *net.Interface

	// XXX: These should come from a config property, so we can tweak it
	// over time and for geographical suitability
	pingAddresses = []string{"8.8.8.8", "1.1.1.1"}
	dnsNames      = []string{"www.google.com", "rpc0.b10e.net"}

	networkTests = []*hTest{wanTest, carrierTest, addrTest,
		connectTest, dnsTest}

	// The following 2 tests are used to determine whether the wan link is
	// alive.  The wan_carrier state is displayed on a dedicated LED and is
	// controlled by the hardware.  We only track it internally so it can be
	// used to trigger higher-level tests.
	wanTest = &hTest{
		name:     "wan_discover",
		testFn:   getWanName,
		period:   10 * time.Second,
		source:   "",                    // initialized at runtime
		triggers: []*hTest{carrierTest}, // new wan -> check carrier
	}
	carrierTest = &hTest{
		name:     "wan_carrier",
		testFn:   getCarrierState,
		period:   time.Second,
		triggers: []*hTest{addrTest},
	}

	// The following 3 tests attempt to determine how much basic network
	// functionality we currently have.  These are used to determine the
	// blink pattern displayed on LED 3.
	addrTest = &hTest{
		name:     "wan_address",
		testFn:   getAddressState,
		period:   5 * time.Second,
		triggers: []*hTest{connectTest},
		ledValue: 10,
	}
	connectTest = &hTest{
		name:     "net_connect",
		testFn:   connCheck,
		period:   30 * time.Second,
		triggers: []*hTest{dnsTest, rpcdTest},
		ledValue: 90,
	}
	dnsTest = &hTest{
		name:     "dns_lookup",
		testFn:   dnsCheck,
		period:   60 * time.Second,
		ledValue: 100,
	}
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
			ring, _ := nic.GetChildString("ring")
			if ring == base_def.RING_WAN {
				nicName = name
				break
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

func getWanLink() netlink.Link {
	var link netlink.Link

	if wanIface != nil {
		l, err := netlink.LinkByIndex(wanIface.Index)
		if err != nil {
			logInfo("failed to get link for %s(%d): %v",
				wanName, wanIface.Index, err)
		} else {
			link = l
		}
	}

	return link
}

// Determine whether we have a live upstream link
func getCarrierState(t *hTest) bool {
	var live bool

	if l := getWanLink(); l != nil {
		a := l.Attrs()
		live = (a != nil && a.OperState == netlink.OperUp)
	}

	return live
}

// Determine the scope of our WAN address (if any)
func getAddressState(t *hTest) bool {
	state := "none"

	if l := getWanLink(); l != nil {
		addrs, err := netlink.AddrList(l, netlink.FAMILY_V4)
		if err != nil {
			logInfo("Failed to get addr for %s: %v", wanName, err)
			addrs = nil
		}

		for _, a := range addrs {
			if a.Flags == unix.IFA_F_PERMANENT && a.IP != nil {
				if a.IP.IsLinkLocalUnicast() {
					state = "self-assigned"
				} else {
					state = "valid"
					break
				}
			}
		}
	}

	t.setState(state)
	return (state == "valid")
}

// Check to see whether we can ping remote sites
func connCheck(t *hTest) bool {
	const count = 3

	// We can't use the ICMP-based connection test unless we're running as
	// root.  We pretend that the connection succeeded, so we continue on to
	// perform the DNS check.
	if os.Geteuid() != 0 {
		t.setState("")
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
		t.setState("fail")
	} else {
		t.setState("success")
	}

	return (t.state == "success")
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
		t.setState("fail")
	} else {
		t.setState("success")
	}

	return (t.state == "success")
}

