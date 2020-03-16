/*
 * COPYRIGHT 2020 Brightgate Inc.  All rights reserved.
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
	"path/filepath"
	"testing"

	"bg/ap_common/platform"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestPkgCommits(t *testing.T) {
	type testData struct {
		path    string
		content string
	}

	tests := []struct {
		name   string
		data   []testData
		result map[string]string
	}{
		{
			name: "one_repo_two_pkg_one_commit",
			data: []testData{
				{
					path:    "XS:bg-bar_0.0-0",
					content: "abc123",
				},
				{
					path:    "XS:bg-foo_0.0-0",
					content: "abc123",
				},
			},
			result: map[string]string{
				"XS": "abc123",
			},
		},
		{
			name: "one_repo_two_pkg_two_commit",
			data: []testData{
				{
					path:    "XS:bg-bar_0.0-0",
					content: "abc123",
				},
				{
					path:    "XS:bg-foo_0.0-0",
					content: "123abc",
				},
			},
			result: map[string]string{
				"XS:bg-bar": "abc123",
				"XS:bg-foo": "123abc",
			},
		},
		{
			name: "two_repos_two_pkg_two_commit",
			data: []testData{
				{
					path:    "XS:bg-bar_0.0-0",
					content: "123abc",
				},
				{
					path:    "PS:bg-app_0.0-0",
					content: "xyz789",
				},
			},
			result: map[string]string{
				"PS": "xyz789",
				"XS": "123abc",
			},
		},
		{
			name: "two_repos_three_pkg_two_commit",
			data: []testData{
				{
					path:    "XS:bg-bar_0.0-0",
					content: "abc123",
				},
				{
					path:    "XS:bg-foo_0.0-0",
					content: "abc123",
				},
				{
					path:    "PS:bg-app_0.0-0",
					content: "xyz789",
				},
			},
			result: map[string]string{
				"PS": "xyz789",
				"XS": "abc123",
			},
		},
		{
			name: "two_repos_three_pkg_three_commit",
			data: []testData{
				{
					path:    "XS:bg-bar_0.0-0",
					content: "abc123",
				},
				{
					path:    "XS:bg-foo_0.0-0",
					content: "123abc",
				},
				{
					path:    "PS:bg-app_0.0-0",
					content: "xyz789",
				},
			},
			result: map[string]string{
				"PS":        "xyz789",
				"XS:bg-bar": "abc123",
				"XS:bg-foo": "123abc",
			},
		},
		{
			name: "two_repos_four_pkg_three_commit",
			data: []testData{
				{
					path:    "XS:bg-bar_0.0-0",
					content: "abc123",
				},
				{
					path:    "XS:bg-foo_0.0-0",
					content: "123abc",
				},
				{
					path:    "PS:bg-app_0.0-0",
					content: "xyz789",
				},
				{
					path:    "PS:bg-wat_0.0-0",
					content: "xyz789",
				},
			},
			result: map[string]string{
				"PS":        "xyz789",
				"XS:bg-bar": "abc123",
				"XS:bg-foo": "123abc",
			},
		},
		{
			name: "two_repos_four_pkg_four_commit",
			data: []testData{
				{
					path:    "XS:bg-bar_0.0-0",
					content: "abc123",
				},
				{
					path:    "XS:bg-foo_0.0-0",
					content: "123abc",
				},
				{
					path:    "PS:bg-app_0.0-0",
					content: "xyz789",
				},
				{
					path:    "PS:bg-wat_0.0-0",
					content: "789xyz",
				},
			},
			result: map[string]string{
				"PS:bg-app": "xyz789",
				"PS:bg-wat": "789xyz",
				"XS:bg-bar": "abc123",
				"XS:bg-foo": "123abc",
			},
		},
		{
			name:   "missing_or_empty_dir",
			data:   []testData{},
			result: map[string]string{},
		},
	}

	verDirPath := platform.NewPlatform().ExpandDirPath(
		platform.APRoot, "etc", "bg-versions.d")
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fs := afero.NewMemMapFs()
			for _, d := range test.data {
				f, _ := fs.Create(filepath.Join(verDirPath, d.path))
				f.WriteString(d.content)
				f.Close()
			}
			commitMap := make(map[string]string)
			getCurrentCommitsPkgs(fs, commitMap)
			require.Equal(t, test.result, commitMap)
		})
	}
}
