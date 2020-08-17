/*
 * Copyright 2020 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package main

import (
	"net"
	"strings"
	"sync"
	"time"
)

type vpnServer struct {
	enabled      bool
	domain       string
	dnsServer    net.IP
	allowedRings map[string]bool
}

var (
	vpnServers map[string]*vpnServer
	vpnLock    sync.Mutex
)

func vpnDNSSearch(vpn *vpnServer, ring, domain string) bool {
	// Can't access DNS on disabled or incompletely defined VPN servers
	if !vpn.enabled || vpn.dnsServer == nil || vpn.domain == "" {
		return false
	}

	// Check to see whether clients on this ring have access to this VPN
	if !vpn.allowedRings[ring] {
		return false
	}

	// If the callers specifies a domain, check to see whether this VPN
	// provides it.
	if domain != "" && !strings.EqualFold(domain, vpn.domain) {
		return false
	}

	return true
}

// If the domain name is hosted by this VPN, and this ring has access to it,
// return the DNS server on the VPN.  Otherwise, return nil.
func vpnGetDNSServer(ring, domain string) net.IP {
	vpnLock.Lock()
	defer vpnLock.Unlock()

	for _, vpn := range vpnServers {
		if vpnDNSSearch(vpn, ring, domain) {
			return vpn.dnsServer
		}
	}

	return nil
}

// Return a list of all active VPN domains this ring has access to
func vpnGetDomains(ring string) []string {
	vpnLock.Lock()
	defer vpnLock.Unlock()

	domains := make([]string, 0)
	for _, vpn := range vpnServers {
		if vpnDNSSearch(vpn, ring, "") {
			domains = append(domains, vpn.domain)
		}
	}

	return domains
}

func vpnGetServer(idx string) *vpnServer {
	vpnLock.Lock()
	defer vpnLock.Unlock()

	v := vpnServers[idx]
	if v == nil {
		v = &vpnServer{
			allowedRings: make(map[string]bool),
		}
		vpnServers[idx] = v
	}
	return v
}

func vpnUpdateEnabled(v *vpnServer, val string) {
	v.enabled = strings.EqualFold(val, "true")
}

func vpnUpdateDomain(v *vpnServer, domain string) {
	v.domain = domain
}

func vpnUpdateServer(v *vpnServer, server string) {
	if server == "" {
		v.dnsServer = nil
	} else {
		v.dnsServer = net.ParseIP(server)
	}
}

func vpnAddAllowed(v *vpnServer, ring string) {
	v.allowedRings[ring] = true
}

func vpnRemoveAllowed(v *vpnServer, ring string) {
	delete(v.allowedRings, ring)
}

func configVpnDNSChanged(path []string, val string, exp *time.Time) {
	// @/network/vpn/client/<idx>/wg/dns_domain
	v := vpnGetServer(path[3])
	if path[5] == "dns_domain" {
		vpnUpdateDomain(v, val)
	} else if path[5] == "dns_server" {
		vpnUpdateDomain(v, val)
	}
	updateDHCPOptions()
}

func configVpnDNSDelExp(path []string) {
	configVpnDNSChanged(path, "", nil)
}

func configVpnEnabledChanged(path []string, val string, exp *time.Time) {
	// @/policy/site/client/<idx>/enabled
	v := vpnGetServer(path[3])
	vpnUpdateEnabled(v, val)
	updateDHCPOptions()
}

func configVpnEnabledDelExp(path []string) {
	configVpnEnabledChanged(path, "false", nil)
}

func configVpnAllowedChanged(path []string, val string, exp *time.Time) {
	// @/policy/ring/<ring>/vpn/client/<idx>/allowed
	v := vpnGetServer(path[5])
	if strings.EqualFold(val, "true") {
		vpnAddAllowed(v, path[2])
	} else {
		vpnRemoveAllowed(v, path[2])
	}
	updateDHCPOptions()
}

func configVpnAllowedDelExp(path []string) {
	configVpnEnabledChanged(path, "", nil)
}

func vpnInfoInit() {
	vpnServers = make(map[string]*vpnServer)

	for idx, vpn := range config.GetChildren("@/network/vpn/client") {
		v := vpnGetServer(idx)

		// check @/network/vpn/client/<idx>/wg/dns_domain to see if
		// there is a domain to add to the search list
		if w, _ := vpn.GetChild("wg"); w != nil {
			if x, _ := w.GetChildString("dns_domain"); x != "" {
				vpnUpdateDomain(v, x)
			}
			if x, _ := w.GetChildString("dns_server"); x != "" {
				vpnUpdateServer(v, x)
			}
		}

		enabledProp := "@/policy/site/vpn/client/" + idx + "/enabled"
		v.enabled, _ = config.GetPropBool(enabledProp)
	}

	for ring := range rings {
		all := config.GetChildren("@/policy/ring/" + ring + "/vpn/client/")
		for idx, x := range all {
			if allowed, _ := x.GetChildBool("allowed"); allowed {
				vpnAddAllowed(vpnGetServer(idx), ring)
			}
		}
	}
}

