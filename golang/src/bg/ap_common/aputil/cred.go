/*
 * COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package aputil

import (
	"fmt"
	"io/ioutil"

	"bg/common/grpcutils"

	"bg/ap_common/platform"
)

// SystemCredential creates a new credential based on the system default
// storage location.
func SystemCredential() (*grpcutils.Credential, error) {
	pl := platform.NewPlatform()
	credPath := pl.ExpandDirPath("__APSECRET__", "rpcd", "cloud.secret.json")

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
