/*
 * COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package clcfg

import (
	"context"
	"fmt"
	"log"
	"time"

	rpc "bg/cloud_rpc"
	"bg/common/cfgapi"
	"bg/common/cfgmsg"
	"bg/common/grpcutils"

	"github.com/golang/protobuf/ptypes"
	"google.golang.org/grpc"
)

type cmdHdl struct {
	cfg    *Configd
	cmdID  int64
	result string
	err    error
}

// Configd represents an established gRPC connection to cl.configd.
type Configd struct {
	sender  string
	uuid    string
	verbose bool
	conn    *grpc.ClientConn
	client  rpc.ConfigFrontEndClient
	timeout time.Duration
	level   cfgapi.AccessLevel
}

// NewConfigd establishes a gRPC connection to cl.configd, and returns a
// handle representing the connection
func NewConfigd(pname, uuid, url string, tlsEnabled bool) (*Configd, error) {
	if uuid == "" {
		return nil, fmt.Errorf("no appliance uuid provided")
	}

	conn, err := grpcutils.NewClientConn(url, tlsEnabled, pname)
	if err != nil {
		return nil, fmt.Errorf("grpc connection () to '%s' failed: %v",
			url, err)
	}
	client := rpc.NewConfigFrontEndClient(conn)

	cfg := &Configd{
		sender:  pname,
		uuid:    uuid,
		conn:    conn,
		client:  client,
		timeout: 10 * time.Second,
		level:   cfgapi.AccessUser,
	}

	return cfg, nil
}

// SetVerbose enables/disables verbose logging
func (c *Configd) SetVerbose(v bool) {
	c.verbose = v
}

// SetTimeout changes the timeout applied to configd communications
func (c *Configd) SetTimeout(d time.Duration) {
	c.timeout = d
}

// SetLevel changes the access level used to perform config operations
func (c *Configd) SetLevel(level cfgapi.AccessLevel) error {
	var err error

	if _, ok := cfgapi.AccessLevelNames[level]; !ok {
		err = fmt.Errorf("invalid access level: %d", level)
	} else {
		c.level = level
	}
	return err
}

// Status returns the current status of a command.  Once we have the command's
// final status, we cache the result locally.  Until the command completes, we
// perform an rpc to cl.configd to get the current status.
func (c *cmdHdl) Status(ctx context.Context) (string, error) {
	var msg string
	var err error

	if c.cfg.verbose {
		log.Printf("checking status of cmdID %d\n", c.cmdID)
	}

	if c.err != nil || c.result != "" {
		err = c.err
		msg = c.result
	} else {
		cmd := rpc.CfgCmdID{
			Time:      ptypes.TimestampNow(),
			CloudUuid: c.cfg.uuid,
			CmdID:     c.cmdID,
		}

		r, rerr := c.cfg.client.Status(ctx, &cmd)
		if rerr != nil {
			err = cfgapi.ErrComm
			msg = fmt.Sprintf("%v", rerr)
		} else {
			msg, err = r.Parse()
		}
	}

	if c.cfg.verbose {
		if err == nil {
			log.Printf("ok\n")
		} else {
			log.Printf("failed: %v\n", err)
		}
	}
	return msg, err
}

// Wait will block until the given command completes or times out
func (c *cmdHdl) Wait(ctx context.Context) (string, error) {
	var msg string
	var err error

	deadline := time.Now().Add(time.Second * c.cfg.timeout)
	for {
		msg, err = c.Status(ctx)
		if err != cfgapi.ErrQueued && err != cfgapi.ErrInProgress {
			break
		}

		if time.Now().After(deadline) {
			err = fmt.Errorf("timed out with %d in-flight",
				c.cmdID)
			break
		}

		time.Sleep(time.Second)
	}
	return msg, err
}

// Send a packaged ConfigQuery to cl.configd.  Build and return a cmdHdl to the
// caller.
func (c *Configd) submit(ctx context.Context, cmd *cfgmsg.ConfigQuery) *cmdHdl {
	if ctx == nil {
		ctx = context.Background()
	}
	deadline := time.Now().Add(c.timeout)
	ctx, ctxcancel := context.WithDeadline(ctx, deadline)
	defer ctxcancel()

	r, err := c.client.Submit(ctx, cmd)
	hdl := cmdHdl{
		cfg:   c,
		cmdID: r.CmdID,
	}
	if err == nil {
		hdl.result, hdl.err = r.Parse()
	} else {
		hdl.err = cfgapi.ErrComm
		hdl.result = fmt.Sprintf("%v", err)
	}
	return &hdl
}

// Ping sends a single ping message to configd just to verify that a round-trip
// grpc call can succeed.
func (c *Configd) Ping(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	deadline := time.Now().Add(c.timeout)
	ctx, ctxcancel := context.WithDeadline(ctx, deadline)
	defer ctxcancel()

	ping := rpc.CfgFrontEndPing{Time: ptypes.TimestampNow()}
	_, err := c.client.Ping(ctx, &ping)

	if c.verbose {
		if err != nil {
			log.Printf("ping failed: %v\n'", err)
		} else {
			log.Printf("ping succeeded\n")
		}
	}
	return err
}

// Execute takes a slice of PropertyOp structs, converts them to a
// cfgapi.ConfigQuery, and sends it to cl.configd.
func (c *Configd) Execute(ctx context.Context, ops []cfgapi.PropertyOp) cfgapi.CmdHdl {
	cmd, err := cfgmsg.NewPropQuery(ops)
	if err != nil {
		return &cmdHdl{cfg: c, err: err}
	}

	cmd.Sender = c.sender
	cmd.Level = int32(c.level)
	cmd.IdentityUuid = c.uuid

	if c.verbose {
		log.Printf("submitting command\n")
	}
	hdl := c.submit(ctx, cmd)
	if c.verbose {
		log.Printf("submitted command.  Assigned cmdID: %d\n", hdl.cmdID)
	}

	return hdl
}

// HandleChange is not supported in the cloud
func (c *Configd) HandleChange(path string,
	fn func([]string, string, *time.Time)) error {

	return cfgapi.ErrNotSupp
}

// HandleDelete is not supported in the cloud
func (c *Configd) HandleDelete(path string, fn func([]string)) error {
	return cfgapi.ErrNotSupp
}

// HandleExpire is not supported in the cloud
func (c *Configd) HandleExpire(path string, fn func([]string)) error {
	return cfgapi.ErrNotSupp
}

// Close cleans up the gRPC connection to cl.configd
func (c *Configd) Close() {
	c.conn.Close()
}
