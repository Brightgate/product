/*
 * COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package main

import (
	"bytes"
	"context"
	"fmt"
	"time"

	rpc "bg/cloud_rpc"
	"bg/common/cfgapi"
	"bg/common/cfgmsg"

	"github.com/golang/protobuf/ptypes"
)

type backEndServer struct {
}

// Issue a "GET @/" to the appliance, which we will use to completely refresh
// our cached copy of the tree
func refreshConfig(ctx context.Context, ap *perAPState, uuid string) {
	slog.Infof("requesting a fresh tree from %s", uuid)

	getOp := []cfgapi.PropertyOp{
		{Op: cfgapi.PropGet, Name: "@/"},
	}

	q, err := cfgmsg.NewPropQuery(getOp)
	if err != nil {
		slog.Warnf("failed to generate GET(@/) for ", uuid, ": ", err)
		return
	}

	_, err = ap.cmdQueue.submit(ctx, ap, q)
	slog.Warnf("failed to submit GET(@/) for %s: %v", uuid, err)
}

// Respond to a Hello() from an appliance.
func (s *backEndServer) Hello(ctx context.Context,
	req *rpc.CfgBackEndHello) (*rpc.CfgBackEndResponse, error) {

	rval := &rpc.CfgBackEndResponse{
		Time:     ptypes.TimestampNow(),
		Response: rpc.CfgBackEndResponse_OK,
	}

	uuid := req.GetCloudUuid()
	if uuid == "" {
		rval.Response = rpc.CfgBackEndResponse_ERROR
		rval.Errmsg = "No UUID provided"
	} else {
		slog.Infof("Hello() from %s", uuid)
		// XXX: as a side effect of the Hello(), we should compare
		// cloud and appliance hash values.
		ap, err := getAPState(ctx, uuid)
		if err != nil {
			// XXX: Currently we respond to an unknown appliance
			// appearing by uploading its state to the cloud.
			// Eventually this should be an error that results in
			// an event being sent.
			ap, err = initAPState(uuid)
			refreshConfig(ctx, ap, uuid)
		}
		if err != nil {
			rval.Response = rpc.CfgBackEndResponse_ERROR
			rval.Errmsg = "unable to support " + uuid
		}
	}

	return rval, nil
}

func (s *backEndServer) Download(ctx context.Context,
	req *rpc.CfgBackEndDownload) (*rpc.CfgBackEndResponse, error) {

	return &rpc.CfgBackEndResponse{
		Time:     ptypes.TimestampNow(),
		Response: rpc.CfgBackEndResponse_OK,
	}, nil
}

// Attempt to apply a single appliance-generated update to our cached copy of
// its config tree.
func update(ap *perAPState, update *rpc.CfgBackEndUpdate_CfgUpdate) error {
	var err error

	prop := update.GetProperty()

	t := ap.cachedTree
	if t == nil {
		// We got our first tree update before getting the tree from the
		// appliance.  This should go away when we start loading the
		// initial config from the database.
		return nil
	}
	t.ChangesetInit()

	switch update.Type {
	case rpc.CfgBackEndUpdate_CfgUpdate_UPDATE:
		var expires *time.Time

		val := update.GetValue()
		if pexpires := update.GetExpires(); pexpires != nil {
			t, _ := ptypes.Timestamp(pexpires)
			expires = &t
		}
		slog.Debugf("Updating %s to %s", prop, update.GetValue())
		err = ap.cachedTree.Add(prop, val, expires)

	case rpc.CfgBackEndUpdate_CfgUpdate_DELETE:
		slog.Debugf("Deleting %s", update.GetProperty())
		_, err = ap.cachedTree.Delete(prop)
	}
	if err == nil {
		th := ap.cachedTree.Root().Hash()
		t.ChangesetCommit()
		if !bytes.Equal(th, update.GetHash()) {
			slog.Warnf("hash mismatch.  Got %x  expected %x",
				th, update.GetHash())
			err = fmt.Errorf("hash mismatch")
		}
	} else {
		t.ChangesetRevert()
		slog.Warnf("update to %s failed: %v", prop, err)
	}
	return err
}

// The appliance has sent a batch of config updates.  Iterate over them,
// applying each to our cached copy of the tree.
func (s *backEndServer) Update(ctx context.Context,
	req *rpc.CfgBackEndUpdate) (*rpc.CfgBackEndResponse, error) {
	rval := &rpc.CfgBackEndResponse{}

	ap, err := getAPState(ctx, req.GetCloudUuid())
	if err != nil {
		rval.Response = rpc.CfgBackEndResponse_ERROR
		rval.Errmsg = fmt.Sprintf("%v", err)
	} else {
		rval.Response = rpc.CfgBackEndResponse_OK
		for _, u := range req.Updates {
			if err = update(ap, u); err != nil {
				refreshConfig(ctx, ap, req.GetCloudUuid())
				break
			}
		}
		if err == nil {
			// XXX: persist the changes we just applied
		}
	}

	rval.Time = ptypes.TimestampNow()

	return rval, nil
}

// The appliance is asking for any pending commands.  Pull them from our pending
// command queue, and return them.
func (s *backEndServer) FetchCmds(ctx context.Context,
	req *rpc.CfgBackEndFetchCmds) (*rpc.CfgBackEndResponse, error) {

	rval := &rpc.CfgBackEndResponse{}

	ap, err := getAPState(ctx, req.GetCloudUuid())
	if err != nil {
		rval.Response = rpc.CfgBackEndResponse_ERROR
		rval.Errmsg = fmt.Sprintf("%v", err)
	} else {
		cmds, err := ap.cmdQueue.fetch(ctx, ap, int64(req.LastCmdID), int64(req.MaxCmds))
		// XXX We return OK even with an error as long as we have some
		// commands to fetch, in order to allow the client to make some
		// forward progress.  For it to succeed after that, we will need
		// to return the erroring command, and possibly use a new
		// response code.
		if err != nil && len(cmds) == 0 {
			rval.Response = rpc.CfgBackEndResponse_ERROR
		} else {
			rval.Response = rpc.CfgBackEndResponse_OK
		}
		rval.Cmds = cmds
	}

	rval.Time = ptypes.TimestampNow()
	return rval, nil
}

// The appliance has sent one or more command completions.  Iterate over the
// array, matching each completion with the command that it completes.
func (s *backEndServer) CompleteCmds(ctx context.Context,
	req *rpc.CfgBackEndCompletions) (*rpc.CfgBackEndResponse, error) {

	rval := &rpc.CfgBackEndResponse{
		Time:     ptypes.TimestampNow(),
		Response: rpc.CfgBackEndResponse_OK,
	}

	ap, err := getAPState(ctx, req.GetCloudUuid())
	if err != nil {
		rval.Response = rpc.CfgBackEndResponse_ERROR
		rval.Errmsg = fmt.Sprintf("%v", err)
	} else {
		for _, comp := range req.Completions {
			// Complete all the commands we can, but return as soon
			// as we get an error.  XXX Ideally we would return a
			// list of commands that successfully completed as well
			// as a list of commands that didn't along with the
			// errors.
			err = ap.cmdQueue.complete(ctx, ap, comp)
			if err != nil && rval.Errmsg == "" {
				rval.Response = rpc.CfgBackEndResponse_OK
				rval.Errmsg = err.Error()
			}
		}
	}
	return rval, nil
}
