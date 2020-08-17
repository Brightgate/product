//
// Copyright 2019 Brightgate Inc.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//


package platform

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var plat *Platform

func TestExpandApRoot(t *testing.T) {
	var np string
	require.NotPanics(t, func() {
		np = plat.ExpandDirPath(APRoot, "top/tuber")
		fmt.Print(np)
	})
	assert.NotContains(t, np, "__")

}

func TestExpandApSecret(t *testing.T) {
	var np string
	require.NotPanics(t, func() {
		np = plat.ExpandDirPath(APSecret, "mystery/tuber")
		fmt.Print(np)
	})
	assert.NotContains(t, np, "__")
}

func TestExpandApPackage(t *testing.T) {
	var np string
	require.NotPanics(t, func() {
		np = plat.ExpandDirPath(APPackage, "immutable/tuber")
		fmt.Print(np)
	})
	assert.NotContains(t, np, "__")
}

func TestExpandApData(t *testing.T) {
	var np string
	require.NotPanics(t, func() {
		np = plat.ExpandDirPath(APData, "mutable/tubers")
		fmt.Print(np)
	})
	assert.NotContains(t, np, "__")
}

func TestExpandApLeftover(t *testing.T) {
	require.Panics(t, func() {
		fmt.Println(plat.ExpandDirPath("__APPOTATO__", "no/such/tuber"))
	})
}

func init() {
	plat = NewPlatform()
}

