/*
 * Copyright 2020 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package wgconf

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"strings"
)

const (
	modeNone = iota
	modeInterface
	modePeer
)

func addClientSetting(m map[string]string, key, val string) error {
	var err error

	switch strings.ToLower(key) {
	case "address":
		var ipnet *net.IPNet

		// Address = 192.168.185.12/32
		if _, ipnet, err = net.ParseCIDR(val); err == nil {
			m["client_address"] = ipnet.IP.String()
		}
	case "privatekey":
		// PrivateKey = EN+a2P5AZ09kjld0kjfdglk39gldkfg5p/vFFC0gdVg=
		if _, err = keyParse(val); err == nil {
			m["client_private"] = val
		}
	case "dns":
		// #DNS = 192.168.185.1
		if ip := net.ParseIP(val); ip != nil {
			// Allow for multiple servers to be specified on
			// multiple lines
			if old := m["dns_server"]; old != "" {
				val = old + "," + val
			}
			m["dns_server"] = val
		} else {
			m["dns_domain"] = val
		}
	case "dnsdomain":
		m["dns_domain"] = val
	default:
		err = fmt.Errorf("unrecognized config key: %s", key)
	}

	return err
}

func addServerSetting(m map[string]string, key, val string) error {
	var err error

	switch strings.ToLower(key) {
	case "publickey":
		// PublicKey = kdCkEgdmoUvlAPvGBTGGiHPSHvF2OEJmF9okYlChXnI=
		if _, err = keyParse(val); err == nil {
			m["server_public"] = val
		}
	case "endpoint":
		// Endpoint = app0.b10e.net:51820
		f := strings.Split(val, ":")
		if len(f) != 2 {
			err = fmt.Errorf("invalid endpoint: %s", val)
		} else {
			m["server_address"] = f[0]
			m["server_port"] = f[1]
		}
	case "allowedips":
		// AllowedIPs = 192.168.185.0/24,10.138.0.0/24
		m["subnets"] = val
	case "persistentkeepalive":
	default:
		err = fmt.Errorf("unrecognized config key: %s", key)
	}

	return err
}

func parseSetting(line string) (string, string, error) {
	var key, value string
	var err error

	if f := strings.SplitN(line, "=", 2); len(f) == 2 {
		key = strings.TrimSpace(f[0])
		value = strings.TrimSpace(f[1])
	}
	if key == "" || value == "" {
		err = fmt.Errorf("invalid line")
	}

	return key, value, err
}

func parseLine(keys map[string]string, mode int, line string) (int, error) {
	// Get rid of comments and leading/trailing whitespace
	line = strings.TrimSpace(strings.SplitN(line, "#", 2)[0])
	if line == "" {
		return mode, nil
	}

	if strings.HasPrefix(line, "[") {
		if strings.EqualFold(line, "[interface]") {
			return modeInterface, nil
		}
		if strings.EqualFold(line, "[peer]") {
			return modePeer, nil
		}
		return modeNone, fmt.Errorf("unrecognized mode: %s", line)
	}

	key, val, err := parseSetting(line)
	if err != nil {
		return mode, err
	}

	if mode == modeInterface {
		err = addClientSetting(keys, key, val)
	} else if mode == modePeer {
		err = addServerSetting(keys, key, val)
	} else {
		err = fmt.Errorf("value setting outside of a mode stanza")
	}

	return mode, err
}

// ParseClientConfig reads a standard WireGuard client configuration file, and
// returns a map mapping the settings in that file onto the property names we
// use.
func ParseClientConfig(r io.Reader) (map[string]string, error) {
	var err error

	keys := make(map[string]string)
	mode := modeNone

	lineNo := 0
	s := bufio.NewScanner(r)
	for s.Scan() {
		lineNo++
		line := s.Text()

		mode, err = parseLine(keys, mode, line)
		if err != nil {
			return nil, fmt.Errorf("at line %d: %v", lineNo, err)
		}
	}

	if err = s.Err(); err != nil {
		err = fmt.Errorf("parsing WireGuard config: %v", err)
	}

	return keys, err
}

