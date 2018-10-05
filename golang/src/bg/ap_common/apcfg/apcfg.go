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

// APConfig is an opaque type representing a connection to ap.configd
type APConfig struct {
	mutex  sync.Mutex
	socket *zmq.Socket
	sender string

	platform       *platform.Platform
	broker         *broker.Broker
	changeHandlers []changeMatch
	deleteHandlers []delexpMatch
	expireHandlers []delexpMatch
	handling       bool
	level          cfgapi.AccessLevel
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

	var host string

	plat := platform.NewPlatform()
	if _, ok := cfgapi.AccessLevelNames[level]; !ok {
		return nil, fmt.Errorf("invalid access level: %d", level)
	}

	sender := fmt.Sprintf("%s(%d)", name, os.Getpid())

	socket, err := zmq.NewSocket(zmq.REQ)
	if err != nil {
		err = fmt.Errorf("failed to create new cfg socket: %v", err)
		return nil, err
	}

	err = socket.SetSndtimeo(time.Duration(base_def.LOCAL_ZMQ_SEND_TIMEOUT * time.Second))
	if err != nil {
		log.Printf("failed to set cfg send timeout: %v\n", err)
		return nil, err
	}

	err = socket.SetRcvtimeo(time.Duration(base_def.LOCAL_ZMQ_RECEIVE_TIMEOUT * time.Second))
	if err != nil {
		log.Printf("failed to set cfg receive timeout: %v\n", err)
		return nil, err
	}

	if aputil.IsSatelliteMode() {
		host = base_def.GATEWAY_ZMQ_URL
	} else {
		host = base_def.LOCAL_ZMQ_URL
	}
	err = socket.Connect(host + base_def.CONFIGD_ZMQ_REP_PORT)
	if err != nil {
		err = fmt.Errorf("failed to connect new cfg socket: %v", err)
		return nil, err
	}

	c := &APConfig{
		sender:         sender,
		socket:         socket,
		broker:         b,
		platform:       plat,
		level:          level,
		changeHandlers: make([]changeMatch, 0),
		deleteHandlers: make([]delexpMatch, 0),
		expireHandlers: make([]delexpMatch, 0),
	}

	if err = c.Ping(nil); err != nil {
		return nil, err
	}

	return cfgapi.NewHandle(c), nil
}

func (c *APConfig) sendOp(query *cfgmsg.ConfigQuery) (string, error) {

	query.Sender = c.sender
	query.Level = int32(c.level)
	op, err := proto.Marshal(query)
	if err != nil {
		return "", fmt.Errorf("unable to build ping: %v", err)
	}

	response := &cfgmsg.ConfigResponse{}
	c.mutex.Lock()
	_, err = c.socket.SendBytes(op, 0)
	if err != nil {
		log.Printf("Failed to send config msg: %v\n", err)
		err = cfgapi.ErrComm
	} else {
		reply, rerr := c.socket.RecvMessageBytes(0)
		if rerr != nil {
			log.Printf("Failed to receive config reply: %v\n", err)
			err = cfgapi.ErrComm
		} else if len(reply) > 0 {
			proto.Unmarshal(reply[0], response)
		}
	}
	c.mutex.Unlock()

	return response.Parse()
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
