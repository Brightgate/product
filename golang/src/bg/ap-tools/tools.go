/*
 * Copyright 2019 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package main

import (
	"fmt"
	"os"
	"path/filepath"

	"bg/ap_common/aputil"
	"bg/ap_common/mcp"
)

var (
	tools = make(map[string]func())
	pname string
)

func addTool(tool string, main func()) {
	tools[tool] = main
}

func findGateway() {
	if os.Getenv("APGATEWAY") != "" {
		return
	}

	mcp, err := mcp.New(pname)
	if err != nil {
		fmt.Printf("%s: connecting to mcp: %v\n", pname, err)
		os.Exit(1)
	}
	ip, err := mcp.Gateway()
	if err != nil {
		fmt.Printf("%s: finding gateway: %v\n", pname, err)
		os.Exit(1)
	}

	mcp.Close()
	os.Setenv("APGATEWAY", ip.String())
}

func listTools() {
	fmt.Printf("Tools:\n")
	for _, n := range aputil.SortStringKeys(tools) {
		if n != pname {
			fmt.Printf("    %s\n", n)
		}
	}
}

func main() {
	pname = filepath.Base(os.Args[0])

	if fn, ok := tools[pname]; ok {
		fn()
	} else {
		fmt.Printf("unknown tool: %s", pname)
	}
}

func init() {
	addTool("ap-tools", listTools)
}

