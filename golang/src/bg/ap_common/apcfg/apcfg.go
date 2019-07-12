/*
 * COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
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
	"bg/ap_common/comms"
	"bg/ap_common/mcp"
	"bg/ap_common/platform"
	"bg/base_def"
	"bg/base_msg"
	"bg/common/cfgapi"
	"bg/common/cfgmsg"

	"github.com/golang/protobuf/proto"
)

const (
	errLimit = 5
	daemon   = "configd"
)

// APConfig is an opaque type representing a connection to ap.configd
type APConfig struct {
	comm   *comms.APComm
	name   string
	sender string

	platform       *platform.Platform
	broker         *broker.Broker
	changeHandlers []changeMatch
	deleteHandlers []delexpMatch
	expireHandlers []delexpMatch
	handling       bool
	level          cfgapi.AccessLevel
	errChan        *chan error

	sync.Mutex
}

func (c *APConfig) sendSysError() {
	sysErr := &base_msg.EventSysError{
		Timestamp: aputil.NowToProtobuf(),
		Sender:    proto.String(c.sender),
		Debug:     proto.String("-"),
		Message:   proto.String(daemon + " not responding."),
	}

	err := c.broker.Publish(sysErr, base_def.TOPIC_ERROR)
	if err != nil {
		log.Printf("couldn't publish %s: %v", base_def.TOPIC_ERROR, err)
	}
}

// HealthMonitor runs as a goroutine to track all failures to communicate with
// configd.  When we exceed a certain threshhold, we assume it means that
// configd is in an unrecoverable state, and we ask mcp to kill it.
func HealthMonitor(api *cfgapi.Handle, mcp *mcp.MCP) {
	var nextCrash time.Time

	c := api.GetComm().(*APConfig)

	c.Lock()
	if c.errChan != nil {
		log.Fatalf("multiple config healthMonitors created")
	}

	errChan := make(chan error)
	c.errChan = &errChan
	c.Unlock()

	errCnt := 0
	for {
		err := <-errChan
		if err == nil {
			if errCnt != 0 {
				log.Printf("clearing after %d errors\n", errCnt)
			}
			errCnt = 0
		} else {
			errCnt++
			if errCnt > errLimit && time.Now().After(nextCrash) {
				log.Printf("configd not responding: " +
					"notifying ap.mcp\n")
				c.Lock()
				c.sendSysError()
				c.Unlock()
				if err = mcp.Do(daemon, "crash"); err != nil {
					log.Printf("failed to crash %s: %v",
						daemon, err)
				}

				// mcp's dependency chain should result in this
				// daemon being taken down right after configd,
				// but this is an asynchronous process.  Give
				// it a little time to complete before trying
				// again.
				nextCrash = time.Now().Add(10 * time.Second)
				errCnt = 0
			}
		}
	}
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

	var url string
	var err error

	plat := platform.NewPlatform()
	if _, ok := cfgapi.AccessLevelNames[level]; !ok {
		return nil, fmt.Errorf("invalid access level: %d", level)
	}

	c := &APConfig{
		name:           name,
		sender:         fmt.Sprintf("%s(%d)", name, os.Getpid()),
		broker:         b,
		platform:       plat,
		level:          level,
		changeHandlers: make([]changeMatch, 0),
		deleteHandlers: make([]delexpMatch, 0),
		expireHandlers: make([]delexpMatch, 0),
	}

	if aputil.IsSatelliteMode() {
		url = base_def.GATEWAY_ZMQ_URL + base_def.CONFIGD_ZMQ_REP_PORT
	} else {
		url = base_def.LOCAL_ZMQ_URL + base_def.CONFIGD_ZMQ_REP_PORT
	}

	c.comm, err = comms.NewAPClient(url)
	if err != nil {
		return nil, fmt.Errorf("creating new client: %v", err)
	}

	if err := c.Ping(nil); err != nil {
		return nil, err
	}

	hdl := cfgapi.NewHandle(c)
	settingsInit(hdl, c)

	return hdl, nil
}

func (c *APConfig) sendOp(query *cfgmsg.ConfigQuery) (string, error) {
	var rval string

	query.Sender = c.sender
	query.Level = int32(c.level)
	msg, err := proto.Marshal(query)
	if err != nil {
		return "", fmt.Errorf("unable to build op: %v", err)
	}

	reply, err := c.comm.Send(msg)

	if c.errChan != nil {
		(*c.errChan) <- err
	}

	if err == nil && len(reply) > 0 {
		r := &cfgmsg.ConfigResponse{}
		proto.Unmarshal(reply, r)
		rval, err = cfgapi.ParseConfigResponse(r)
	}

	return rval, err
}

// Ping performs a simple round-trip communication with ap.configd, just to
// verify that the connection is up and running.
func (c *APConfig) Ping(ctx context.Context) error {
	query := cfgapi.NewPingQuery()
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
		query, err := cfgapi.PropOpsToQuery(ops)
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
