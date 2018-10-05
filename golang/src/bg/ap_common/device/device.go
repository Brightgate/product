/*
 * COPYRIGHT 2017 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package device

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"time"

	"bg/common/cfgapi"
)

// IDBase is the minimum device ID
const IDBase = 2

// Device describes a single device
type Device struct {
	Obsolete       bool
	UpdateTime     time.Time
	Devtype        string
	Vendor         string
	ProductName    string
	ProductVersion string   `json:"Version,omitempty"`
	UDPPorts       []int    `json:"UDP,omitempty"`
	InboundPorts   []int    `json:"InboundPorts,omitempty"`
	OutboundPorts  []int    `json:"OutboundPorts,omitempty"`
	DNS            []string `json:"DNS,omitempty"`
	Notes          string   `json:"Notes,omitempty"`
}

// Collection describes a collection of devices, indexed by DeviceID
type Collection map[uint32]*Device

// DevicesLoad will read a JSON-formatted device database file, and returns a
// populated Collection
func DevicesLoad(name string) (Collection, error) {
	var devices Collection
	var file []byte
	var err error

	if file, err = ioutil.ReadFile(name); err != nil {
		err = fmt.Errorf("failed to load device database from %s: %v",
			name, err)
	} else if err = json.Unmarshal(file, &devices); err != nil {
		err = fmt.Errorf("failed to import device database from %s: %v",
			name, err)
	}

	return devices, err
}

// GetDeviceByPath fetches a single device by its path
func GetDeviceByPath(c *cfgapi.Handle, path string) (*Device, error) {
	var dev Device

	ops := []cfgapi.PropertyOp{
		{Op: cfgapi.PropGet, Name: path},
	}
	tree, err := c.Execute(nil, ops).Wait(nil)

	if err != nil {
		err = fmt.Errorf("failed to retrieve %s: %v", path, err)
	} else if err = json.Unmarshal([]byte(tree), &dev); err != nil {
		err = fmt.Errorf("failed to decode %s: %v", tree, err)
	}

	return &dev, err
}

// GetDeviceByID fetches a single device by its ID #
func GetDeviceByID(c *cfgapi.Handle, devid int) (*Device, error) {
	path := fmt.Sprintf("@/devices/%d", devid)
	return GetDeviceByPath(c, path)
}
