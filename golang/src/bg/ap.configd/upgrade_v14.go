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

func upgradeV14() error {
	slog.Infof("Adding @/network/ntpservers")

	propTree.Add("@/network/ntpservers/1", "time1.google.com", nil)
	propTree.Add("@/network/ntpservers/2", "time2.google.com", nil)
	propTree.Add("@/network/ntpservers/3", "time3.google.com", nil)
	propTree.Add("@/network/ntpservers/4", "time4.google.com", nil)

	return nil
}

func init() {
	addUpgradeHook(14, upgradeV14)
}
