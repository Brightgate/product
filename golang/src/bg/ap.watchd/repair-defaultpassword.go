/*
 * Copyright 2019 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package main

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	"bg/ap_common/apvuln"
)

var (
	resetsRunning *hostmap
)

const (
	dpPropFmt = `@/clients/%s/vulnerabilities/defaultpassword/%s`
)

func dpPropPath(mac, prop string) string {
	return fmt.Sprintf(dpPropFmt, mac, prop)
}

func dpRepairPropRE() string {
	return "^" + fmt.Sprintf(dpPropFmt, ".*", "repair") + "$"
}

func execPasswordChange(ipaddr string, dpVuln apvuln.DPvulnerability) (apvuln.DPcredentials, error) {
	var newCreds apvuln.DPcredentials
	if dpVuln.Service != "ssh" {
		return newCreds, fmt.Errorf("Unsupported password repair service %s", dpVuln.Service)
	}
	defaultpass := plat.ExpandDirPath("__APPACKAGE__", "bin/ap-defaultpass")
	resetData := fmt.Sprintf("%s:%s:%s:%s", dpVuln.Service, dpVuln.Port,
		dpVuln.Credentials.Username, dpVuln.Credentials.Password)
	cmd := exec.Command(defaultpass, "-i", ipaddr, "-r", resetData, "-human-password")
	stdout, err := cmd.Output()
	if err != nil {
		slog.Errorf("%s %v failed: %s", defaultpass, cmd.Args, err)
		return newCreds, err
	}
	response := strings.SplitN(string(stdout), " ", 2)
	if len(response) < 2 || response[0] != "success" {
		err = fmt.Errorf("%s %v failed: %s", defaultpass, cmd.Args, err)
		slog.Errorf("%s", err)
		return newCreds, err
	}
	newCredsStr := strings.TrimRight(response[1], "\r\n")
	newData := strings.SplitN(newCredsStr, ":", 2)
	if len(newData) != 2 {
		err = fmt.Errorf("Invalid result: %s", response[1])
		slog.Errorf("%s", err)
		return newCreds, err
	}
	newCreds.Username = newData[0]
	newCreds.Password = newData[1]

	return newCreds, nil
}

func markRepairDefaultPasswordFailed(mac string) {
	err := config.SetProp(dpPropPath(mac, "repair"), "false", nil)
	if err != nil {
		slog.Warnf("Error setting %v = false",
			dpPropPath(mac, "repair"), err)
	}
}

func configRepairDefaultPassword(path []string, value string, expires *time.Time) {
	if value == "false" || (expires != nil && time.Now().After(*expires)) {
		slog.Debugf("configRepairDefaultPassword: " +
			"repair false or expired")
		return
	}

	mac := path[1]

	vMap := config.GetVulnerabilities(mac)
	vInfo := vMap["defaultpassword"]

	var repairedAt, latestDetected time.Time
	if vInfo.RepairedAt != nil {
		repairedAt = *vInfo.RepairedAt
	}
	if vInfo.LatestDetected != nil {
		latestDetected = *vInfo.LatestDetected
	}

	if (repairedAt).After(latestDetected) {
		slog.Warnf("configRepairDefaultPassword: RepairedAt %v later "+
			"than LatestDetected %v. Skipping.", repairedAt,
			latestDetected)
		return
	}

	clients := config.GetClients()
	clientIP := clients[mac].IPv4.String()

	oldDetails := strings.TrimSpace(vInfo.Details)
	if len(strings.Split(oldDetails, "\n")) > 1 {
		slog.Errorf("Multi-line details for %s", dpPropPath(mac, ""))
		markRepairDefaultPasswordFailed(mac)
		return
	}

	dpVuln := apvuln.DPvulnerability{IP: clientIP}
	err := apvuln.ParseDetailsSummary(&dpVuln, oldDetails)
	if err != nil {
		slog.Errorf("Error parsing details: %s", err)
		markRepairDefaultPasswordFailed(mac)
		return
	}

	userAndMac := fmt.Sprintf("%s@%s", dpVuln.Credentials.Username, mac)

	if err = resetsRunning.add(userAndMac); err != nil {
		slog.Infof("Already trying to reset password for %s", userAndMac)
		return
	}
	defer resetsRunning.del(userAndMac)

	slog.Infof("configRepairDefaultPassword for %s", userAndMac)

	var newCreds apvuln.DPcredentials
	if newCreds, err = execPasswordChange(clientIP, dpVuln); err != nil {
		slog.Warnf("default password repair attempt failed: %v", err)
		markRepairDefaultPasswordFailed(mac)
		return
	}

	err = config.CreateProp(dpPropPath(mac, "details"),
		apvuln.RepairedDPDetails(dpVuln, newCreds), nil)
	if err != nil {
		// TODO: Figure out a more secure place to stash these, but
		// if the config tree failed we don't want to lose them
		slog.Errorf("Error updating %v with newCreds %#v: %v", dpPropPath(mac, "details"), newCreds, err)
	} else {
		slog.Infof("Changed default password for %s@%s; "+
			"see config data for new password.",
			dpVuln.Credentials.Username, clientIP)
	}

	// We want to update these, but it's not fatal if these fail
	err = config.CreateProp(dpPropPath(mac, "repaired"),
		time.Now().Format(time.RFC3339), nil)
	if err != nil {
		slog.Warnf("Error creating %v", dpPropPath(mac, "repaired"), err)
	}
	err = config.DeleteProp(dpPropPath(mac, "repair"))
	if err != nil {
		slog.Warnf("Error deleting %v", dpPropPath(mac, "repair"), err)
	}
}

func repairDefaultPasswordFini(w *watcher) {
	w.running = false
}

func repairDefaultPasswordInit(w *watcher) {
	// This assumes 'hostmap' accepts arbitrary strings
	resetsRunning = hostmapCreate()

	config.HandleChange(dpRepairPropRE(), configRepairDefaultPassword)

	w.running = true
}

func init() {
	addWatcher("repairpassword", repairDefaultPasswordInit, repairDefaultPasswordFini)
}

