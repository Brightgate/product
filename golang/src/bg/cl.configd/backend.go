/*
 * Copyright 2019 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package main

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"bg/cl_common/daemonutils"
	rpc "bg/cloud_rpc"
	"bg/common/cfgapi"
	"bg/common/cfgmsg"

	"github.com/golang/protobuf/ptypes"
)

type backEndServer struct {
}

// Issue a "GET" to the appliance, which we will use to refresh our cached
// copy of a tree
func refreshConfig(ctx context.Context, site *siteState, uuid, root string) {
	_, slog := daemonutils.EndpointLogger(ctx)

	switch root {
	case rootPath:
		slog.Infof("requesting a fresh tree from %s", uuid)
	case metricsPath:
		slog.Debugf("refreshing %s from %s", root, uuid)
	default:
		slog.Warnf("requesting refresh of unsupported tree: %s", root)
		return
	}

	getOp := []cfgapi.PropertyOp{
		{Op: cfgapi.PropGet, Name: root},
	}

	q, err := cfgapi.PropOpsToQuery(getOp)
	if err != nil {
		slog.Warnf("failed to generate GET(%s) for %s: %v",
			root, uuid, err)

	} else if _, err = site.cmdQueue.submit(ctx, site, q); err != nil {
		slog.Warnf("failed to submit GET(%s) for %s: %v",
			root, uuid, err)
	}
}

// Respond to a Hello() from an appliance.
func (s *backEndServer) Hello(ctx context.Context,
	req *rpc.CfgBackEndHello) (*rpc.CfgBackEndResponse, error) {

	rval := &rpc.CfgBackEndResponse{
		Time:     ptypes.TimestampNow(),
		Response: rpc.CfgBackEndResponse_OK,
	}

	_, slog := daemonutils.EndpointLogger(ctx)

	uuid := req.GetSiteUUID()
	if uuid == "" {
		rval.Response = rpc.CfgBackEndResponse_ERROR
		rval.Errmsg = "No UUID provided"
	} else {
		slog.Infof("Hello() from %s", uuid)
		// XXX: as a side effect of the Hello(), we should compare
		// cloud and appliance hash values.
		siteState, err := getSiteState(ctx, uuid)
		if err != nil {
			// XXX: Currently we respond to an unknown appliance
			// appearing by asking it to upload its state to the
			// cloud.  Eventually this should be an error that
			// results in an event being sent.
			siteState = initSiteState(uuid)
			refreshConfig(ctx, siteState, uuid, rootPath)
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

	rval := &rpc.CfgBackEndResponse{
		Time:     ptypes.TimestampNow(),
		Response: rpc.CfgBackEndResponse_OK,
	}

	site, err := getSiteState(ctx, req.GetSiteUUID())
	if err != nil {
		rval.Response = rpc.CfgBackEndResponse_ERROR
		rval.Errmsg = fmt.Sprintf("%v", err)

	} else if site.cachedTree == nil {
		rval.Response = rpc.CfgBackEndResponse_NOCONFIG
		rval.Errmsg = "no config cached in cloud"

	} else {
		rval.Value = site.cachedTree.Export(false)
	}

	return rval, nil
}

// Attempt to apply a single appliance-generated update to our cached copy of
// its config tree.
func update(ctx context.Context, site *siteState, update *rpc.CfgUpdate) error {
	var err error

	_, slog := daemonutils.EndpointLogger(ctx)

	prop := update.GetProperty()

	t := site.cachedTree
	if t == nil {
		// We got our first tree update before getting the tree from the
		// site.  This should go away when we start loading the
		// initial config from the database.
		return nil
	}
	t.ChangesetInit()

	switch update.Type {
	case rpc.CfgUpdate_UPDATE:
		var expires *time.Time

		val := update.GetValue()
		if pexpires := update.GetExpires(); pexpires != nil {
			ts, _ := ptypes.Timestamp(pexpires)
			expires = &ts
			slog.Debugf("Updating %s to %q (expires %s)", prop, val, ts)
		} else {
			slog.Debugf("Updating %s to %q", prop, val)
		}
		err = t.Add(prop, val, expires)

	case rpc.CfgUpdate_DELETE:
		slog.Debugf("Deleting %s", update.GetProperty())
		_, err = t.Delete(prop)
	}
	if err == nil {
		th := site.cachedTree.Root().Hash()
		t.ChangesetCommit()
		if !bytes.Equal(th, update.GetHash()) {
			slog.Warnf("hash mismatch.  Got %x  expected %x",
				th, update.GetHash())
			err = fmt.Errorf("hash mismatch")
		}
		site.postUpdate(update)
	} else {
		t.ChangesetRevert()
		slog.Warnf("update to %s failed: %v", prop, err)
	}
	return err
}

// The site has sent a batch of config updates.  Iterate over them,
// applying each to our cached copy of the tree.
func (s *backEndServer) Update(ctx context.Context,
	req *rpc.CfgBackEndUpdate) (*rpc.CfgBackEndResponse, error) {
	_, slog := daemonutils.EndpointLogger(ctx)
	rval := &rpc.CfgBackEndResponse{}

	site, err := getSiteState(ctx, req.GetSiteUUID())
	if err != nil {
		rval.Response = rpc.CfgBackEndResponse_ERROR
		rval.Errmsg = fmt.Sprintf("%v", err)
	} else {
		rval.Response = rpc.CfgBackEndResponse_OK
		for _, u := range req.Updates {
			// If an update fails, it means our cached copy of the
			// tree is out of sync with the appliance.  Ignore all
			// of the remaining updates (since they'll fail as
			// well), and ask for a fresh copy of the full tree.
			if err = update(ctx, site, u); err != nil {
				refreshConfig(ctx, site, req.GetSiteUUID(),
					rootPath)
				break
			}
		}
		if err == nil {
			// Persist the changes we just applied
			if err = site.store(ctx); err != nil {
				slog.Errorf("Failed to store updated config: %v", err)
			}
		}
	}

	rval.Time = ptypes.TimestampNow()

	return rval, nil
}

// The appliance is asking for any pending commands.  Pull them from our pending
// command queue, and return them.
func (s *backEndServer) FetchCmds(ctx context.Context,
	req *rpc.CfgBackEndFetchCmds) (*rpc.CfgBackEndResponse, error) {

	_, slog := daemonutils.EndpointLogger(ctx)

	rval := &rpc.CfgBackEndResponse{}

	site, err := getSiteState(ctx, req.GetSiteUUID())
	if err != nil {
		rval.Response = rpc.CfgBackEndResponse_ERROR
		rval.Errmsg = fmt.Sprintf("%v", err)
	} else {
		maxCmds := req.MaxCmds
		// Default unset Maxcmds
		if maxCmds == 0 {
			maxCmds = 1
		}

		cmds, err := site.cmdQueue.fetch(ctx, site, int64(req.LastCmdID),
			maxCmds, false)

		// XXX We return OK even with an error as long as we have some
		// commands to fetch, in order to allow the client to make some
		// forward progress.  For it to succeed after that, we will need
		// to return the erroring command, and possibly use a new
		// response code.
		if err != nil && len(cmds) == 0 {
			rval.Response = rpc.CfgBackEndResponse_ERROR
		} else {
			if len(cmds) > 0 {
				slog.Debugf("%s fetches %d commands starting at %d",
					req.GetSiteUUID(), len(cmds), cmds[0].CmdID)
			} else {
				slog.Debugf("%s fetches 0 commands", req.GetSiteUUID())
			}
			rval.Response = rpc.CfgBackEndResponse_OK
		}
		rval.Cmds = cmds
	}

	rval.Time = ptypes.TimestampNow()
	return rval, nil
}

// As new commands are pushed into our submission queue, forward them to the
// appliance using a gRPC stream
func (s *backEndServer) FetchStream(req *rpc.CfgBackEndFetchCmds,
	stream rpc.ConfigBackEnd_FetchStreamServer) error {

	rval := &rpc.CfgBackEndResponse{}

	ctx := stream.Context()
	uuid := req.GetSiteUUID()

	_, slog := daemonutils.EndpointLogger(ctx)

	site, err := getSiteState(ctx, uuid)
	if err != nil {
		rval.Response = rpc.CfgBackEndResponse_ERROR
		rval.Errmsg = fmt.Sprintf("%v", err)
		stream.Send(rval)
		return nil
	}

	cmdID := int64(req.LastCmdID)
	max := uint32(req.MaxCmds)
	for err == nil {
		var cmds []*cfgmsg.ConfigQuery

		cmds, err = site.cmdQueue.fetch(ctx, site, cmdID, max, true)
		if err == nil && len(cmds) == 0 {
			// shouldn't happen
			slog.Warnf("fetch() returned no commands or errors")
			time.Sleep(time.Second)
		} else if err == context.Canceled {
			slog.Infof("client %s disconnected", uuid)
			err = nil
			break
		} else {
			if last := len(cmds) - 1; last >= 0 {
				cmdID = cmds[last].CmdID
			}
			if err == nil {
				rval.Response = rpc.CfgBackEndResponse_OK
				rval.Cmds = cmds
			} else {
				rval.Response = rpc.CfgBackEndResponse_ERROR
				rval.Errmsg = fmt.Sprintf("%v", err)
			}
			if rerr := stream.Send(rval); rerr != nil {
				slog.Infof("stream.Send failed: %v", rerr)
				err = rerr
			}
		}
	}

	return err
}

// The appliance has sent one or more command completions.  Iterate over the
// array, matching each completion with the command that it completes.
func (s *backEndServer) CompleteCmds(ctx context.Context,
	req *rpc.CfgBackEndCompletions) (*rpc.CfgBackEndResponse, error) {

	rval := &rpc.CfgBackEndResponse{
		Time:     ptypes.TimestampNow(),
		Response: rpc.CfgBackEndResponse_OK,
	}

	site, err := getSiteState(ctx, req.GetSiteUUID())
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
			err = site.cmdQueue.complete(ctx, site, comp)
			if err != nil && rval.Errmsg == "" {
				rval.Response = rpc.CfgBackEndResponse_OK
				rval.Errmsg = err.Error()
			}
		}
	}
	return rval, nil
}

