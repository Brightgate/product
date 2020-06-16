/*
 * COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

// Package network contains helper functions for reading a writing packets to a
// network interface.
package network

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"regexp"
	"strings"
	"syscall"
	"time"
)

// Well known addresses
var (
	MacZero     = net.HardwareAddr([]byte{0, 0, 0, 0, 0, 0})
	MacZeroInt  = HWAddrToUint64(MacZero)
	MacBcast    = net.HardwareAddr([]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF})
	MacBcastInt = HWAddrToUint64(MacBcast)
	IPLocalhost = net.IPv4(127, 0, 0, 1)

	// Multicast addresses for mDNS
	MacmDNSv4 = net.HardwareAddr([]byte{0x01, 0x00, 0x5E, 0x00, 0x00, 0xFB})
	MacmDNSv6 = net.HardwareAddr([]byte{0x33, 0x33, 0x00, 0x00, 0x00, 0xFB})
	IpmDNSv4  = net.IPv4(224, 0, 0, 251)
	IpmDNSv6  = net.IP{0xFF, 0x02, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0xFB}

	// Multicast addresses for SSDP
	IPSSDPv4       = net.IPv4(239, 255, 255, 250)
	IPSSDPv6Link   = net.IP{0xFF, 0x02, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x0C}
	IPSSDPv6Site   = net.IP{0xFF, 0x05, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x0C}
	IPSSDPv6Org    = net.IP{0xFF, 0x08, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x0C}
	IPSSDPv6Global = net.IP{0xFF, 0x0E, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x0C}

	// Multicast prefix
	macMcast = net.HardwareAddr([]byte{0x01, 0x00, 0x5E})
)

// IsPrivate determines whether the provided IP address falls into one of the 3
// IPv4 address ranges for private networks.
func IsPrivate(ip net.IP) bool {
	_, a, _ := net.ParseCIDR("10.0.0.0/8")
	_, b, _ := net.ParseCIDR("172.16.0.0/12")
	_, c, _ := net.ParseCIDR("192.168.0.0/16")

	return a.Contains(ip) || b.Contains(ip) || c.Contains(ip)
}

// IsMacMulticast checks if the supplied MAC address begins 01:00:5E
func IsMacMulticast(a net.HardwareAddr) bool {
	return a[3]&0x80 == 0x80 && bytes.HasPrefix(a, macMcast)
}

// HWAddrToUint64 encodes a net.HardwareAddr as a uint64
func HWAddrToUint64(a net.HardwareAddr) uint64 {
	hwaddr := make([]byte, 8)
	hwaddr[0] = 0
	hwaddr[1] = 0
	copy(hwaddr[2:], a)

	return binary.BigEndian.Uint64(hwaddr)
}

// Uint64ToHWAddr decodes a uint64 into a net.HardwareAddr
func Uint64ToHWAddr(a uint64) net.HardwareAddr {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, a)
	return net.HardwareAddr(b[2:])
}

// Uint64ToMac decodes a uint64 into a mac string
func Uint64ToMac(a uint64) string {
	return Uint64ToHWAddr(a).String()
}

// MacToUint64 decodes a mac string into a uint64
func MacToUint64(mac string) uint64 {
	var rval uint64

	if hwaddr, err := net.ParseMAC(mac); err == nil {
		rval = HWAddrToUint64(hwaddr)
	}
	return rval
}

// IPAddrToUint32 encodes a net.IP as a uint32
func IPAddrToUint32(a net.IP) uint32 {
	var rval uint32

	if b := a.To4(); b != nil {
		rval = binary.BigEndian.Uint32(b)
	}
	return rval
}

// Uint32ToIPAddr decodes a uint32 into a new.IP
func Uint32ToIPAddr(a uint32) net.IP {
	var ipv4 net.IP

	if a != 0 {
		ipv4 = make(net.IP, net.IPv4len)
		binary.BigEndian.PutUint32(ipv4, a)
	}
	return ipv4
}

// SubnetRouter derives the router's IP address from the network.
//    e.g., 192.168.136.0/28 -> 192.168.136.1
func SubnetRouter(subnet string) string {
	_, network, _ := net.ParseCIDR(subnet)
	raw := network.IP.To4()
	raw[3]++
	router := (net.IP(raw)).String()
	return router
}

// SubnetBroadcast derives the subnet's broadcast address
//    e.g., 192.168.136.0/28 -> 192.168.136.15
func SubnetBroadcast(subnet string) net.IP {
	_, network, _ := net.ParseCIDR(subnet)
	raw := network.IP.To4()
	for i := 0; i < 4; i++ {
		raw[i] |= (0xff ^ network.Mask[i])
	}

	return raw
}

// WaitForDevice will wait for a network device to leave the 'down' state.
// Returns an error on timeout or if the device doesn't exist
func WaitForDevice(dev string, timeout time.Duration) error {
	fn := "/sys/class/net/" + dev + "/operstate"

	start := time.Now()
	for {
		state, err := ioutil.ReadFile(fn)
		if err == nil &&
			(len(state) < 4 || string(state[0:3]) != "down") {
			break
		}
		if time.Since(start) >= timeout {
			return fmt.Errorf("timeout: %s not online: %s", dev, state)
		}
		time.Sleep(time.Millisecond * 100)
	}
	return nil
}

var legalHostname = regexp.MustCompile(`^([a-z0-9]|[a-z0-9][a-z0-9\-]*[a-z0-9])$`)

// ValidHostname checks whether the provided hostname is RFC1123-compliant.
// A hostname may contain only letters, digits, and hyphens.  It may neither
// start nor end with hyphen.
func ValidHostname(hostname string) bool {
	if len(hostname) == 0 || len(hostname) > 63 {
		return false
	}

	lower := []byte(strings.ToLower(hostname))
	return legalHostname.Match(lower)
}

var legalDNSlabel = regexp.MustCompile(`^([a-z0-9_]|[_a-z0-9][_a-z0-9\-]*[_a-z0-9])$`)
var minimalDNSlabel = regexp.MustCompile(`[a-z0-9]`)

// ValidDNSLabel checks whether the provided string is a valid DNS label.
func ValidDNSLabel(label string) bool {
	if len(label) == 0 || len(label) > 63 {
		return false
	}

	lower := []byte(strings.ToLower(label))
	return legalDNSlabel.Match(lower) && minimalDNSlabel.Match(lower)
}

// ValidDNSName checks whether the provided name is a valid DNS name.  A DNS
// name may have multiple labels.  Each label must satisfy the same constraints
// as a Hostname, but the underscore character may be used anywhere.
func ValidDNSName(name string) bool {
	labels := strings.Split(name, ".")
	for _, label := range labels {
		if !ValidDNSLabel(label) {
			return false
		}
	}

	return true
}

// GenerateDNSName takes an arbitrary string and returns a derived string that
// can legally be used as a DNS name.  This string is generated by stripping out
// invalid characters, converting spaces and dashes into underscores, and
// limiting its length.  There is no guarantee that the returned string is
// unique.
func GenerateDNSName(name string) string {
	const maxLen = 16
	var last rune

	dns := ""
	for _, c := range name {
		if len(dns) >= maxLen {
			break
		}

		// turn spaces and underscores into dashes
		if c == ' ' || c == '_' {
			c = '-'
		}

		// Avoid starting a name with a dash.  Avoid multiple dashes in
		// a row.
		if c == '-' && (len(dns) == 0 || last == '-') {
			continue
		}

		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '-' {
			dns += string(c)
			last = c
		}
	}

	// Trim any trailing dashes
	dns = strings.TrimRight(dns, "-")

	return strings.ToLower(dns)
}

// ChoosePort returns a local port number which is currently not being used.  If
// passed no arguments, it will choose an ephemeral port number.  If passed one
// integer, it will check that port and return it if available.  If passed two
// integers, it will check ports in that range, inclusive of both ends, until it
// finds an available port, returning that.
//
// Port availability is checked only on localhost.
//
// Callers must be cognizant of the possibility of a race on the returned port.
// The port may be put into use by another party between the time it's checked
// in this function and the code the caller wants to be using it.
func ChoosePort(a ...int) (int, error) {
	var minPort, maxPort int
	errStr := "couldn't find available ephemeral port"

	if len(a) == 1 {
		minPort = a[0]
		maxPort = a[0]
		errStr = fmt.Sprintf("port %d not available", minPort)
	} else if len(a) == 2 {
		minPort = a[0]
		maxPort = a[1]
		if maxPort < minPort {
			panic("port range must be in ascending order")
		}
		errStr = fmt.Sprintf("no available ports between %d and %d",
			minPort, maxPort)
	} else if len(a) > 2 {
		panic("ChoosePort() can have a maximum of two arguments")
	}

	for port := minPort; port <= maxPort; port++ {
		addr := net.TCPAddr{
			IP:   net.IPv4(127, 0, 0, 1),
			Port: port,
		}

		l, err := net.ListenTCP("tcp", &addr)
		if err != nil {
			inUse := false
			if oe, ok := err.(*net.OpError); ok {
				if sce, ok := oe.Err.(*os.SyscallError); ok {
					if errno, ok := sce.Err.(syscall.Errno); ok {
						if errno == syscall.EADDRINUSE {
							inUse = true
						}
					}
				}
			}
			if inUse {
				continue
			}
			return 0, fmt.Errorf("unable to open a new port: %v", err)
		}
		port = l.Addr().(*net.TCPAddr).Port
		l.Close()
		return port, nil
	}

	return 0, fmt.Errorf(errStr)
}
