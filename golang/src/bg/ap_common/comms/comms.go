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
	"time"

	"nanomsg.org/go/mangos/v2"
	"nanomsg.org/go/mangos/v2/protocol/rep"
	"nanomsg.org/go/mangos/v2/protocol/req"
	// Importing the TCP transport
	_ "nanomsg.org/go/mangos/v2/transport/tcp"
)

// APComm is an opaque handle representing either a client or server
// communications endpoint
type APComm struct {
	url    string
	client bool
	isOpen bool
	debug  bool

	active bool
	socket mangos.Socket

	sendTimeout time.Duration
	recvTimeout time.Duration
	openTimeout time.Duration

	sync.Mutex
}

func newAPComm(url string, client bool) (*APComm, error) {
	var err error
	var sock mangos.Socket

	c := &APComm{
		url:         url,
		client:      client,
		active:      true,
		sendTimeout: 2 * time.Second,
		recvTimeout: 5 * time.Second,
		openTimeout: time.Second,
	}

	if client {
		sock, err = req.NewSocket()
	} else {
		sock, err = rep.NewSocket()
	}
	if err != nil {
		return nil, fmt.Errorf("creating socket: %v", err)
	}

	sock.SetOption(mangos.OptionWriteQLen, 0)
	c.socket = sock
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

// SetDebug enables/disables debug messages
func (c *APComm) SetDebug(val bool) {
	c.debug = val
}

// Close closes the socket
func (c *APComm) close() {
	if c.isOpen {
		c.socket.Close()
		c.isOpen = false
	}
}

// Make a single attempt at creating the socket and either opening the
// server port or connecting to the server.
func (c *APComm) tryOpen() error {
	var err error

	if c.isOpen {
		return nil
	}

	if c.debug {
		log.Printf("open attempt")
	}

	if c.client {
		if err = c.socket.Dial(c.url); err != nil {
			err = fmt.Errorf("dialing socket %s: %v", c.url, err)
		}
	} else {
		if err = c.socket.Listen(c.url); err != nil {
			err = fmt.Errorf("listening on socket %s: %v", c.url, err)
		}
	}
	c.isOpen = (err == nil)

	if c.debug {
		if c.isOpen {
			log.Printf("open successful")
		} else {
			log.Printf("open failed: %v", err)
		}
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

	if c.debug {
		log.Printf("Starting open() deadline: %v", deadline)
	}
	for c.active {
		if err = c.tryOpen(); err == nil {
			break
		}

		now := time.Now()
		if now.After(nextWarn) {
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

	if c.debug {
		if err != nil {
			log.Printf("open failed: %v", err)
		} else {
			log.Printf("open successful")
		}
	}
	return err
}

// Send is used by a client to send a message to a server.  After sending the
// message, the call will block until the server sends a reply, which is
// returned as the result of this call.
func (c *APComm) Send(msg []byte) ([]byte, error) {
	var reply []byte
	var err error
	var deadlineType string

	c.Lock()
	defer c.Unlock()

	if !c.client {
		return nil, fmt.Errorf("servers can't Send()")
	}

	var deadline time.Time
	if c.socket == nil {
		deadline = time.Now().Add(c.openTimeout)
		deadlineType = "open"
	} else if c.recvTimeout < c.sendTimeout {
		deadline = time.Now().Add(c.recvTimeout)
		deadlineType = "recv"
	} else {
		deadline = time.Now().Add(c.sendTimeout)
		deadlineType = "send"
	}

	if c.debug {
		log.Printf("Sending %d bytes.  %s deadline: %v",
			len(msg), deadlineType, deadline)
	}

	for c.active {
		if time.Now().After(deadline) {
			err = fmt.Errorf("timed out")
			break
		}

		if err = c.tryOpen(); err != nil {
			continue
		}

		phase := "sending"
		if c.debug {
			log.Printf("sending")
		}
		timeout := deadline.Sub(time.Now())
		err = c.socket.SetOption(mangos.OptionSendDeadline, timeout)
		if err != nil {
			log.Printf("setting send deadline: %v", err)
		}
		if err = c.socket.Send(msg); err == nil {
			phase = "receiving reply"
			if c.debug {
				log.Printf("receiving")
			}
			timeout = deadline.Sub(time.Now())
			err = c.socket.SetOption(mangos.OptionRecvDeadline, timeout)
			reply, err = c.socket.Recv()
		}
		if err == nil {
			break
		}

		err = fmt.Errorf("%s: %v", phase, err)
		if c.debug {
			log.Printf("failed: %v", err)
		}
		c.close()
	}

	if c.debug {
		if err != nil {
			log.Printf("send failed: %v", err)
		} else {
			log.Printf("sent %d bytes, got %d bytes",
				len(msg), len(reply))
		}
	}

	return reply, err
}

// Serve is used by a server to handle incoming messages from clients.  The
// caller provides a callback which will be invoked for each message received.
func (c *APComm) Serve(cb func([]byte) []byte) error {
	c.Lock()
	defer c.Unlock()

	if c.client {
		return fmt.Errorf("called Serve() on a client endpoint")
	}

	for c.active {
		if !c.isOpen {
			c.open()
			continue
		}

		c.Unlock()
		msg, err := c.socket.Recv()
		c.Lock()
		if err != nil {
			c.close()
		} else if len(msg) > 0 {
			resp := cb(msg)
			if c.isOpen {
				c.socket.Send(resp)
			}
		}
	}
	return nil
}

// Close closes the endpoint
func (c *APComm) Close() {
	c.Lock()
	defer c.Unlock()

	c.active = false
	c.close()
}
