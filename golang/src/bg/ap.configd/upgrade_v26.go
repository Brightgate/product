/*
 * COPYRIGHT 2020 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package main

func addOne(prop, val string) {
	path := "@/rings/vpn/" + prop
	if err := propTree.Add(path, val, nil); err != nil {
		slog.Errorf("adding %s: %v", path, err)
	}
}

func upgradeV26() error {
	addOne("lease_duration", "0")
	addOne("vap", "")
	addOne("vlan", "-1")

	return nil
}

func init() {
	addUpgradeHook(26, upgradeV26)
}
