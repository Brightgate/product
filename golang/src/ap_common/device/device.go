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
)

// Definition of a single device
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

type DeviceMap map[uint32]*Device

func DevicesLoad(name string) (DeviceMap, error) {
	var devices DeviceMap
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
