/*
 * COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"bg/cl_common/daemonutils"
	"bg/common/cfgapi"
	"bg/common/deviceid"
)

const (
	deviceDB = "etc/devices.json"
)

var (
	devices deviceid.Collection
)

// Handle a request for a single device.
func getDevice(prop string) (string, error) {
	// The path must look like @/devices/<devid>
	c := strings.Split(prop, "/")
	if len(c) != 3 {
		return "", fmt.Errorf("invalid device path: %s", prop)
	}

	devid, err := strconv.Atoi(c[2])
	if err != nil || devid == 0 {
		return "", fmt.Errorf("invalid device id: %s", c[2])
	}

	d := devices[uint32(devid)]
	if d == nil {
		return "", cfgapi.ErrNoProp
	}

	b, err := json.Marshal(d)
	if err == nil {
		return string(b), nil
	}
	return "", err
}

func deviceDBInit() error {
	var err error

	path := filepath.Join(daemonutils.ClRoot(), deviceDB)
	devices, err = deviceid.DevicesLoad(path)
	return err
}
