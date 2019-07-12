/*
 * COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package comms

import (
	"fmt"
	"log"
	"sync"
	"syscall"
	"time"

	zmq "github.com/pebbe/zmq4"
)

const ()

// APComm is an opaque handle representing either a client or server
// communications endpoint
type APComm struct {
	url    string
	client bool

	active     bool
	socket     *zmq.Socket
	socketType zmq.Type

	sendTimeout time.Duration
	recvTimeout time.Duration
	openTimeout time.Duration

	sync.Mutex
}

func newAPComm(url string, client bool) (*APComm, error) {
	c := &APComm{
		url:         url,
		client:      client,
		active:      true,
		sendTimeout: 2 * time.Second,
	}

	if client {
		c.openTimeout = 5 * time.Second
		c.recvTimeout = 5 * time.Second
		c.socketType = zmq.REQ
	} else {
		c.socketType = zmq.REP
	}

	if err := c.open(); err != nil {
		return nil, err
	}

	return c, nil
}

// NewAPClient will connect to a server, and will return a handle used for
// subsequent interactions with that server.
func NewAPClient(url string) (*APComm, error) {
	return newAPComm(url, true)
}

// NewAPServer will open a server port, and will return a handle used for
// subsequent interactions with that server.
func NewAPServer(url string) (*APComm, error) {
	return newAPComm(url, false)
}

// SetRecvTimeout limits the amount of time we will block waiting for a receive
// to complete
func (c *APComm) SetRecvTimeout(d time.Duration) {
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

func (c *APComm) close() {
	if c.socket != nil {
		c.socket.Close()
		c.socket = nil
	}
}

// Make a single attempt at creating the ZMQ socket and either opening the
// server port or connecting to the server.
func (c *APComm) tryOpen() error {
	c.close()

	socket, err := zmq.NewSocket(c.socketType)
	if err != nil {
		return fmt.Errorf("creating socket: %v", err)
	}

	if c.sendTimeout > 0 {
		socket.SetSndtimeo(c.sendTimeout)
	}

	if c.recvTimeout > 0 {
		socket.SetRcvtimeo(c.recvTimeout)
	}

	if c.client {
		if err = socket.Connect(c.url); err != nil {
			err = fmt.Errorf("connecting: %v", err)
		}
	} else {
		if err = socket.Bind(c.url); err != nil {
			err = fmt.Errorf("binding: %v", err)
		}
	}

	if err == nil {
		c.socket = socket
	}

	return nil
}

// Try to open either the client or server port.  Continue trying until it
// succeeds or the openTimeout deadline expires.
func (c *APComm) open() error {
	var err error

	deadline := time.Now().Add(c.openTimeout)
	backoff := time.Duration(time.Millisecond)
	nextWarn := time.Now()

	for c.active {
		if err = c.tryOpen(); err == nil {
			break
		}

		now := time.Now()
		if now.After(nextWarn) {
			log.Printf("open failed: %v", err)
			nextWarn = time.Now().Add(time.Minute)
		}

		if c.openTimeout != 0 && now.After(deadline) {
			err = fmt.Errorf("open timed out")
			break
		}

		time.Sleep(backoff)
		if backoff *= 2; backoff > time.Second {
			backoff = time.Second
		}
	}

	return err
}

// Send is used by a client to send a message to a server.  After sending the
// message, the call will block until the server sends a reply, which is
// returned as the result of this call.
func (c *APComm) Send(msg []byte) ([]byte, error) {
	var reply [][]byte
	var err error

	c.Lock()
	defer c.Unlock()

	if !c.client {
		return nil, fmt.Errorf("servers can't Send()")
	}

	var deadline time.Time
	if c.socket == nil {
		deadline = time.Now().Add(c.openTimeout)
	} else {
		deadline = time.Now().Add(c.recvTimeout)
	}

	for c.active {
		if time.Now().After(deadline) {
			err = fmt.Errorf("timed out")
			break
		}

		if c.socket == nil {
			err = c.open()
			continue
		}

		phase := "sending"
		_, err = c.socket.SendBytes(msg, 0)
		if err == nil {
			phase = "receiving reply"
			// If the read fails with EINTR, it most likely means
			// that we got a SIGCHLD when a worker process exited.
			// Some versions of ZeroMQ will silently retry this
			// read internally.  Since our version returns the
			// error, we do the retry ourselves.  If we fail
			// with EINTR multiple times, we'll give up and attempt
			// to build a new connection.
			for retries := 0; retries < 3; retries++ {
				reply, err = c.socket.RecvMessageBytes(0)
				if err != zmq.Errno(syscall.EINTR) {
					break
				}
			}
		}
		if err == nil {
			break
		}
		err = fmt.Errorf("%s: %v", phase, err)

		// Because of ZeroMQ's rigid state machine, a failed message can
		// leave a socket permanently broken.  To avoid this, we will
		// close and reopen the socket on an error.  If it fails
		// repeatedly, we give up and return the error to the caller.
		c.close()
	}

	if err == nil && len(reply) > 0 {
		return reply[0], nil
	}

	return nil, err
}

// Serve is used by a server to handle incoming messages from clients.  The
// caller provides a callback which will be invoked for each message received.
func (c *APComm) Serve(cb func([]byte) []byte) error {

	if c.client {
		return fmt.Errorf("called Serve() on a client endpoint")
	}

	for c.active {
		if c.socket == nil {
			c.open()
			continue
		}

		msg, err := c.socket.RecvMessageBytes(0)
		if err != nil {
			c.close()

		} else if len(msg) > 0 {
			resp := cb(msg[0])
			if s := c.socket; s != nil {
				s.SendBytes(resp, 0)
			}
		}
	}
	return nil
}

// Close closes the endpoint
func (c *APComm) Close() {
	c.active = false
	c.close()
}
