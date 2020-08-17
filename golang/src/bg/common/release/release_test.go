/*
 * Copyright 2019 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package release

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFilenameByPattern(t *testing.T) {
	assert := require.New(t)

	artifacts := [][]Artifact{
		// No artifacts
		[]Artifact{},
		// One artifact
		[]Artifact{
			Artifact{URL: "https://storage/bucket/filename.deb"},
		},
		// Two artifacts, same extension
		[]Artifact{
			Artifact{URL: "https://storage/bucket/filename1.ipk"},
			Artifact{URL: "https://storage/bucket/filename2.ipk"},
		},
		// Two artifacts, different extension
		[]Artifact{
			Artifact{URL: "https://storage/bucket/filename1.ipk"},
			Artifact{URL: "https://storage/bucket/filename2.EXT"},
		},
		// Two artifacts, different pattern in base name
		[]Artifact{
			Artifact{URL: "https://storage/bucket/name_ver-sion_arch-i-tecture.ipk"},
			Artifact{URL: "https://storage/bucket/name_ver-sion_other-tecture.ipk"},
		},
	}
	releases := make([]Release, len(artifacts))
	for i, aa := range artifacts {
		releases[i].Artifacts = aa
	}

	patternResults := map[string][][]string{
		"*.ipk": {
			[]string{},
			[]string{},
			[]string{"filename1.ipk", "filename2.ipk"},
			[]string{"filename1.ipk"},
			[]string{"name_ver-sion_arch-i-tecture.ipk", "name_ver-sion_other-tecture.ipk"},
		},
		"*.deb": {
			[]string{},
			[]string{"filename.deb"},
			[]string{},
			[]string{},
			[]string{},
		},
		"name_*_arch-i-tecture.ipk": {
			[]string{},
			[]string{},
			[]string{},
			[]string{},
			[]string{"name_ver-sion_arch-i-tecture.ipk"},
		},
	}

	for pattern, expected := range patternResults {
		for i, release := range releases {
			res := release.FilenameByPattern(pattern)
			assert.Equal(expected[i], res)
		}
	}
}

