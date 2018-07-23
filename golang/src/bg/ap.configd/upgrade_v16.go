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

func repatchTree(indent string, node *pnode, path string) {
	oldPath := node.path

	node.path = path + "/" + node.name
	log.Printf("%smoving %s to %s\n", indent, oldPath, node.path)

	for _, child := range node.Children {
		repatchTree(indent+"   ", child, node.path)
	}
}

func upgradeV16() error {
	log.Printf("Moving @/nodes/<node>/<nic> -> @/nodes/<node>/nics/<nic>")

	if nodes := propertySearch("@/nodes"); nodes != nil {
		for uuid, node := range nodes.Children {
			if len(node.Children) == 0 {
				continue
			}

			nicsPath := "@/nodes/" + uuid + "/nics"
			nicsRoot := propertySearch(nicsPath)
			if nicsRoot == nil {
				nicsRoot, _ = propertyInsert(nicsPath)
			}
			if nicsRoot.Children == nil {
				nicsRoot.Children = make(map[string]*pnode)
			}

			for id, nic := range node.Children {
				if id != "platform" && id != "nics" {
					nicsRoot.Children[id] = nic
					nic.parent = nicsRoot
					repatchTree("   ", nic, nicsPath)
					delete(node.Children, id)
				}
			}
		}
	}
	return nil
}

func init() {
	addUpgradeHook(16, upgradeV16)
}
