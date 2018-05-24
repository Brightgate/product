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

func upgradeV11() error {
	log.Printf("Adding @/users\n")
	propertyInsert("@/users")

	return nil
}

func init() {
	addUpgradeHook(11, upgradeV11)
}
