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

const (
	newDNSProp = "@/network/dns/server"
	oldDNSProp = "@/network/dnsserver"
)

func upgradeV29() error {
	p, _ := propTree.GetNode(oldDNSProp)
	if p != nil {
		slog.Infof("preserving DNS server: %s", p.Value)
		_ = propTree.Add(newDNSProp, p.Value, p.Expires)
		propTree.Delete(oldDNSProp)
	}
	return nil
}

func init() {
	addUpgradeHook(29, upgradeV29)
}
