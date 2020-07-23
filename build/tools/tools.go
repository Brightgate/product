/*
 * COPYRIGHT 2020 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

// +build tools

package tools

import (
	// These imports are simply to allow the go.mod and go.sum files to be
	// maintained, as described in the Go wiki at
	// https://github.com/golang/go/wiki/Modules#how-can-i-track-tool-dependencies-for-a-module
	_ "github.com/golang/protobuf/protoc-gen-go"
	_ "github.com/golangci/golangci-lint/cmd/golangci-lint"
	_ "github.com/vektra/mockery/v2/cmd"
)
