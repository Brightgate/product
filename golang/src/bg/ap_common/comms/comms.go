/*
 * COPYRIGHT 2020 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package comms

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strings"
	"sync"
	"time"
)

const maxMsgSize = (16 * 1024 * 1024)

// APComm is an opaque handle representing either a client or server
// communications endpoint
type APComm struct {
	name  string
	addr  string
	debug bool

	conn net.Conn
	inq  chan *msg

	hdrTimeout  time.Duration
	recvTimeout time.Duration
	sendTimeout time.Duration
	openTimeout time.Duration

	stats     *CommStats
	statsLock sync.Mutex

	sync.Mutex
}

type msg struct {
	in        []byte
	out       []byte
	recvd     time.Time
	dequeued  time.Time
	completed time.Time

	done chan bool
}

func newAPComm(name, addr string) (*APComm, error) {
	if !strings.HasPrefix(addr, "tcp://") {
		return nil, fmt.Errorf("invalid server prefix")
	}
	addr = strings.TrimPrefix(addr, "tcp://")

	c := &APComm{
		name:        name,
		addr:        addr,
		hdrTimeout:  3 * time.Second,
		recvTimeout: 4 * time.Second,
		sendTimeout: 3 * time.Second,
		openTimeout: 3 * time.Second,
	}

	return c, nil
}

// convert an integer into a 4-byte array
func lenEncode(l int) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, uint32(l))
	return b
}

// convert a 4-byte array into an integer
func lenDecode(b []byte) int {
	l := binary.BigEndian.Uint32(b)
	return int(l)
}

func (c *APComm) warnf(f string, a ...interface{}) {
	msg := fmt.Sprintf(f, a...)

	log.Printf("\tWARN\t%s", msg)
}

func (c *APComm) debugf(f string, a ...interface{}) {
	msg := fmt.Sprintf(f, a...)
	if c.debug {
		log.Printf("\tDEBUG\t%s", msg)
	}
}

// block until we receive 'msgSize' bytes from the client
func rxOne(conn net.Conn, msgSize int, deadline time.Time) ([]byte, error) {
	b := make([]byte, msgSize)
	off := 0
	for off < msgSize {
		if !deadline.IsZero() {
			conn.SetReadDeadline(deadline)
		}
		n, err := conn.Read(b[off:])
		if err != nil {
			if os.IsTimeout(err) && off > 0 {
				err = fmt.Errorf("timeout after %d of %d bytes",
					off, msgSize)
			}

			return nil, err
		}

		off += n
	}

	return b, nil
}

// Block until we receive a single message as a (length, body) pair
func (c *APComm) rx() ([]byte, error) {
	var hdrDeadline, recvDeadline time.Time
	var mbuf []byte

	hdrDeadline = time.Now().Add(c.hdrTimeout)
	recvDeadline = time.Now().Add(c.recvTimeout)

	// Get the message length
	buf, err := rxOne(c.conn, 4, hdrDeadline)
	if err == nil {
		// Get the message body
		l := lenDecode(buf)
		if l > maxMsgSize {
			err = fmt.Errorf("unreasonably large response: %d", l)
		} else {
			mbuf, err = rxOne(c.conn, l, recvDeadline)
		}
	}

	return mbuf, err
}

// Send the provided message across the network as a (length, body) pair
func (c *APComm) tx(data []byte) error {
	deadline := time.Now().Add(c.sendTimeout)
	b := lenEncode(len(data))

	// Send the message length
	c.conn.SetWriteDeadline(deadline)
	n, err := c.conn.Write(b)
	if err != nil {
		return fmt.Errorf("sending msg header: %v", err)
	} else if n != len(b) {
		return fmt.Errorf("hdr fail: wrote %d of %d", n, len(b))
	}

	// Send the message body
	c.conn.SetWriteDeadline(deadline)
	n, err = c.conn.Write(data)
	if err != nil {
		return fmt.Errorf("sending msg body: %v", err)
	} else if n != len(data) {
		return fmt.Errorf("body fail: wrote %d of %d", n, len(data))
	}

	return nil
}

// ReqRepl is used by a client to send a message to a server.  After sending the
// message, the call will block until the server sends a reply, which is
// returned as the result of this call.
func (c *APComm) ReqRepl(data []byte) ([]byte, error) {
	var rval []byte
	var err error

	c.Lock()
	defer c.Unlock()

	if c.conn == nil {
		conn, err := net.DialTimeout("tcp", c.addr, c.openTimeout)
		if err != nil {
			return nil, fmt.Errorf("connect failed: %v", err)
		}

		c.conn = conn
	}

	if err = c.tx(data); err == nil {
		rval, err = c.rx()
	}

	if err != nil {
		c.conn.Close()
		c.conn = nil
	}

	return rval, err
}

// NewAPClient will connect to a server, and will return a handle used for
// subsequent interactions with that server.
func NewAPClient(name, addr string) (*APComm, error) {
	c, err := newAPComm(name, addr)
	if err == nil {
		// perform a round trip transaction to the server to verify the
		// connection
		data, err := c.ReqRepl([]byte(name))
		if err == nil && string(data) != name {
			err = fmt.Errorf("initial transaction failed")
		}
	}

	return c, err
}

// The first transaction from a new client is always a ping/pong of its name.
func (c *APComm) hello() (string, error) {
	var data []byte
	var err error

	deadline := time.Now().Add(c.openTimeout)
	for time.Now().Before(deadline) {
		data, err = c.rx()
		if err == nil {
			err = c.tx(data)
		} else if os.IsTimeout(err) {
			continue
		}
		break
	}

	return string(data), err
}

// handle requests from a single client
func (c *APComm) serverLoop() {
	var data []byte
	var err error

	defer c.conn.Close()

	c.name, err = c.hello()
	if err != nil {
		c.debugf("connection from %s failed: %v\n", c.addr, err)
		return
	}
	c.debugf("new client: %v is %s\n", c.addr, c.name)

	for {
		// wait for new requests to arrive from the client
		if data, err = c.rx(); err != nil {
			if os.IsTimeout(err) {
				continue
			}
			break
		}

		// package the request into a msg structure, and push it on the
		// request queue
		doneChan := make(chan bool)
		m := &msg{in: data, done: doneChan}
		c.observeRcvd(m)
		c.inq <- m

		// wait for the request to be completed
		<-doneChan

		// push the result back to the client
		if err = c.tx(m.out); err != nil {
			break
		}
		c.observeReplied(m)

		// make eligible for GC immediately
		m = nil
		data = nil
	}
	if err == io.EOF {
		c.debugf("%s closed\n", c.name)
	} else {
		c.debugf("%s failed: %v", c.name, err)
	}
}

// wait for new clients to connect to the server
func (c *APComm) waitForClients() {
	for {
		c.debugf("Listening on %s\n", c.addr)
		ln, err := net.Listen("tcp", c.addr)
		if err != nil {
			c.warnf("unable to listen on %s\n", c.addr)
			time.Sleep(time.Second)
			continue
		}
		defer ln.Close()
		for {
			conn, err := ln.Accept()
			if err != nil {
				c.warnf("Unable to accept on %s\n", c.addr)
				ln.Close()
				break
			}
			addr := "tcp://" + conn.RemoteAddr().String()
			client, err := newAPComm("", addr)
			if err != nil {
				c.warnf("new client failed: %v\n", err)
			} else {
				client.conn = conn
				client.stats = c.stats
				client.inq = c.inq
				go client.serverLoop()
			}
		}
	}
}

// Serve is used by a server to handle incoming messages from clients.  The
// caller provides a callback which will be invoked for each message received.
func (c *APComm) Serve(cb func([]byte) []byte) error {
	for {
		msg := <-c.inq
		c.observeDeqd(msg)

		msg.out = cb(msg.in)
		c.observeExeced(msg)

		msg.done <- true
	}
}

// NewAPServer will open a server port, and will return a handle used for
// subsequent interactions with that server.
func NewAPServer(name, addr string) (*APComm, error) {
	c, err := newAPComm(name, addr)
	c.stats = &CommStats{}

	c.debug = true
	if err == nil {
		c.inq = make(chan *msg, 32)
		go c.waitForClients()
	}

	return c, err
}

// SetRecvTimeout limits the amount of time we will block waiting for a receive
// to complete
func (c *APComm) SetRecvTimeout(d time.Duration) {
	c.hdrTimeout = d
	c.recvTimeout = d
}

// SetSendTimeout limits the amount of time we will block waiting for a send
// to complete
func (c *APComm) SetSendTimeout(d time.Duration) {
	c.sendTimeout = d
}

// SetOpenTimeout limits the amount of time we will block waiting for an open
// to complete
func (c *APComm) SetOpenTimeout(d time.Duration) {
	c.openTimeout = d
}

// Close closes and releases any open TCP connection
func (c *APComm) Close() {
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
}
