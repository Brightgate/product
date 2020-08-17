/*
 * Copyright 2020 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
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

