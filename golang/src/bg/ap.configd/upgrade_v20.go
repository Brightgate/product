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
	"strings"
)

func upgradeV20() error {
	// Update @/siteid to include the full domain name.  It will change
	// again once a cert is retrieved and a real domain is claimed, but it
	// should be valid until that time.

	if prop, err := propTree.GetNode("@/siteid"); err == nil {
		if strings.HasSuffix(prop.Value, ".brightgate.net") {
			// siteid already is in the new form.
			return nil
		}
		// Old form: append ".brightgate.net"
		newVal := prop.Value + ".brightgate.net"
		slog.Infof("Updating @/siteid from %q to %q", prop.Value, newVal)
		return propTree.Add("@/siteid", newVal, nil)
	}

	// Unlikely, but if siteid didn't exist, give it the default.
	slog.Info("Creating @/siteid: setup.brightgate.net")
	return propTree.Add("@/siteid", "setup.brightgate.net", nil)
}

func init() {
	addUpgradeHook(20, upgradeV20)
}
