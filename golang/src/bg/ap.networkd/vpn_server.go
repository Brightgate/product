/*
 * COPYRIGHT 2020 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package main

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"bg/ap_common/aputil"
	"bg/ap_common/wgctl"
	"bg/base_def"
	"bg/common/cfgapi"
	"bg/common/wgconf"
	"bg/common/wgsite"
)

const (
	vpnServerNic = "wgs0"
)

var (
	wgServer       *wgconf.Server
	wgServerUpdate = make(chan bool, 4)
)

func vpnUpdateRings(path []string, val string, expires *time.Time) {
	applyFilters()
}

func vpnDeleteRings(path []string) {
	applyFilters()
}

func vpnServerUpdateEnabled(path []string, val string, expires *time.Time) {
	if wgServer == nil {
		return
	}

	if strings.EqualFold(val, "true") && !wgServer.Enabled {
		slog.Infof("enabling the vpn server")
		wgServer.SetEnabled()
		wgServerUpdate <- true
	} else if strings.EqualFold(val, "false") && wgServer.Enabled {
		slog.Infof("disabling the vpn server")
		wgServer.SetDisabled()
		wgServerUpdate <- true
	}
}

func vpnServerDeleteEnabled(path []string) {
	vpnServerUpdateEnabled(path, "false", nil)
}

func vpnUpdateUser(path []string, val string, expires *time.Time) {
	if wgServer == nil {
		return
	}

	mac := path[3]
	field := path[4]
	slog.Debugf("updating %s for %s", strings.Join(path, "/"), field)

	switch field {
	case "assigned_ip":
		if err := wgServer.SetClientKeyIP(mac, val); err != nil {
			slog.Warnf("updating ip address for '%s': %v", mac, err)
		}
		wgServerUpdate <- true

	case "allowed_ips":
		if err := wgServer.SetClientKeySubnets(mac, val); err != nil {
			slog.Warnf("updating subnets for '%s': %v", mac, err)
		}
		wgServerUpdate <- true

	case "public_key":
		if err := wgServer.SetClientKeyPublic(mac, val); err != nil {
			slog.Warnf("updating public key for '%s': %v", mac, err)
		}
		wgServerUpdate <- true
	}
}

func vpnDeleteUser(path []string) {
	if wgServer == nil {
		return
	}

	slog.Debugf("delete %s", strings.Join(path, "/"))
	mac := path[3]

	if len(path) == 4 {
		wgServer.DeleteClientKey(mac)
	} else if path[4] == "public_key" {
		wgServer.SetClientKeyPublic(mac, "")
	} else if path[4] == "allowed_ips" {
		wgServer.SetClientKeySubnets(mac, "")
	} else if path[4] == "assigned_ip" {
		wgServer.SetClientKeyIP(mac, "")
	}
	wgServerUpdate <- true
}

func vpnServerUpdate(setting, val string) {
	if setting == "public_key" {
		wgctl.ServerLoadKeys(wgServer)
		wgServerUpdate <- true

	} else if setting == "port" {
		if err := wgServer.SetListenPort(val); err != nil {
			slog.Warn("invalid vpn port %s: %v", val, err)
		}
		wgServerUpdate <- true
	}
}

func vpnServerDelete(path []string) {
	if wgServer == nil {
		return
	}

	if len(path) < 5 {
		wgServer.Delete()
	} else {
		vpnServerUpdate(path[4], "")
	}
	wgServerUpdate <- true
}

// After any change is made to the user- or system-level vpn configuration,
// regenerate the wireguard configuration.
func vpnServerLoop(wg *sync.WaitGroup, doneChan chan bool) {
	defer wg.Done()

	done := false
	updateNeeded := true
	for !done {
		if updateNeeded {
			applyFilters()
			if wgServer != nil {
				wgctl.ServerConfig(wgServer)
			}
			updateNeeded = false
		}

		select {
		case done = <-doneChan:
		case updateNeeded = <-wgServerUpdate:
		}

		// Multiple properties may be updated at once, so drain the
		// channel
		for drained := false; !drained; {
			select {
			case x := <-wgServerUpdate:
				updateNeeded = updateNeeded || x
			default:
				drained = true
			}
		}
	}

	wgctl.ServerDevDown(wgServer)
}

func vpnServerFirewallRules() []string {
	if wgServer == nil || !wgServer.Enabled {
		return nil
	}

	port := strconv.Itoa(base_def.WIREGUARD_PORT)
	if wgServer.ListenPort > 0 {
		port = strconv.Itoa(wgServer.ListenPort)
	}
	rules := []string{"ACCEPT UDP FROM IFACE wan TO AP DPORTS " + port}

	rings, err := config.GetProp(wgsite.RingsProp)
	if err == cfgapi.ErrNoProp {
		rings = "standard,devices"
	}

	// XXX - need to handle per-user exceptions
	for _, ring := range slice(rings) {
		if ring == "" {
			continue
		}
		if cfgapi.ValidRings[ring] {
			var rule string
			if ring == base_def.RING_WAN {
				rule = "ACCEPT FROM RING vpn to IFACE wan"
			} else {
				rule = "ACCEPT FROM RING vpn to RING " + ring
			}
			rules = append(rules, rule)
		}
	}

	subnets, err := config.GetProp(wgsite.SubnetsProp)
	for _, subnet := range slice(subnets) {
		if subnet == "" {
			continue
		}
		if _, _, err := net.ParseCIDR(subnet); err != nil {
			slog.Infof("bad vpn-allowed subnet %s: %v", subnet, err)
		} else {
			rule := "ACCEPT FROM RING vpn to ADDR " + subnet
			rules = append(rules, rule)
		}
	}

	rule := "ACCEPT TCP FROM RING vpn to AP DPORTS 53"
	rules = append(rules, rule)

	return rules
}

// Load the per-user vpn configuration from the config tree, and insert it into
// our user-indexed map
func vpnUserInit() {
	for _, user := range config.GetUsers() {
		for _, key := range user.WGConfig {
			if key.IPAddress == nil || key.Key == nil {
				slog.Warnf("skipping incomplete vpn key %q",
					key.Mac)
			}
			if err := wgServer.AddClientKey(key); err != nil {
				slog.Warnf("adding client key: %v", err)
			}
		}
	}
}

func vpnServerInit() error {
	ring, ok := rings[base_def.RING_VPN]
	if !ok {
		return fmt.Errorf("vpn ring is undefined")
	}

	keyFile := plat.ExpandDirPath(wgsite.SecretDir, wgsite.PrivateFile)
	if !aputil.FileExists(keyFile) {
		public, err := wgctl.CreateServerKeys(keyFile)
		if err != nil {
			return fmt.Errorf("creating server key: %v", err)
		}

		props := map[string]string{
			wgsite.PublicProp:  public,
			wgsite.LastMacProp: "00:40:54:00:00:00",
			wgsite.RingsProp:   "standard,devices",
			wgsite.PortProp:    strconv.Itoa(base_def.WIREGUARD_PORT),
		}

		if err := config.CreateProps(props, nil); err != nil {
			slog.Warnf("failed to create props: %v", err)
		}
	}

	wgServer = wgconf.NewServer(keyFile, vpnServerNic)

	if err := wgctl.ServerLoadKeys(wgServer); err != nil {
		return fmt.Errorf("getting WireGuard system keys: %v", err)
	}

	if port, err := config.GetProp(wgsite.PortProp); err != nil {
		if err := wgServer.SetListenPort(port); err != nil {
			return fmt.Errorf("setting port to %s: %v", port, err)
		}
	}

	addr := localRouter(ring)
	if err := wgServer.SetIPAddress(addr); err != nil {
		return fmt.Errorf("setting server address (%s): %v", addr, err)
	}

	if err := wgServer.SetSubnets(ring.Subnet); err != nil {
		return fmt.Errorf("setting subnet (%s): %v", ring.Subnet, err)
	}

	if enabled, _ := config.GetPropBool(wgsite.EnabledProp); enabled {
		wgServer.SetEnabled()
	} else {
		wgServer.SetDisabled()
	}

	err := wgctl.ServerDevUp(wgServer)

	vpnUserInit()

	return err
}
