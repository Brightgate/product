/*
 * Copyright 2019 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package aputil

// implementation of a circular buffer
type circularBuf struct {
	data  []byte
	size  int
	ptr   int
	total int
}

func newCBuf(sz int) *circularBuf {
	c := &circularBuf{
		data:  make([]byte, sz),
		size:  sz,
		ptr:   0,
		total: 0,
	}

	return c
}

// Write implements the io.Writer interface
func (c *circularBuf) Write(data []byte) (int, error) {
	n := len(data)
	c.total += n

	// If the incoming data doesn't fit in the buffer, we can just ignore
	// the first (n-size) bytes.
	rdPtr := 0
	if n > c.size {
		rdPtr = n - c.size
		n = c.size
	}

	for n > 0 {
		chunk := c.size - c.ptr
		if chunk > n {
			chunk = n
		}

		copy(c.data[c.ptr:], data[rdPtr:])
		c.ptr = (c.ptr + chunk) % c.size
		rdPtr += chunk
		n -= chunk
	}

	return len(data), nil
}

func (c *circularBuf) contents() []byte {
	if c.total <= c.size {
		return c.data[:c.ptr]
	}

	rval := c.data[c.ptr:]
	rval = append(rval, c.data[:c.ptr]...)
	return rval
}

func (c *circularBuf) reset() {
	c.ptr = 0
	c.total = 0
}

