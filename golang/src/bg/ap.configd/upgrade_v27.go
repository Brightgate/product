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

func upgradeV27() error {
	name := "smb-egress"
	rule := "BLOCK TCP TO IFACE wan DPORTS 445"
	base := "@/firewall/rules/" + name

	if err := propTree.Add(base+"/rule", rule, nil); err != nil {
		slog.Errorf("adding firewall rule %s: %v", name, err)
	} else if err := propTree.Add(base+"/active", "true", nil); err != nil {
		slog.Errorf("activiating firewall rule %s: %v", name, err)
	}

	return nil
}

func init() {
	addUpgradeHook(27, upgradeV27)
}
