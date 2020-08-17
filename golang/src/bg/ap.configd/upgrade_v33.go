/*
 * Copyright 2020 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package main

import (
	"bg/base_def"
)

func upgradeV33() error {
	clients, _ := propTree.GetNode("@/clients")
	if clients == nil {
		return nil
	}

	for mac, c := range clients.Children {
		if ring, ok := c.Children["ring"]; ok {
			if ring.Value == base_def.RING_INTERNAL {
				slog.Infof("preserving %s ring assignment", mac)
				path := "@/clients/" + mac + "/home"
				propTree.Add(path, base_def.RING_INTERNAL, nil)
			}
		}
	}

	return nil
}

func init() {
	addUpgradeHook(33, upgradeV33)
}

