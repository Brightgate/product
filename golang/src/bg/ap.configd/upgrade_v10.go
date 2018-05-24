/*
 * COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package main

import (
	"log"
)

type rules struct {
	name   string
	rule   string
	active string
}

var newRules = []rules{
	{
		name:   "ssh-internal",
		rule:   "ACCEPT TCP FROM IFACE NOT wan TO AP DPORTS 22",
		active: "false",
	},
	{
		name:   "ssh-external",
		rule:   "ACCEPT TCP FROM IFACE wan TO AP DPORTS 22",
		active: "true",
	},
}

//
// Add rules allowing ssh access from clients and the WAN.  The client rule is
// enabled, the WAN rule is disabled.
//
func upgradeV10() error {
	log.Printf("Adding ssh firewall rules\n")

	for _, r := range newRules {
		base := "@/firewall/rules/" + r.name
		if prop, _ := propertyInsert(base + "rule"); prop != nil {
			prop.Value = r.rule
		}
		if prop, _ := propertyInsert(base + "active"); prop != nil {
			prop.Value = r.active
		}
	}

	log.Printf("Renaming @/firewall/active to @/firewall/blocked\n")
	if firewall := propertySearch("@/firewall"); firewall != nil {
		if active, ok := firewall.Children["active"]; ok {
			delete(firewall.Children, "active")
			firewall.Children["blocked"] = active
			active.name = "blocked"
		}
	}

	return nil
}

func init() {
	addUpgradeHook(10, upgradeV10)
}
