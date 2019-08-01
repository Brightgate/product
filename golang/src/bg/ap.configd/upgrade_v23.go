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
	"fmt"

	"bg/base_def"
	"bg/common/cfgtree"
)

func getChild(root *cfgtree.PNode, name string) *cfgtree.PNode {
	if root != nil && root.Children != nil {
		return root.Children[name]
	}

	return nil
}

// On Mediatek boards we are now using the serial number as a nodeID rather than
// a self-generated UUID.  If we find a node in the config tree with a UUID as a
// name and nic configured in the WAN ring, assume that that is the config data
// for this node using the old name.  Move that subtree from @/nodes/<uuid> to
// @/nodes/<serial#>.

func upgradeV23() error {
	var gw *cfgtree.PNode

	nodeID, err := plat.GetNodeID()
	if err != nil {
		return fmt.Errorf("getting nodeid: %v", err)
	}
	if nodeID == "" {
		return fmt.Errorf("no nodeID found")
	}

	nodes, _ := propTree.GetNode("@/nodes")
	if nodes == nil {
		return nil
	}

	for name, node := range nodes.Children {
		var isGateway bool

		if nodeID == name {
			// This node is already in the tree
			gw = node
			break
		}

		if validateUUID(name) != nil {
			continue
		}

		if nics := getChild(node, "nics"); nics != nil {
			for _, nic := range nics.Children {
				ring := getChild(nic, "ring")
				if ring != nil && ring.Value == base_def.RING_WAN {
					isGateway = true
				}
			}
		}

		if isGateway {
			if gw == nil {
				gw = node
			} else {
				slog.Warnf("found multiple gw nodes - not upgrading")
				return nil
			}
		}
	}

	if gw != nil {
		oldPath := gw.Path()
		newPath := "@/nodes/" + nodeID

		if oldPath != newPath {
			err = gw.Move(newPath)
			if err == nil {
				slog.Infof("moved %s to %s\n", oldPath, newPath)
			} else {
				slog.Infof("failed to move %s to %s: %v",
					oldPath, newPath, err)
			}
		}

		if getChild(gw, "name") == nil {
			propTree.Add(newPath+"/name", "gateway", nil)
		}
	}
	return err
}

func init() {
	addUpgradeHook(23, upgradeV23)
}
