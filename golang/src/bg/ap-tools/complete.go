/*
 * Copyright 2018 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"bg/ap_common/apcfg"
	"bg/ap_common/mcp"
	"bg/common/cfgapi"
)

// Either expand the list of legal commands, or verify that we are operating on
// a legal command.  Returns 'true' if the caller should continue expanding the
// prefix.
func cmdCheck(util, prefix, prior string, commands []string) bool {
	if prior == util {
		// The prior word on the command line is the name of the
		// utility, so we want to expand the list of commands
		for _, c := range commands {
			if strings.HasPrefix(c, prefix) {
				fmt.Printf("%s \n", c)
			}
		}
		return false
	}

	// Verify that the prior word on the CLI was a legal command for this
	// utility.
	for _, c := range commands {
		if prior == c {
			return true
		}
	}
	return false
}

func completeMCP(prefix, prior string) {
	commands := []string{"status", "stop", "start", "restart"}

	if !cmdCheck("ap-ctl", prefix, prior, commands) {
		return
	}

	var states mcp.DaemonList
	if mcp, err := mcp.New(pname); err == nil {
		if rval, err := mcp.GetState("all"); err == nil {
			json.Unmarshal([]byte(rval), &states)
		}
	}

	for _, s := range states {
		if strings.HasPrefix(s.Name, prefix) {
			fmt.Printf("%s\n", s.Name)
		}
	}
}

func completeConfig(prefix, prior string) {
	commands := []string{"add", "set", "get", "del", "mon"}

	if !cmdCheck("ap-configctl", prefix, prior, commands) {
		return
	}

	// Special-case the handling of the root node
	if prefix == "" || prefix == "@" {
		fmt.Printf("@/\n")
		return
	}

	// If we are performing a 'get', attempt to complete the two special
	// formatted options.
	if prior == "get" && prefix != "" {
		formatted := []string{"clients", "rings"}
		for _, f := range formatted {
			if strings.HasPrefix(f, prefix) {
				fmt.Printf("%s\n", f)
			}
		}
	}

	// If we seem to be expanding a config tree path, split the path
	// into completed and uncompleted components.
	var path, partial string
	if slash := strings.LastIndex(prefix, "/"); slash >= 0 {
		path = prefix[0:slash]
		partial = prefix[slash+1:]
	} else {
		return
	}

	configd, err := apcfg.NewConfigd(nil, pname, cfgapi.AccessUser)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot connect to configd: %v\n", err)
		return
	}
	root, _ := configd.GetProps(path)
	if root == nil {
		return
	}

	for n, c := range root.Children {
		if strings.HasPrefix(n, partial) {
			if len(c.Children) > 0 {
				fmt.Printf("%s/%s/\n", path, n)
			} else {
				fmt.Printf("%s/%s\n", path, n)
			}
		}
	}

}

func complete() {
	if len(os.Args) != 4 {
		fmt.Printf("Usage: %s <command> <prefix> <previous>\n", pname)
		os.Exit(1)
	}

	switch os.Args[1] {
	case "ap-configctl":
		completeConfig(os.Args[2], os.Args[3])
	case "ap-ctl":
		completeMCP(os.Args[2], os.Args[3])
	}
}

func init() {
	addTool("ap-complete", complete)
}

