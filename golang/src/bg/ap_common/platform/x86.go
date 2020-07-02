/*
 * COPYRIGHT 2020 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

// Note - this 'x86' platform is specifically intended to support the http-dev
// and cloudapp models on Google Cloud systems.  Moving to a real appliance or a
// different cloud provider will likely require updating this support.

package platform

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"

	"bg/common/release"
)

var (
	x86Platform *Platform
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

func x86NicIsVirtual(nic string) bool {
	return false
}

func x86NicIsWireless(nic string) bool {
	return false
}

func x86NicIsWired(nic string) bool {
	return strings.HasPrefix(nic, "eth")
}

func x86NicIsWan(name, mac string) bool {
	return name == "eth0"
}

func x86NicGetID(name, mac string) string {
	return name
}

func x86NicLocation(name string) string {
	return ""
}

// This is a very simplistic "upgrade" procedure that doesn't change any of the
// bits on disk, but does put the release.json symlink in place so that ap.rpcd
// can report the release to the cloud.  This is only used in testing.
func x86Upgrade(rel release.Release) ([]byte, error) {
	linkDir := x86Platform.ExpandDirPath(APPackage, "etc")
	relDir := x86Platform.ExpandDirPath(APData, "release", rel.Release.UUID.String())

	relPath, err := filepath.Rel(linkDir, filepath.Join(relDir, "release.json"))
	if err != nil {
		return nil, err
	}
	curLinkPath := filepath.Join(linkDir, "release.json")

	os.Remove(curLinkPath)
	if err = os.Symlink(relPath, curLinkPath); err != nil {
		return nil, err
	}

	return []byte("This is\nlots of\noutput\nwoo\nhoo\n"), nil
}

func x86DataDir() string {
	return LSBDataDir
}

func init() {
	x86Platform = &Platform{
		name:             "x86",
		CefDeviceProduct: "Test x86",

		ResetSignal:  syscall.SIGINT,
		ReloadSignal: syscall.SIGINT,
		HostapdCmd:   "/usr/sbin/hostapd",
		SysctlCmd:    "/sbin/sysctl",
		IPCmd:        "/sbin/ip",
		IwCmd:        "/sbin/iw",
		IPTablesCmd:  "/sbin/iptables",
		EthtoolCmd:   "/sbin/ethtool",
		CurlCmd:      "/usr/bin/curl",
		DigCmd:       "/usr/bin/dig",
		RestoreCmd:   "/sbin/iptables-restore",

		probe:         x86Probe,
		setNodeID:     debianSetNodeID,
		getNodeID:     debianGetNodeID,
		GenNodeID:     debianGenNodeID,
		NicIsVirtual:  x86NicIsVirtual,
		NicIsWireless: x86NicIsWireless,
		NicIsWired:    x86NicIsWired,
		NicIsWan:      x86NicIsWan,
		NicID:         x86NicGetID,
		NicLocation:   x86NicLocation,
		DataDir:       x86DataDir,

		GetDHCPInterfaces: debianGetDHCPInterfaces,
		GetDHCPInfo:       debianGetDHCPInfo,
		DHCPPidfile:       debianDHCPPidfile,

		NetworkManaged: false,
		NetConfig:      debianNetConfig,

		NtpdService:    "ntpd",
		MaintainTime:   func() {},
		RestartService: debianRestartService,

		Upgrade: x86Upgrade,
	}
	addPlatform(x86Platform)
}
