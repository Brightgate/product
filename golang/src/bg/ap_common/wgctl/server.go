/*
 * Copyright 2020 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package wgctl

import (
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"path/filepath"

	"bg/ap_common/aputil"
	"bg/common/wgconf"

	"golang.zx2c4.com/wireguard/wgctrl"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

// CreateServerKeys creates a WireGuard private key and writes it to the
// provided file.  It returns the corresponding public key to the caller.
func CreateServerKeys(file string) (string, error) {
	var public string

	private, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		return "", fmt.Errorf("generating wireguard key: %v", err)
	}

	if dir := filepath.Dir(file); !aputil.FileExists(dir) {
		if err = os.MkdirAll(dir, 0700); err != nil {
			return "", fmt.Errorf("mkdir(%s): %v", dir, err)
		}
	}

	data := []byte(private.String())
	err = ioutil.WriteFile(file, data, 0600)
	if err != nil {
		err = fmt.Errorf("persisting private key at %s: %v", file, err)
	} else {
		public = private.PublicKey().String()
	}

	return public, err
}

// ServerDevUp instantiates and plumbs a WireGuard network device
func ServerDevUp(s *wgconf.Server) error {
	return devUp(s.ToEndpoint())
}

// ServerDevDown removes a WireGuard network device
func ServerDevDown(s *wgconf.Server) error {
	return devDown(s.ToEndpoint())
}

// ServerConfig configures a server-side WireGuard device.  Any existing
// configuration is overwritten.
func ServerConfig(s *wgconf.Server) error {
	var peers []wgtypes.PeerConfig

	s.Lock()
	defer s.Unlock()

	if s.Key == nil {
		return fmt.Errorf("vpn configuration missing private key")
	}

	peers = make([]wgtypes.PeerConfig, 0)
	for _, key := range s.UserKeys {
		if key.Key == nil || key.IPAddress == nil {
			continue
		}

		peer := wgtypes.PeerConfig{
			PublicKey:  *key.Key,
			AllowedIPs: []net.IPNet{*key.IPAddress},
		}
		peers = append(peers, peer)
	}

	c := wgtypes.Config{
		PrivateKey:   s.Key,
		ListenPort:   &s.ListenPort,
		ReplacePeers: true,
		Peers:        peers,
	}

	client, err := wgctrl.New()
	if err != nil {
		return fmt.Errorf("creating wgctrl client: %v", err)
	}
	defer client.Close()

	if err = client.ConfigureDevice(s.Devname, c); err != nil {
		return fmt.Errorf("configuring %s: %v", s.Devname, err)
	}

	return nil
}

// ServerLoadKeys causes a server's private key to be loaded from its designated
// file.  This needs to be done when the server is first instantiated and any
// time the key changes.
func ServerLoadKeys(s *wgconf.Server) error {
	s.Key = nil

	text, err := ioutil.ReadFile(s.Keyfile)
	if err != nil {
		err = fmt.Errorf("reading key from %s: %v", s.Keyfile, err)

	} else if err = s.SetKey(string(text)); err != nil {
		err = fmt.Errorf("invalid private key: %v", err)
	}

	return err
}

