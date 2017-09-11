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
	"io/ioutil"
	"strconv"
	"strings"

	"ap_common/apcfg"

	"base_msg"
)

const (
	deviceDB = "devices.json"
)

var (
	devices map[uint32]*apcfg.Device
)

// Handle a request for a single device.
func getDevHandler(q *base_msg.ConfigQuery) (rval string, err error) {

	// The path must look like @/devices/<devid>
	c := strings.Split(*q.Property, "/")
	if len(c) != 3 {
		err = fmt.Errorf("invalid device path: %s", *q.Property)
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

func setDevHandler(q *base_msg.ConfigQuery, add bool) error {
	return fmt.Errorf("the device tree is read-only")
}

func delDevHandler(q *base_msg.ConfigQuery) error {
	return fmt.Errorf("the device tree is read-only")
}

func DeviceDBInit() error {
	var file []byte
	var err error

	name := *propdir + deviceDB
	if file, err = ioutil.ReadFile(name); err != nil {
		err = fmt.Errorf("failed to load device database from %s: %v",
			name, err)
	} else if err = json.Unmarshal(file, &devices); err != nil {
		err = fmt.Errorf("failed to import device database from %s: %v",
			name, err)
	}

	return err
}
