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
	"log"
)

func upgradeV5() error {
	log.Printf("Removing obsolete @/dhcp subtree\n")
	propertyDelete("@/dhcp")
	log.Printf("Removing obsolete @/network/domainname property\n")
	propertyDelete("@/network/domainname")
	log.Printf("Adding @/siteid\n")
	propertyUpdate("@/siteid", "7410", nil, true)

	return nil
}

func init() {
	addUpgradeHook(5, upgradeV5)
}
