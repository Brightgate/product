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
	"log"
	"os"
	"regexp"
	"strings"
	"sync"
	"syscall"

	"github.com/satori/uuid"
)

// Platform is used to encapsulate the differences between the different
// hardware platforms we support as appliances.
type Platform struct {
	name          string
	machineIDFile string

	ResetSignal  syscall.Signal
	ReloadSignal syscall.Signal
	HostapdCmd   string
	BrctlCmd     string
	SysctlCmd    string
	IPCmd        string
	IwCmd        string
	IPTablesCmd  string
	RestoreCmd   string
	VconfigCmd   string

	probe         func() bool
	parseNodeID   func([]byte) (string, error)
	setNodeID     func(string, string) error
	NicIsVirtual  func(string) bool
	NicIsWireless func(string) bool
	NicIsWired    func(string) bool
	NicIsWan      func(string, string) bool
	NicID         func(string, string) string
}

var (
	platformLock sync.Mutex
	platform     *Platform
	nodeID       string

	rpiPlatform = Platform{
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
	}

	mtPlatform = Platform{
		name:          "mediatek",
		machineIDFile: "/opt/etc/machine-id",

		ResetSignal:  syscall.SIGINT,
		ReloadSignal: syscall.SIGHUP,
		HostapdCmd:   "/opt/bin/hostapd",
		BrctlCmd:     "/usr/sbin/brctl",
		SysctlCmd:    "/sbin/sysctl",
		IPCmd:        "/sbin/ip",
		IwCmd:        "/usr/sbin/iw",
		IPTablesCmd:  "/usr/sbin/iptables",
		RestoreCmd:   "/usr/sbin/iptables-restore",
		VconfigCmd:   "/opt/bin/vconfig",

		probe:         mtProbe,
		parseNodeID:   mtParseNodeID,
		setNodeID:     mtSetNodeID,
		NicIsVirtual:  mtNicIsVirtual,
		NicIsWireless: mtNicIsWireless,
		NicIsWired:    mtNicIsWired,
		NicIsWan:      mtNicIsWan,
		NicID:         mtNicGetID,
	}

	x86Platform = Platform{
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
	}
	knownPlatforms = []*Platform{&rpiPlatform, &mtPlatform, &x86Platform}
)

/******************************************************************
 *
 * Shared utility routines
 */

// NewPlatform detects the platform being run on, and returns a handle to that
// platform's structure.
func NewPlatform() *Platform {
	platformLock.Lock()
	defer platformLock.Unlock()

	if platform != nil {
		return platform
	}

	// Allow the caller to force a platform selection using the APPLATFORM
	// environment variable
	if name := os.Getenv("APPLATFORM"); name != "" {
		for _, p := range knownPlatforms {
			if p.name == name {
				platform = p
				return p
			}
		}
		log.Fatalf("unsupported platform: %s", name)
	}

	for _, p := range knownPlatforms {
		if p.probe() {
			platform = p
			return p
		}
	}

	log.Fatalf("unable to detect platform type")
	return nil
}

/******************************************************************
 *
 * Raspberry Pi support
 */

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

/******************************************************************
 *
 * Unielec / MediaTek support
 */
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
	return strings.HasPrefix(nic, "lan") && strings.Contains(nic, ".")
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

/******************************************************************
 *
 * x86 Debian support
 */
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

/******************************************************************
 *
 * Common wrapper code
 */

// GetPlatform returns the name of this node's platform
func (p *Platform) GetPlatform() string {
	return p.name
}

// SetNodeID will persist the provided nodeID in the correct file for this
// platform
func (p *Platform) SetNodeID(uuidStr string) error {
	return p.setNodeID(p.machineIDFile, uuidStr)
}

// GetNodeID returns a string containing this device's UUID
func (p *Platform) GetNodeID() (string, error) {
	platformLock.Lock()
	defer platformLock.Unlock()

	if nodeID != "" {
		// nodeID is already set, no need to reload it
		return nodeID, nil
	}

	data, err := ioutil.ReadFile(p.machineIDFile)
	if err != nil {
		return "", err
	}

	uuidStr, err := p.parseNodeID(data)
	if err != nil {
		return "", fmt.Errorf("%s: %v", p.machineIDFile, err)
	}

	uuidStr = strings.ToLower(uuidStr)
	if _, err = uuid.FromString(uuidStr); err != nil {
		return "", fmt.Errorf("unable to parse %s: %v", uuidStr, err)
	}

	nodeID = uuidStr
	return nodeID, nil
}
