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

func upgradeV32() error {
	const vapProp = "@/rings/quarantine/vap"
	const oldDefault = "psk"
	const newDefault = "psk,eap,guest"

	vap, _ := propTree.GetNode(vapProp)
	if vap == nil {
		slog.Warnf("quarantine ring undefined")
	} else if vap.Value == oldDefault {
		slog.Infof("changing %s from %s to %s", vapProp,
			oldDefault, newDefault)
		propTree.Set(vapProp, newDefault, nil)
	} else {
		slog.Infof("leaving %s as %s", vapProp, vap.Value)
	}

	return nil
}

func init() {
	addUpgradeHook(32, upgradeV32)
}
