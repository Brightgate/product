/*
 * Copyright 2020 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package main

import (
	"strings"
	"time"

	"bg/ap_common/aputil"
)

func updateSetting(op int, prop, val string) {
	slog.Debugf("updating setting: %s to %s", prop, val)

	path := strings.Split(prop, "/")
	if len(path) != 4 || path[2] != pname {
		return
	}

	if op == propDelete || op == propExpire {
		// revert to default on setting deletion
		if setting, ok := configdSettings[path[3]]; ok {
			val = setting.valDefault
		}
	}

	val = strings.ToLower(val)
	switch path[3] {
	case "verbose":
		if val == "true" {
			*verbose = true
		} else {
			*verbose = false
		}
	case "store_freq":
		f, err := time.ParseDuration(val)
		if err == nil {
			*storeFreq = f
		} else {
			slog.Warnf("ignoring bad %s: %s", path[3], val)
		}
	case "log_level":
		*logLevel = val
		aputil.LogSetLevel("", *logLevel)
	case "downgrade":
		if val == "true" {
			*allowDowngrade = true
		} else {
			*allowDowngrade = false
		}
	}
}

// Add @/settings equivalents of each of our option flags
func initSettings() {
	base := "@/settings/" + pname + "/"

	for p, s := range configdSettings {
		addSetting(base+p, s.valType)
	}

	// Apply any settings already present in the config tree
	for name, node := range propTree.GetChildren(base) {
		updateSetting(propChange, base+name, node.Value)
	}
}

