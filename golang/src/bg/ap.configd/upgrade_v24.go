/*
 * COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package main

import (
	"bg/common/cfgtree"
	"bg/common/wifi"
)

func rename(parent *cfgtree.PNode, oldProp, newProp string) {
	oldNode := parent.Children[oldProp]
	if oldNode != nil {
		newPath := parent.Path() + "/" + newProp
		oldPath := parent.Path() + "/" + oldProp
		propTree.Add(newPath, oldNode.Value, nil)
		propTree.Delete(oldPath)
	}
}

// Rename per-nic properties:
// @/nodes/%nodeid%/nics/%nic%/band - > @/nodes/%nodeid%/nics/%nic%/cfg_band
// @/nodes/%nodeid%/nics/%nic%/channel - > @/nodes/%nodeid%/nics/%nic%/cfg_channel
// @/nodes/%nodeid%/nics/%nic%/width - > @/nodes/%nodeid%/nics/%nic%/cfg_width
// @/nodes/%nodeid%/nics/%nic%/disabled - > @/nodes/%nodeid%/nics/%nic%/state
//
// Remove obsolete global property:
// @/network/%wifiband% deleted

func upgradeV24() error {
	nodes, _ := propTree.GetNode("@/nodes")
	if nodes == nil {
		return nil
	}

	propTree.Delete("@/network/" + wifi.LoBand)
	propTree.Delete("@/network/" + wifi.HiBand)

	for _, node := range nodes.Children {
		if nics := getChild(node, "nics"); nics != nil {
			for _, nic := range nics.Children {
				if nic.Children == nil {
					continue
				}

				p := nic.Children["pseudo"]
				if p != nil && p.Value == "true" {
					continue
				}

				state := wifi.DevOK
				p = nic.Children["disabled"]
				if p != nil {
					if p.Value == "true" {
						state = wifi.DevDisabled
					}
					propTree.Delete(nic.Path() + "/disabled")
				}
				propTree.Add(nic.Path()+"/state", state, nil)

				rename(nic, "band", "cfg_band")
				rename(nic, "channel", "cfg_channel")
				rename(nic, "width", "cfg_width")
			}
		}
	}
	return nil
}

func init() {
	addUpgradeHook(24, upgradeV24)
}
