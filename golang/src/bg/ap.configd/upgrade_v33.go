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

import (
	"bg/base_def"
)

func upgradeV33() error {
	clients, _ := propTree.GetNode("@/clients")
	if clients == nil {
		return nil
	}

	for mac, c := range clients.Children {
		if ring, ok := c.Children["ring"]; ok {
			if ring.Value == base_def.RING_INTERNAL {
				slog.Infof("preserving %s ring assignment", mac)
				path := "@/clients/" + mac + "/home"
				propTree.Add(path, base_def.RING_INTERNAL, nil)
			}
		}
	}

	return nil
}

func init() {
	addUpgradeHook(33, upgradeV33)
}
