/*
 * Copyright 2020 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package wgconf

import (
	"fmt"
	"net"
	"strings"
	"sync"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

// Endpoint represents one half of a WireGuard connection.
type Endpoint struct {
	Devname string
	Enabled bool

	Key       *wgtypes.Key
	IPAddress *net.IPNet
	Subnets   []net.IPNet

	sync.Mutex
}

func subnetList(subnets string) ([]net.IPNet, error) {
	var err error

	list := make([]net.IPNet, 0)
	for _, subnet := range strings.Split(subnets, ",") {
		var perr error

		trimmed := strings.TrimSpace(subnet)
		if trimmed == "" {
			continue
		}

		_, ipnet, perr := net.ParseCIDR(trimmed)
		if perr != nil {
			err = fmt.Errorf("bad subnet '%s': %v", subnet, perr)
		} else {
			list = append(list, *ipnet)
		}
	}
	return list, err
}

func keyParse(key string) (*wgtypes.Key, error) {
	var rval *wgtypes.Key

	parsed, err := wgtypes.ParseKey(key)
	if err != nil {
		err = fmt.Errorf("invalid key %s: %v", key, err)
	} else {
		rval = &parsed
	}

	return rval, err
}

// SetEnabled sets the Enabled flag to true
func (e *Endpoint) SetEnabled() {
	e.Enabled = true
}

// SetDisabled sets the Enabled flag to false
func (e *Endpoint) SetDisabled() {
	e.Enabled = false
}

// SetKey verifies that the string represents a valid key, and uses it to update
// the current key field.
func (e *Endpoint) SetKey(text string) error {
	e.Lock()
	defer e.Unlock()

	key, err := keyParse(text)
	if err != nil {
		e.Key = nil
	} else {
		e.Key = key
	}

	return err
}

func (e *Endpoint) setIPAddressLocked(ip net.IP) {
	e.IPAddress = &net.IPNet{
		IP:   ip,
		Mask: net.IPv4Mask(255, 255, 255, 255),
	}
}

// SetIPAddress verifies that the string represents a valid IP address, and uses
// it to update the endpoint's address.
func (e *Endpoint) SetIPAddress(ip string) error {
	var err error

	e.Lock()
	defer e.Unlock()

	if ipaddr := net.ParseIP(ip); ipaddr != nil {
		e.setIPAddressLocked(ipaddr)
	} else {
		e.IPAddress = nil
		err = fmt.Errorf("bad ip address: %s", ip)
	}

	return err
}

// SetSubnets verifies that the string represents a set of valid subnets.
// If so, it will update the Subnets field.
func (e *Endpoint) SetSubnets(subnets string) error {
	list, err := subnetList(subnets)

	e.Lock()
	defer e.Unlock()
	if err == nil && len(list) > 0 {
		e.Subnets = list
	} else {
		e.Subnets = nil
	}
	return err
}

