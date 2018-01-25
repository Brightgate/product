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

var ringToAuth = map[string]string{
	"setup":      "open",
	"internal":   "open",
	"unenrolled": "wpa-psk",
	"core":       "wpa-eap",
	"standard":   "wpa-psk",
	"devices":    "wpa-psk",
	"guest":      "wpa-psk",
	"quarantine": "wpa-psk",
}

func upgradeV8() error {
	log.Printf("Adding auth to @/rings\n")
	for ring, mgmt := range ringToAuth {
		if pnode := propertySearch("@/rings/" + ring); pnode != nil {
			new := propertyAdd(pnode, "auth")
			new.Value = mgmt
		} else {
			log.Printf("ring '%s' missing from config tree\n", ring)
		}
	}

	return nil
}

func init() {
	addUpgradeHook(8, upgradeV8)
}
