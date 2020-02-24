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
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"bg/common/bgioutil"
	"bg/common/mfg"
	"bg/common/release"

	"github.com/pkg/errors"
	"github.com/satori/uuid"
)

var (
	mtPlatform *Platform
)

const (
	uciCmd  = "/sbin/uci"
	ubusCmd = "/bin/ubus"
	fwCmd   = "/usr/sbin/fw_printenv"
)

var (
	mtMachineIDFile string

	ifaceDumpCache    *netDump
	ifaceDumpCachedAt time.Time
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
	idstr := strings.ToLower(string(data))

	if _, err := uuid.FromString(idstr); err == nil {
		return idstr, nil
	}

	idstr = string(data)
	if mfg.ValidExtSerial(idstr) {
		return idstr, nil
	}

	return "", fmt.Errorf("%s not a valid nodeID", idstr)
}

func mtSetNodeID(newNodeID string) error {
	if _, err := mtParseNodeID([]byte(newNodeID)); err != nil {
		return err
	}

	id := []byte(newNodeID)
	if err := bgioutil.WriteFileSync(mtMachineIDFile, id, 0644); err != nil {
		return fmt.Errorf("writing %s: %v", mtMachineIDFile, err)
	}

	nodeID = newNodeID
	return nil
}

func mtGetSerialNumber() ([]byte, error) {
	var serial []byte

	opts := []string{"-n", "bg_ext_serial"}
	out, err := exec.Command(fwCmd, opts...).CombinedOutput()
	if err == nil {
		test := strings.TrimSpace(string(out))
		if mfg.ValidExtSerial(test) {
			serial = []byte(test)
		} else {
			err = fmt.Errorf("no serial number found")
		}
	}
	return serial, err
}

func mtGetNodeID() (string, error) {
	var fwNodeID, cachedNodeID string

	// First check the machineID file to see if we've already discovered
	// and/or assigned an ID to this system.
	data, cacheErr := ioutil.ReadFile(mtMachineIDFile)
	if cacheErr == nil {
		cachedNodeID, cacheErr = mtParseNodeID(data)
	}

	data, err := mtGetSerialNumber()
	if err == nil {
		fwNodeID, err = mtParseNodeID(data)
		if (err == nil) && (fwNodeID != cachedNodeID) {
			// We cache the serial number in a file to protect
			// against fw corruption and so that non-root
			// users can access it.
			if serr := mtSetNodeID(fwNodeID); serr != nil {
				fmt.Printf("caching nodeID: %v\n", serr)
			}
		}
	}

	if err != nil && cacheErr == nil {
		return cachedNodeID, nil
	}

	return fwNodeID, err
}

func mtGenNodeID(model int) string {
	serial := mfg.NewExtSerialRandom(model)
	return serial.String()
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
type netAddr struct {
	Address  string `json:"address,omitempty"`
	MaskBits int    `json:"mask,omitempty"`
}

type netRoute struct {
	Target  string `json:"target,omitempty"`
	Mask    int    `json:"mask,omitempty"`
	NextHop string `json:"nexthop,omitempty"`
}

type dhcpData struct {
	LeaseTime int    `json:"leasetime,omitempty"`
	Opt43     string `json:"opt43,omitempty"`
	Opt60     string `json:"opt60,omitempty"`
}

type netConfig struct {
	Iface      string     `json:"interface,omitempty"`
	Up         bool       `json:"up,omitempty"`
	Proto      string     `json:"proto,omitempty"`
	Ipv4       []netAddr  `json:"ipv4-address,omitempty"`
	Routes     []netRoute `json:"route,omitempty"`
	DNSServers []string   `json:"dns-server,omitempty"`
	Domains    []string   `json:"dns-search,omitempty"`
	Data       dhcpData   `json:"data,omitempty"`
}

type netDump struct {
	Interfaces []netConfig `json:"interface,omitempty"`
}

func getIfaceDump(freshness time.Duration) (*netDump, error) {
	var d netDump

	if ifaceDumpCachedAt.Add(freshness).Before(time.Now()) {
		// Use the /bin/ubus utility to retrieve a json-formatted list
		// of all the network interface configurations
		args := []string{"call", "network.interface", "dump"}
		out, err := exec.Command(ubusCmd, args...).Output()
		if err != nil {
			return nil, fmt.Errorf("ubus failed: %v", err)
		}

		if err = json.Unmarshal(out, &d); err != nil {
			return nil, fmt.Errorf("unmarshaling: %v", err)
		}

		ifaceDumpCache = &d
		ifaceDumpCachedAt = time.Now()
	}

	return ifaceDumpCache, nil
}

func getNetConfig(nic string) (*netConfig, error) {
	var found *netConfig

	d, err := getIfaceDump(time.Second)
	if err == nil {
		// Look for a stanza describing the wan/dhcp settings
		for _, iface := range d.Interfaces {
			if iface.Iface == nic {
				found = &iface
				if iface.Proto == "dhcp" {
					break
				}
			}
		}

		if found == nil {
			err = fmt.Errorf("no config found for %s", nic)
		}
	}
	return found, err
}

func mtGetDHCPInterfaces() ([]string, error) {
	list := make([]string, 0)

	d, err := getIfaceDump(time.Minute)
	if err == nil {
		// Look for a stanza describing the wan/dhcp settings
		for _, iface := range d.Interfaces {
			if iface.Proto == "dhcp" {
				list = append(list, iface.Iface)
			}
		}
	}

	return list, err
}

func mtGetDHCPInfo(iface string) (map[string]string, error) {
	w, err := getNetConfig(iface)
	if w == nil {
		return nil, err
	}
	if w.Proto != "dhcp" {
		return nil, fmt.Errorf("%s not configured for DHCP", iface)
	}

	data := make(map[string]string)
	if len(w.Ipv4) > 0 {
		data["ip_address"] = fmt.Sprintf("%s/%d",
			w.Ipv4[0].Address, w.Ipv4[0].MaskBits)
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

// Fetch the current network config from OpenWRT
func mtGetProps(nic string) (map[string]string, error) {
	props := make(map[string]string)

	nc, err := getNetConfig(nic)
	if err != nil {
		return props, err
	}

	props["proto"] = nc.Proto
	if nc.Proto == "dhcp" {
		// the 'reqopts' field isn't included in the dump for some
		// reason.  We assume that it was set along with the dhcp proto.
		props["reqopts"] = "60 43"
	} else if len(nc.Ipv4) > 0 {
		bits := strconv.Itoa(nc.Ipv4[0].MaskBits)
		props["ipaddr"] = nc.Ipv4[0].Address + "/" + bits
		if len(nc.Routes) > 0 {
			props["gateway"] = nc.Routes[0].NextHop
		}
	}

	if len(nc.DNSServers) > 0 {
		props["dns"] = nc.DNSServers[0]
	}

	return props, nil
}

// Update the current OpenWRT network config
func mtNetConfig(nic, proto, ipaddr, gw, dnsserver string) error {
	all := []string{"proto", "ipaddr", "dns", "gateway", "reqopts"}

	current, err := mtGetProps(nic)

	// If we couldn't get the current config, assume we need to reset all
	// the properties
	force := (err != nil)

	// Construct a list of all the OpenWRT settings needed to persist this
	// configuration
	newProps := make(map[string]string)
	switch proto {
	case "dhcp":
		newProps["proto"] = "dhcp"
		newProps["reqopts"] = "60 43"
		if dnsserver == "" {
			newProps["dns"] = current["dns"]
		} else {
			newProps["dns"] = dnsserver
		}
	case "static":
		newProps["proto"] = "static"
		newProps["gateway"] = gw
		newProps["ipaddr"] = ipaddr
		newProps["dns"] = dnsserver

	default:
		return fmt.Errorf("unsupported protocol: %s", proto)
	}

	// Compare the new settings with the current settings to determine
	// whether we need to change anything
	updated := false
	for _, prop := range all {
		full := "network.wan." + prop
		val := newProps[prop]
		if !force && (val == current[prop]) {
			continue
		}

		updated = true
		if val != "" {
			cmd := full + "=" + val
			_, err := exec.Command(uciCmd, "set", cmd).Output()
			if err != nil {
				return fmt.Errorf("set %s failed: %v", cmd, err)
			}
		} else {
			// Delete any properties that aren't set.  Ignore
			// errors, since they are likely just letting us know
			// that the property is already unset.
			_, _ = exec.Command(uciCmd, "delete", full).Output()
		}
	}

	if updated {
		if out, err := exec.Command(uciCmd, "commit").Output(); err != nil {
			return fmt.Errorf("uci commit failed: %v", out)
		}
		if err := mtServiceOp("network", "reload"); err != nil {
			return err
		}
		if err := mtServiceOp("network", "restart"); err != nil {
			return err
		}
	}

	return nil
}

// On the MT7623, we don't have a battery-backed clock, so the time gets reset
// on every reboot.  There's a sysfixtime service that runs early in boot and
// grabs the timestamp of the newest file in /etc and sets the system time to
// that, so we take advantage of that by touching a file there periodically to
// make sure it's as up-to-date as possible.  It's no substitute for getting
// network time, but if that doesn't work, or takes too long to sync, this is a
// reasonable backup strategy.
func mtMaintainTime() {
	for {
		time.Sleep(time.Minute)
		if file, _ := os.Create("/etc/.bg-timestamp"); file != nil {
			file.Close()
		}
	}
}

func mtServiceOp(service, op string) error {
	path := "/etc/init.d/" + service
	cmd := exec.Command(path, op)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to %s %s: %v", op, service, err)
	}

	return nil
}

func mtRestartService(service string) error {
	return mtServiceOp(service, "restart")
}

func mtUpgrade(rel release.Release) ([]byte, error) {
	downloadDir := mtPlatform.ExpandDirPath(APData, "release",
		rel.Release.UUID.String())

	apFactory := mtPlatform.ExpandDirPath(APPackage, "bin", "ap-factory")

	pkgs := rel.FilenameByPattern("*.ipk")
	args := []string{"install", "-C", "-d", downloadDir}
	for _, pkg := range pkgs {
		args = append(args, "-P", filepath.Join(downloadDir, pkg))
	}

	// Make sure other side is unmounted, or the install will fail.  If this
	// fails, try anyway.
	umount := exec.Command(apFactory, "umount-other")
	_ = umount.Run()

	cmd := exec.Command(apFactory, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return output, errors.Wrapf(err, "failed to upgrade (%s):\n%s",
			cmd.Args, output)
	}
	return output, nil
}

func mtDataDir() string {
	return "__APROOT__/data"
}

func init() {
	mtPlatform = &Platform{
		name:             "mt7623",
		CefDeviceProduct: "Model 100",

		ResetSignal:  syscall.SIGINT,
		ReloadSignal: syscall.SIGHUP,
		HostapdCmd:   "/usr/sbin/hostapd",
		SysctlCmd:    "/sbin/sysctl",
		IPCmd:        "/sbin/ip",
		IwCmd:        "/usr/sbin/iw",
		IPTablesCmd:  "/usr/sbin/iptables",
		EthtoolCmd:   "/usr/sbin/ethtool",
		DigCmd:       "/usr/bin/dig",
		CurlCmd:      "/usr/bin/curl",
		RestoreCmd:   "/usr/sbin/iptables-restore",

		probe:         mtProbe,
		setNodeID:     mtSetNodeID,
		getNodeID:     mtGetNodeID,
		GenNodeID:     mtGenNodeID,
		NicIsVirtual:  mtNicIsVirtual,
		NicIsWireless: mtNicIsWireless,
		NicIsWired:    mtNicIsWired,
		NicIsWan:      mtNicIsWan,
		NicID:         mtNicGetID,
		NicLocation:   mtNicLocation,
		DataDir:       mtDataDir,

		GetDHCPInterfaces: mtGetDHCPInterfaces,
		GetDHCPInfo:       mtGetDHCPInfo,
		DHCPPidfile:       mtDHCPPidfile,

		NetworkManaged: true,
		NetConfig:      mtNetConfig,

		NtpdService:    "chronyd",
		MaintainTime:   mtMaintainTime,
		RestartService: mtRestartService,

		Upgrade: mtUpgrade,
	}
	addPlatform(mtPlatform)

	mtMachineIDFile = mtPlatform.ExpandDirPath(mtDataDir(), "mcp/serial")
}
