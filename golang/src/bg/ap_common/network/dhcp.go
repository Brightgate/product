/*
 * COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package network

import (
	"encoding/hex"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"bg/ap_common/platform"

	dhcp "github.com/krolaw/dhcp4"
)

// DHCPInfo contains a summary of an outstanding DHCP lease held by this device.
type DHCPInfo struct {
	Addr          net.IP
	Router        net.IP
	DomainName    string
	LeaseStart    time.Time
	LeaseDuration time.Duration
	Vendor        string
	Mode          string
}

// DHCPDecodeOptions parses a bytestream into a slice of DHCP options
func DHCPDecodeOptions(s []byte) (opts []dhcp.Option, err error) {
	end := len(s)
	idx := 0
	for idx+3 < end {
		code := s[idx]
		valLen := int(s[idx+1])
		idx += 2
		if valLen < 1 || idx+valLen > end {
			err = fmt.Errorf("illegal option length: %d", valLen)
			break
		}
		val := s[idx : idx+valLen]
		idx += valLen

		o := dhcp.Option{
			Code:  dhcp.OptionCode(code),
			Value: val,
		}
		opts = append(opts, o)
	}
	return
}

// DHCPEncodeOptions marshals a slice of DHCP options into a bytestream as
// described in RFC-2132
func DHCPEncodeOptions(opts []dhcp.Option) (s []byte, err error) {
	for _, opt := range opts {
		if opt.Code == 0 || opt.Code >= dhcp.End {
			err = fmt.Errorf("bad option code: %d", opt.Code)
			break
		}

		s = append(s, byte(opt.Code))
		s = append(s, byte(len(opt.Value)))
		s = append(s, opt.Value...)
	}
	s = append(s, byte(dhcp.End))

	return
}

// GetLease queries the dhcp client about the provided interface, and returns a
// DHCPInfo structure containing whatever information we were able to retrieve.
func GetLease(iface string) (*DHCPInfo, error) {

	plat := platform.NewPlatform()

	data, err := plat.GetLeaseInfo(iface)
	if err != nil {
		return nil, err
	}

	d := &DHCPInfo{
		Addr:       net.ParseIP(data["ip_address"]),
		Router:     net.ParseIP(data["routers"]),
		DomainName: data["domain_name"],
		Vendor:     data["vendor_class_identifier"],
	}

	vendorOptions := data["vendor_encapsulated_options"]
	if strings.Contains(d.Vendor, "Brightgate") && vendorOptions != "" {
		// The vendor options are encapsulated in a binary stream of
		// [code, len, value] triples, which is then converted into a
		// binhex string.  If our DHCP server is a brightgate device,
		// it will only have a single option: '1' which is the device
		// mode.
		if s, err := hex.DecodeString(vendorOptions); err == nil {
			opts, _ := DHCPDecodeOptions(s)
			for _, o := range opts {
				if o.Code == 1 {
					d.Mode = string(o.Value)
					break
				}
			}
		}
	}

	if val, ok := data["dhcp_lease_start"]; ok {
		start, _ := time.Parse(time.RFC3339, val)
		d.LeaseStart = start
	}
	if val, ok := data["dhcp_lease_time"]; ok {
		seconds, _ := strconv.Atoi(val)
		d.LeaseDuration = time.Duration(seconds) * time.Second
	}

	return d, nil
}
