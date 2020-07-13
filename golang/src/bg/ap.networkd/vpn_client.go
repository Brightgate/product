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
	"strings"
	"sync"
	"time"

	"bg/ap_common/wgctl"
	"bg/common/wgconf"
)

var (
	wgClient       *wgconf.Client
	wgClientUpdate = make(chan bool, 4)
	wgClientReset  = make(chan bool, 4)
)

func vpnClientUpdateEnabled(path []string, val string, expires *time.Time) {
	enable := strings.EqualFold(val, "true")

	if wgClient == nil {
		wgClientUpdate <- true
	} else if enable != wgClient.Enabled {
		if enable {
			slog.Infof("enabling the vpn client")
			wgClient.SetEnabled()
		} else {
			slog.Infof("disabling the vpn client")
			wgClient.SetDisabled()
		}
		wgClientUpdate <- true
	}
}

func vpnClientDeleteEnabled(path []string) {
	vpnClientUpdateEnabled(path, "false", nil)
}

func vpnClientUpdateAllowed(path []string, val string, expires *time.Time) {
	// With only a single client supported, the only path we need to react
	// to is: @/policy/ring/<ring>/vpn/client/0/allowed
	if len(path) == 7 && path[5] == "0" {
		applyFilters()
	}
}

func vpnClientDeleteAllowed(path []string) {
	vpnClientUpdateAllowed(path, "false", nil)
}

func vpnClientUpdate(prop, val string) {
	if wgClient != nil {
		slog.Infof("updating %s to %s", prop, val)
		err := wgctl.HandleSettingChange(wgClient, prop, val)
		if err != nil {
			slog.Errorf("updating %s to %s: %v", prop, val, err)
		} else {
			wgClientUpdate <- true
		}
	}
}

func vpnClientDelete(path []string) {
	if wgClient == nil {
		return
	}

	slog.Infof("deleting %s", strings.Join(path, "/"))
	if len(path) < 5 {
		wgClientReset <- true
	} else {
		err := wgctl.HandleSettingChange(wgClient, path[4], "")
		if err != nil {
			slog.Warnf("deleting %s: %v", path[4], err)
		}
	}
	wgClientReset <- true
}

func vpnClientLoop(wg *sync.WaitGroup, doneChan chan bool) {
	var err error

	defer wg.Done()

	isUp := false
	done := false
	for !done {
		setDown := false

		if wgClient == nil {
			wgClient, err = wgctl.GetClient(config, 0)
			if err != nil {
				slog.Errorf("getting WireGuard client: %v", err)
				isUp = false
			} else {
				slog.Debugf("got wireguard config: %v", wgClient)
			}
		}

		if wgClient != nil && wgClient.Enabled && !isUp {
			if err = wgctl.ClientDevUp(wgClient); err != nil {
				slog.Errorf("instantiating client device %s: %n",
					wgClient.Devname, err)
			} else {
				slog.Infof("WireGuard client device %s created",
					wgClient.Devname)
				isUp = true
			}
		}
		if isUp {
			if err := wgctl.InstallConfig(wgClient); err != nil {
				slog.Errorf("installing WireGuard client config: %v",
					err)
			}
		}
		applyFilters()

		select {
		case <-wgClientUpdate:
			if wgClient != nil && !wgClient.Enabled {
				setDown = true
			}
		case <-wgClientReset:
			setDown = true
		case done = <-doneChan:
			setDown = true
		}

		if isUp && setDown {
			slog.Infof("bringing down client device")
			wgctl.ClientDevDown(wgClient)
			wgClient = nil
			isUp = false
		}
	}
}

func vpnClientFirewallRules() []string {
	slog.Infof("Adding wg client rules")
	if wgClient == nil || !wgClient.Enabled {
		return nil
	}

	rules := make([]string, 0)

	for ring := range rings {
		prop := "@/policy/ring/" + ring + "/vpn/client/0/allowed"
		if allowed, _ := config.GetPropBool(prop); allowed {
			ifaceForwardRules(ring, "wgc0")
			fwdRule := "ACCEPT from RING " + ring + " TO IFACE " +
				"vpnclient0"
			rules = append(rules, fwdRule)
		}
	}

	return rules
}

func vpnClientInit() error {
	return nil
}
