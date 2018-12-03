/*
 * COPYRIGHT 2018 Brightgate Inc. All rights reserved.
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
	"sort"
)

var (
	tools = make(map[string]func())
	pname string
)

func addTool(tool string, main func()) {
	tools[tool] = main
}

func listTools() {
	list := make([]string, 0)
	for n := range tools {
		if n != pname {
			list = append(list, n)
		}
	}
	sort.Strings(list)
	fmt.Printf("Tools:\n")
	for _, n := range list {
		fmt.Printf("    %s\n", n)
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
