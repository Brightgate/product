/*
 * Copyright 2018 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package main

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHostmap(t *testing.T) {
	var err error
	assert := require.New(t)

	hm := hostmapCreate()
	assert.False(hm.contains("success"))

	err = hm.add("success")
	assert.NoError(err)
	assert.False(hm.contains("success"))

	err = hm.add("success")
	assert.Error(err)
	assert.True(hm.contains("success"))

	err = hm.del("success")
	assert.NoError(err)
	assert.False(hm.contains("success"))

	err = hm.del("success")
	assert.Error(err)
	assert.False(hm.contains("success"))
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}

