/*
 * COPYRIGHT 2020 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package wgsite

import (
	"fmt"
	"net"
	"strings"

	"bg/common/cfgapi"
)

var (
	vpnOUIPrefix = []byte{0x00, 0x40, 0x54} // all MACs start with 00:40:54
)

// Convert 00:40:54:XX:YY:ZZ to 0xXXYYZZ
func macToShort(mac string) (int, error) {
	hwaddr, err := net.ParseMAC(mac)
	if err != nil || len(hwaddr) != 6 {
		return -1, fmt.Errorf(" bad MAC address: %s", mac)
	}

	for i, b := range vpnOUIPrefix {
		if hwaddr[i] != b {
			return -1, fmt.Errorf("invalid OUI: %s", mac)
		}
	}

	short := int(hwaddr[3])<<16 | int(hwaddr[4])<<8 | int(hwaddr[5])

	return short, nil

}

// Convert 0xXXYYZZ to 00:40:54:XX:YY:ZZ
func shortToMac(short int) string {
	hwaddr := make(net.HardwareAddr, 6)

	for i, b := range vpnOUIPrefix {
		hwaddr[i] = b
	}

	hwaddr[3] = byte((short >> 16) & 0xff)
	hwaddr[4] = byte((short >> 8) & 0xff)
	hwaddr[5] = byte(short & 0xff)

	return strings.ToLower(hwaddr.String())

}

// Returns the last mac allocated and a map of all in-use mac addresses.
func (s *Site) getInuseMacs(users cfgapi.UserMap) (int, map[int]bool, error) {
	var lastAllocated int

	// iterate over all the assigned VPN keys, adding their addresses to the
	// inuse map
	inuse := make(map[int]bool)
	for _, u := range users {
		for _, key := range u.WGConfig {
			if short, err := macToShort(key.Mac); err == nil {
				inuse[short] = true
			}
		}
	}

	lastMac, err := s.config.GetProp(LastMacProp)
	if err == nil {
		lastAllocated, _ = macToShort(lastMac)
	} else {
		err = fmt.Errorf("fetching %s: %v", LastMacProp, err)
	}

	return lastAllocated, inuse, err
}

// Returns the last mac address assigned and a candidate mac address for this
// new key.
func (s *Site) chooseMacAddress(users cfgapi.UserMap) (string, string, error) {
	const mask = 0xffffff // only iterate over the 3 low-order bytes
	var lastMac, newMac string

	last, inuse, err := s.getInuseMacs(users)
	if err == nil {
		err = fmt.Errorf("no mac addresses available")
		lastMac = shortToMac(last)
		for try := last + 1; try != last; try = (try + 1) & mask {
			if !inuse[try] {
				newMac = shortToMac(try)
				err = nil
				break
			}
		}
	}

	return lastMac, newMac, err
}
