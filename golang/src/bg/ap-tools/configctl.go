/*
 * Copyright 2019 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"bg/ap_common/apcfg"
	"bg/ap_common/aputil"
	"bg/ap_common/broker"
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

	findGateway()
	slog := aputil.NewLogger(pname)
	brokerd, err := broker.NewBroker(slog, pname)
	if err != nil {
		slog.Fatal(err)
	}
	defer brokerd.Fini()
	configd, err := apcfg.NewConfigd(brokerd, pname, l)

	if err != nil {
		fmt.Printf("cannot connect to configd: %v\n", err)
		os.Exit(1)
	}

	args := flag.Args()
	err = configctl.Exec(context.Background(), pname, configd, args)

	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func init() {
	addTool("ap-configctl", configctlMain)
}

