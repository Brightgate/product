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

//
// Add rules allowing ssh access from clients and the WAN.  The client rule is
// enabled, the WAN rule is disabled.
//
func upgradeV10() error {
	log.Printf("Adding ssh firewall rules\n")

	prop := propertyInsert("@/firewall/rules/ssh-internal/active")
	prop.Value = "true"
	prop = propertyInsert("@/firewall/rules/ssh-internal/rule")
	prop.Value = "ACCEPT TCP FROM IFACE NOT wan TO AP DPORTS 22"

	prop = propertyInsert("@/firewall/rules/ssh-external/active")
	prop.Value = "false"
	prop = propertyInsert("@/firewall/rules/ssh-external/rule")
	prop.Value = "ACCEPT TCP FROM IFACE wan TO AP DPORTS 22"

	log.Printf("Renaming @/firewall/active to @/firewall/blocked\n")
	prop = propertySearch("@/firewall/active")
	if prop != nil {
		prop.Name = "blocked"
	}

	return nil
}

func init() {
	addUpgradeHook(10, upgradeV10)
}
