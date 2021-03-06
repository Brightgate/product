/*
 * Copyright 2019 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package platform

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"

	"bg/common/release"
)

// Platform is used to encapsulate the differences between the different
// hardware platforms we support as appliances.
type Platform struct {
	name string
	// CefDeviceProduct is the product string we use within public CEF
	// messages.
	CefDeviceProduct string

	ResetSignal  syscall.Signal
	ReloadSignal syscall.Signal
	HostapdCmd   string
	SysctlCmd    string
	IPCmd        string
	IwCmd        string
	IPTablesCmd  string
	EthtoolCmd   string // ap.httpd diags
	DigCmd       string // ap.httpd diags
	CurlCmd      string // ap.httpd diags
	RestoreCmd   string

	probe         func() bool
	setNodeID     func(string) error
	getNodeID     func() (string, error)
	GenNodeID     func(int) string
	NicIsVirtual  func(string) bool
	NicIsWireless func(string) bool
	NicIsWired    func(string) bool
	NicIsWan      func(string, string) bool
	NicID         func(string, string) string
	NicLocation   func(string) string
	DataDir       func() string

	GetDHCPInterfaces func() ([]string, error)
	GetDHCPInfo       func(string) (map[string]string, error)
	DHCPPidfile       func(string) string

	NetworkManaged bool
	NetConfig      func(string, string, string, string, string) error

	NtpdService    string
	MaintainTime   func()
	RestartService func(string) error

	Upgrade func(release.Release) ([]byte, error)
}

const (
	// APData will expand to the location for mutable files.
	APData = "__APDATA__"
	// APPackage will expand to the base of the package installation.
	APPackage = "__APPACKAGE__"
	// APRoot will expand to the base of the OS installation.
	APRoot = "__APROOT__"
	// APSecret will expand to the location for protected mutable files.
	APSecret = "__APSECRET__"

	// LSBDataDir is our standard location for data files on platforms.
	LSBDataDir = "__APPACKAGE__/var/spool"
)

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

// ClearPlatform discards a previously captured platform handle.
func ClearPlatform() {
	platformLock.Lock()
	defer platformLock.Unlock()

	platform = nil
}

// GetPlatform returns the name of this node's platform
func (p *Platform) GetPlatform() string {
	return p.name
}

// SetNodeID will persist the provided nodeID in the correct file for this
// platform
func (p *Platform) SetNodeID(uuidStr string) error {
	if nodeID != "" {
		return fmt.Errorf("existing nodeID can't be reset")
	}

	return p.setNodeID(uuidStr)
}

// GetNodeID returns a string containing this device's UUID
func (p *Platform) GetNodeID() (string, error) {
	platformLock.Lock()
	defer platformLock.Unlock()

	if nodeID != "" {
		// nodeID is already set, no need to reload it
		return nodeID, nil
	}

	return p.getNodeID()
}

// ExpandDirPath takes a splat of path components and will translate it into an
// absolute APROOT-and-platform-aware path.
func (p *Platform) ExpandDirPath(paths ...string) string {
	np := filepath.Join(paths...)
	np = strings.Replace(np, "__APSECRET__", "__APDATA__/secret", -1)
	np = strings.Replace(np, "__APDATA__", p.DataDir(), -1)
	np = strings.Replace(np, "__APPACKAGE__", "__APROOT__/opt/com.brightgate", -1)

	root := os.Getenv("APROOT")

	np = strings.Replace(np, "__APROOT__", root, -1)

	re := regexp.MustCompile(`__[^/]+__`)
	if re.MatchString(np) {
		panic("unexpanded dbl-underscore token in path")
	}

	return filepath.Clean(np)
}

