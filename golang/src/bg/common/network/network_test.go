/*
 * Copyright 2017 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package network

import (
	"net"
	"testing"
)

func TestIsMacMulticast(t *testing.T) {
	validMcast := net.HardwareAddr([]byte{0x01, 0x00, 0x5E, 0x83, 0x07, 0x20})
	notMcast := net.HardwareAddr([]byte{0x00, 0x00, 0x00, 0x03, 0x01, 0xf0})

	if IsMacMulticast(notMcast) {
		t.Error()
	}

	if !IsMacMulticast(validMcast) {
		t.Error()
	}
}

func TestIPAddrToUint32(t *testing.T) {
	example := net.IPv4(128, 148, 26, 157)

	result := IPAddrToUint32(example)

	if result != 0x80941a9d {
		t.Error()
	}
}

func TestSubnetRouter(t *testing.T) {
	result := SubnetRouter("128.148.26.0/24")

	if result != "128.148.26.1" {
		t.Error()
	}
}

func TestSubnetBroadcast(t *testing.T) {
	result := SubnetBroadcast("128.148.26.0/24").String()

	if result != "128.148.26.255" {
		t.Error()
	}
}

