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
	"log"
	"os"
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
	NicLocation   func(string) string

	GetDHCPInfo func(string) (map[string]string, error)
	DHCPPidfile func(string) string

	NtpdConfPath   string
	RestartService func(string) error
}

var (
	platformLock sync.Mutex
	platform     *Platform
	nodeID       string

	knownPlatforms = make([]*Platform, 0)
)

func addPlatform(p *Platform) {
	knownPlatforms = append(knownPlatforms, p)
}

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
