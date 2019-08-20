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

	"bg/common/release"

	"github.com/pkg/errors"
)

var (
	// Extract the two components of a DHCP option from lines like:
	//   domain_name_servers='192.168.52.1'
	//   vendor_class_identifier='Brightgate, Inc.'
	//   vendor_encapsulated_options='0109736174656c6c697465ff'
	rpiOptionRE = regexp.MustCompile(`(\w+)='(.*)'`)

	rpiPlatform *Platform
)

const (
	ntpdSystemdService = "chrony.service"
	rpiMachineIDFile   = "/etc/machine-id"
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

func rpiSetNodeID(uuidStr string) error {
	return fmt.Errorf("setting the nodeID is unsupported")
}

func rpiGetNodeID() (string, error) {
	data, err := ioutil.ReadFile(rpiMachineIDFile)
	if err != nil {
		return "", fmt.Errorf("reading %s: %v", rpiMachineIDFile, err)
	}

	id, err := rpiParseNodeID(data)

	if err == nil {
		nodeID = id
	}
	return nodeID, nil
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

func rpiDHCPPidfile(nic string) string {
	return "/var/run/dhcpcd.pid"
}

func rpiNetConfig(nic, proto, ipaddr, gw, dnsserver string) error {
	if proto != "dhcp" {
		return fmt.Errorf("unsupported protocol: %s", proto)
	}

	return nil
}

func rpiRestartService(service string) error {
	cmd := exec.Command("/bin/systemctl", "restart", service+".service")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to restart %s: %v", service, err)
	}

	return nil
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
		name: "rpi3",

		ResetSignal:  syscall.SIGINT,
		ReloadSignal: syscall.SIGINT,
		HostapdCmd:   "/usr/sbin/hostapd",
		BrctlCmd:     "/sbin/brctl",
		SysctlCmd:    "/sbin/sysctl",
		IPCmd:        "/sbin/ip",
		IwCmd:        "/sbin/iw",
		IPTablesCmd:  "/sbin/iptables",
		EthtoolCmd:   "/sbin/ethtool",
		DigCmd:       "/usr/bin/dig",
		CurlCmd:      "/usr/bin/curl",
		RestoreCmd:   "/sbin/iptables-restore",
		VconfigCmd:   "/sbin/vconfig",

		probe:         rpiProbe,
		setNodeID:     rpiSetNodeID,
		getNodeID:     rpiGetNodeID,
		NicIsVirtual:  rpiNicIsVirtual,
		NicIsWireless: rpiNicIsWireless,
		NicIsWired:    rpiNicIsWired,
		NicIsWan:      rpiNicIsWan,
		NicID:         rpiNicGetID,
		NicLocation:   rpiNicLocation,
		DataDir:       rpiDataDir,

		GetDHCPInfo: rpiGetDHCPInfo,
		DHCPPidfile: rpiDHCPPidfile,

		NetworkManaged: false,
		NetConfig:      rpiNetConfig,

		NtpdService:    "chrony",
		MaintainTime:   func() {},
		RestartService: rpiRestartService,

		Upgrade: rpiUpgrade,
	}
	addPlatform(rpiPlatform)
}
