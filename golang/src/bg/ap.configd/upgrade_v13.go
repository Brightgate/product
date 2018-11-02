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
	"bg/base_def"
)

//
// Add default rings for psk and eap networks.  If we already have a default
// ring defined, that gets migrated to the default for the appropriate
// authentication type.  Otherwise, the default will be the least trusted ring
// for that auth type.
func upgradeV13() error {
	trustOrder := []string{
		base_def.RING_CORE,
		base_def.RING_STANDARD,
		base_def.RING_DEVICES,
		base_def.RING_GUEST,
		base_def.RING_UNENROLLED,
	}

	ringToAuth := make(map[string]string)
	if rings, _ := propTree.GetNode("@/rings"); rings != nil {
		for name, config := range rings.Children {
			if anode, ok := config.Children["auth"]; ok {
				ringToAuth[name] = anode.Value
			}
		}
	} else {
		slog.Infof("@/rings missing")
	}

	// For each authentication type, identify the least trusted ring that
	// can be used with that type.
	authTypeToDefaultRing := make(map[string]string)
	for _, r := range trustOrder {
		if authType, ok := ringToAuth[r]; ok {
			authTypeToDefaultRing[authType] = r
		}
	}

	// If there is an existing default set, preserve it for the appropriate
	// auth type.
	if node, _ := propTree.GetNode("@/network/default_ring"); node != nil {
		authType := ringToAuth[node.Value]
		if authType != "" {
			authTypeToDefaultRing[authType] = node.Value
		}
		node.Value = ""
	}

	// Any client plugged into the 'internal' network will be treated as
	// UNENROLLED until the admin explicitly indicates that it should be
	// trusted as a satellite node.
	authTypeToDefaultRing["open"] = base_def.RING_UNENROLLED

	for a, r := range authTypeToDefaultRing {
		node, _ := propTree.GetNode("@/network/default_ring/" + a)
		if node != nil {
			node.Value = r
			slog.Infof("Set %s default ring to %s", a, r)
		}
	}

	return nil
}

func init() {
	addUpgradeHook(13, upgradeV13)
}
