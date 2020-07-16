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
	"strconv"
	"strings"
	"time"

	"bg/ap_common/publiclog"
	"bg/base_def"
	"bg/common/cfgapi"
	"bg/common/wifi"
)

func configNetworkRadiusSecretChanged(path []string, val string, expires *time.Time) {
	wifidStop("surprising change to network/radius_auth_secret")
}

func configNicChanged(path []string, val string, expires *time.Time) {
	var eval bool

	if len(path) != 5 {
		return
	}
	p := wirelessNics[path[3]]
	if p == nil || p.pseudo {
		return
	}

	switch path[4] {
	case "cfg_channel":
		x, _ := strconv.Atoi(val)
		if eval = (p.wifi != nil && p.wifi.configChannel != x); eval {
			p.wifi.configChannel = x
		}
	case "cfg_width":
		x, _ := strconv.Atoi(val)
		if eval = (p.wifi != nil && p.wifi.configWidth != x); eval {
			p.wifi.configWidth = x
		}
	case "cfg_band":
		if eval = (p.wifi != nil && p.wifi.configBand != val); eval {
			p.wifi.configBand = val
		}
	case "ring":
		if p.ring != val {
			p.ring = val
			wifidStop("exiting to rebuild network")
		}
	case "state":
		newState := strings.ToLower(val)
		if newState == wifi.DevDisabled || newState == wifi.DevOK {
			oldVal := p.disabled
			p.disabled = (newState == wifi.DevDisabled)
			eval = (oldVal != p.disabled)
			setState(p)
		}
	}

	if eval {
		wifiEvaluate = true
		hostapd.reset()
	}
}

func configNicDeleted(path []string) {
	if len(path) == 5 {
		switch path[4] {
		case "cfg_channel", "cfg_width", "cfg_band", "ring", "state":
			configNicChanged(path, "", nil)
		}
	}
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

		c = &cfgapi.ClientInfo{}
		clients[hwaddr] = c
	}
	clientsMtx.Unlock()

	switch path[2] {
	case "node":
		if c.ConnNode != val {
			slog.Infof("Moving %s from %s to %s", hwaddr,
				c.ConnNode, val)
			c.ConnNode = val
		}
	case "ring", "home":
		var reload bool

		if path[2] == "ring" && c.Ring != val {
			c.Ring = val
			reload = true
		} else if path[2] == "home" && c.Home != val {
			c.Home = val
			reload = true
		}

		if reload {
			hostapd.reload()
			hostapd.disassociate(hwaddr)
			if val == base_def.RING_QUARANTINE {
				publiclog.SendLogDeviceQuarantine(brokerd, hwaddr)
			}
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
		} else {
			configClientChanged(path, "", nil)
		}
	}
}

func configUserChanged(path []string, val string, expires *time.Time) {
	slog.Infof("%s changed to %s", strings.Join(path, "/"), val)
	if len(path) == 3 && path[2] == "user_md4_password" {
		radiusUserChange(path[1], val)
	}
}

func configUserDeleted(path []string) {
	slog.Infof("%s deleted", strings.Join(path, "/"))
	if len(path) == 2 ||
		path[2] == "user_password" || path[2] == "user_md4_password" {
		radiusUserChange(path[1], "")
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

	if path[2] == "vap" {
		oldList := r.VirtualAPs
		newList := make([]string, 0)
		for _, x := range strings.Split(val, ",") {
			if vap := strings.TrimSpace(x); vap != "" {
				newList = append(newList, vap)
			}
			r.VirtualAPs = newList
			slog.Infof("Changing VAP for ring %s from %v to %v",
				ring, oldList, newList)
			hostapd.reset()
		}
	}
}

func configNetworkDeleted(path []string) {
	configNetworkChanged(path, "", nil)
}

func configSiteIDChanged(path []string, val string, expires *time.Time) {
	// XXX - is this necessary?
	wifidStop("site_id changed - exiting to rebuild network")
}

func configCertStateChange(path []string, val string, expires *time.Time) {
	if val == "installed" {
		wifidStop("exiting due to renewed certificate")
	}
}

func configNetworkChanged(path []string, val string, expires *time.Time) {
	var reload bool

	if len(path) == 2 && path[1] == "radius_auth_secret" {
		prop := &wconf.radiusSecret
		if prop != nil && *prop != val {
			slog.Infof("radius_auth_secret changed to '%s'", val)
			*prop = val
			reload = true
		}
	}
	if len(path) == 4 && path[1] == "vap" {
		reload = true
	}

	if reload {
		wifiEvaluate = true
		hostapd.reload()
	}
}
