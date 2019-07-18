/*
 * COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
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
	"time"

	"bg/cl_common/daemonutils"
	rpc "bg/cloud_rpc"
	"bg/common/cfgapi"
	"bg/common/cfgmsg"
	"bg/common/cfgtree"

	"github.com/golang/protobuf/ptypes"
)

type frontEndServer struct {
}

func (s *frontEndServer) Ping(ctx context.Context,
	ops *rpc.CfgFrontEndPing) (*rpc.CfgFrontEndPing, error) {

	rval := rpc.CfgFrontEndPing{
		Time: ptypes.TimestampNow(),
	}

	return &rval, nil
}

// Extract the property, value, and (optional) expiration parameters from the
// ConfigOp message.
func getParams(op *cfgmsg.ConfigOp) (string, string, *time.Time, error) {
	var expires *time.Time
	var err error

	prop := op.GetProperty()
	val := op.GetValue()
	if pexp := op.GetExpires(); pexp != nil {
		tmp, terr := ptypes.Timestamp(pexp)
		if terr == nil {
			expires = &tmp
		} else {
			err = fmt.Errorf("invalid expiration: %v", terr)
		}
	}

	return prop, val, expires, err
}

func makeFailedResponse(err error) *cfgmsg.ConfigResponse {
	return &cfgmsg.ConfigResponse{
		Timestamp: ptypes.TimestampNow(),
		Errmsg:    fmt.Sprintf("%v", err),
		Response:  cfgmsg.ConfigResponse_FAILED,
	}
}

// Accept a command from a front-end client and do some basic sanity checking.
// If we can handle the command from our in-core state, do so and return the
// result to the caller.  Otherwise, push the command onto the pending command
// queue for asynchronous execution.
func (s *frontEndServer) Submit(ctx context.Context,
	query *cfgmsg.ConfigQuery) (rval *cfgmsg.ConfigResponse, rerr error) {

	_, slog := daemonutils.EndpointLogger(ctx)
	slog.Debugw("Frontend submit", "query", query)
	rval = &cfgmsg.ConfigResponse{
		Timestamp: ptypes.TimestampNow(),
		Response:  cfgmsg.ConfigResponse_FAILED,
	}

	state, err := getSiteState(ctx, query.SiteUUID)
	if err == cfgapi.ErrNoConfig {
		rval.Errmsg = err.Error()
		rval.Response = cfgmsg.ConfigResponse_NOCONFIG
	} else if err != nil {
		rval.Errmsg = fmt.Sprintf("%v", err)
		return
	}
	if state == nil {
		rval.Errmsg = "Empty state with no error"
		return
	}
	if state.cachedTree == nil {
		rval.Errmsg = "missing tree"
		return
	}

	// Check for no-op
	if len(query.Ops) == 0 {
		rval.Response = cfgmsg.ConfigResponse_OK
		return
	}

	// Sanity check all operations
	getProp := ""
	for i, o := range query.Ops {
		errHead := fmt.Sprintf("op %d: ", i)
		prop := o.Property
		if prop == "" {
			rval.Errmsg = errHead + "missing property"
			return
		}

		switch o.Operation {
		case cfgmsg.ConfigOp_GET:
			getProp = prop
			if len(query.Ops) > 1 {
				rval.Errmsg = "compound GETs not supported"
				return
			}

		case cfgmsg.ConfigOp_DELETE:
			// nothing to do

		case cfgmsg.ConfigOp_TEST:
			// nothing to do

		case cfgmsg.ConfigOp_TESTEQ:
			if o.GetValue() == "" {
				rval.Errmsg = errHead + "missing value"
				return
			}

		case cfgmsg.ConfigOp_SET,
			cfgmsg.ConfigOp_CREATE:

			if o.GetValue() == "" {
				rval.Errmsg = errHead + "missing value"
				return
			}

		case cfgmsg.ConfigOp_REPLACE:
			if len(query.Ops) > 1 {
				rval.Errmsg = "compound REPLACEs not supported"
				return
			}

			if o.GetValue() == "" {
				rval.Errmsg = errHead + "missing value"
				return
			}

		default:
			rval.Errmsg = errHead + "illegal operation type"
			return
		}
	}

	// GET operations are satisfied from our cached copy of the config,
	// metrics, or device trees. Everything else needs to be queued for the
	// appliance to execute later.
	if getProp != "" {
		var err error
		var payload string

		if strings.HasPrefix(getProp, devicesPath) {
			payload, err = getDevice(getProp)

		} else if strings.HasPrefix(getProp+"/", metricsPath) {
			payload, err = state.metricsGet(getProp)

		} else {
			state.Lock()
			payload, err = state.cachedTree.Get(getProp)
			state.Unlock()
		}

		if err == nil {
			rval.Response = cfgmsg.ConfigResponse_OK
			rval.Value = payload
		} else if err == cfgtree.ErrNoProp {
			rval.Response = cfgmsg.ConfigResponse_NOPROP
		} else {
			rval.Errmsg = fmt.Sprintf("%v", err)
			rval.Response = cfgmsg.ConfigResponse_FAILED
		}

	} else {
		// strip out the cloud UUID before sending it to the client
		query.SiteUUID = ""
		rval.CmdID, err = state.cmdQueue.submit(ctx, state, query)
		if err != nil {
			rval = makeFailedResponse(err)
		} else {
			rval.Response = cfgmsg.ConfigResponse_QUEUED
		}
	}

	return
}

// Attempt to cancel a pending operation.
func (s *frontEndServer) Cancel(ctx context.Context,
	cmd *rpc.CfgCmdID) (*cfgmsg.ConfigResponse, error) {

	var rval *cfgmsg.ConfigResponse

	state, err := getSiteState(ctx, cmd.SiteUUID)
	if err != nil {
		rval = makeFailedResponse(err)
	} else {
		rval, err = state.cmdQueue.cancel(ctx, state, cmd.CmdID)
		if err != nil {
			rval = makeFailedResponse(err)
		}
	}

	return rval, nil
}

// Get the status of a submitted operation.
func (s *frontEndServer) Status(ctx context.Context,
	cmd *rpc.CfgCmdID) (*cfgmsg.ConfigResponse, error) {

	var rval *cfgmsg.ConfigResponse

	state, err := getSiteState(ctx, cmd.SiteUUID)
	if err != nil {
		rval = makeFailedResponse(err)
	} else {
		rval, err = state.cmdQueue.status(ctx, state, cmd.CmdID)
		if err != nil {
			rval = makeFailedResponse(err)
		}
	}

	return rval, nil
}

func (s *frontEndServer) Monitor(req *rpc.CfgFrontEndMonitor,
	stream rpc.ConfigFrontEnd_MonitorServer) error {

	var update rpc.CfgFrontEndUpdate

	ctx := stream.Context()
	site, err := getSiteState(ctx, req.SiteUUID)
	if err != nil {
		update.Response = rpc.CfgFrontEndUpdate_FAILED
		update.Errmsg = fmt.Sprintf("%v", err)
		stream.Send(&update)
		return nil
	}

	queue := site.newUpdateQueue()
	defer queue.finalize()

	_, slog := daemonutils.EndpointLogger(ctx)

	for {
		updates := queue.fetch()
		update.Response = rpc.CfgFrontEndUpdate_OK
		update.Updates = updates

		if err = stream.Send(&update); err != nil {
			if err == context.Canceled {
				slog.Infof("client %s disconnected",
					req.SiteUUID)
				err = nil
			} else {
				slog.Infof("stream.Send failed: %v", err)
			}
			break
		}
	}

	return err
}
