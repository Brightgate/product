/*
 * COPYRIGHT 2017 Brightgate Inc.  All rights reserved.
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
	"strconv"
	"strings"
	"time"

	"bg/ap_common/device"
)

const (
	deviceDB = "devices.json"
)

var (
	devices device.Collection
)

// Handle a request for a single device.
func getDevHandler(prop string) (rval string, err error) {

	// The path must look like @/devices/<devid>
	c := strings.Split(prop, "/")
	if len(c) != 3 {
		err = fmt.Errorf("invalid device path: %s", prop)
		return
	}

	devid, err := strconv.Atoi(c[2])
	if err != nil || devid == 0 {
		err = fmt.Errorf("invalid device id: %s", c[2])
		return
	}

	d := devices[uint32(devid)]
	if d == nil {
		err = fmt.Errorf("no such device id: %s", c[2])
		return
	}

	if b, err := json.Marshal(d); err == nil {
		rval = string(b)
	}
	return
}

func setDevHandler(prop string, val *string, exp *time.Time, add bool) error {
	return fmt.Errorf("the device tree is read-only")
}

func delDevHandler(prop string) error {
	return fmt.Errorf("the device tree is read-only")
}

func deviceDBInit() error {
	var err error

	devices, err = device.DevicesLoad(*propdir + deviceDB)
	return err
}
