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

	"bg/common/cfgtree"
	"bg/common/network"
)

func checkUUID(prop, uuid string) error {
	const nullUUID = "00000000-0000-0000-0000-000000000000"

	node, _ := propTree.GetNode(prop)
	if node != nil && node.Value != nullUUID {
		return fmt.Errorf("cannot change an appliance's UUID")
	}
	return nil
}

//
// Check to see whether the given hostname is already inuse as either a device's
// dns_name or as the left hand side of a cname.  We can optionally indicate a
// device to ignore, allowing us to answer the question "is any other device
// using this hostname?"
//
func dnsNameInuse(ignore *cfgtree.PNode, hostname string) bool {
	for _, device := range propTree.GetChildren("@/clients") {
		if device == ignore {
			continue
		}

		dns := getChild(device, "dns_name")
		friendly := getChild(device, "friendly_dns")
		if strings.EqualFold(dns, hostname) ||
			strings.EqualFold(friendly, hostname) {
			return true
		}
	}

	for name, record := range propTree.GetChildren("@/dns/cnames") {
		if record != ignore && strings.EqualFold(name, hostname) {
			return true
		}
	}

	return false
}

// We only allow setting a static WAN address on platforms with the underlying
// infrastructure to support it
func checkWan(prop, val string) error {
	var err error

	if !plat.NetworkManaged {
		err = fmt.Errorf("static wan addresses not supported " +
			"on this platform")
	}

	return err
}

// Validate the hostname that will be used to generate DNS A records
// for this device
func checkDNS(prop, hostname string) error {
	var parent *cfgtree.PNode
	var err error

	if node, _ := propTree.GetNode(prop); node != nil {
		parent = node.Parent()
	}

	dnsProp := strings.HasSuffix(prop, "dns_name")

	if !network.ValidDNSLabel(hostname) {
		err = fmt.Errorf("invalid hostname: %s", hostname)
	} else if dnsProp && dnsNameInuse(parent, hostname) {
		err = fmt.Errorf("duplicate hostname")
	}

	return err
}

// Validate both the hostname and the canonical name that will be
// used to generate DNS CNAME records
func checkCname(prop, hostname string) error {
	var err error
	var cname string

	// The validation code and the regexp that got us here should guarantee
	// that the structure of the path is @/dns/cnames/<hostname>
	path := strings.Split(prop, "/")
	if len(path) != 4 {
		err = fmt.Errorf("invalid property path: %s", prop)
	} else {
		cname = path[3]

		if !network.ValidHostname(cname) {
			err = fmt.Errorf("invalid hostname: %s", cname)
		} else if !network.ValidHostname(hostname) {
			err = fmt.Errorf("invalid canonical name: %s", hostname)
		} else if dnsNameInuse(nil, cname) {
			err = fmt.Errorf("duplicate hostname")
		}
	}

	return err
}

// Build the set of per-ring subnets that would result from this property change
func proposedSubnets(prop, val string) (map[string]*net.IPNet, error) {
	const basePath = "@/network/base_address"
	const sitePath = "@/site_index"
	var addr, idx string
	var err error

	if prop == basePath {
		addr = val
	} else if p, err := propTree.GetProp(basePath); err == nil {
		addr = p
	} else {
		return nil, fmt.Errorf("missing %s", basePath)
	}
	if _, _, err := net.ParseCIDR(addr); err != nil {
		return nil, err
	}

	if prop == sitePath {
		idx = val
	} else if p, err := propTree.GetProp(sitePath); err == nil {
		idx = p
	} else {
		return nil, fmt.Errorf("missing %s", sitePath)
	}
	idxInt, err := strconv.Atoi(idx)
	if err != nil {
		return nil, err
	}

	// Calculate the default subnets from base_address and site_index
	si := &subnetInfo{
		siteIndex:   idxInt,
		baseAddress: addr,
	}
	recalculateRingSubnets(si)

	// If the property being changed is a per-ring subnet, that gets
	// inserted now.  (@/rings/<ring>/subnet)
	f := strings.Split(prop, "/")
	if len(f) == 4 && f[1] == "rings" && f[3] == "subnet" {
		ring := f[2]
		_, si.perRing[ring], _ = net.ParseCIDR(val)
	}

	return si.perRing, nil
}

// Validate that this subnet-related change will allow us to generate legal
// subnet addresses and will not violate any client's existing static IP
// assignment.
func checkSubnet(prop, val string) error {
	errors := make([]string, 0)

	subnets, err := proposedSubnets(prop, val)
	if err != nil {
		return err
	}

	for r1, s1 := range subnets {
		if s1 == nil {
			errors = append(errors, r1+" no subnet")
			continue
		}

		for r2, s2 := range subnets {
			if r1 == r2 || s2 == nil {
				continue
			}
			if s2.Contains(s1.IP) || s1.Contains(s2.IP) {
				errors = append(errors,
					r1+" and "+r2+" overlap")
			}
		}
	}

	// Make sure we haven't moved a subnet with static client assignments
	clients := propTree.GetChildren("@/clients")
	for mac, client := range clients {
		var ip net.IP
		var subnet *net.IPNet

		if x, ok := client.Children["ipv4"]; ok && x.Expires == nil {
			ip = net.ParseIP(x.Value)
		}

		if x, ok := client.Children["ring"]; ok {
			subnet = subnets[x.Value]
		}

		if ip != nil && subnet != nil && !subnet.Contains(ip) {
			errors = append(errors, mac+" lost subnet")
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("invalid %s (%s): %s",
			prop, val, strings.Join(errors, ","))

	}
	return nil
}

// Validate an ipv4 assignment for this device
func checkIPv4(prop, addr string) error {
	var updateMac string

	ipv4 := net.ParseIP(addr)
	if ipv4 == nil {
		return fmt.Errorf("invalid address: %s", addr)
	}
	if path := strings.Split(prop, "/"); len(path) > 3 {
		updateMac = path[2]
	} else {
		// We should only get to this routine if the property path
		// passes the above test.
		return fmt.Errorf("internal error")
	}

	// Verify that the new IP address is within the ring to which the client
	// is assigned.
	ring, _ := propTree.GetProp("@/clients/" + updateMac + "/ring")
	if ring == "" {
		return fmt.Errorf("client not assigned to a ring")
	}

	subnet := ringSubnets.perRing[ring]
	if subnet == nil {
		return fmt.Errorf("no subnet defined for %s ring", ring)
	} else if !subnet.Contains(ipv4) {
		return fmt.Errorf("address outside of %s ring's subnet (%s)",
			ring, subnet)
	}

	// Make sure the address isn't already assigned
	clients := propTree.GetChildren("@/clients")
	for mac, device := range clients {
		if updateMac == mac {
			// Reassigning the device's address to itself is fine
			continue
		}

		if addr := getChild(device, "ipv4"); addr != "" {
			if ipv4.Equal(net.ParseIP(addr)) {
				return fmt.Errorf("%s in use by %s", addr, mac)
			}
		}
	}

	return nil
}

