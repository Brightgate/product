/*
 * COPYRIGHT 2018 Brightgate Inc. All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"bg/ap_common/aputil"
	"bg/base_msg"
	rpc "bg/cloud_rpc"
	"bg/common/cfgapi"
	"bg/common/cfgmsg"

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
)

const (
	maxCmds        = 64
	maxCompletions = 64
	maxUpdates     = 64
	maxBacklog     = (2 * maxCompletions)
)

type rpcClient struct {
	connected bool
	ctx       context.Context
	client    rpc.ConfigBackEndClient
}

// Map for translating cloud operations into local cfgapi operations
var opMap = map[cfgmsg.ConfigOp_Operation]int{
	cfgmsg.ConfigOp_GET:    cfgapi.PropGet,
	cfgmsg.ConfigOp_SET:    cfgapi.PropSet,
	cfgmsg.ConfigOp_CREATE: cfgapi.PropCreate,
	cfgmsg.ConfigOp_DELETE: cfgapi.PropDelete,
}

type cloudQueue struct {
	updates     []*rpc.CfgBackEndUpdate_CfgUpdate
	completions []*cfgmsg.ConfigResponse
	lastOp      int64
	updated     chan bool
	sync.Mutex
}

var queued = cloudQueue{
	updates:     make([]*rpc.CfgBackEndUpdate_CfgUpdate, 0),
	completions: make([]*cfgmsg.ConfigResponse, 0),
	updated:     make(chan bool, 4),
}

// utility function to attach a deadline to the current context
func (c *rpcClient) getCtx() (context.Context, context.CancelFunc) {
	ctx, err := applianceCred.MakeGRPCContext(c.ctx)
	if err != nil {
		slog.Fatalf("Failed to make GRPC context: %+v", err)
	}

	clientDeadline := time.Now().Add(*deadlineFlag)
	return context.WithDeadline(ctx, clientDeadline)
}

// execute a single Hello() gRPC.
func (c *rpcClient) hello() error {
	helloOp := &rpc.CfgBackEndHello{
		Time:    ptypes.TimestampNow(),
		Version: cfgapi.Version,
	}

	ctx, ctxcancel := c.getCtx()
	rval, err := c.client.Hello(ctx, helloOp)
	ctxcancel()

	if err != nil {
		err = fmt.Errorf("failed to send Hello() rpc: %v", err)
	} else if rval.Response == rpc.CfgBackEndResponse_ERROR {
		err = fmt.Errorf("Hello() failed: %s", rval.Errmsg)
	}

	return err
}

// send any queued Completions to the cloud
func (c *rpcClient) pushCompletions() error {
	var completions []*cfgmsg.ConfigResponse

	if len(queued.completions) == 0 {
		return nil
	}

	queued.Lock()
	if len(queued.completions) < maxCompletions {
		completions = queued.completions
		queued.completions = make([]*cfgmsg.ConfigResponse, 0)
	} else {
		completions = queued.completions[:maxCompletions]
		queued.completions = queued.completions[maxCompletions:]
	}
	queued.Unlock()

	slog.Debugf("completing %d cmds starting at %d",
		len(completions), completions[0].CmdID)
	completeOp := &rpc.CfgBackEndCompletions{
		Time:        ptypes.TimestampNow(),
		Completions: completions,
	}

	ctx, ctxcancel := c.getCtx()
	resp, err := c.client.CompleteCmds(ctx, completeOp)
	ctxcancel()

	if err != nil {
		c.connected = false
		slog.Infof("lost connection to cl.configd")
	} else if resp.Response == rpc.CfgBackEndResponse_ERROR {
		err = fmt.Errorf("CompleteCmds() failed: %s", resp.Errmsg)
	}

	// If the push fails re-queue the completions for a subsequent retry.
	if err != nil {
		queued.Lock()
		queued.completions = append(completions, queued.completions...)
		queued.Unlock()
	}
	return err
}

// send all of the accumulated config tree updates to the cloud
func (c *rpcClient) pushUpdates() error {
	var updates []*rpc.CfgBackEndUpdate_CfgUpdate

	if len(queued.updates) == 0 {
		return nil
	}

	queued.Lock()
	if len(queued.updates) < maxUpdates {
		updates = queued.updates
		queued.updates = make([]*rpc.CfgBackEndUpdate_CfgUpdate, 0)
	} else {
		updates = queued.updates[:maxUpdates]
		queued.updates = queued.updates[maxUpdates:]
	}
	queued.Unlock()

	updateOp := &rpc.CfgBackEndUpdate{
		Time:    ptypes.TimestampNow(),
		Version: cfgapi.Version,
		Updates: updates,
	}

	ctx, ctxcancel := c.getCtx()
	resp, err := c.client.Update(ctx, updateOp)
	ctxcancel()

	if err != nil {
		c.connected = false
		slog.Infof("lost connection to cl.configd")
	} else if resp.Response == rpc.CfgBackEndResponse_ERROR {
		err = fmt.Errorf("Update() failed: %s", resp.Errmsg)
	}

	// If we failed to forward the updates to the cloud, requeue them to
	// try again later
	if err != nil {
		queued.Lock()
		queued.updates = append(updates, queued.updates...)
		queued.Unlock()
	}

	return err
}

// Open a gRPC stream to cl.configd to receive commands from the cloud
func (c *rpcClient) fetchStream() error {
	fetchOp := &rpc.CfgBackEndFetchCmds{
		Time:      ptypes.TimestampNow(),
		Version:   cfgapi.Version,
		LastCmdID: queued.lastOp,
		MaxCmds:   maxCmds,
	}

	ctx, err := applianceCred.MakeGRPCContext(c.ctx)
	stream, err := c.client.FetchStream(ctx, fetchOp)
	if err != nil {
		slog.Fatalf("Failed to make GRPC context: %+v", err)
	}

	for {
		// XXX - can we attach a context to this, so we can do a clean
		// disconnect when this daemon goes down?
		slog.Debugf("blocking on config stream")
		resp, rerr := stream.Recv()
		if rerr != nil {
			c.connected = false
			slog.Infof("lost connection to cl.configd")
			return fmt.Errorf("failed to read from FetchStream: %v", err)
		}

		if resp.Response == rpc.CfgBackEndResponse_ERROR {
			return fmt.Errorf("FetchStream() failed: %s", resp.Errmsg)
		}

		if len(resp.Cmds) > 0 {
			cmds := resp.Cmds
			slog.Debugf("Got %d cmds, starting with %d", len(cmds),
				cmds[0].CmdID)
			for _, cmd := range cmds {
				exec(cmd)
			}
		}
		for len(queued.completions) > maxBacklog {
			slog.Debugf("blocking on completion backlog")
			time.Sleep(time.Second)
		}
	}
}

// utility function to determine whether any of the operations in a query are
// GETs
func isGet(ops []*cfgmsg.ConfigOp) bool {
	for _, op := range ops {
		if op.Operation == cfgmsg.ConfigOp_GET {
			return true
		}
	}
	return false
}

// Translate a cfgmsg protobuf into the equivalent cfgapi structure.  This is
// where a command to cl.configd becomes a command to ap.configd.
func translateOp(cloudOp *cfgmsg.ConfigOp) (cfgapi.PropertyOp, error) {
	var op cfgapi.PropertyOp
	var err error

	if cfgOp, ok := opMap[cloudOp.Operation]; ok {
		op.Op = cfgOp
		op.Name = cloudOp.GetProperty()
		op.Value = cloudOp.GetValue()
		if cloudOp.Expires != nil {
			t, _ := ptypes.Timestamp(cloudOp.Expires)
			op.Expires = &t
		}
	} else {
		err = fmt.Errorf("unrecognized operation")
	}

	return op, err
}

// Execute a single ConfigQuery fetched from the cloud
func exec(cmd *cfgmsg.ConfigQuery) {
	var err error
	var payload string

	slog.Debugf("executing cmd %d", cmd.CmdID)
	resp := cfgmsg.ConfigResponse{
		CmdID: cmd.CmdID,
	}

	// Convert the ConfigQuery into an array of one or more PropertyOp
	// operations
	ops := make([]cfgapi.PropertyOp, 0)
	for _, cloudOp := range cmd.Ops {
		var op cfgapi.PropertyOp

		op, err = translateOp(cloudOp)
		if err == nil {
			ops = append(ops, op)
		} else {
			break
		}
	}

	// Send the command to ap.configd and wait for the result
	if err == nil {
		payload, err = config.Execute(nil, ops).Wait(nil)
	}

	if err == nil {
		resp.Response = cfgmsg.ConfigResponse_OK
		resp.Value = payload
	} else {
		resp.Response = cfgmsg.ConfigResponse_FAILED
		resp.Errmsg = fmt.Sprintf("%v", err)
	}

	queued.Lock()
	if len(queued.completions) == 0 {
		queued.updated <- true
	}
	queued.completions = append(queued.completions, &resp)
	if resp.CmdID > queued.lastOp {
		queued.lastOp = resp.CmdID
	}
	queued.Unlock()

}

// An EventConfig event arrived on the 0MQ bus.  Convert the contents into a
// CfgUpdate message, which will be forwarded to the cloud config daemon.
func configEvent(raw []byte) {
	var update *rpc.CfgBackEndUpdate_CfgUpdate

	event := &base_msg.EventConfig{}
	proto.Unmarshal(raw, event)

	// Ignore messages without an explicit type.  Also ignore messages
	// without a hash, as they represent interim changes that will be
	// subsumed by a larger-scale update that will have a hash.
	if event.Type == nil || event.Hash == nil || event.Property == nil {
		return
	}

	hash := make([]byte, len(event.Hash))
	copy(hash, event.Hash)

	etype := *event.Type
	if etype == base_msg.EventConfig_CHANGE {
		if *event.Property == urlProperty {
			slog.Infof("Moving to new RPC server: " +
				*event.NewValue)
			go daemonStop()
			return
		}
		slog.Debugf("updated %s - %x", *event.Property, hash)
		update = &rpc.CfgBackEndUpdate_CfgUpdate{
			Type:     rpc.CfgBackEndUpdate_CfgUpdate_UPDATE,
			Property: *event.Property,
			Value:    *event.NewValue,
			Hash:     hash,
		}
		if event.Expires != nil {
			t := aputil.ProtobufToTime(event.Expires)
			p, _ := ptypes.TimestampProto(*t)
			update.Expires = p
		}
	} else if etype == base_msg.EventConfig_DELETE {
		slog.Debugf("deleted %s - %x", *event.Property, hash)
		update = &rpc.CfgBackEndUpdate_CfgUpdate{
			Type:     rpc.CfgBackEndUpdate_CfgUpdate_DELETE,
			Property: *event.Property,
			Hash:     hash,
		}
	}
	if update != nil {
		queued.Lock()
		if len(queued.updates) == 0 {
			queued.updated <- true
		}
		queued.updates = append(queued.updates, update)
		queued.Unlock()
	}
}

// Establish and maintain a connection to cl.configd
func (c *rpcClient) connectLoop(wg *sync.WaitGroup, doneChan chan bool) {

	defer wg.Done()
	done := false

	ticker := time.NewTicker(time.Second)
	nextLog := time.Now()
	slog.Infof("connect loop starting")
	for !done {
		if !c.connected {
			if err := c.hello(); err == nil {
				c.connected = true
				nextLog = time.Now()
				slog.Infof("established connection to cl.configd")
				queued.updated <- true
			} else if nextLog.Before(time.Now()) {
				slog.Errorf("Failed hello: %s", err)
				nextLog = time.Now().Add(10 * time.Minute)
			}
		}

		select {
		case done = <-doneChan:
		case <-ticker.C:
		}
	}
	slog.Infof("connect loop done")
}

// push property updates and command completions to the cloud
func (c *rpcClient) pushLoop(wg *sync.WaitGroup, doneChan chan bool) {

	done := false
	defer wg.Done()

	warned := false
	slog.Infof("push loop starting")
	ticker := time.NewTicker(time.Second)
	for !done {
		queued.Lock()
		pending := len(queued.updates) + len(queued.completions)
		queued.Unlock()

		if pending > 0 {
			if c.connected {
				// Push any queued updates or completions to the cloud
				if err := c.pushCompletions(); err != nil {
					slog.Error(err)
				}
				if err := c.pushUpdates(); err != nil {
					slog.Error(err)
				}
			} else {
				if !warned {
					slog.Infof("blocking on connect")
					warned = true
				}
				select {
				case done = <-doneChan:
				case <-ticker.C:
				}
			}
		} else {
			select {
			case done = <-doneChan:
			case <-queued.updated:
			}
		}
	}
	slog.Infof("push loop done")
}

func (c *rpcClient) pullLoop(wg *sync.WaitGroup, doneChan chan bool) {

	done := false
	defer wg.Done()

	slog.Infof("pull loop starting")

	ticker := time.NewTicker(time.Second)
	for !done {
		if c.connected {
			if err := c.fetchStream(); err != nil {
				slog.Warnf("fetchStream failed: %v", err)
				continue
			}
		}
		select {
		case done = <-doneChan:
		case <-ticker.C:
		}
	}
	slog.Infof("pull loop done")
}

func configLoop(ctx context.Context, client rpc.ConfigBackEndClient,
	wg *sync.WaitGroup, doneChan chan bool) {

	c := rpcClient{
		ctx:    ctx,
		client: client,
	}

	go c.pushLoop(wg, addDoneChan())
	go c.pullLoop(wg, addDoneChan())
	c.connectLoop(wg, doneChan)
}
