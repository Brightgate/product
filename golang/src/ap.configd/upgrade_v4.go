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

//
// Remove any existing device identities, as we switch from using unstructured
// text to integral DeviceIDs

func upgradeV4() error {
	clients := propertySearch("@/clients")
	if clients == nil {
		log.Printf("V3 config file missing @/clients")
		return nil
	}

	for _, client := range clients.Children {
		for _, property := range client.Children {
			if property.Name == "identity" {
				log.Printf("Removing identity '%s' from %s\n",
					property.Value, client.Name)
				deleteChild(client, property)
				break
			}
		}
	}
	return nil
}

func init() {
	addUpgradeHook(4, upgradeV4)
}
