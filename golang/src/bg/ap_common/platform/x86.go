/*
 * COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package platform

import (
	"fmt"
	"io/ioutil"
	"regexp"
	"syscall"
)

func x86Probe() bool {
	const devFile = "/proc/version"

	if data, err := ioutil.ReadFile(devFile); err == nil {
		devMatch := regexp.MustCompile("^Linux.*-amd64.*Debian.*")
		if devMatch.Match(data) {
			return true
		}
	}

	return false
}

func x86ParseNodeID(data []byte) (string, error) {
	return rpiParseNodeID(data)
}

func x86SetNodeID(file, uuidStr string) error {
	return fmt.Errorf("setting the nodeID is unsupported")
}

func x86NicIsVirtual(nic string) bool {
	return false
}

func x86NicIsWireless(nic string) bool {
	return false
}

func x86NicIsWired(nic string) bool {
	return false
}

func x86NicIsWan(name, mac string) bool {
	return false
}

func x86NicGetID(name, mac string) string {
	return name
}

func x86NicLocation(name string) string {
	return ""
}

func x86GetDHCPInfo(iface string) (map[string]string, error) {
	return rpiGetDHCPInfo(iface)
}

func x86DHCPPidfile(nic string) string {
	return ""
}

func init() {
	addPlatform(&Platform{
		name:          "x86-debian",
		machineIDFile: "/etc/machine-id",

		ResetSignal:  syscall.SIGINT,
		ReloadSignal: syscall.SIGINT,
		HostapdCmd:   "/usr/sbin/hostapd",
		BrctlCmd:     "/sbin/brctl",
		SysctlCmd:    "/sbin/sysctl",
		IPCmd:        "/sbin/ip",
		IwCmd:        "/sbin/iw",
		IPTablesCmd:  "/sbin/iptables",
		RestoreCmd:   "/sbin/iptables-restore",
		VconfigCmd:   "/opt/bin/vconfig",

		probe:         x86Probe,
		parseNodeID:   x86ParseNodeID,
		setNodeID:     x86SetNodeID,
		NicIsVirtual:  x86NicIsVirtual,
		NicIsWireless: x86NicIsWireless,
		NicIsWired:    x86NicIsWired,
		NicIsWan:      x86NicIsWan,
		NicID:         x86NicGetID,
		NicLocation:   x86NicLocation,

		GetDHCPInfo: x86GetDHCPInfo,
		DHCPPidfile: x86DHCPPidfile,
	})
}
