/*
 * COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
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
	"os/exec"
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

func x86SetNodeID(uuidStr string) error {
	return fmt.Errorf("setting the nodeID is unsupported")
}

func x86GetNodeID() (string, error) {
	return rpiGetNodeID()
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

func x86NetConfig(nic, proto, ipaddr, gw, dnsserver string) error {
	return nil
}

func x86RestartService(service string) error {
	cmd := exec.Command("/bin/systemctl", "restart", service+".service")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to restart %s: %v", service, err)
	}

	return nil
}

func x86DataDir() string {
	return LSBDataDir
}

func init() {
	addPlatform(&Platform{
		name: "x86-debian",

		ResetSignal:  syscall.SIGINT,
		ReloadSignal: syscall.SIGINT,
		HostapdCmd:   "/usr/sbin/hostapd",
		BrctlCmd:     "/sbin/brctl",
		SysctlCmd:    "/sbin/sysctl",
		IPCmd:        "/sbin/ip",
		IwCmd:        "/sbin/iw",
		IPTablesCmd:  "/sbin/iptables",
		EthtoolCmd:   "/sbin/ethtool",
		CurlCmd:      "/usr/bin/curl",
		DigCmd:       "/usr/bin/dig",
		RestoreCmd:   "/sbin/iptables-restore",
		VconfigCmd:   "/opt/bin/vconfig",

		probe:         x86Probe,
		setNodeID:     x86SetNodeID,
		getNodeID:     rpiGetNodeID,
		NicIsVirtual:  x86NicIsVirtual,
		NicIsWireless: x86NicIsWireless,
		NicIsWired:    x86NicIsWired,
		NicIsWan:      x86NicIsWan,
		NicID:         x86NicGetID,
		NicLocation:   x86NicLocation,
		DataDir:       x86DataDir,

		GetDHCPInfo: x86GetDHCPInfo,
		DHCPPidfile: x86DHCPPidfile,

		NetworkManaged: false,
		NetConfig:      x86NetConfig,

		NtpdService:    "chrony",
		MaintainTime:   func() {},
		RestartService: x86RestartService,
	})
}
