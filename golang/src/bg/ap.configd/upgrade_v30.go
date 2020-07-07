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
	"os"

	"bg/ap_common/aputil"
	"bg/common/wgsite"
)

func upgradeV30() error {
	var err error

	changes := map[string]string{
		"@/network/vpn/address":      "@/network/vpn/server/0/address",
		"@/network/vpn/public_key":   "@/network/vpn/server/0/public_key",
		"@/network/vpn/escrowed_key": "@/network/vpn/server/0/escrowed_key",
		"@/network/vpn/port":         "@/network/vpn/server/0/port",
		"@/network/vpn/last_mac":     "@/network/vpn/server/0/last_mac",
		"@/policy/site/vpn/enabled":  "@/policy/site/vpn/server/0/enabled",
		"@/policy/site/vpn/rings":    "@/policy/site/vpn/server/0/rings",
		"@/policy/site/vpn/subnets":  "@/policy/site/vpn/server/0/subnets",
	}

	for from, to := range changes {
		if node, err := propTree.GetNode(from); err == nil {
			slog.Infof("Moving %s -> %s", from, to)
			if err = node.Move(to); err != nil {
				return err
			}
		}
	}

	oldKeyFile := plat.ExpandDirPath("__APSECRET__/vpn", wgsite.PrivateFile)
	newKeyFile := plat.ExpandDirPath(wgsite.SecretDir, wgsite.PrivateFile)
	if aputil.FileExists(oldKeyFile) {
		slog.Infof("Moving %s to %s", oldKeyFile, newKeyFile)
		newKeyDir := plat.ExpandDirPath(wgsite.SecretDir)
		err = os.MkdirAll(newKeyDir, 0700)
		if err != nil {
			slog.Warnf("failed: %v", err)
		}

		if err == nil || err == os.ErrExist {
			err = os.Rename(oldKeyFile, newKeyFile)
		}
	}

	return err
}

func init() {
	addUpgradeHook(30, upgradeV30)
}
