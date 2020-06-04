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

func upgradeV31() error {
	all, _ := propTree.GetNode("@/policy/site/network/forward/")
	if all == nil {
		return nil
	}

	for _, protocol := range all.Children {
		for _, port := range protocol.Children {
			oldPath := port.Path()
			if port.Value == "" {
				slog.Infof("removing empty forward: %s",
					oldPath)
				propTree.Delete(oldPath)
			} else {
				newPath := oldPath + "/tgt"

				slog.Infof("moving %s from  %s to %s",
					port.Value, oldPath, newPath)
				propTree.Delete(oldPath)
				propTree.Add(newPath, port.Value, port.Expires)
			}
		}
	}

	return nil
}

func init() {
	addUpgradeHook(31, upgradeV31)
}
