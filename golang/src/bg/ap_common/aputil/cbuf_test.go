/*
 * COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package aputil

import (
	"testing"
)

var (
	testBuf *circularBuf
)

func (b *circularBuf) oneTest(t *testing.T, add, expected string) {
	n, err := b.Write([]byte(add))
	if err != nil {
		t.Fatalf("Failed to write '%s': %v", add, err)

	} else if n != len(add) {
		t.Fatalf("Wrote %d of %d bytes of '%s'", n, len(add), add)

	} else {
		c := string(b.contents())

		if c != expected {
			t.Fatalf("buffer contains '%s' - expected '%s'", c, expected)
		}
	}
}

func TestAll(t *testing.T) {
	testBuf = newCBuf(10)

	// test a simple insertion
	testBuf.oneTest(t, "test", "test")

	// test appending
	testBuf.oneTest(t, "test", "testtest")

	// test wrapping
	testBuf.oneTest(t, "1234", "sttest1234")

	// test an empty stringz
	testBuf.oneTest(t, "", "sttest1234")

	// test reseting
	testBuf.reset()
	testBuf.oneTest(t, "", "")
	testBuf.oneTest(t, "abcd", "abcd")

	// test inserting a string too large for the buffer
	testBuf.oneTest(t, "123456789ABCDEFG", "789ABCDEFG")
}
