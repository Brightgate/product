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

	// "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// func (p *Platform) ExpandDirPath(paths ...string) string {
var plat *Platform

func TestExpandApRoot(t *testing.T) {
	require.NotPanics(t, func() {
		fmt.Print(plat.ExpandDirPath("__APROOT__", "top/tuber"))
	})

}

func TestExpandApSecret(t *testing.T) {
	require.NotPanics(t, func() {
		fmt.Print(plat.ExpandDirPath("__APSECRET", "mystery/tuber"))
	})
}

func TestExpandApPackage(t *testing.T) {
	require.NotPanics(t, func() {
		fmt.Print(plat.ExpandDirPath("__APPACKAGE", "immutable/tuber"))
	})
}

func TestExpandApData(t *testing.T) {
	require.NotPanics(t, func() {
		fmt.Print(plat.ExpandDirPath("__APDATA", "mutable/tubers"))
	})
}

func TestExpandApLeftover(t *testing.T) {
	require.Panics(t, func() {
		fmt.Println(plat.ExpandDirPath("__APPOTATO__", "no/such/tuber"))
	})
}

func init() {
	plat = NewPlatform()
}
