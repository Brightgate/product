/*
 * Copyright 2019 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
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

