/*
 * Copyright 2019 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package main

import (
	"net"
	"time"

	"bg/ap_common/dhcp"
	"bg/base_def"
)

// Try to identify the NIC connecting us to the world.  We should have at most
// one with a DHCP address, and it should be on an expected interface.
func findWan() (string, *dhcp.Info) {
	var wanNic string
	var wanLease *dhcp.Info

	interfaces, _ := plat.GetDHCPInterfaces()
	logDebug("dhcp interfaces: %v", interfaces)
	for _, name := range interfaces {
		iface, err := net.InterfaceByName(name)
		if err != nil {
			logWarn("unresolvable DHCP interface %s: %v", name, err)
			continue
		}

		hwaddr := iface.HardwareAddr.String()

		if plat.NicIsVirtual(name) {
			continue
		}

		lease, _ := dhcp.GetLease(name)
		expectedWan := plat.NicIsWan(name, hwaddr)

		if expectedWan {
			if wanNic != "" {
				logWarn("%s and %s both appear to be WAN nics",
					wanNic, name)
			} else {
				wanNic = name
				wanLease = lease
			}
		} else if lease != nil {
			logWarn("internal NIC %s has a dhcp lease: %v",
				name, lease)
		}
	}

	return wanNic, wanLease
}

// If we can't determine the mode when we first start up, we assume that we're
// running as a gateway.  We will keep checking until we get a DHCP lease, which
// will give us the definitive answer.
func modeMonitor() {
	var nic, oldMode, newMode string

	if oldMode = nodeMode; oldMode != base_def.MODE_GATEWAY {
		logPanic("should not enter nodeMonitor() in %s mode", oldMode)
	}

	delay := time.Second
	for {
		var lease *dhcp.Info

		if oldMode != nodeMode {
			logPanic("mode unexpectedly changed from %s to %s",
				oldMode, newMode)
		}

		if nic, lease = findWan(); lease != nil {
			newMode = lease.Mode
			break
		}
		time.Sleep(delay)
		if delay *= 2; delay > time.Minute {
			delay = time.Minute
		}
	}

	if newMode != base_def.MODE_SATELLITE {
		logInfo("DHCP lease confirms %s mode", nodeMode)
		return
	}

	logInfo("Switching from %s to %s mode", oldMode, newMode)

	all := "all"
	handleStop(selectTargets(&all))

	// Just in case the networkd shutdown/cleanup changed the wan interface,
	// we let the DHCP daemon reconfigure it.
	logInfo("Renewing DHCP leases")
	dhcp.RenewLease(nic)
	time.Sleep(2 * time.Second)

	nodeMode = newMode
	nodeName, _ = plat.GetNodeID()
	daemonReinit()

	handleStart(selectTargets(&all))
	go satelliteLoop()
}

