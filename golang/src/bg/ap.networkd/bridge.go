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
	"strconv"
	"strings"
	"time"

	"bg/ap_common/aputil"
	"bg/ap_common/mcp"
	"bg/ap_common/netctl"
	"bg/base_def"
	"bg/base_msg"
	"bg/common/cfgapi"

	"github.com/golang/protobuf/proto"
)

func addDevToRingBridge(dev *physDevice, ring string) error {
	var err error

	if err = netctl.LinkUp(dev.name); err != nil {
		slog.Warnf("Failed to enable %s: %v", dev.name, err)
	}

	if config := rings[ring]; config != nil {
		br := config.Bridge
		slog.Debugf("Connecting %s (%s) to the %s bridge: %s",
			dev.name, dev.hwaddr, ring, br)
		if err = netctl.BridgeAddIface(br, dev.name); err != nil {
			err = fmt.Errorf("adding %s to %s: %v", dev.name, br, err)
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

	slog.Debugf("rebuilding internal network")
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
			if !cfgapi.SystemRings[name] {
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
	slog.Debugf("adding nic %s to %s", vif, bridge)

	err := netctl.LinkDelete(vif)
	if err != nil && err != netctl.ErrNoDevice {
		slog.Warnf("LinkDelete(%s) failed: %v", vif, err)
	}

	if err = netctl.VlanAdd(nic, vlan); err != nil {
		slog.Warnf("Failed to create vif %s: %v", vif, err)

	} else if err = netctl.BridgeAddIface(bridge, vif); err != nil {
		slog.Warnf("Failed to add %s to %s: %v", vif, bridge, err)

	} else if err = netctl.LinkUp(vif); err != nil {
		slog.Warnf("Failed to enable %s: %v", vif, err)
	}
}

func deleteBridge(bridge string) {
	slog.Debugf("deleting bridge %s", bridge)
	err := netctl.LinkDown(bridge)
	if err != nil && err != netctl.ErrNoDevice {
		slog.Warnf("LinkDown(%s) failed: %v", bridge, err)
	}

	err = netctl.BridgeDestroy(bridge)
	if err != nil && err != netctl.ErrNoDevice {
		slog.Warnf("BridgeDestroy(%s) failed: %v", bridge, err)
	}
}

// Delete the bridges associated with each ring.  This gets us back to a known
// ground state, simplifying the task of rebuilding everything when hostapd
// starts back up.
func deleteBridges() {
	for _, conf := range rings {
		if conf.Bridge != "" {
			deleteBridge(conf.Bridge)
		}
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

func createBridge(ringName string) {
	ring := rings[ringName]
	bridge := ring.Bridge

	slog.Infof("Creating %s ring: %s %s", ringName, bridge, ring.Subnet)

	if err := netctl.BridgeCreate(bridge); err != nil {
		slog.Warnf("addbr %s failed: %v", bridge, err)
		return
	}

	if err := netctl.LinkUp(bridge); err != nil {
		slog.Warnf("bridge %s failed to come up: %v", bridge, err)
		return
	}
}

// Prepare a ring's bridge: clean up any old state, assign a new address, set up
// routes, etc.
func plumbBridge(ring *cfgapi.RingConfig, iface string) {
	if err := netctl.AddrFlush(iface); err != nil {
		slog.Warnf("flushing old addresses from %s: %v", iface, err)
	}

	if err := netctl.RouteDel(ring.Subnet); err != nil {
		slog.Warnf("deleting route: %v", err)
	}

	slog.Infof("setting %s to %s", iface, localRouter(ring))
	if err := netctl.AddrAdd(iface, localRouter(ring)); err != nil {
		slog.Fatalf("Failed to set the router address: %v", err)
	}

	if err := netctl.LinkUp(iface); err != nil {
		slog.Fatalf("Failed to enable iface: %v", err)
	}

	if err := netctl.RouteAdd(ring.Subnet, iface); err != nil {
		slog.Fatalf("Failed to add %s as route: %v", ring.Subnet, err)
	}
}

func createBridges() {
	satNode := aputil.IsSatelliteMode()

	for name, ring := range rings {
		// Satellite nodes don't build an internal ring - they connect
		// to the primary node's internal ring using DHCP.
		if satNode && name == base_def.RING_INTERNAL {
			continue
		}

		// The VPN ring doesn't live on a bridge.  Its packets get
		// routed.
		if name == base_def.RING_VPN {
			continue
		}

		createBridge(name)
		plumbBridge(ring, ring.Bridge)
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

	start := time.Now()
	deleteBridges()
	createBridges()
	rebuildLan()
	rebuildInternalNet()
	slog.Debugf("network rebuild took %v", time.Since(start))

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
			err := netctl.LinkDelete(name)
			if err != nil && err != netctl.ErrNoDevice {
				slog.Warnf("LinkDelete(%s) failed: %v",
					name, err)
			}
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
