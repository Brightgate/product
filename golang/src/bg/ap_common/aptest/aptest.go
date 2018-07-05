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

package aptest

import (
	"io/ioutil"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

// TestRoot represents an instance of a file hierarchy used for testing
// purposes.
type TestRoot struct {
	Root     string
	saveRoot string
}

// NewTestRoot prepares a new TestRoot instance, and populates a temporary
// directory with various appliance directories.  It updates $APROOT to
// point to the test root directory.
func NewTestRoot(t *testing.T) *TestRoot {
	dir, err := ioutil.TempDir("", url.PathEscape(filepath.ToSlash(t.Name())))
	if err != nil {
		panic(err)
	}
	os.MkdirAll(filepath.Join(dir, "var/spool/identifierd"), 0755)
	os.MkdirAll(filepath.Join(dir, "var/spool/rpc"), 0755)
	os.MkdirAll(filepath.Join(dir, "var/spool/watchd/droplog"), 0755)
	os.MkdirAll(filepath.Join(dir, "var/spool/watchd/stats"), 0755)

	tr := &TestRoot{
		Root:     dir,
		saveRoot: os.Getenv("APROOT"),
	}
	os.Setenv("APROOT", dir)
	return tr
}

// Fini removes the test root and restores $APROOT to its previous value
func (tr *TestRoot) Fini() {
	os.Setenv("APROOT", tr.saveRoot)
	os.RemoveAll(tr.Root)
}
