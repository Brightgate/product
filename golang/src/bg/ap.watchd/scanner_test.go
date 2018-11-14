/*
 * COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 *
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
	assert.Equal(hm.contains("success"), false)

	err = hm.add("success")
	assert.NoError(err)
	assert.Equal(hm.contains("success"), true)

	err = hm.add("success")
	assert.Error(err)
	assert.Equal(hm.contains("success"), true)

	err = hm.del("success")
	assert.NoError(err)
	assert.Equal(hm.contains("success"), false)

	err = hm.del("success")
	assert.Error(err)
	assert.Equal(hm.contains("success"), false)
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
