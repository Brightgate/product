/*
 * COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 *
 */

package registry

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLabel(t *testing.T) {
	assert := require.New(t)

	longOne := strings.Repeat("a", 100)
	testCases := []struct {
		in  string
		out string
	}{
		{"", ""},
		{"LoLoL", "lolol"},
		{"!@#$%", "_____"},
		{"argh@#$%", "argh____"},
		{"dashes-are-ok", "dashes-are-ok"},
		{"Brightgate, Inc.", "brightgate__inc_"},
		{longOne, longOne[0:62]},
	}

	for _, tc := range testCases {
		out := cleanLabelValue(tc.in)
		assert.Equal(tc.out, out)
	}
}
