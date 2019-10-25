/*
 * COPYRIGHT 2019 Brightgate Inc. All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
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
