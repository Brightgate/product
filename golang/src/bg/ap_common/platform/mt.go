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
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"

	"github.com/satori/uuid"
)

func mtProbe() bool {
	const devFile = "/proc/device-tree/model"

	if data, err := ioutil.ReadFile(devFile); err == nil {
		devMatch := regexp.MustCompile("^UniElec.*")
		if devMatch.Match(data) {
			return true
		}
	}

	return false
}

func mtParseNodeID(data []byte) (string, error) {
	return string(data), nil
}

func mtSetNodeID(file, uuidStr string) error {
	if nodeID != "" {
		return fmt.Errorf("existing nodeID can't be reset")
	}

	if _, err := uuid.FromString(uuidStr); err != nil {
		return fmt.Errorf("Failed to parse %s as a UUID: %v",
			uuidStr, err)
	}

	id := []byte(uuidStr)
	if err := ioutil.WriteFile(file, id, 0644); err != nil {
		return fmt.Errorf("failed to write %s: %v", file, err)
	}

	nodeID = uuidStr
	return nil
}

func mtNicIsVirtual(nic string) bool {
	return (strings.HasPrefix(nic, "lan") || strings.HasPrefix(nic, "wan")) &&
		strings.Contains(nic, ".")
}

func mtNicIsWireless(nic string) bool {
	return strings.HasPrefix(nic, "wlan")
}

func mtNicIsWired(nic string) bool {
	return strings.HasPrefix(nic, "wan") || strings.HasPrefix(nic, "lan")
}

func mtNicIsWan(name, mac string) bool {
	return strings.HasPrefix(name, "wan")
}

func mtNicGetID(name, mac string) string {
	return name
}

func mtNicLocation(name string) string {
	path, err := filepath.EvalSymlinks("/sys/class/net/" + name + "/device")
	if err != nil {
		return ""
	}
	fn := filepath.Base(path)
	if strings.Contains(path, "/pci") {
		// This gives us the slot name, assuming it's in a PCI path
		// 0000:02:00.0 == "slot" 2
		// domain:bus:slot.function
		addr := strings.Split(fn, ":")
		if len(addr) == 3 && addr[0] == "0000" && addr[2] == "00.0" {
			return fmt.Sprintf("PCI slot %s (%s)",
				strings.TrimLeft(addr[1], "0"), fn)
		}
		return fmt.Sprintf("PCI (%s)", fn)
	} else if strings.Contains(path, "/usb") {
		return fmt.Sprintf("unknown USB (%s)", fn)
	}
	return ""
}

// The following structs are all used as targets when unmarshaling the JSON data
// returned by ubus.
type dhcpAddr struct {
	Address  string `json:"address,omitempty"`
	MaskBits int    `json:"mask,omitempty"`
}

type dhcpRoute struct {
	Target  string `json:"target,omitempty"`
	Mask    int    `json:"mask,omitempty"`
	NextHop string `json:"nexthop,omitempty"`
}

type dhcpData struct {
	LeaseTime int    `json:"leasetime,omitempty"`
	Opt43     string `json:"opt43,omitempty"`
	Opt60     string `json:"opt60,omitempty"`
}

type dhcpInterface struct {
	Iface   string      `json:"interface,omitempty"`
	Up      bool        `json:"up,omitempty"`
	Proto   string      `json:"proto,omitempty"`
	Ipv4    []dhcpAddr  `json:"ipv4-address,omitempty"`
	Routes  []dhcpRoute `json:"route,omitempty"`
	Domains []string    `json:"dns-search,omitempty"`
	Data    dhcpData    `json:"data,omitempty"`
}

type dhcpDump struct {
	Interfaces []dhcpInterface `json:"interface,omitempty"`
}

func getDHCP(nic string) (*dhcpInterface, error) {
	var d dhcpDump

	// Use the /bin/ubus utility to retrieve a json-formatted list of all
	// the network interface configurations
	args := []string{"call", "network.interface.wan", "dump"}
	out, err := exec.Command("/bin/ubus", args...).Output()
	if err != nil {
		return nil, fmt.Errorf("ubus failed: %v", err)
	}

	if err = json.Unmarshal(out, &d); err != nil {
		return nil, fmt.Errorf("unmarshaling: %v", err)
	}

	// Look for a stanza describing the wan/dhcp settings
	for _, iface := range d.Interfaces {
		if iface.Iface == nic && iface.Proto == "dhcp" {
			return &iface, nil
		}
	}

	return nil, nil
}

func mtGetDHCPInfo(iface string) (map[string]string, error) {
	w, err := getDHCP(iface)
	if w == nil {
		return nil, err
	}

	data := make(map[string]string)
	if len(w.Ipv4) > 0 {
		data["ip_address"] = w.Ipv4[0].Address
		data["subnet_cidr"] = strconv.Itoa(w.Ipv4[0].MaskBits)
	}
	if len(w.Routes) > 0 {
		data["routers"] = w.Routes[0].NextHop
	}
	if len(w.Domains) > 0 {
		data["domain_name"] = w.Domains[0]
	}
	if len(w.Data.Opt60) > 0 {
		if decoded, err := hex.DecodeString(w.Data.Opt60); err == nil {
			data["vendor_class_identifier"] = string(decoded)
		}
	}
	data["vendor_encapsulated_options"] = w.Data.Opt43
	data["dhcp_lease_start"] = ""
	data["dhcp_lease_time"] = strconv.Itoa(w.Data.LeaseTime)

	return data, nil
}

func mtDHCPPidfile(nic string) string {
	return "/var/run/udhcpc-" + nic + ".pid"
}

func mtRestartService(service string) error {
	path := "/etc/init.d/" + service
	cmd := exec.Command(path, "restart")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to restart %s: %v", service, err)
	}

	return nil
}

func mtDataDir() string {
	return "__APROOT__/data"
}

func init() {
	addPlatform(&Platform{
		name:          "mediatek",
		machineIDFile: "/data/mcp/machine-id",

		ResetSignal:  syscall.SIGINT,
		ReloadSignal: syscall.SIGHUP,
		HostapdCmd:   "/usr/sbin/hostapd",
		BrctlCmd:     "/usr/sbin/brctl",
		SysctlCmd:    "/sbin/sysctl",
		IPCmd:        "/sbin/ip",
		IwCmd:        "/usr/sbin/iw",
		IPTablesCmd:  "/usr/sbin/iptables",
		RestoreCmd:   "/usr/sbin/iptables-restore",
		VconfigCmd:   "/sbin/vconfig",

		probe:         mtProbe,
		parseNodeID:   mtParseNodeID,
		setNodeID:     mtSetNodeID,
		NicIsVirtual:  mtNicIsVirtual,
		NicIsWireless: mtNicIsWireless,
		NicIsWired:    mtNicIsWired,
		NicIsWan:      mtNicIsWan,
		NicID:         mtNicGetID,
		NicLocation:   mtNicLocation,
		DataDir:       mtDataDir,

		GetDHCPInfo: mtGetDHCPInfo,
		DHCPPidfile: mtDHCPPidfile,

		NtpdConfPath:   "/var/etc/chrony.conf",
		NtpdService:    "chronyd",
		RestartService: mtRestartService,
	})
}
