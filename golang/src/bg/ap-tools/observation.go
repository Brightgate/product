/*
 * Copyright 2020 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package main

import (
	"bg/ap_common/aputil"
	"bg/base_msg"
	"bg/common/network"
	"fmt"
	"io/ioutil"
	"log"
	"os"

	"github.com/golang/protobuf/proto"
)

func observation() {
	args := os.Args[1:]
	if len(args) == 0 {
		log.Fatalf("must supply a path to an observation file")
	}
	errors := 0
	for _, arg := range args {
		f, err := os.Open(arg)
		if err != nil {
			log.Printf("failed to open %s: %v", arg, err)
			errors++
			continue
		}
		buf, err := ioutil.ReadAll(f)
		if err != nil {
			log.Printf("failed to read %s: %v", arg, err)
			errors++
			continue
		}
		inv := &base_msg.DeviceInventory{}
		err = proto.Unmarshal(buf, inv)
		if err != nil {
			log.Printf("failed to unmarshal %s to inventory: %v", arg, err)
			errors++
			continue
		}
		fmt.Printf("--------- %s %s\n", arg, aputil.ProtobufToTime(inv.Timestamp))
		for _, dev := range inv.GetDevices() {
			fmt.Printf("[[%s]]\n", network.Uint64ToHWAddr(dev.GetMacAddress()))
			fmt.Printf("%s\n\n", proto.MarshalTextString(dev))
		}
	}
	if errors > 0 {
		os.Exit(1)
	}
}

func init() {
	addTool("ap-observation", observation)
}

