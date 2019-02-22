/*
 * COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 19 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package main

func upgradeV19() error {
	slog.Info("Adding config properties for guest AP")
	propTree.Add("@/network/vap/guest/ssid", "setme-guest", nil)
	propTree.Add("@/network/vap/guest/5ghz", "false", nil)
	propTree.Add("@/network/vap/guest/keymgmt", "wpa-psk", nil)
	propTree.Add("@/network/vap/guest/passphrase", "sosecretive", nil)

	// If there are any clients currently configured on the guest ring, we
	// can't move it to a new ssid automatically.
	clients, _ := propTree.GetNode("@/clients")
	guestUsed := 0
	for _, info := range clients.Children {
		prop := info.Children["ring"]
		if prop != nil && prop.Value == "guest" {
			guestUsed++
		}
	}
	if guestUsed == 0 {
		slog.Info("moving the guest ring to the guest vap")
		propTree.Add("@/network/vap/guest/default_ring", "guest", nil)
		propTree.Add("@/network/vap/eap/default_ring", "standard", nil)
		propTree.Add("@/rings/guest/vap", "guest", nil)
	} else {
		slog.Info("guest ring has %d clients configured - "+
			"leaving it in place", guestUsed)
	}

	return nil
}

func init() {
	addUpgradeHook(19, upgradeV19)
}
