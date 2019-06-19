/*
 * COPYRIGHT 2019 Brightgate Inc. All rights reserved.
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
	"strings"
	"sync"
	"time"

	"bg/ap_common/aputil"
	"bg/base_def"
	"bg/base_msg"
	rpc "bg/cloud_rpc"
	"bg/common/cfgapi"
	"bg/common/cfgmsg"
	"bg/common/cfgtree"

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
)

const (
	restoreProp    = "@/cloud/restore_config"
	bucketProperty = "@/cloud/update/bucket"
)

type rpcClient struct {
	connected bool
	ctx       context.Context
	client    rpc.ConfigBackEndClient
}

type cloudQueue struct {
	updates     []*rpc.CfgUpdate
	completions []*cfgmsg.ConfigResponse
	lastOp      int64
	updated     chan bool
	sync.Mutex
}

var queued = cloudQueue{
	updates:     make([]*rpc.CfgUpdate, 0),
	completions: make([]*cfgmsg.ConfigResponse, 0),
	updated:     make(chan bool, 4),
}

// utility function to attach a deadline to the current context
func (c *rpcClient) getCtx() (context.Context, context.CancelFunc) {
	ctx, err := applianceCred.MakeGRPCContext(c.ctx)
	if err != nil {
		slog.Fatalf("Failed to make GRPC context: %+v", err)
	}

	clientDeadline := time.Now().Add(*rpcDeadline)
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
	if len(queued.completions) < *maxCompletions {
		completions = queued.completions
		queued.completions = make([]*cfgmsg.ConfigResponse, 0)
	} else {
		completions = queued.completions[:*maxCompletions]
		queued.completions = queued.completions[*maxCompletions:]
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
	var updates []*rpc.CfgUpdate

	if len(queued.updates) == 0 {
		return nil
	}

	queued.Lock()
	if len(queued.updates) < *maxUpdates {
		updates = queued.updates
		queued.updates = make([]*rpc.CfgUpdate, 0)
	} else {
		updates = queued.updates[:*maxUpdates]
		queued.updates = queued.updates[*maxUpdates:]
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

// If the cloud has asked for multiple copies of the full config tree, turn all
// but the first request into no-ops.
func trimRefreshDups(cmds []*cfgmsg.ConfigQuery) {
	refreshed := false

	// Find all "Get @/" operations
	nulled := 0
	for _, cmd := range cmds {
		if len(cmd.Ops) == 1 &&
			cmd.Ops[0].Operation == cfgmsg.ConfigOp_GET &&
			cmd.Ops[0].Property == "@/" {

			if refreshed {
				cmd.Ops = nil
				nulled++
			} else {
				refreshed = true
			}
		}
	}

	if nulled != 0 {
		slog.Debugf("nulled %d redundant gets", nulled)
	}
	if refreshed {
		// If we are sending back a full tree, then we should drop all
		// pending updates, as the contents of the updates will be
		// included in the full tree.  In addition, a tree request means
		// that the cloud has gotten out of sync, so all of the pending
		// updates will fail with a hash mismatch anyway.
		queued.Lock()
		if len(queued.updates) > 0 {
			slog.Infof("dropping %d stale updates", len(queued.updates))
			queued.updates = make([]*rpc.CfgUpdate, 0)
		}
		queued.Unlock()
	}
}

// Open a gRPC stream to cl.configd to receive commands from the cloud
func (c *rpcClient) fetchStream(ctx context.Context) error {
	fetchOp := &rpc.CfgBackEndFetchCmds{
		Time:      ptypes.TimestampNow(),
		Version:   cfgapi.Version,
		LastCmdID: queued.lastOp,
		MaxCmds:   uint32(*maxCmds),
	}

	ctx, err := applianceCred.MakeGRPCContext(ctx)
	if err != nil {
		slog.Fatalf("Failed to make GRPC context: %+v", err)
	}
	stream, err := c.client.FetchStream(ctx, fetchOp)
	if err != nil {
		slog.Fatalf("Failed to FetchStream: %+v", err)
	}

	for {
		// When ctx is canceled, this should abort
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
			trimRefreshDups(cmds)
			for _, cmd := range cmds {
				execQuery(cmd)
			}
		}
		for len(queued.completions) > 2**maxCompletions {
			slog.Debugf("blocking on completion backlog")
			time.Sleep(time.Second)
		}
	}
}

// Execute a single ConfigQuery fetched from the cloud
func execQuery(cmd *cfgmsg.ConfigQuery) {
	var payload string

	slog.Debugf("executing cmd %d", cmd.CmdID)

	ops, err := cfgapi.QueryToPropOps(cmd)

	// Send the command to ap.configd and wait for the result
	if err == nil {
		payload, err = config.Execute(nil, ops).Wait(nil)
	}

	resp := cfgapi.GenerateConfigResponse(payload, err)
	resp.CmdID = cmd.CmdID

	queued.Lock()
	emptyQueue := (len(queued.updates) == 0)
	queued.completions = append(queued.completions, resp)
	if resp.CmdID > queued.lastOp {
		queued.lastOp = resp.CmdID
	}
	queued.Unlock()
	if emptyQueue {
		queued.updated <- true
	}
}

// Look for the property changes that affect this daemon's behaviour
func handleChanges(prop, val string) {
	if strings.HasPrefix(prop, "@/cloud/svc_rpc/") {
		slog.Infof("change to RPC server config - restarting")
		go daemonStop()

	} else if prop == bucketProperty {
		configBucketChanged(val)
	}
}

// An EventConfig event arrived on the 0MQ bus.  Convert the contents into a
// CfgUpdate message, which will be forwarded to the cloud config daemon.
func configEvent(raw []byte) {
	var update *rpc.CfgUpdate

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
		slog.Debugf("updated %s - %x", *event.Property, hash)
		handleChanges(*event.Property, *event.NewValue)
		update = &rpc.CfgUpdate{
			Type:     rpc.CfgUpdate_UPDATE,
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
		update = &rpc.CfgUpdate{
			Type:     rpc.CfgUpdate_DELETE,
			Property: *event.Property,
			Hash:     hash,
		}
	}
	if update != nil {
		queued.Lock()
		emptyQueue := (len(queued.updates) == 0)
		queued.updates = append(queued.updates, update)
		queued.Unlock()
		if emptyQueue {
			queued.updated <- true
		}
	}
}

// attempt to download a fresh copy of the config tree from the cloud, and pass
// it to ap.configd.
func (c *rpcClient) restore() error {
	op := &rpc.CfgBackEndDownload{
		Time:    ptypes.TimestampNow(),
		Version: cfgapi.Version,
	}

	ctx, ctxcancel := c.getCtx()
	rval, err := c.client.Download(ctx, op)
	ctxcancel()

	if err != nil {
		return fmt.Errorf("failed to send Download() rpc: %v", err)
	}

	if rval.Response != rpc.CfgBackEndResponse_OK {
		if rval.Response == rpc.CfgBackEndResponse_NOCONFIG {
			slog.Infof("No config tree found in cloud")
			config.DeleteProp(restoreProp)
			return nil
		}

		return fmt.Errorf("Download() failed: %s", rval.Errmsg)
	}
	if rval.Value == nil {
		slog.Infof("Cloud doesn't support restore")
		config.DeleteProp(restoreProp)
		return nil
	}

	tree, err := cfgtree.NewPTree("@/", rval.Value)
	if err != nil {
		return fmt.Errorf("failed to parse tree from cl.rpcd: %v", err)
	}

	// Make sure our restored config tree doesn't send us into an infinite
	// loop of restoring
	tree.Delete(restoreProp)

	slog.Infof("Sending downloaded tree to configd")
	if err = config.Replace(tree.Export(false)); err != nil {
		return fmt.Errorf("restore failed: %v", err)
	}

	return nil
}

// Attempt to establish a connection to cl.configd (via cl.rpcd).  If the
// @/cloud/restore_config property is set, try to download a copy of the config
// tree from the cloud before allowing the upload/download goroutines to
// proceed.
func (c *rpcClient) connect() error {
	var restore bool

	prop, err := config.GetProp(restoreProp)
	if err == nil {
		restore = (strings.ToLower(prop) == "true")
	} else if err != cfgapi.ErrNoProp {
		// If we can't communicate with the local configd, don't try to
		// talk to the remote configd.
		return fmt.Errorf("fetching %s: %v", restoreProp, err)
	}

	err = c.hello()

	if err == nil && restore {
		err = c.restore()
		if err != nil {
			slog.Warnf("Failed to restore from cloud: %v", err)
		}
	}

	return err
}

// Establish and maintain a connection to cl.configd
func (c *rpcClient) connectLoop(wg *sync.WaitGroup, doneChan chan bool) {

	defer wg.Done()
	done := false

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	nextLog := time.Now()
	slog.Infof("connect loop starting")
	for !done {
		if !c.connected {
			if err := c.connect(); err == nil {
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
	defer ticker.Stop()
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
	defer ticker.Stop()

	ctx, cancel := context.WithCancel(c.ctx)
	doneFetch := make(chan bool)
	for !done {
		// If not connected, check periodically for
		// connection establishment or for the signal
		// to shutdown.
		if !c.connected {
			select {
			case <-ticker.C:
			case done = <-doneChan:
			}
			continue
		}

		// If connected, create the stream, and monitor
		// for errors, or for the signal to shutdown.
		go func() {
			err := c.fetchStream(ctx)
			if err != nil && !done {
				slog.Warnf("fetchStream failed: %v", err)
			}
			doneFetch <- true
		}()
		select {
		case <-doneFetch:
		case done = <-doneChan:
		}
	}
	cancel()
	slog.Infof("pull loop done")
}

func configLoop(ctx context.Context, client rpc.ConfigBackEndClient,
	wg *sync.WaitGroup, doneChan chan bool) {

	c := rpcClient{
		ctx:    ctx,
		client: client,
	}

	brokerd.Handle(base_def.TOPIC_CONFIG, configEvent)

	go c.pushLoop(wg, addDoneChan())
	go c.pullLoop(wg, addDoneChan())
	c.connectLoop(wg, doneChan)
}
