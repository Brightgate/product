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
	"github.com/pkg/errors"
)

// MaxCmds represents the maximum number of commands to fetch at once
const MaxCmds = 64

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
	sync.Mutex
}

var queued = cloudQueue{
	updates:     make([]*rpc.CfgBackEndUpdate_CfgUpdate, 0),
	completions: make([]*cfgmsg.ConfigResponse, 0),
}

// utility function to attach a deadline to the current context
func setCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	ctx, err := applianceCred.MakeGRPCContext(ctx)
	if err != nil {
		slogger.Fatalf("Failed to make GRPC credential: %+v", err)
	}

	clientDeadline := time.Now().Add(*deadlineFlag)
	return context.WithDeadline(ctx, clientDeadline)
}

// execute a single Hello() gRPC.
func hello(ctx context.Context, cclient rpc.ConfigBackEndClient) error {
	ctx, ctxcancel := setCtx(ctx)
	defer ctxcancel()

	helloOp := &rpc.CfgBackEndHello{
		Time:    ptypes.TimestampNow(),
		Version: cfgapi.Version,
	}

	rval, err := cclient.Hello(ctx, helloOp)
	if err != nil {
		err = errors.Wrapf(err, "failed to send Hello() rpc")
	} else if rval.Response == rpc.CfgBackEndResponse_ERROR {
		err = fmt.Errorf("Hello() failed: %s", rval.Errmsg)
	}

	return err
}

// send any queued Completions to the cloud
func pushCompletions(ctx context.Context, cclient rpc.ConfigBackEndClient) error {
	if len(queued.completions) == 0 {
		return nil
	}

	queued.Lock()
	c := queued.completions
	queued.completions = make([]*cfgmsg.ConfigResponse, 0)
	queued.Unlock()

	completeOp := &rpc.CfgBackEndCompletions{
		Time:        ptypes.TimestampNow(),
		Completions: c,
	}

	ctx, ctxcancel := setCtx(ctx)
	defer ctxcancel()
	resp, err := cclient.CompleteCmds(ctx, completeOp)
	if err == nil && resp.Response == rpc.CfgBackEndResponse_ERROR {
		err = fmt.Errorf("CompleteCmds() failed: %s", resp.Errmsg)
	}

	// If the push fails re-queue the completions for a subsequent retry.
	if err != nil {
		queued.Lock()
		queued.completions = append(c, queued.completions...)
		queued.Unlock()
	}
	return err
}

// send all of the accumulated config tree updates to the cloud
func pushUpdates(ctx context.Context, cclient rpc.ConfigBackEndClient) error {
	if len(queued.updates) == 0 {
		return nil
	}

	queued.Lock()
	u := queued.updates
	queued.updates = make([]*rpc.CfgBackEndUpdate_CfgUpdate, 0)
	queued.Unlock()

	updateOp := &rpc.CfgBackEndUpdate{
		Time:    ptypes.TimestampNow(),
		Version: cfgapi.Version,
		Updates: u,
	}

	ctx, ctxcancel := setCtx(ctx)
	defer ctxcancel()
	resp, err := cclient.Update(ctx, updateOp)
	if err == nil && resp.Response == rpc.CfgBackEndResponse_ERROR {
		err = fmt.Errorf("Update() failed: %s", resp.Errmsg)
	}

	// If we failed to forward the updates to the cloud, requeue them to
	// try again later
	if err != nil {
		queued.Lock()
		queued.updates = append(u, queued.updates...)
		queued.Unlock()
	}

	return err
}

// connect to cl.rpcd to collect any pending update commands from the cloud
func fetch(ctx context.Context, cclient rpc.ConfigBackEndClient) ([]*cfgmsg.ConfigQuery, error) {
	ctx, ctxcancel := setCtx(ctx)
	defer ctxcancel()

	fetchOp := &rpc.CfgBackEndFetchCmds{
		Time:      ptypes.TimestampNow(),
		Version:   cfgapi.Version,
		LastCmdID: queued.lastOp,
		MaxCmds:   MaxCmds,
	}

	resp, err := cclient.FetchCmds(ctx, fetchOp)
	if err != nil {
		return nil, errors.Wrapf(err, "sending FetchCmds() rpc")
	}
	if resp.Response == rpc.CfgBackEndResponse_ERROR {
		return nil, fmt.Errorf("FetchCmds() failed: %s", resp.Errmsg)
	}

	if len(resp.Cmds) > 0 {
		slogger.Debugf("Got %d cmds, starting with %d", len(resp.Cmds),
			resp.Cmds[0].CmdID)
	}

	return resp.Cmds, nil
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
			slogger.Infof("Moving to new RPC server: " +
				*event.NewValue)
			go daemonStop()
			return
		}
		slogger.Debugf("updated %s - %x", *event.Property, hash)
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
		slogger.Debugf("deleted %s - %x", *event.Property, hash)
		update = &rpc.CfgBackEndUpdate_CfgUpdate{
			Type:     rpc.CfgBackEndUpdate_CfgUpdate_DELETE,
			Property: *event.Property,
			Hash:     hash,
		}
	}
	if update != nil {
		queued.Lock()
		queued.updates = append(queued.updates, update)
		queued.Unlock()
	}
}

func configLoop(ctx context.Context, client rpc.ConfigBackEndClient,
	wg *sync.WaitGroup, doneChan chan bool) {
	var live, done bool

	defer wg.Done()

	// XXX: can we have one go routine doing a long poll for fetching
	// commands, and another doing on-demand pushing of updates and
	// completions?

	ticker := time.NewTicker(time.Second)
	nextLog := time.Now()
	for !done {

		// Try to (re)establish a config gRPC connection to cl.rpcd
		if !live {
			if err := hello(ctx, client); err == nil {
				live = true
				nextLog = time.Now()
				slogger.Infof("cl.configd connection live")
			} else if nextLog.Before(time.Now()) {
				slogger.Errorf("Failed hello: %s", err)
				nextLog = time.Now().Add(10 * time.Minute)
			}
		}

		if live {
			// Push any queued updates or completions to the cloud
			if err := pushCompletions(ctx, client); err != nil {
				slogger.Error(err)
			}
			if err := pushUpdates(ctx, client); err != nil {
				slogger.Error(err)
			}

			// Check for any new commands pending in the cloud
			cmds, err := fetch(ctx, client)
			if err != nil {
				live = false
				slogger.Warnf("fetch failed: %v", err)
			} else if cmds != nil {
				for _, cmd := range cmds {
					exec(cmd)
				}
			}
		}

		if (len(queued.updates) + len(queued.completions)) == 0 {
			select {
			case done = <-doneChan:
			case <-ticker.C:
			}
		}
	}
	slogger.Infof("config loop exiting")
}
