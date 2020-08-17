/*
 * Copyright 2019 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
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

