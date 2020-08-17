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
	"net"
	"strconv"
	"strings"

	"bg/ap_common/aputil"
	"bg/ap_common/wificaps"
	"bg/common/cfgapi"
	"bg/common/network"
	"bg/common/wifi"
)

func configUpdateRing(nic *physDevice) {
	id := plat.NicID(nic.name, nic.hwaddr)
	base := "@/nodes/" + nodeID + "/nics/" + id
	config.CreateProp(base+"/ring", nic.ring, nil)
}

func newNicOps(id string, nic *physDevice,
	cur *cfgapi.PropertyNode) []cfgapi.PropertyOp {

	ops := make([]cfgapi.PropertyOp, 0)
	newVals := make(map[string]string)

	if nic != nil {
		newVals["name"] = nic.name
		newVals["mac"] = nic.hwaddr
		if nic.ring != "" {
			newVals["ring"] = nic.ring
		}
		if nic.wireless {
			newVals["kind"] = "wireless"
			if cap := nic.cap; cap != nil {
				b := aputil.SortStringKeys(cap.WifiBands)
				m := aputil.SortStringKeys(cap.WifiModes)
				x := aputil.SortIntKeys(cap.Channels)
				c := make([]string, 0)
				for _, channel := range x {
					c = append(c, strconv.Itoa(channel))
				}
				if len(b) > 0 {
					newVals["bands"] = list(b)
				}
				if len(m) > 0 {
					newVals["modes"] = list(m)
				}
				if len(c) > 0 {
					newVals["channels"] = list(c)
				}
			}

		} else {
			newVals["kind"] = "wired"
		}

		// Check to see whether anything has changed before we send any
		// updates to configd
		if cur != nil {
			matches := 0
			for prop, val := range newVals {
				if old, ok := cur.Children[prop]; ok {
					if old.Value == val {
						matches++
					}
				}
			}
			if matches == len(newVals) {
				// everything matches - send back an empty slice
				return ops
			}
		} else {
			newVals["state"] = wifi.DevOK
		}
	}

	base := "@/nodes/" + nodeID + "/nics/" + id
	if cur != nil {
		op := cfgapi.PropertyOp{
			Op:   cfgapi.PropDelete,
			Name: base,
		}
		ops = append(ops, op)
	}
	for prop, val := range newVals {
		op := cfgapi.PropertyOp{
			Op:    cfgapi.PropCreate,
			Name:  base + "/" + prop,
			Value: val,
		}
		ops = append(ops, op)
		slog.Debugf("Setting %s to %s", op.Name, op.Value)
	}

	return ops
}

// Update the config tree with the current NIC inventory
func updateConfigTree(all map[string]*physDevice) {
	needName := !aputil.IsSatelliteMode()

	inventory := make(map[string]*physDevice)
	for id, d := range all {
		inventory[id] = d
	}

	// Get the information currently recorded in the config tree
	root := "@/nodes/" + nodeID
	nics := make(cfgapi.ChildMap)
	if c := config.GetChildren(root); c != nil {
		if c["name"] != nil {
			needName = false
		}
		if n := c["nics"]; n != nil {
			nics = n.Children
		}
	}

	// Examine each entry in the config tree to determine whether it matches
	// our current inventory.
	ops := make([]cfgapi.PropertyOp, 0)
	for id, nic := range nics {
		var newOps []cfgapi.PropertyOp

		if dev := inventory[id]; dev != nil {
			newOps = newNicOps(id, dev, nic)
			delete(inventory, id)
		} else {
			// This nic is in the config tree, but not in our
			// current inventory.  Clean it up.
			newOps = newNicOps(id, nil, nic)
		}
		ops = append(ops, newOps...)
	}

	// If we have any remaining NICs that weren't already in the
	// tree, add them now.
	for id, d := range inventory {
		newOps := newNicOps(id, d, nil)
		ops = append(ops, newOps...)
	}

	// If this is the gateway node and it doesn't already have a name,
	// give it the default value of "gateway"
	if needName {
		op := cfgapi.PropertyOp{
			Op:    cfgapi.PropCreate,
			Name:  root + "/name",
			Value: "gateway",
		}
		ops = append(ops, op)
	}

	if len(ops) != 0 {
		if _, err := config.Execute(nil, ops).Wait(nil); err != nil {
			slog.Warnf("Error updating NIC inventory: %v", err)
		}
	}
}

func getEthernet(i net.Interface) *physDevice {
	d := physDevice{
		name:   i.Name,
		hwaddr: i.HardwareAddr.String(),
	}
	return &d
}

func getWireless(i net.Interface) *physDevice {
	var err error

	d := physDevice{
		name:     i.Name,
		hwaddr:   i.HardwareAddr.String(),
		wireless: true,
	}

	if strings.HasPrefix(d.hwaddr, "02:00") {
		slog.Debugf("Skipping emulated device %s (%s)",
			d.name, d.hwaddr)
		return nil
	}

	if d.cap, err = wificaps.GetCapabilities(d.name); err != nil {
		slog.Warnf("Couldn't determine wifi capabilities of %s: %v",
			d.name, err)
		return nil
	}

	slog.Infof("device: %s", d.name)
	// Emit one line at a time to the log, or only the first line will get
	// the log prefix.
	capstr := fmt.Sprintf("%s", d.cap)
	for _, line := range strings.Split(strings.TrimSuffix(capstr, "\n"), "\n") {
		slog.Debugf(line)
	}

	// When we create multiple SSIDs, hostapd will generate additional
	// bssids by incrementing the final octet of the nic's mac address.
	// hostapd requires that the base and generated mac addresses share the
	// upper 47 bits, so we need to ensure that the base address has the
	// lowest bits set to 0.
	oldMac := d.hwaddr
	d.hwaddr, err = network.MacUpdateLastOctet(d.hwaddr, 0)
	if err != nil {
		slog.Warnf("Updating %s from %v: %v", d.name, oldMac, err)
	} else if d.hwaddr != oldMac {
		slog.Debugf("Changed mac from %s to %s", oldMac, d.hwaddr)
	}

	// If we generate new macs for multiple SSIDs, those generated macs will
	// have the locally administered bit set.  Because we need the upper
	// bits of all macs to match, we have to set the bit for the base mac
	// even if we haven't modified it.
	d.hwaddr = network.MacSetLocal(d.hwaddr)

	return &d
}

// Find the other nodes on which a device with this mac is present.  Returns
// strings listing the remote nodes and the offline nodes, with each instance
// named "<node>/<device name>".
func getRemoteWifi(mac string, nodes []cfgapi.NodeInfo) (string, string) {
	remote := make([]string, 0)
	offline := make([]string, 0)

	for _, node := range nodes {
		if node.ID == nodeID {
			continue
		}
		for _, nic := range node.Nics {
			if nic.MacAddr == mac && nic.WifiInfo != nil {
				n := node.ID + "/" + nic.Name
				if node.Alive == nil {
					offline = append(offline, n)
				} else {
					remote = append(remote, n)
				}
			}
		}
	}

	return list(remote), list(offline)
}

//
// Inventory the physical network devices in the system
//
func discoverDevices() {
	ifaces, err := net.Interfaces()
	if err != nil {
		slog.Fatalf("Unable to inventory network devices: %v", err)
	}

	nodes, err := config.GetNodes()
	if err != nil {
		slog.Warnf("getting @/nodes: %v", err)
	}

	macs := make(map[string]*physDevice)
	allNics := make(map[string]*physDevice)
	wiredNics = make(map[string]*physDevice)

	for _, i := range ifaces {
		var d *physDevice

		if i.HardwareAddr.String() == "00:00:00:00:00:00" {
			slog.Warnf("bogus mac address for %s: %s", i.Name,
				i.HardwareAddr.String())
			continue
		}
		if plat.NicIsVirtual(i.Name) {
			continue
		}
		if plat.NicIsWired(i.Name) {
			d = getEthernet(i)
		} else if plat.NicIsWireless(i.Name) {
			d = getWireless(i)
		}

		// If this is a wireless device and we already have another
		// wireless nic with the same mac address, we want to leave this
		// one offline.
		if d != nil && d.wireless {
			var conflicts, faults string

			name := d.name
			mac := d.hwaddr
			if local := macs[mac]; local != nil {
				faults = " local: " + local.name
				d = nil
			}

			remote, offline := getRemoteWifi(mac, nodes)
			if len(remote) > 0 {
				faults += " remote nodes: " + remote
				d = nil
			}
			if len(offline) > 0 {
				// If the other node is offline, it's safe to
				// use this device despite the conflict.  It's
				// still worth noting in the log.
				conflicts = " offline nodes: " + offline
			}

			if len(faults+conflicts) > 0 {
				msg := fmt.Sprintf("multiple instances of %s:%s",
					mac, faults+conflicts)
				slog.Warn(msg)

				if len(faults) > 0 {
					aputil.ReportHardware(name, msg)
				}
			}
		}

		if d != nil {
			id := plat.NicID(d.name, d.hwaddr)
			allNics[id] = d
			if !d.wireless {
				wiredNics[id] = d
			}
			macs[d.hwaddr] = d
		}
	}

	nicsProp := "@/nodes/" + nodeID + "/nics"
	for nicID, nic := range config.GetChildren(nicsProp) {
		if d := wiredNics[nicID]; d != nil {
			d.ring, _ = nic.GetChildString("ring")
			x, _ := nic.GetChildString("state")
			d.disabled = strings.EqualFold(x, "disabled")
		}
	}

	updateConfigTree(allNics)
}

