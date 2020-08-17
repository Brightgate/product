/*
 * Copyright 2019 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
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

