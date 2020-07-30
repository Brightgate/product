/*
 * COPYRIGHT 2020 Brightgate Inc. All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or  alteration will be a violation of federal law.
 */

package main

import (
	"context"
	"fmt"
	"os"

	"bg/ap_common/apcfg"
	"bg/ap_common/platform"
	"bg/common/cfgapi"
	"bg/common/vpntool"
	"bg/common/wgsite"
)

func vpntoolMain() {
	var err error

	configd, err := apcfg.NewConfigd(nil, pname, cfgapi.AccessInternal)

	if err != nil {
		fmt.Printf("cannot connect to configd: %v\n", err)
		os.Exit(1)
	}

	// Provide the tool with a path to the private key, so it can verify
	// that it corresponds with the public key in the config file.
	plat := platform.NewPlatform()
	keyFile := plat.ExpandDirPath(wgsite.SecretDir, wgsite.PrivateFile)
	vpntool.SetKeyFile(keyFile)

	err = vpntool.Exec(context.Background(), pname, configd, os.Args[1:])

	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func init() {
	addTool("ap-vpntool", vpntoolMain)
}
