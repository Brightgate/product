/*
 * Copyright 2020 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package wgsite

import (
	"fmt"
	"io/ioutil"
	"net"
	"os"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

func checkPublicKeys(public, escrowed string) string {
	var warn string

	if public == "" {
		warn = "no public key defined"
	} else if escrowed == "" {
		warn = "private key hasn't been escrowed yet"
	} else if escrowed != public {
		warn = "public key doesn't match escrowed key"
	}
	return warn
}

func checkPrivateKey(privatePath, publicKey string) string {
	// The absence of a pathname indicates that the tool is being run in the
	// cloud, where we only have access to the config tree - not the file
	// containing the server's private key.
	if privatePath == "" {
		return ""
	}

	if _, err := os.Stat(privatePath); os.IsNotExist(err) {
		return "server private key doesn't exist"
	}

	data, err := ioutil.ReadFile(privatePath)
	if err != nil {
		return fmt.Sprintf("unable to read private key %s: %v",
			privatePath, err)
	}

	private, err := wgtypes.ParseKey(string(data))
	if err != nil {
		return fmt.Sprintf("unable to parse private key %s: %v",
			privatePath, err)
	}

	if private.PublicKey().String() != publicKey {
		return "public key doesn't match private key"
	}

	return ""
}

func checkAddress(addr string) string {
	if addr == "" {
		return "no server address defined"
	}

	if ip := net.ParseIP(addr); ip != nil {
		// The server's location was specified by IP address, so there
		// is no possibility of a DNS lookup failure
		return ""
	}

	if _, err := net.LookupHost(addr); err != nil {
		return fmt.Sprintf("unable to resolve server address %s: %v",
			addr, err)
	}

	return ""
}

// SanityCheck examines the configuration for the site, and returns a list of
// warnings about potential problems.
func (s *Site) SanityCheck(privateKeyPath string) []string {
	var conf keyConfig

	list := make([]string, 0)

	if !s.IsEnabled() {
		list = append(list, "server is not enabled")
	}

	err := s.getServerConfig(&conf)
	if err != nil && err != errIncomplete {
		list = append(list, fmt.Sprintf("%v", err))
	} else {
		x := checkPublicKeys(conf.ServerPublicKey, conf.serverEscrowed)
		if x != "" {
			list = append(list, x)
		}

		x = checkPrivateKey(privateKeyPath, conf.ServerPublicKey)
		if x != "" {
			list = append(list, x)
		}

		x = checkAddress(conf.ServerAddress)
		if x != "" {
			list = append(list, x)
		}

		if conf.ServerPort == 0 {
			list = append(list, "no server port defined")
		}
	}
	if s.vpnStart == nil {
		list = append(list, "no subnet defined for VPN traffic")
	}
	if s.vpnRouter == nil {
		list = append(list, "no gateway defined for VPN network")
	}

	// Examine each published client key to see if the current server key
	// is the one in use when the client key was generated
	keys, err := s.GetKeys("")
	if err != nil {
		list = append(list, "unable to retrieve any user keys")
	} else if len(keys) == 0 {
		list = append(list, "no user keys defined")
	} else {
		for mac, key := range keys {
			if key.IsStale {
				list = append(list, mac+" key is likely stale")
			}
		}
	}

	return list
}

