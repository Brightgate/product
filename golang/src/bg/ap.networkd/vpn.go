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
	"fmt"
	"strconv"

	"bg/common/cfgapi"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

const (
	vpnPublicProp  = "@/network/vpn/public_key"
	vpnPrivateProp = "@/network/vpn/private_key"
	vpnPortProp    = "@/network/vpn/port"
)

// Look up a single property.  It's OK for the property not to exist.  Any other
// error should be returned.
func getStr(p string) (string, error) {
	v, err := config.GetProp(p)
	if err != nil && err != cfgapi.ErrNoProp {
		return "", fmt.Errorf("fetching %s: %v", p, err)
	}

	return v, nil
}

// Attempt to pull the system-level vpn configuration from the config tree.  If
// it doesn't exist, create it and insert into the tree.
func vpnSystemInit() error {
	var err error
	var public, private, port string

	// XXX: First look for private key in a file.  If it's not there, then
	// the config tree.  If all else fails, generate a new one.  When
	// generating a new key, insert it into the config tree.  It can be
	// removed after being escrowed.

	if public, err = getStr(vpnPublicProp); err == nil {
		if private, err = getStr(vpnPrivateProp); err == nil {
			port, err = getStr(vpnPortProp)
		}
	}

	if err != nil {
		return err
	}

	if public == "" || private == "" {
		if public == "" && private == "" {
			slog.Infof("generating initial wireguard config")
		} else {
			slog.Infof("replacing incomplete wireguard config")
		}

		newPrivate, err := wgtypes.GeneratePrivateKey()
		if err != nil {
			slog.Warnf("generating wireguard private key: %v", err)
			return err
		}

		private = newPrivate.String()
		config.CreateProp(vpnPrivateProp, private, nil)

		public = newPrivate.PublicKey().String()
		config.CreateProp(vpnPublicProp, public, nil)
	}

	if port == "" {
		port = "3200"
		config.CreateProp(vpnPortProp, port, nil)
	}

	if _, err = strconv.Atoi(port); err != nil {
		slog.Warnf("invalid vpn listen port: %s", port)
	}

	return err
}

func vpnInit() error {
	slog.Infof("vpninit")

	return vpnSystemInit()
}
