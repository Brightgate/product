/*
 * COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package apcfg

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"syscall"
	"time"

	"bg/ap_common/aputil"
	"bg/ap_common/broker"
	"bg/ap_common/platform"
	"bg/base_def"
	"bg/common/cfgapi"
	"bg/common/cfgmsg"

	"github.com/golang/protobuf/proto"

	// Ubuntu: requires libzmq3-dev, which is 0MQ 4.2.1.
	zmq "github.com/pebbe/zmq4"
)

const (
	sendTimeout = base_def.LOCAL_ZMQ_SEND_TIMEOUT * time.Second
	recvTimeout = base_def.LOCAL_ZMQ_RECV_TIMEOUT * time.Second
)

// APConfig is an opaque type representing a connection to ap.configd
type APConfig struct {
	socket *zmq.Socket
	sender string

	platform       *platform.Platform
	broker         *broker.Broker
	changeHandlers []changeMatch
	deleteHandlers []delexpMatch
	expireHandlers []delexpMatch
	handling       bool
	level          cfgapi.AccessLevel

	sync.Mutex
}

type cmdStatus struct {
	rval string
	err  error
}

func (c *cmdStatus) Status(ctx context.Context) (string, error) {
	return c.rval, c.err
}

func (c *cmdStatus) Wait(ctx context.Context) (string, error) {
	return c.Status(ctx)
}

// NewConfigd will connect to ap.configd, and will return a handle used for
// subsequent interactions with the daemon
func NewConfigd(b *broker.Broker, name string,
	level cfgapi.AccessLevel) (*cfgapi.Handle, error) {

	plat := platform.NewPlatform()
	if _, ok := cfgapi.AccessLevelNames[level]; !ok {
		return nil, fmt.Errorf("invalid access level: %d", level)
	}

	c := &APConfig{
		sender:         fmt.Sprintf("%s(%d)", name, os.Getpid()),
		broker:         b,
		platform:       plat,
		level:          level,
		changeHandlers: make([]changeMatch, 0),
		deleteHandlers: make([]delexpMatch, 0),
		expireHandlers: make([]delexpMatch, 0),
	}

	if err := c.reconnect(); err != nil {
		return nil, err
	}

	if err := c.Ping(nil); err != nil {
		return nil, err
	}

	return cfgapi.NewHandle(c), nil
}

func (c *APConfig) reconnect() error {
	var url string

	c.disconnect()

	socket, err := zmq.NewSocket(zmq.REQ)
	if err != nil {
		return fmt.Errorf("failed to create new cfg socket: %v", err)
	}

	if err = socket.SetSndtimeo(sendTimeout); err != nil {
		log.Printf("failed to set cfg send timeout: %v\n", err)
	}

	if err = socket.SetRcvtimeo(recvTimeout); err != nil {
		log.Printf("failed to set cfg receive timeout: %v\n", err)
	}

	if aputil.IsSatelliteMode() {
		url = base_def.GATEWAY_ZMQ_URL + base_def.CONFIGD_ZMQ_REP_PORT
	} else {
		url = base_def.LOCAL_ZMQ_URL + base_def.CONFIGD_ZMQ_REP_PORT
	}

	if err = socket.Connect(url); err != nil {
		return fmt.Errorf("failed to connect to configd: %v", err)
	}
	c.socket = socket

	return nil
}

func (c *APConfig) disconnect() {
	if c.socket != nil {
		c.socket.Close()
		c.socket = nil
	}
}

func (c *APConfig) sendOp(query *cfgmsg.ConfigQuery) (string, error) {
	const retryLimit = 3
	var reply [][]byte

	query.Sender = c.sender
	query.Level = int32(c.level)
	op, err := proto.Marshal(query)
	if err != nil {
		return "", fmt.Errorf("unable to build op: %v", err)
	}

	// Because of ZeroMQ's rigid state machine, a failed message can leave a
	// socket permanently broken.  To avoid this, we will close and reopen
	// the socket on an error.  If it fails repeatedly, we give up and
	// return the error to the caller.  (this most likely means that configd
	// is crashed, and mcp will be restarting everything anyway.)
	c.Lock()
	for retries := 0; retries < retryLimit; retries++ {
		if c.socket == nil {
			if err = c.reconnect(); err != nil {
				log.Printf("%v\n", err)
				continue
			}
		}

		phase := "sending"
		_, err = c.socket.SendBytes(op, 0)
		if err == nil {
			phase = "receiving"
			// If the read fails with EINTR, it most likely means
			// that we got a SIGCHLD when a worker process exited.
			// Some versions of ZeroMQ will silently retry this
			// read internally.  Since our version returns the
			// error, we do the retry ourselves.  If we fail
			// with EINTR multiple times, we'll give up and attempt
			// to build a new connection.
			for ; retries < retryLimit; retries++ {
				reply, err = c.socket.RecvMessageBytes(0)
				if err != zmq.Errno(syscall.EINTR) {
					break
				}
			}
		}
		if err == nil {
			break
		}

		log.Printf("while %s: %v\n", phase, err)
		c.disconnect()
	}
	c.Unlock()

	var rval string
	if err == nil && len(reply) > 0 {
		response := &cfgmsg.ConfigResponse{}
		proto.Unmarshal(reply[0], response)
		rval, err = response.Parse()
	}

	return rval, err
}

// Ping performs a simple round-trip communication with ap.configd, just to
// verify that the connection is up and running.
func (c *APConfig) Ping(ctx context.Context) error {
	query := cfgmsg.NewPingQuery()
	_, err := c.sendOp(query)
	if err != nil {
		err = fmt.Errorf("ping failed: %v", err)
	}
	return err
}

// Execute takes a slice of PropertyOp structures, marshals them into a protobuf
// query, and sends that to ap.configd.  It then unmarshals the result from
// ap.configd, and returns that to the caller.
func (c *APConfig) Execute(ctx context.Context, ops []cfgapi.PropertyOp) cfgapi.CmdHdl {

	rval := &cmdStatus{}

	if len(ops) != 0 {
		query, err := cfgmsg.NewPropQuery(ops)
		if query == nil {
			rval.err = err
		} else {
			rval.rval, rval.err = c.sendOp(query)
		}
	}

	return rval
}

// Close closes the link to *.configd.  On the appliance, this is a no-op.
func (c *APConfig) Close() {
}
