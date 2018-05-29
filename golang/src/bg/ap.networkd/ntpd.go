/*
 * COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
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
	"log"
	"os"
	"os/exec"
	"text/template"
	"time"

	"bg/ap_common/apcfg"
	"bg/ap_common/aputil"
)

type ntpdConf struct {
	Rings   apcfg.RingMap
	Servers []string
}

const (
	ntpserversConfig   = "@/network/ntpservers"
	ntpdPath           = "/usr/sbin/chronyd"
	ntpdConfPath       = "/etc/chrony/chrony.conf"
	ntpdSystemdService = "chrony.service"
)

func getNTPServers() ([]string, error) {
	ret := make([]string, 0)
	props, err := config.GetProps(ntpserversConfig)
	if err != nil {
		log.Printf("Failed to get properties %s: %v\n", ntpserversConfig, err)
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

	cf, err := os.Create(ntpdConfPath)
	if err != nil {
		return err
	}
	defer cf.Close()

	return t.Execute(cf, conf)
}

// Chrony doesn't have any sort of configuration reload mechanism, and injecting
// new configuration we need for this through chronyc isn't really possible, so
// we have to just restart the daemon.
func configNTPServersChanged(path []string, val string, expires *time.Time) {
	runNTPDaemon()
}

func configNTPServersDeleted(path []string) {
	runNTPDaemon()
}

func runNTPDaemon() {
	if err := generateNTPDConf(); err != nil {
		log.Printf("Failed to generate %s: %v\n", ntpdConfPath, err)
		return
	}
	// "restart" will start the service if it's not already running.
	cmd := exec.Command("/bin/systemctl", "restart", ntpdSystemdService)
	if err := cmd.Run(); err != nil {
		log.Printf("Failed to restart %s: %v\n", ntpdSystemdService, err)
	}
}

func ntpdSetup() {
	config.HandleChange(`^@/network/ntpservers/`, configNTPServersChanged)
	config.HandleDelete(`^@/network/ntpservers/`, configNTPServersDeleted)
	config.HandleExpire(`^@/network/ntpservers/`, configNTPServersDeleted)

	// We kick the daemon to start with, because, even if it's already
	// running, it might be running with pre-Brightgate configuration.
	runNTPDaemon()
}
