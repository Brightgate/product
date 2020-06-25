/*
 * COPYRIGHT 2020 Brightgate Inc.  All rights reserved.
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
	"net"
	"strings"
	"time"

	"bg/ap_common/publiclog"
	"bg/base_def"
	"bg/common/cfgapi"
	"bg/common/wifi"
)

func configNicChanged(path []string, val string, expires *time.Time) {
	if len(path) != 5 {
		return
	}
	p := wiredNics[path[3]]
	if p == nil {
		return
	}

	if path[4] == "ring" && p.ring != val {
		networkdStop("exiting to rebuild network")
	}

	if path[4] == "state" {
		newState := strings.ToLower(val)
		if newState == wifi.DevDisabled || newState == wifi.DevOK {
			disabled := (newState == wifi.DevDisabled)
			if disabled != p.disabled {
				networkdStop("exiting to rebuild network")
			}
		}
	}
}

func configNicDeleted(path []string) {
	configNicChanged(path, "", nil)
}

func configClientChanged(path []string, val string, expires *time.Time) {
	hwaddr := path[1]
	clientsMtx.Lock()
	c, ok := clients[hwaddr]
	if !ok {
		if val == "" {
			clientsMtx.Unlock()
			return
		}

		slog.Infof("new client: %s", hwaddr)
		c = &cfgapi.ClientInfo{}
		clients[hwaddr] = c
	}
	clientsMtx.Unlock()

	if path[2] == "ring" && c.Ring != val {
		c.Ring = val
		if val == base_def.RING_QUARANTINE {
			publiclog.SendLogDeviceQuarantine(brokerd, hwaddr)
		}
	}

	if path[2] == "ipv4" {
		ip := net.ParseIP(val)
		if !ip.Equal(c.IPv4) {
			c.IPv4 = ip
			forwardUpdateTarget(hwaddr, val)
		}
	}
}

func configClientDeleted(path []string) {
	hwaddr := path[1]
	if _, ok := clients[hwaddr]; ok {
		if len(path) == 2 {
			clientsMtx.Lock()
			delete(clients, hwaddr)
			clientsMtx.Unlock()
			forwardUpdateTarget(hwaddr, "")
		} else {
			configClientChanged(path, "", nil)
		}
	}
}

func configUserChanged(path []string, val string, expires *time.Time) {
	if len(path) == 5 && path[2] == "vpn" {
		vpnUpdateUser(path, val, expires)
	}
}

func configUserDeleted(path []string) {
	if len(path) == 2 {
		vpnDeleteUser(path)
	} else if len(path) > 2 && path[2] == "vpn" {
		vpnDeleteUser(path)
	}
}

func configRingSubnetDeleted(path []string) {
	ring := path[1]

	if _, ok := rings[ring]; !ok {
		slog.Warnf("Unknown ring: %s", ring)
	} else {
		slog.Infof("Deleted subnet for ring %s", ring)
		networkdStop("exiting to rebuild network")
	}
}

func configRingChanged(path []string, val string, expires *time.Time) {
	if len(path) != 3 {
		return
	}
	ring := path[1]
	r, ok := rings[ring]
	if !ok {
		slog.Warnf("Unknown ring: %s", ring)
		return
	}

	if path[2] == "subnet" && r.Subnet != val {
		slog.Infof("Changing subnet for ring %s from %s to %s",
			ring, r.Subnet, val)
		networkdStop("exiting to rebuild network")
	}
}

func configSet(name, val string) bool {
	var reload bool

	switch name {
	case "base_address":
		networkdStop("base_address changed - exiting to rebuild network")
		return false

	case "dnsserver":
		wanStaticChanged(name, val)
	}

	return reload
}

func configNetworkDeleted(path []string) {
	if len(path) >= 2 && path[1] == "vpn" {
		vpnDelete(path)

	} else if len(path) >= 3 && path[1] == "wan" && path[2] == "static" {
		field := "all"
		if len(path) > 3 {
			field = path[3]
		}
		wanStaticDeleted(field)
	}
}

func configSiteIndexChanged(path []string, val string, expires *time.Time) {
	networkdStop("site_index changed - exiting to rebuild network")
}

func configNetworkChanged(path []string, val string, expires *time.Time) {
	if len(path) == 3 && path[1] == "vpn" {
		vpnUpdate(path, val, expires)
	} else if len(path) == 4 && path[1] == "wan" && path[2] == "static" {
		wanStaticChanged(path[3], val)
	}
}
