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
