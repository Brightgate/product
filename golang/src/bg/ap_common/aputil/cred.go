/*
 * COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package aputil

import (
	"flag"
	"fmt"
	"io/ioutil"

	"bg/common/grpcutils"
)

var (
	credPathFlag string
)

// SystemCredential creates a new credential based on the system default
// storage location, or -cred-path, if given.
func SystemCredential() (*grpcutils.Credential, error) {
	credPath := ExpandDirPath(credPathFlag)

	credFile, err := ioutil.ReadFile(credPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read creds file %s: %v",
			credPath, err)
	}
	cred, err := grpcutils.NewCredentialFromJSON(credFile)
	if err != nil {
		return nil, fmt.Errorf("failed to build credential: %v", err)
	}
	return cred, nil
}

func init() {
	flag.StringVar(&credPathFlag, "cloud-cred-path",
		"/etc/secret/cloud/cloud.secret.json",
		"cloud service JSON credential")
}
