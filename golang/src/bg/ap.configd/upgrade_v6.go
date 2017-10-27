/*
 * COPYRIGHT 2017 Brightgate Inc.  All rights reserved.
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
	"log"
)

var ringToVlan = map[string]string{
	"setup":      "-1",
	"unenrolled": "0",
	"core":       "3",
	"standard":   "4",
	"devices":    "5",
	"guest":      "6",
	"quarantine": "7",
}

func upgradeV6() error {
	log.Printf("Getting per-ring subnets from @/interfaces\n")
	subnets := make(map[string]string)
	for ring := range ringToVlan {
		ifaceProp := "@/rings/" + ring + "/interface"
		iface := propertySearch(ifaceProp)
		if iface == nil {
			return fmt.Errorf("ring %s has no interface", ring)
		}
		subnet := propertySearch("@/interfaces/" + iface.Value + "/subnet")
		if subnet == nil {
			return fmt.Errorf("interface %s has no subnet",
				iface.Value)
		}
		subnets[ring] = subnet.Value
		propertyDelete(ifaceProp)
	}

	log.Printf("Adding VLANs and subnets to @/rings\n")
	for ring, vlan := range ringToVlan {
		pnode := propertySearch("@/rings/" + ring)
		new := propertyAdd(pnode, "subnet")
		new.Value = subnets[ring]
		new = propertyAdd(pnode, "vlan")
		new.Value = vlan
	}

	log.Printf("Moving all 'wired' clients to 'standard'\n")
	pnode := propertySearch("@/clients")
	for _, client := range pnode.Children {
		ring := childSearch(client, "ring")
		if ring != nil && ring.Value == "wired" {
			log.Printf("  moving %s to 'standard'\n", client.Name)
			ring.Value = "standard"
		}
	}

	log.Printf("Deleting obsolete properties\n")
	propertyDelete("@/interfaces")
	propertyDelete("@/network/wired_nic")
	propertyDelete("@/network/wifi_nic")
	propertyDelete("@/network/wifi")
	propertyDelete("@/rings/wired")

	return nil
}

func init() {
	addUpgradeHook(6, upgradeV6)
}
