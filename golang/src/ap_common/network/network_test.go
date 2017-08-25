/*
 * COPYRIGHT 2017 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
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
