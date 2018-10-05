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
	"context"
	"fmt"
	"time"

	rpc "bg/cloud_rpc"
	"bg/common/cfgmsg"

	"github.com/golang/protobuf/ptypes"
)

type frontEndServer struct {
}

func execGet(state *perAPState, prop string) *cfgmsg.ConfigResponse {
	slog.Infof("Getting %s", prop)
	val, err := state.cachedTree.Get(prop)

	rval := &cfgmsg.ConfigResponse{
		Timestamp: ptypes.TimestampNow(),
	}

	if err == nil {
		rval.Response = cfgmsg.ConfigResponse_OK
		rval.Value = val
	} else {
		rval.Errmsg = fmt.Sprintf("%v", err)
		rval.Response = cfgmsg.ConfigResponse_FAILED
	}

	return rval
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

// Accept a command from a front-end client and do some basic sanity checking.
// If we can handle the command from our in-core state, do so and return the
// result to the caller.  Otherwise, push the command onto the pending command
// queue for asynchronous execution.
func (s *frontEndServer) Submit(ctx context.Context,
	ops *cfgmsg.ConfigQuery) (rval *cfgmsg.ConfigResponse, rerr error) {

	rval = &cfgmsg.ConfigResponse{
		Timestamp: ptypes.TimestampNow(),
		Response:  cfgmsg.ConfigResponse_FAILED,
	}

	state, err := getAPState(ops.IdentityUuid)
	if err != nil {
		rval.Errmsg = fmt.Sprintf("%v", err)
		return
	}

	// Check for no-op
	if len(ops.Ops) == 0 {
		rval.Response = cfgmsg.ConfigResponse_OK
		return
	}

	// Sanity check all operations
	getProp := ""
	for i, o := range ops.Ops {
		errHead := fmt.Sprintf("op %d: ", i)
		prop := o.Property
		if prop == "" {
			rval.Errmsg = errHead + "missing property"
			return
		}

		switch o.Operation {
		case cfgmsg.ConfigOp_GET:
			getProp = prop
			if len(ops.Ops) > 1 {
				rval.Errmsg = "compound GETs not supported"
				return
			}
		case cfgmsg.ConfigOp_DELETE:
			// nothing to do

		case cfgmsg.ConfigOp_SET,
			cfgmsg.ConfigOp_CREATE:

			if o.GetValue() == "" {
				rval.Errmsg = errHead + "missing value"
				return
			}
		default:
			rval.Errmsg = errHead + "illegal operation type"
			return
		}
	}

	// GET operations can be satisfied from our cached copy of the config
	// tree.  Everything else needs to be queued for the appliance to
	// execute later.
	if getProp != "" {
		state.Lock()
		payload, err := state.cachedTree.Get(getProp)
		state.Unlock()
		if err == nil {
			rval.Response = cfgmsg.ConfigResponse_OK
			rval.Value = payload
		} else {
			rval.Errmsg = fmt.Sprintf("%v", err)
		}
	} else {
		rval.CmdID = cmdSubmit(state, ops)
		rval.Response = cfgmsg.ConfigResponse_QUEUED
	}

	return
}

// Attempt to cancel a pending operation.
func (s *frontEndServer) Cancel(ctx context.Context,
	cmd *rpc.CfgCmdID) (*cfgmsg.ConfigResponse, error) {

	var rval *cfgmsg.ConfigResponse

	state, err := getAPState(cmd.CloudUuid)
	if err != nil {
		rval = &cfgmsg.ConfigResponse{
			Timestamp: ptypes.TimestampNow(),
			Errmsg:    fmt.Sprintf("%v", err),
			Response:  cfgmsg.ConfigResponse_FAILED,
		}
	} else {
		rval = cmdCancel(state, cmd.CmdID)
	}

	return rval, nil
}

// Get the status of a submitted operation.
func (s *frontEndServer) Status(ctx context.Context,
	cmd *rpc.CfgCmdID) (*cfgmsg.ConfigResponse, error) {

	var rval *cfgmsg.ConfigResponse

	state, err := getAPState(cmd.CloudUuid)
	if err != nil {
		rval = &cfgmsg.ConfigResponse{
			Timestamp: ptypes.TimestampNow(),
			Errmsg:    fmt.Sprintf("%v", err),
			Response:  cfgmsg.ConfigResponse_FAILED,
		}
	} else {
		rval = cmdStatus(state, cmd.CmdID)
	}

	return rval, nil
}
