/*
 * Copyright 2020 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package main

import (
	"fmt"
	"io/ioutil"
	"net"
	"strings"
	"time"

	"bg/ap_common/netctl"
	"bg/base_def"
)

func addNicToBridge(bridge, nic string) error {
	slog.Infof("Adding %s to bridge %s", nic, bridge)
	if _, err := net.InterfaceByName(nic); err != nil {
		return fmt.Errorf("looking for %s: %v", nic, err)
	}

	if err := netctl.LinkUp(nic); err != nil {
		return fmt.Errorf("Failed to enable %s: %v", nic, err)
	}

	if err := netctl.BridgeAddIface(bridge, nic); err != nil {
		return fmt.Errorf("adding %s to %s: %v", nic, bridge, err)
	}

	return nil
}

func removeNicFromBridge(bridge, nic string) error {
	slog.Infof("Deleting %s from bridge %s", nic, bridge)

	if err := netctl.BridgeDelIface(bridge, nic); err != nil {
		return fmt.Errorf("deleting %s from bridge %s: %v",
			nic, bridge, err)
	}
	return nil
}

// If hostapd authorizes a client that isn't assigned to a VLAN, it gets
// connected to the physical wifi device rather than a virtual interface.
// Connect those physical devices to the UNENROLLED bridge once hostapd is
// running.  We don't have a good way to determine when hostapd has gotten far
// enough for this operation to succeed, so we just keep trying.
func rebuildUnenrolled(devs []*physDevice, interrupt chan bool) {
	bridge := rings[base_def.RING_UNENROLLED].Bridge

	t := time.NewTicker(time.Second)
	defer t.Stop()
	for len(devs) > 0 {
		select {
		case <-interrupt:
			return
		case <-t.C:
		}

		bad := make([]*physDevice, 0)
		for _, dev := range devs {
			if dev.disabled {
				continue
			}

			if err := addNicToBridge(bridge, dev.name); err != nil {
				slog.Warnf("%v", err)
				bad = append(bad, dev)
			}
		}
		devs = bad
	}
}

// Iterate over all devices on a bridge, removing any wireless NICs
func wifiBridgeCleanup(bridge string) {
	devs, _ := ioutil.ReadDir("/sys/devices/virtual/net/" + bridge + "/brif")

	slog.Debugf("cleaning up %s", bridge)
	for _, dev := range devs {
		name := dev.Name()

		if plat.NicIsWireless(name) {
			removeNicFromBridge(bridge, name)
		}
	}
}

// Iterate over all bridges in the system, looking for wireless devices that
// should be removed before allowing hostapd to run.
func wifiCleanup() {
	devs, _ := ioutil.ReadDir("/sys/devices/virtual/net")

	for _, dev := range devs {
		name := dev.Name()

		if strings.HasPrefix(name, "b") {
			wifiBridgeCleanup(name)
		}
	}
}

