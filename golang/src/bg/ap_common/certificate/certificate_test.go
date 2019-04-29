//
// COPYRIGHT 2019 Brightgate Inc. All rights reserved.
//
// This copyright notice is Copyright Management Information under 17 USC 1202
// and is included to protect this work and deter copyright infringement.
// Removal or alteration of this Copyright Management Information without the
// express written permission of Brightgate Inc is prohibited, and any
// such unauthorized removal or alteration will be a violation of federal law.
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

	killGen := make(chan bool)
	paths, err := createSSKeyCert(nil, dn, "testhost.local", killGen)
	close(killGen)

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
