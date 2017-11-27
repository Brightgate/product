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

const newSubnet = "192.168.130.0/28"

func upgradeV7() error {
	log.Printf("Adding 'internal' ring.  Using subnet %s.\n", newSubnet)
	rings := propertySearch("@/rings")
	internal := propertyAdd(rings, "internal")
	node := propertyAdd(internal, "vlan")
	node.Value = "1"
	node = propertyAdd(internal, "subnet")
	node.Value = newSubnet
	node = propertyAdd(internal, "lease_duration")
	node.Value = "1440"

	log.Printf("Deleting obsolete properties\n")
	propertyDelete("@/network/setup_nic")
	propertyDelete("@/network/wan_nic")

	return nil
}

func init() {
	addUpgradeHook(7, upgradeV7)
}
