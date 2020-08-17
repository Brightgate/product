//
// Copyright 2019 Brightgate Inc.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//


package certificate

import (
	"io/ioutil"
	"os"
	"testing"
)

// Test that we can create self-signed certificates with some
// non-colliding filename.
func TestSelfSigned(t *testing.T) {
	// create a temporary directory
	dn, err := ioutil.TempDir("", "certificate_test.")
	if err != nil {
		t.Errorf("couldn't create temporary directory '%s': %v\n", dn, err)
	}

	paths, err := createSSKeyCert(nil, dn, "testhost.local", "0")

	if err != nil {
		t.Errorf("err = %v\n", err)
	} else {
		t.Logf("keyfn = %s, certfn = %s, chainfn = %s, fullchainfn = %s\n",
			paths.Key, paths.Cert, paths.Chain, paths.FullChain)
	}

	// remove temporary directory
	err = os.RemoveAll(dn)
	if err != nil {
		t.Errorf("couldn't remove temporary directory '%s': %v\n", dn, err)
	}
}

