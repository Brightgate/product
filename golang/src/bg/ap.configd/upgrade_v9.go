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

	"bg/base_def"
)

func upgradeV9() error {
	if p := propertySearch("@/network/default_ring"); p == nil {
		log.Printf("Adding default ring\n")
		pnode := propertySearch("@/network")
		if pnode == nil {
			log.Fatalf("config file missing @/network")
		}

		new := propertyAdd(pnode, "default_ring")
		new.Value = base_def.RING_UNENROLLED
	}
	log.Printf("Deleting setup ring\n")
	propertyDelete("@/network/setup_ssid")
	propertyDelete("@/rings/setup")

	return nil
}

func init() {
	addUpgradeHook(9, upgradeV9)
}
