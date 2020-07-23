/*
 * COPYRIGHT 2020 Brightgate Inc.  All rights reserved.
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
	"strconv"
	"strings"

	"bg/common/cfgapi"
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

// Validate that a given site_index and base_address will allow us to generate
// legal subnet addresses
func checkSubnet(prop, val string) error {
	const basePath = "@/network/base_address"
	const sitePath = "@/site_index"
	var baseProp, siteProp string

	if prop == basePath {
		baseProp = val
	} else if p, err := propTree.GetProp(basePath); err == nil {
		baseProp = p
	} else {
		baseProp = "192.168.0.2/24"
	}

	if prop == sitePath {
		siteProp = val
	} else if p, err := propTree.GetProp(sitePath); err == nil {
		siteProp = p
	} else {
		siteProp = "0"
	}
	siteIdx, err := strconv.Atoi(siteProp)
	if err != nil {
		return fmt.Errorf("invalid %s: %v", sitePath, err)
	}

	// Make sure the base network address generates a valid subnet for both
	// the lowest and highest subnet indices.
	_, err = cfgapi.GenSubnet(baseProp, siteIdx, 0)
	if err != nil {
		err = fmt.Errorf("invalid %s: %v", prop, err)
	} else {
		_, err = cfgapi.GenSubnet(baseProp, siteIdx, cfgapi.MaxRings-1)
		if err != nil {
			err = fmt.Errorf("invalid %s for max subnet: %v",
				prop, err)
		}
	}

	return err
}

// Validate an ipv4 assignment for this device
func checkIPv4(prop, addr string) error {
	var updating string

	ipv4 := net.ParseIP(addr)
	if ipv4 == nil {
		return fmt.Errorf("invalid address: %s", addr)
	}

	// Make sure the address isn't already assigned
	clients, _ := propTree.GetNode("@/clients")
	if clients == nil {
		return nil
	}

	if path := strings.Split(prop, "/"); len(path) > 3 {
		updating = path[2]
	}

	for name, device := range clients.Children {
		if updating == name {
			// Reassigning the device's address to itself is fine
			continue
		}

		if addr := getChild(device, "ipv4"); addr != "" {
			if ipv4.Equal(net.ParseIP(addr)) {
				return fmt.Errorf("%s in use by %s", addr, name)
			}
		}
	}

	return nil
}
