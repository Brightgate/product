//
// COPYRIGHT 2019 Brightgate Inc. All rights reserved.
//
// This copyright notice is Copyright Management Information under 17 USC 1202
// and is included to protect this work and deter copyright infringement.
// Removal or alteration of this Copyright Management Information without the
// express written permission of Brightgate Inc is prohibited, and any
// such unauthorized removal or  alteration will be a violation of federal law.
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
