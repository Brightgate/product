//
// Copyright 2020 Brightgate Inc.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//


package main

import (
	"strings"
	"testing"

	"bg/ap_common/publiclog"
	"bg/base_msg"

	"github.com/golang/protobuf/proto"
	"github.com/stretchr/testify/require"
)

// Properties of a valid CEF message:
// - split on '|' should be N elements
// - certain fields are known constants
func validCEFMsgStructure(s string) bool {
	l := strings.Split(s, "|")

	return len(l) == 5
}

func validCEFMsgConstantContent(s string) bool {
	l := strings.Split(s, "|")

	if l[0] != publiclog.CefVersion {
		return false
	}

	if l[1] != publiclog.CefDeviceVendor {
		return false
	}

	return true
}

func TestPublicLogMsg(t *testing.T) {
	assert := require.New(t)

	l := base_msg.EventNetPublicLog{
		EventClassId: proto.String("internal-testing-message"),
	}
	p := &l

	f := fmtCefPublicLog(p)
	assert.True(validCEFMsgStructure(f), "message structure")
	assert.True(validCEFMsgConstantContent(f), "message constant content")

	l.CefReason = proto.String("test")

	f = fmtCefPublicLog(p)
	assert.True(validCEFMsgStructure(f), "message structure")
	assert.True(validCEFMsgConstantContent(f), "message constant content")
}

