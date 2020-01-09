/*
 * COPYRIGHT 2020 Brightgate Inc.  All rights reserved.
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
	"sync"
	"time"

	rpc "bg/cloud_rpc"
	"bg/common/cfgapi"
	"bg/common/cfgmsg"
	"bg/common/grpcutils"

	"github.com/golang/protobuf/ptypes"
	"google.golang.org/grpc"
)

type cmdHdl struct {
	cfg      *Configd
	cmdID    int64
	inflight bool
	result   string
	err      error
}

// Configd represents an established gRPC connection to cl.configd.
type Configd struct {
	sender   string
	uuid     string
	verbose  bool
	conn     *grpc.ClientConn
	client   rpc.ConfigFrontEndClient
	timeout  time.Duration
	level    cfgapi.AccessLevel
	monState *monitorState

	sync.Mutex
}

// NewConfigd establishes a gRPC connection to cl.configd, and returns a
// handle representing the connection
func NewConfigd(pname, uuid, url string, tlsEnabled bool) (*Configd, error) {
	var conn *grpc.ClientConn
	var err error

	if uuid == "" {
		return nil, fmt.Errorf("no site uuid provided")
	}

	if tlsEnabled {
		conn, err = grpcutils.NewClientTLSConn(url, "", pname, "")
	} else {
		conn, err = grpcutils.NewClientConn(url, pname)
	}
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

func (c *cmdHdl) String() string {
	return fmt.Sprintf("%s:%d", c.cfg.uuid, c.cmdID)
}

func (c *cmdHdl) update(r *cfgmsg.ConfigResponse) {
	if !c.inflight {
		log.Printf("Updating completed cmd %v\n", c)
	}

	c.result, c.err = cfgapi.ParseConfigResponse(r)
	c.inflight = (c.err == cfgapi.ErrQueued || c.err == cfgapi.ErrInProgress)
}

func (c *Configd) getContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	deadline := time.Now().Add(c.timeout)
	return context.WithDeadline(ctx, deadline)
}

// Status returns the current status of a command.  Once we have the command's
// final status, we cache the result locally.  Until the command completes, we
// perform an rpc to cl.configd to get the current status.
func (c *cmdHdl) Status(ctx context.Context) (string, error) {
	var msg string
	var err error

	if ctx == nil {
		ctx = context.Background()
	}
	if c.cfg.verbose {
		log.Printf("checking status of %v\n", c)
	}

	if c.inflight {
		cmd := rpc.CfgCmdID{
			Time:     ptypes.TimestampNow(),
			SiteUUID: c.cfg.uuid,
			CmdID:    c.cmdID,
		}

		r, rerr := c.cfg.client.Status(ctx, &cmd)
		if rerr != nil {
			err = cfgapi.ErrComm
			msg = fmt.Sprintf("getting status: %v", rerr)
		} else {
			c.update(r)
			if c.err != nil {
				msg = c.err.Error()
				err = c.err
			}
		}
	} else {
		msg = c.result
		err = c.err
	}

	if c.cfg.verbose {
		if err == nil {
			log.Printf("ok: %s\n", msg)
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

	deadline := time.Now().Add(c.cfg.timeout)
	for {
		ctx, ctxcancel := c.cfg.getContext(ctx)
		msg, err = c.Status(ctx)
		ctxcancel()
		if !c.inflight {
			break
		}

		if time.Now().After(deadline) {
			err = cfgapi.ErrTimeout
			break
		}
		time.Sleep(time.Second)
	}
	return msg, err
}

// Ping sends a single ping message to configd just to verify that a round-trip
// grpc call can succeed.
func (c *Configd) Ping(ctx context.Context) error {
	ctx, ctxcancel := c.getContext(ctx)
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

// ExecuteAt takes a slice of PropertyOp structs, converts them to a
// cfgapi.ConfigQuery, and sends it to cl.configd to be executed at the
// specified access level.
func (c *Configd) ExecuteAt(ctx context.Context, ops []cfgapi.PropertyOp,
	level cfgapi.AccessLevel) cfgapi.CmdHdl {
	hdl := &cmdHdl{
		cfg:      c,
		inflight: true,
	}

	cmd, err := cfgapi.PropOpsToQuery(ops)
	if err != nil {
		hdl.err = err
		return hdl
	}

	cmd.Sender = c.sender
	cmd.Level = int32(level)
	cmd.SiteUUID = c.uuid

	ctx, ctxcancel := c.getContext(ctx)
	defer ctxcancel()

	if c.verbose {
		log.Printf("submitting command\n")
	}
	r, err := c.client.Submit(ctx, cmd)
	if err == nil {
		hdl.cmdID = r.CmdID
		hdl.update(r)
		if c.verbose {
			log.Printf("submitted command: %v\n", hdl)
		}
	} else {
		hdl.err = cfgapi.ErrComm
		hdl.result = fmt.Sprintf("%v", err)
		hdl.inflight = false
		if c.verbose {
			log.Printf("submit failed: %v\n", err)
		}
	}

	return hdl
}

// Execute executes a set of operations at the default access level for this
// config handle.
func (c *Configd) Execute(ctx context.Context, ops []cfgapi.PropertyOp) cfgapi.CmdHdl {
	return c.ExecuteAt(ctx, ops, c.level)
}

// Close cleans up the gRPC connection to cl.configd
func (c *Configd) Close() {
	c.Lock()
	defer c.Unlock()

	c.conn.Close()
	if c.monState != nil {
		c.monState.cancelFunc()
	}
}
