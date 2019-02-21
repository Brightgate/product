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
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

var (
	// Extract the two components of a DHCP option from lines like:
	//   domain_name_servers='192.168.52.1'
	//   vendor_class_identifier='Brightgate, Inc.'
	//   vendor_encapsulated_options='0109736174656c6c697465ff'
	rpiOptionRE = regexp.MustCompile(`(\w+)='(.*)'`)
)

const (
	ntpdSystemdService = "chrony.service"
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

func rpiParseNodeID(data []byte) (string, error) {
	s := string(data)
	if len(s) < 32 {
		return "", fmt.Errorf("does not contain a UUID")
	}

	uuidStr := fmt.Sprintf("%8s-%4s-%4s-%4s-%12s",
		s[0:8], s[8:12], s[12:16], s[16:20], s[20:32])
	return uuidStr, nil
}

func rpiSetNodeID(file, uuidStr string) error {
	return fmt.Errorf("setting the nodeID is unsupported")
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
	return strings.HasPrefix(mac, "b8:27:eb:")
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

func rpiGetDHCPInfo(iface string) (map[string]string, error) {
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
	options := rpiOptionRE.FindAllStringSubmatch(string(out), -1)
	for _, opt := range options {
		name := opt[1]
		val := opt[2]

		data[name] = strings.Trim(val, "'")
	}

	fileName := leaseDir + "dhcpcd-" + iface + ".lease"
	if f, err := os.Stat(fileName); err == nil {
		data["dhcp_lease_start"] = f.ModTime().Format(time.RFC3339)
	}

	return data, nil
}

func rpiDHCPPidfile(nic string) string {
	return "/var/run/dhcpcd.pid"
}

func rpiRunNTPDaemon() error {
	// "restart" will start the service if it's not already running.
	cmd := exec.Command("/bin/systemctl", "restart", ntpdSystemdService)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to restart %s: %v", ntpdSystemdService, err)
	}

	return nil
}

func init() {
	addPlatform(&Platform{
		name:          "rpi3",
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
		VconfigCmd:   "/sbin/vconfig",

		probe:         rpiProbe,
		parseNodeID:   rpiParseNodeID,
		setNodeID:     rpiSetNodeID,
		NicIsVirtual:  rpiNicIsVirtual,
		NicIsWireless: rpiNicIsWireless,
		NicIsWired:    rpiNicIsWired,
		NicIsWan:      rpiNicIsWan,
		NicID:         rpiNicGetID,
		NicLocation:   rpiNicLocation,

		GetDHCPInfo: rpiGetDHCPInfo,
		DHCPPidfile: rpiDHCPPidfile,

		RunNTPDaemon: rpiRunNTPDaemon,
		NtpdConfPath: "/etc/chrony/chrony.conf",
	})
}
