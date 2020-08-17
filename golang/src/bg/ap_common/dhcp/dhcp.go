/*
 * Copyright 2019 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package dhcp

import (
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"bg/ap_common/platform"

	dhcp "github.com/krolaw/dhcp4"
)

// Info contains a summary of an outstanding DHCP lease held by this device.
type Info struct {
	Addr          string
	Route         string
	DomainName    string
	LeaseStart    time.Time
	LeaseDuration time.Duration
	Vendor        string
	Mode          string
}

// DecodeOptions parses a bytestream into a slice of DHCP options
func DecodeOptions(s []byte) (opts []dhcp.Option, err error) {
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

// EncodeOptions marshals a slice of DHCP options into a bytestream as
// described in RFC-2132
func EncodeOptions(opts []dhcp.Option) (s []byte, err error) {
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
func GetLease(iface string) (*Info, error) {

	plat := platform.NewPlatform()

	data, err := plat.GetDHCPInfo(iface)
	if data == nil {
		return nil, err
	}

	addr := data["ip_address"]
	if _, _, err := net.ParseCIDR(addr); err != nil {
		return nil, err
	}

	d := &Info{
		Addr:       addr,
		Route:      data["routers"],
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
			opts, _ := DecodeOptions(s)
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

// RenewLease sends the DHCP daemon a SIGHUP, causing it to attempt to renew its
// current lease, and set the IP address and route accordingly.
func RenewLease(nic string) error {
	plat := platform.NewPlatform()
	pidfile := plat.DHCPPidfile(nic)

	data, err := ioutil.ReadFile(pidfile)
	if err != nil {
		return fmt.Errorf("unable to read %s: %v", pidfile, err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return fmt.Errorf("bad pid in %s: %v", pidfile, err)
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("no dhcp process %d: %v", pid, err)
	}

	p.Signal(syscall.SIGHUP)
	return nil
}

