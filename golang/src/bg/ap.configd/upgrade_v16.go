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

func upgradeV16() error {
	slog.Infof("Moving @/nodes/<node>/<nic> -> @/nodes/<node>/nics/<nic>")

	if nodes, _ := propTree.GetNode("@/nodes"); nodes != nil {
		for uuid, node := range nodes.Children {
			if len(node.Children) == 0 {
				continue
			}

			nicsPath := "@/nodes/" + uuid + "/nics"
			for id, nic := range node.Children {
				if id != "platform" && id != "nics" {
					newPath := nicsPath + "/" + id
					nic.Move(newPath)
				}
			}
		}
	}
	return nil
}

func init() {
	addUpgradeHook(16, upgradeV16)
}
