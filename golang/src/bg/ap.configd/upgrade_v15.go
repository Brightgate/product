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

const bkt = "https://storage.googleapis.com/bg-blocklist-a198e4a0-5823-4d16-8950-ad34b32ace1c"

func upgradeV15() error {
	log.Printf("Adding @/cloud/update/bucket")

	propTree.Add("@/cloud/update/bucket", bkt, nil)
	return nil
}

func init() {
	addUpgradeHook(15, upgradeV15)
}
