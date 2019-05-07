/*
 * COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 *
 */

package main

import (
	"os"
	"text/template"
	"time"

	"bg/ap_common/aputil"
	"bg/common/cfgapi"
)

type ntpdConf struct {
	Rings    cfgapi.RingMap
	Servers  []string
	DriftDir string
}

const (
	ntpserversConfig = "@/network/ntpservers"
)

func getNTPServers() ([]string, error) {
	ret := make([]string, 0)
	props, err := config.GetProps(ntpserversConfig)
	if err != nil {
		slog.Warnf("Failed to get properties %s: %v\n", ntpserversConfig, err)
		return ret, err
	}
	for _, c := range props.Children {
		ret = append(ret, c.Value)
	}
	return ret, nil
}

func generateNTPDConf() error {
	tfile := *templateDir + "/chrony.conf.got"

	t, err := template.ParseFiles(tfile)
	if err != nil {
		return err
	}

	// The gateway and its satellites have different configuration.  The
	// satellites are merely clients, and should point to the gateway as
	// their server; the gateway should point to the configured servers and
	// open itself as a server to all devices on the network.
	conf := ntpdConf{}
	if aputil.IsSatelliteMode() {
		conf.Servers = []string{getGatewayIP()}
	} else {
		conf.Rings = rings
		conf.Servers, err = getNTPServers()
		if err != nil {
			return err
		}
	}
	conf.DriftDir = plat.ExpandDirPath(plat.NtpdDriftDir)

	cf, err := os.Create(plat.NtpdConfPath)
	if err != nil {
		return err
	}
	defer cf.Close()

	return t.Execute(cf, conf)
}

func restartNTP() {
	if err := generateNTPDConf(); err != nil {
		slog.Errorf("Failed to generate %s: %v\n", plat.NtpdConfPath, err)
	} else {
		plat.RestartService(plat.NtpdService)
	}
}

// Chrony doesn't have any sort of configuration reload mechanism, and injecting
// new configuration we need for this through chronyc isn't really possible, so
// we have to just restart the daemon.
func configNTPServersChanged(path []string, val string, expires *time.Time) {
	restartNTP()
}

func configNTPServersDeleted(path []string) {
	restartNTP()
}

func ntpdSetup() {
	config.HandleChange(`^@/network/ntpservers/`, configNTPServersChanged)
	config.HandleDelete(`^@/network/ntpservers/`, configNTPServersDeleted)
	config.HandleExpire(`^@/network/ntpservers/`, configNTPServersDeleted)

	// We kick the daemon to start with, because, even if it's already
	// running, it might be running with pre-Brightgate configuration.
	restartNTP()
}
