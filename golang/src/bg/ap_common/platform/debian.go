/*
 * Copyright 2020 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package platform

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/satori/uuid"
)

var (
	// Extract the two components of a DHCP option from lines like:
	//   domain_name_servers='192.168.52.1'
	//   vendor_class_identifier='Brightgate, Inc.'
	//   vendor_encapsulated_options='0109736174656c6c697465ff'
	debianOptionRE = regexp.MustCompile(`(\w+)='(.*)'`)
)

const (
	machineIDFile  = "/etc/machine-id"
	dhcpcdConfFile = "/etc/dhcpcd.conf"
)

func debianParseNodeID(data []byte) (string, error) {
	s := string(data)
	if len(s) < 32 {
		return "", fmt.Errorf("does not contain a UUID")
	}

	uuidStr := fmt.Sprintf("%8s-%4s-%4s-%4s-%12s",
		s[0:8], s[8:12], s[12:16], s[16:20], s[20:32])
	return uuidStr, nil
}

func debianGenNodeID(model int) string {
	return uuid.NewV4().String()
}

func debianSetNodeID(uuidStr string) error {
	return fmt.Errorf("setting the nodeID is unsupported")
}

func debianGetNodeID() (string, error) {
	// This is primarily a developer override
	e := os.Getenv("B10E_NODEID")
	if e != "" {
		nodeID = e
		return nodeID, nil
	}

	data, err := ioutil.ReadFile(machineIDFile)
	if err != nil {
		return "", fmt.Errorf("reading %s: %v", machineIDFile, err)
	}

	id, err := debianParseNodeID(data)

	if err == nil {
		nodeID = id
	}
	return nodeID, nil
}

func addRegexps(l []string, addTo map[string]*regexp.Regexp) {
	for _, f := range l {
		var exp string

		// Convert the dhcpcd.conf regexps into Go-style
		for _, c := range f {
			if c == '*' {
				exp += `.*`
			} else if c == '.' {
				exp += `\.`
			} else {
				exp += string(c)
			}
		}

		re, err := regexp.Compile(exp)
		if err != nil {
			fmt.Printf("failed: %v\n", err)
		} else {
			addTo[exp] = re
		}
	}
}

func debianGetDHCPInterfaces() ([]string, error) {
	list := make([]string, 0)
	allow := make(map[string]*regexp.Regexp)
	deny := make(map[string]*regexp.Regexp)

	file, err := os.Open(dhcpcdConfFile)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %v", dhcpcdConfFile, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		f := strings.Fields(scanner.Text())
		if len(f) < 2 {
			continue
		}
		if strings.EqualFold(f[0], "allowinterfaces") {
			addRegexps(f[1:], allow)
		} else if strings.EqualFold(f[0], "denyinterfaces") {
			addRegexps(f[1:], deny)
		}
	}

	all, err := net.Interfaces()
	for _, iface := range all {
		var allowed bool

		for _, re := range allow {
			if allowed = re.MatchString(iface.Name); allowed {
				break
			}
		}
		for _, re := range deny {
			if re.MatchString(iface.Name) {
				allowed = false
				break
			}
		}

		if allowed {
			list = append(list, iface.Name)
		}
	}

	return list, err
}

func debianGetDHCPInfo(iface string) (map[string]string, error) {
	const dhcpDump = "/sbin/dhcpcd"
	const leaseDir = "/var/lib/dhcpcd5/"

	data := make(map[string]string)
	out, err := exec.Command(dhcpDump, "-4", "-U", iface).Output()
	if err != nil {
		return data, fmt.Errorf("failed to get lease data for %s: %v",
			iface, err)
	}

	// Each line in the dump output is structured as key='val'.
	// We generate a key-indexed map, with the single quotes stripped from
	// the value.
	options := debianOptionRE.FindAllStringSubmatch(string(out), -1)
	for _, opt := range options {
		name := opt[1]
		val := opt[2]

		data[name] = strings.Trim(val, "'")
	}

	// Convert the simple assigned address into a CIDR
	if addr, ok := data["ip_address"]; ok {
		bits, ok := data["subnet_cidr"]
		if !ok {
			bits = "24"
		}
		data["ip_address"] = addr + "/" + bits
	}

	fileName := leaseDir + "dhcpcd-" + iface + ".lease"
	if f, err := os.Stat(fileName); err == nil {
		data["dhcp_lease_start"] = f.ModTime().Format(time.RFC3339)
	}

	return data, nil
}

func debianDHCPPidfile(nic string) string {
	return "/var/run/dhcpcd.pid"
}

func debianNetConfig(nic, proto, ipaddr, gw, dnsserver string) error {
	if proto != "dhcp" {
		return fmt.Errorf("unsupported protocol: %s", proto)
	}
	// XXX: add support for static IP addresses

	return nil
}

func debianRestartService(service string) error {
	cmd := exec.Command("/bin/systemctl", "restart", service+".service")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to restart %s: %v", service, err)
	}

	return nil
}

