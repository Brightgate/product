/*
 * COPYRIGHT 2020 Brightgate Inc.  All rights reserved.
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
	"fmt"
	"io/ioutil"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"bg/ap_common/aputil"
	"bg/ap_common/mcp"
	"bg/base_def"
	"bg/base_msg"
	"bg/common/cfgapi"

	"github.com/golang/protobuf/proto"
)

func addDevToRingBridge(dev *physDevice, ring string) error {
	var err error

	err = exec.Command(plat.IPCmd, "link", "set", "up", dev.name).Run()
	if err != nil {
		slog.Warnf("Failed to enable %s: %v", dev.name, err)
	}

	if config := rings[ring]; config != nil {
		br := config.Bridge
		slog.Debugf("Connecting %s (%s) to the %s bridge: %s",
			dev.name, dev.hwaddr, ring, br)
		c := exec.Command(plat.BrctlCmd, "addif", br, dev.name)
		if out, rerr := c.CombinedOutput(); rerr != nil {
			err = fmt.Errorf(string(out))
		}
	} else {
		err = fmt.Errorf("non-existent ring %s", ring)
	}

	if err != nil {
		slog.Warnf("Failed to add %s: %v", dev.name, err)
	}
	return err
}

func rebuildInternalNet() {
	satNode := aputil.IsSatelliteMode()

	// For each internal network device, create a virtual device for each
	// LAN ring and attach it to the bridge for that ring
	for _, dev := range physDevices {
		if dev.disabled {
			continue
		}

		if dev.ring != base_def.RING_INTERNAL {
			continue
		}

		if !satNode {
			err := addDevToRingBridge(dev, base_def.RING_INTERNAL)
			if err != nil {
				continue
			}
		}
		for name, ring := range rings {
			if name != base_def.RING_INTERNAL {
				addVif(dev.name, ring.Vlan, ring.Bridge)
			}
		}
	}
}

func rebuildLan() {
	// Connect all the wired LAN NICs to ring-appropriate bridges.
	for _, dev := range physDevices {
		if !dev.disabled && dev.wifi == nil &&
			!plat.NicIsVirtual(dev.name) &&
			dev.ring != base_def.RING_INTERNAL &&
			dev.ring != base_def.RING_WAN {
			addDevToRingBridge(dev, dev.ring)
		}
	}
}

// If hostapd authorizes a client that isn't assigned to a VLAN, it gets
// connected to the physical wifi device rather than a virtual interface.
// Connect those physical devices to the UNENROLLED bridge once hostapd is
// running.  We don't have a good way to determine when hostapd has gotten far
// enough for this operation to succeed, so we just keep trying.
func rebuildUnenrolled(devs []*physDevice, interrupt chan bool) {
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

			_, err := net.InterfaceByName(dev.name)
			if err == nil {
				err = addDevToRingBridge(dev,
					base_def.RING_UNENROLLED)
			}
			if err != nil {
				bad = append(bad, dev)
			}
		}
		devs = bad
	}
}

// Create a virtual port for the given NIC / VLAN pair.  Attach the new virtual
// port to the bridge for the associated VLAN.
func addVif(nic string, vlan int, bridge string) {
	vid := strconv.Itoa(vlan)
	vif := nic + "." + vid

	deleteVif(vif)
	err := exec.Command(plat.VconfigCmd, "add", nic, vid).Run()
	if err != nil {
		slog.Warnf("Failed to create vif %s: %v", vif, err)
		return
	}

	err = exec.Command(plat.BrctlCmd, "addif", bridge, vif).Run()
	if err != nil {
		slog.Warnf("Failed to add %s to %s: %v", vif, bridge, err)
		return
	}

	err = exec.Command(plat.IPCmd, "link", "set", "up", vif).Run()
	if err != nil {
		slog.Warnf("Failed to enable %s: %v", vif, err)
	}
}

func deleteVif(vif string) {
	slog.Debugf("deleting nic %s", vif)
	exec.Command(plat.IPCmd, "link", "del", vif).Run()
}

func deleteBridge(bridge string) {
	slog.Debugf("deleting bridge %s", bridge)
	exec.Command(plat.IPCmd, "link", "set", "down", bridge).Run()
	exec.Command(plat.BrctlCmd, "delbr", bridge).Run()
}

// Delete the bridges associated with each ring.  This gets us back to a known
// ground state, simplifying the task of rebuilding everything when hostapd
// starts back up.
func deleteBridges() {
	for _, conf := range rings {
		deleteBridge(conf.Bridge)
	}
}

// Determine the address to be used for the given ring's router on this node.
// If the AP has an address of 192.168.131.x on the internal subnet, then the
// router for each ring will be the corresponding .x address in that ring's
// subnet.
func localRouter(ring *cfgapi.RingConfig) string {
	raw := ring.IPNet.IP.To4()
	raw[3] = networkNodeIdx
	return (net.IP(raw)).String()
}

//
// Prepare a ring's bridge: clean up any old state, assign a new address, set up
// routes, etc.
//
func createBridge(ringName string) {
	ring := rings[ringName]
	bridge := ring.Bridge

	slog.Infof("Preparing %s ring: %s %s", ringName, bridge, ring.Subnet)

	err := exec.Command(plat.BrctlCmd, "addbr", bridge).Run()
	if err != nil {
		slog.Warnf("addbr %s failed: %v", bridge, err)
		return
	}

	err = exec.Command(plat.IPCmd, "link", "set", "up", bridge).Run()
	if err != nil {
		slog.Warnf("bridge %s failed to come up: %v", bridge, err)
		return
	}

	// ip addr flush dev brvlan0
	cmd := exec.Command(plat.IPCmd, "addr", "flush", "dev", bridge)
	if err := cmd.Run(); err != nil {
		slog.Fatalf("Failed to remove existing IP address: %v", err)
	}

	// ip route del 192.168.136.0/24
	cmd = exec.Command(plat.IPCmd, "route", "del", ring.Subnet)
	cmd.Run()

	// ip addr add 192.168.136.1 dev brvlan0
	router := localRouter(ring)
	cmd = exec.Command(plat.IPCmd, "addr", "add", router, "dev", bridge)
	slog.Debugf("Setting %s to %s", bridge, router)
	if err := cmd.Run(); err != nil {
		slog.Fatalf("Failed to set the router address: %v", err)
	}

	// ip link set up brvlan0
	cmd = exec.Command(plat.IPCmd, "link", "set", "up", bridge)
	if err := cmd.Run(); err != nil {
		slog.Fatalf("Failed to enable bridge: %v", err)
	}
	// ip route add 192.168.136.0/24 dev brvlan0
	cmd = exec.Command(plat.IPCmd, "route", "add", ring.Subnet, "dev", bridge)
	if err := cmd.Run(); err != nil {
		slog.Fatalf("Failed to add %s as the new route: %v",
			ring.Subnet, err)
	}
}

func createBridges() {
	satNode := aputil.IsSatelliteMode()

	for ring := range rings {
		if satNode && ring == base_def.RING_INTERNAL {
			// Satellite nodes don't build an internal ring - they connect
			// to the primary node's internal ring using DHCP.
			continue
		}

		createBridge(ring)
	}
}

// Tear down all the bridges and interfaces we created and then build it all
// back up again.  We do this each time hostapd restarts, so we can be sure that
// the system is in a clean state where hostapd can create its virtual
// interfaces and add them to our bridges.
func resetInterfaces() {
	if err := sanityCheckSubnets(); err != nil {
		slog.Errorf("%v", err)
		mcpd.SetState(mcp.BROKEN)
		networkdStop("subnet sanity check failed")
		return
	}

	hotplugBlock()
	deleteBridges()
	createBridges()
	rebuildLan()
	rebuildInternalNet()
	hotplugUnblock()

	resource := &base_msg.EventNetUpdate{
		Timestamp: aputil.NowToProtobuf(),
		Sender:    proto.String(brokerd.Name),
		Debug:     proto.String("-"),
	}

	if err := brokerd.Publish(resource, base_def.TOPIC_UPDATE); err != nil {
		slog.Warnf("couldn't publish %s: %v", base_def.TOPIC_UPDATE, err)
	}
}

// Delete all virtual interfaces and bridges we find, getting the system into a
// clean state before we start building up our own infrastructure.
func networkCleanup() {
	devs, _ := ioutil.ReadDir("/sys/devices/virtual/net")
	slog.Debugf("deleting virtual NICs")
	for _, dev := range devs {
		name := dev.Name()

		if plat.NicIsVirtual(name) {
			deleteVif(name)
		}
	}

	slog.Debugf("deleting bridges")
	for _, dev := range devs {
		name := dev.Name()

		if strings.HasPrefix(name, "b") {
			deleteBridge(name)
		}
	}

	wan.dhcpRenew()
}
