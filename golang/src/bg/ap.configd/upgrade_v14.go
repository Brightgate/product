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

func upgradeV14() error {
	log.Printf("Adding @/network/ntpservers\n")

	prop, _ := propertyInsert("@/network/ntpservers/1")
	prop.Value = "time1.google.com"
	prop, _ = propertyInsert("@/network/ntpservers/2")
	prop.Value = "time2.google.com"
	prop, _ = propertyInsert("@/network/ntpservers/3")
	prop.Value = "time3.google.com"
	prop, _ = propertyInsert("@/network/ntpservers/4")
	prop.Value = "time4.google.com"

	return nil
}

func init() {
	addUpgradeHook(14, upgradeV14)
}
