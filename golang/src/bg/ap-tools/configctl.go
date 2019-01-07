/*
 * COPYRIGHT 2018 Brightgate Inc. All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or  alteration will be a violation of federal law.
 */

package main

import (
	"flag"
	"fmt"
	"os"

	"bg/ap_common/apcfg"
	"bg/common/cfgapi"
	"bg/common/configctl"
)

var (
	accessLevel string
)

func configctlFlagInit() {
	flag.StringVar(&accessLevel, "l", "admin", "configd access level")
	flag.Parse()
}

func configctlMain() {
	var err error

	configctlFlagInit()

	l, ok := cfgapi.AccessLevels[accessLevel]
	if !ok {
		fmt.Printf("no such access level: %s\n", accessLevel)
		os.Exit(1)
	}

	configd, err := apcfg.NewConfigd(nil, pname, l)

	if err != nil {
		fmt.Printf("cannot connect to configd: %v\n", err)
		os.Exit(1)
	}

	args := flag.Args()
	err = configctl.Exec(pname, configd, args)

	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func init() {
	addTool("ap-configctl", configctlMain)
}