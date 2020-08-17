/*
 * Copyright 2019 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package aptest

import (
	"os"
	"path/filepath"
	"testing"

	"bg/ap_common/platform"
)

// TestRoot represents an instance of a platform-aware file hierarchy
// used for testing purposes.
type TestRoot struct {
	T        *testing.T
	Root     string
	saveRoot string
	tracked  []string
}

var dirs []string
var plat *platform.Platform

// Clean erases the contents of a TestRoot instance.
func (tr *TestRoot) Clean() {

	for d := range dirs {
		xd := plat.ExpandDirPath("__APDATA__", dirs[d])
		tr.T.Logf("clean TestRoot directory: %s", xd)
		files, _ := filepath.Glob(xd + "/*")
		tr.T.Logf("clean TestRoot files: %s", files)

		for _, f := range files {
			if err := os.RemoveAll(f); err != nil {
				panic(err)
			}
		}
	}
}

// NewTestRoot prepares a new TestRoot instance, and populates the data
// directory with various appliance directories.  APROOT should be set
// in the environment.
func NewTestRoot(t *testing.T) *TestRoot {
	dirs = []string{"antiphishing", "identifierd", "rpcd", "watchd/droplog", "watchd/stats"}

	for d := range dirs {
		xd := plat.ExpandDirPath("__APDATA__", dirs[d])
		t.Logf("mkdirall %s", xd)
		os.MkdirAll(xd, 0755)
	}

	tr := &TestRoot{
		T:        t,
		Root:     os.Getenv("APROOT"),
		saveRoot: os.Getenv("APROOT"),
	}

	tr.Clean()

	return tr
}

// Fini removes the test root.
func (tr *TestRoot) Fini() {
	tr.Clean()
}

func init() {
	plat = platform.NewPlatform()
}

