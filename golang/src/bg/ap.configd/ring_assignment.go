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
	"strings"

	"bg/base_def"
	"bg/common/cfgapi"
	"bg/common/cfgtree"
)

/****************************************************************************
 *
 * Logic for determining the proper ring on which to place a newly discovered
 * device.
 */
func defaultRingInit() {
	vapToRing := make(map[string]string)
	ringOnVap := make(map[string]bool)

	// For each virtual AP, find @/network/vap/<id>/default_ring
	for id, vap := range propTree.GetChildren("@/network/vap") {
		if ring, ok := vap.Children["default_ring"]; ok {
			vapToRing[id] = ring.Value
		}
	}

	// For each virtual ring, find @/rings/<ring>/vap
	for ring, config := range propTree.GetChildren("@/rings") {
		if vapNode, ok := config.Children["vap"]; ok {
			vaps := strings.Split(vapNode.Value, ",")
			for _, vap := range vaps {
				key := ring + "/" + strings.TrimSpace(vap)
				ringOnVap[key] = true
			}
		}
	}
	virtualAPToDefaultRing = vapToRing
	ringOnVirtualAP = ringOnVap
}

func getChild(n *cfgtree.PNode, prop string) string {
	var rval string

	if n != nil && n.Children != nil {
		c := n.Children[prop]
		if c != nil && !c.Expired() {
			rval = c.Value
		}
	}

	return rval
}

func selectRing(mac string, client *cfgtree.PNode, vap, ring string) string {
	var newRing string

	if ring != "" && !cfgapi.ValidRings[ring] {
		// Should really never happen
		slog.Warnf("invalid incoming ring for %s: %s", mac, ring)
		ring = ""
	}

	oldRing := getChild(client, "ring")
	homeRing := getChild(client, "home")
	slog.Debugf("selectRing(mac: %s  vap: %s  ring: %s (was: %s)  home: %s",
		mac, vap, ring, oldRing, homeRing)

	if ring == base_def.RING_INTERNAL && homeRing != base_def.RING_INTERNAL {
		slog.Warnf("unexpected client %s found on %s", mac, ring)
		return ""
	}

	if vap != "" {
		// With a VAP in the event, it came from wifid detecting a new
		// client attaching.  If the client is already assigned to a
		// ring on the VAP, stick with it.  If it's not assigned to that
		// VAP, but its home ring is, then reassign it to the home ring.
		// If all else fails, it gets the default ring for that VAP.

		if key := oldRing + "/" + vap; ringOnVirtualAP[key] {
			slog.Debugf("%s stays on %s", mac, key)
			newRing = oldRing

		} else if key := homeRing + "/" + vap; ringOnVirtualAP[key] {
			slog.Infof("%s migrates to home ring %s", mac, key)
			newRing = homeRing

		} else if defaultRing, ok := virtualAPToDefaultRing[vap]; ok {
			slog.Infof("%s migrates to default ring %s/%s", mac,
				vap, defaultRing)
			newRing = defaultRing

		} else {
			slog.Warnf("%s has no ring on %s", mac, vap)
		}
	} else if ring != "" {
		var action string

		// With no VAP in the event, this event came from a DHCP
		// request.  This is primarily interesting as a way to identify
		// wired clients, as we can rely on ap.wifid to identify
		// wireless clients.

		if ring == base_def.RING_UNENROLLED {
			// We don't let DHCP migrate a client to the UNENROLLED
			// ring.  Only the admin or ap.wifid can do that.
			action = "ignoring on"
			ring = oldRing
		} else if oldRing == ring {
			action = "stays on"
		} else if oldRing == "" {
			action = "assigned to"
		} else {
			action = "migrating from " + oldRing + " to"
		}
		slog.Infof("%s: %s %s ring", mac, action, ring)
		newRing = ring
	}

	return newRing
}

// When a client's home ring is reset and that client is currently assigned to a
// different ring, clear the current ring setting.  The next time the client
// connects, the empty setting will cause it to be reassigned to its new home.
// If the client is currently connected, this will also cause ap.wifid to force
// a disconnect, so the desired reconnect/reassignment will happen immediately.
func updateClientHome(op int, prop, val string) {
	client, _ := propTree.GetNode(prop)
	if client != nil {
		ringProp := strings.TrimSuffix(prop, "home") + "ring"
		currentRing, _ := propTree.GetProp(ringProp)

		if currentRing != val {
			_ = propTree.Set(ringProp, "", nil)
		}
	}
}

func updateDefaultRing(op int, prop, val string) {
	slog.Infof("updating default ring: %s to %s", prop, val)
	defaultRingInit()
}
