/*
 * COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package main

import (
	"fmt"
	"net"

	"bg/base_def"
	"bg/common/cfgapi"
)

// This map was copied from cfgapi, because we don't want it to become part of
// the public interface.
var ringToSubnetIdx = map[string]int{
	base_def.RING_INTERNAL:   0,
	base_def.RING_UNENROLLED: 1,
	base_def.RING_CORE:       2,
	base_def.RING_STANDARD:   3,
	base_def.RING_DEVICES:    4,
	base_def.RING_GUEST:      5,
	base_def.RING_QUARANTINE: 6,
}

func upgradeV18() error {
	const ringWidth = 8

	propTree.Add("@/site_index", "0", nil)

	// By default, the unenrolled subnet occupies the lowest portion of the
	// ip space allocated by an appliance, so we use that as a starting
	// point when calculating the new ip allocations.
	slog.Infof("Determining new subnet base address")
	baseSubnet := "192.168.2.0/24"
	prop, err := propTree.GetNode("@/rings/unenrolled/subnet")
	if err == nil {
		n := prop.Value
		slog.Infof("prop: %s", n)
		if _, _, err := net.ParseCIDR(n); err != nil {
			slog.Warnf("unenrolled subnet is invalid %s: %v", n, err)
		} else {
			baseSubnet = n
		}
	}
	base, _, _ := net.ParseCIDR(baseSubnet)
	newBase := fmt.Sprintf("%v/%d", base, 32-ringWidth)
	propTree.Add("@/network/base_address", newBase, nil)

	// Remove all of the old per-ring subnet properties
	slog.Infof("Looking for old ring/subnet properties")
	deleteProps := make([]string, 0)
	for ring := range cfgapi.ValidRings {
		subnetProp := "@/rings/" + ring + "/subnet"
		deleteProps = append(deleteProps, subnetProp)
	}

	// Calculate the new subnets for each ring
	subnets := make(map[string]*net.IPNet)
	for ring := range cfgapi.ValidRings {
		s, _ := cfgapi.GenSubnet(newBase, 0, ringToSubnetIdx[ring])
		_, ipnet, _ := net.ParseCIDR(s)
		subnets[ring] = ipnet
	}

	// iterate over clients, delete any ip address that doesn't fit in its
	// assigned ring's new subnet
	slog.Info("Looking for leases that are invalid under new subnetting")
	clients, _ := propTree.GetNode("@/clients")
	for client, info := range clients.Children {
		var ipv4 net.IP
		var subnet *net.IPNet

		if prop := info.Children["ipv4"]; prop != nil {
			ipv4 = net.ParseIP(prop.Value)
		}
		if prop := info.Children["ring"]; prop != nil {
			subnet = subnets[prop.Value]
		}

		if ipv4 != nil && (subnet == nil || !subnet.Contains(ipv4)) {
			slog.Infof("client %s address %v not in ring subnet: %v",
				client, ipv4, *subnet)
			addrProp := "@/clients/" + client + "/ipv4"
			deleteProps = append(deleteProps, addrProp)
		}

	}

	slog.Info("Removing obsoleted ring/subnet properties")
	for _, prop := range deleteProps {
		propTree.Delete(prop)
	}

	return nil
}

func init() {
	addUpgradeHook(18, upgradeV18)
}
