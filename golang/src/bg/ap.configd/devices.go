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

	"bg/common/cfgapi"
	"bg/common/cfgmsg"
	"bg/common/deviceid"
)

const (
	deviceDB = "devices.json"
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

func devPropHandler(query *cfgmsg.ConfigQuery) (string, error) {
	var rval string
	var err error

	for _, op := range query.Ops {
		var prop string

		if prop, _, _, err = getParams(op); err != nil {
			break
		}

		switch op.Operation {
		case cfgmsg.ConfigOp_GET:
			rval, err = getDevice(prop)

		case cfgmsg.ConfigOp_PING:
			// no-op

		default:
			name, _ := cfgmsg.OpName(op.Operation)
			err = fmt.Errorf("%s not supported for @/devices", name)
		}

		if err != nil {
			break
		}
	}

	return rval, err
}

func deviceDBInit() error {
	var err error

	devices, err = deviceid.DevicesLoad(plat.ExpandDirPath(staticDir, deviceDB))
	return err
}
