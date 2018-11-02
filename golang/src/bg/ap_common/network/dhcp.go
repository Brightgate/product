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
	"io/ioutil"
	"net"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	dhcp "github.com/krolaw/dhcp4"
)

const (
	leaseDir = "/var/lib/dhcpcd5/"
	dhcpDump = "/sbin/dhcpcd"
)

var (

	// Extract the two components of a DHCP option from lines like:
	//   domain_name_servers='192.168.52.1'
	//   vendor_class_identifier='Brightgate, Inc.'
	//   vendor_encapsulated_options='0109736174656c6c697465ff'
	optionRE = regexp.MustCompile(`(\w+)='(.*)'`)

	// Look for file names like 'dhcpcd-eth0.lease'
	leaseRE = regexp.MustCompile("dhcpcd-(.*).lease")
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

func getLeaseInfo(iface string) (map[string]string, error) {
	data := make(map[string]string)
	out, err := exec.Command(dhcpDump, "-4", "-U", iface).Output()
	if err != nil {
		return data, fmt.Errorf("failed to get lease data for %s: %v",
			iface, err)
	}

	// Each line in the dump output is structured as key='val'.
	// We generate a key-indexed map, with the single quotes stripped from
	// the value.
	options := optionRE.FindAllStringSubmatch(string(out), -1)
	for _, opt := range options {
		name := opt[1]
		val := opt[2]

		data[name] = strings.Trim(val, "'")
	}

	return data, nil
}

func getLease(iface string) (DHCPInfo, error) {
	var d DHCPInfo

	data, err := getLeaseInfo(iface)
	if err != nil {
		return d, err
	}

	d.Addr = net.ParseIP(data["ip_address"])
	d.Router = net.ParseIP(data["routers"])
	d.DomainName = data["domain_name"]

	d.Vendor = data["vendor_class_identifier"]
	vendorOptions := data["vendor_encapsulated_options"]
	if strings.Contains(d.Vendor, "Brightgate") && vendorOptions != "" {
		var s []byte
		// The vendor options are encapsulated in a binary stream of
		// [code, len, value] triples, which is then converted into a
		// binhex string.  If our DHCP server is a brightgate device,
		// it will only have a single option: '1' which is the device
		// mode.
		if s, err = hex.DecodeString(vendorOptions); err == nil {
			opts, _ := DHCPDecodeOptions(s)
			for _, o := range opts {
				if o.Code == 1 {
					d.Mode = string(o.Value)
					break
				}
			}
		}
	}

	seconds, err := strconv.Atoi(data["dhcp_lease_time"])
	d.LeaseDuration = time.Duration(seconds) * time.Second

	return d, nil
}

// GetAllLeases will parse all of the active DHCP leases, and returns a map
// of summary DHCPInfo structures indexed by the interface name.  For a
// Brightgate device, we would normally expect this map to have a single entry.
func GetAllLeases() (map[string]DHCPInfo, error) {
	files, err := ioutil.ReadDir(leaseDir)
	if err != nil {
		return nil, fmt.Errorf("unable to find leases in %s: %v",
			leaseDir, err)
	}

	rval := make(map[string]DHCPInfo)
	for _, file := range files {
		// Iterate over all of the file names in the lease directory,
		// looking for 'dhcpcd-<interface>.lease'.  Rather than parsing
		// the file ourselves, we pass the interface name to the
		// external dhcpcd utility to parse it for us.
		m := leaseRE.FindStringSubmatch(file.Name())
		if len(m) == 2 {
			iface := m[1]
			d, err := getLease(iface)
			if err == nil {
				d.LeaseStart = file.ModTime()
				rval[iface] = d
			}
		}
	}

	return rval, nil
}
