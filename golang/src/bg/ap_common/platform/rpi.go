/*
 * Copyright 2020 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package platform

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"

	"bg/common/release"

	"github.com/pkg/errors"
)

var (
	rpiPlatform *Platform
)

func rpiProbe() bool {
	const devFile = "/proc/device-tree/model"

	if data, err := ioutil.ReadFile(devFile); err == nil {
		devMatch := regexp.MustCompile("^Raspberry Pi 3.*")
		if devMatch.Match(data) {
			return true
		}
	}
	return false
}

func rpiNicIsVirtual(nic string) bool {
	return strings.HasPrefix(nic, "eth") && strings.Contains(nic, ".")
}

func rpiNicIsWireless(nic string) bool {
	return strings.HasPrefix(nic, "wlan")
}

func rpiNicIsWired(nic string) bool {
	return strings.HasPrefix(nic, "eth")
}

func rpiNicIsWan(name, mac string) bool {
	// On Raspberry Pi 3, use the OUI to identify the
	// on-board port.
	return rpiNicIsWired(name) && strings.HasPrefix(mac, "b8:27:eb:")
}

func rpiNicGetID(name, mac string) string {
	return mac
}

func rpiNicLocation(name string) string {
	path, err := filepath.EvalSymlinks("/sys/class/net/" + name + "/device")
	if err != nil {
		return ""
	}
	fn := filepath.Base(path)
	if strings.Contains(path, "/mmc") {
		if fn == "mmc1:0001:1" {
			return "onboard wifi"
		}
	} else if strings.Contains(path, "/usb") {
		desc := map[string]string{
			"1-1.2:1.0": "upper left USB port",
			"1-1.3:1.0": "upper right USB port",
			"1-1.4:1.0": "lower left USB port",
			"1-1.5:1.0": "lower right USB port",
		}[fn]
		if desc == "" {
			desc = "unknown USB"
		}
		return fmt.Sprintf("%s (%s)", desc, fn)
	}
	return ""
}

func rpiUpgrade(rel release.Release) ([]byte, error) {
	downloadDir := rpiPlatform.ExpandDirPath(APData, "release", rel.Release.UUID.String())

	pkgs := rel.FilenameByPattern("*.deb")
	args := []string{"-i"}
	for _, pkg := range pkgs {
		args = append(args, filepath.Join(downloadDir, pkg))
	}

	cmd := exec.Command("/usr/bin/dpkg", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return output, errors.Wrapf(err, "failed to upgrade (%s):\n%s",
			cmd.Args, output)
	}

	// Stash a symlink to release.json
	linkDir := rpiPlatform.ExpandDirPath(APPackage, "etc")
	relPath, err := filepath.Rel(linkDir,
		filepath.Join(downloadDir, "release.json"))
	if err != nil {
		return output, errors.Wrap(err, "failed to establish relative path")
	}

	curLinkPath := filepath.Join(linkDir, "release.json")
	// Remove the link so the creation won't fail.
	err = os.Remove(curLinkPath)
	if perr, ok := err.(*os.PathError); ok {
		if serr, ok := perr.Err.(syscall.Errno); ok {
			if serr == syscall.ENOENT {
				err = nil
			}
		}
	}
	if err != nil {
		return output, errors.Wrap(err, "failed to remove old release symlink")
	}

	if err = os.Symlink(relPath, curLinkPath); err != nil {
		return output, errors.Wrap(err, "failed to create release symlink")
	}

	return output, nil
}

func rpiDataDir() string {
	return LSBDataDir
}

func init() {
	rpiPlatform = &Platform{
		name:             "rpi3",
		CefDeviceProduct: "Test RPi",

		ResetSignal:  syscall.SIGINT,
		ReloadSignal: syscall.SIGINT,
		HostapdCmd:   "/usr/sbin/hostapd",
		SysctlCmd:    "/sbin/sysctl",
		IPCmd:        "/sbin/ip",
		IwCmd:        "/sbin/iw",
		IPTablesCmd:  "/sbin/iptables",
		EthtoolCmd:   "/sbin/ethtool",
		DigCmd:       "/usr/bin/dig",
		CurlCmd:      "/usr/bin/curl",
		RestoreCmd:   "/sbin/iptables-restore",

		probe:         rpiProbe,
		setNodeID:     debianSetNodeID,
		getNodeID:     debianGetNodeID,
		GenNodeID:     debianGenNodeID,
		NicIsVirtual:  rpiNicIsVirtual,
		NicIsWireless: rpiNicIsWireless,
		NicIsWired:    rpiNicIsWired,
		NicIsWan:      rpiNicIsWan,
		NicID:         rpiNicGetID,
		NicLocation:   rpiNicLocation,
		DataDir:       rpiDataDir,

		GetDHCPInterfaces: debianGetDHCPInterfaces,
		GetDHCPInfo:       debianGetDHCPInfo,
		DHCPPidfile:       debianDHCPPidfile,

		NetworkManaged: false,
		NetConfig:      debianNetConfig,

		NtpdService:    "chrony",
		MaintainTime:   func() {},
		RestartService: debianRestartService,

		Upgrade: rpiUpgrade,
	}
	addPlatform(rpiPlatform)
}

