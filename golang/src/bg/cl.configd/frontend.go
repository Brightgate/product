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

	"github.com/golang/protobuf/ptypes"
)

type frontEndServer struct {
}

func execGet(state *perAPState, prop string) *rpc.CfgPropResponse {
	slog.Infof("Getting %s", prop)
	val, err := state.cachedTree.Get(prop)

	rval := &rpc.CfgPropResponse{
		Time: ptypes.TimestampNow(),
	}

	if err == nil {
		rval.Response = rpc.CfgPropResponse_OK
		rval.Payload = val
	} else {
		rval.Errmsg = fmt.Sprintf("%v", err)
		rval.Response = rpc.CfgPropResponse_FAILED
	}

	return rval
}

func (s *frontEndServer) Ping(ctx context.Context,
	ops *rpc.CfgFrontEndPing) (*rpc.CfgPropResponse, error) {

	return &rpc.CfgPropResponse{
		Time:     ptypes.TimestampNow(),
		Response: rpc.CfgPropResponse_OK,
		Payload:  "pong",
	}, nil
}

// Extract the property, value, and (optional) expiration parameters from the
// CfgPropOp message.
func getParams(op *rpc.CfgPropOps_CfgPropOp) (string, string, *time.Time, error) {
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
	ops *rpc.CfgPropOps) (rval *rpc.CfgPropResponse, rerr error) {

	rval = &rpc.CfgPropResponse{
		Time:     ptypes.TimestampNow(),
		Response: rpc.CfgPropResponse_FAILED,
	}

	state, err := getAPState(ops.CloudUuid)
	if err != nil {
		rval.Errmsg = fmt.Sprintf("%v", err)
		return
	}

	// Check for no-op
	if len(ops.Ops) == 0 {
		rval.Response = rpc.CfgPropResponse_OK
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
		case rpc.CfgPropOps_CfgPropOp_GET:
			getProp = prop
			if len(ops.Ops) > 1 {
				rval.Errmsg = "compound GETs not supported"
				return
			}
		case rpc.CfgPropOps_CfgPropOp_DELETE:
			// nothing to do

		case rpc.CfgPropOps_CfgPropOp_SET,
			rpc.CfgPropOps_CfgPropOp_CREATE:

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
			rval.Response = rpc.CfgPropResponse_OK
			rval.Payload = payload
		} else {
			rval.Errmsg = fmt.Sprintf("%v", err)
		}
	} else {
		rval.CmdID = cmdSubmit(state, ops)
		rval.Response = rpc.CfgPropResponse_QUEUED
	}

	return
}

// Attempt to cancel a pending operation.
func (s *frontEndServer) Cancel(ctx context.Context,
	cmd *rpc.CfgCmdID) (*rpc.CfgPropResponse, error) {

	var rval *rpc.CfgPropResponse

	state, err := getAPState(cmd.CloudUuid)
	if err != nil {
		rval = &rpc.CfgPropResponse{
			Time:     ptypes.TimestampNow(),
			Errmsg:   fmt.Sprintf("%v", err),
			Response: rpc.CfgPropResponse_FAILED,
		}
	} else {
		rval = cmdCancel(state, cmd.CmdID)
	}

	return rval, nil
}

// Get the status of a submitted operation.
func (s *frontEndServer) Status(ctx context.Context,
	cmd *rpc.CfgCmdID) (*rpc.CfgPropResponse, error) {

	var rval *rpc.CfgPropResponse

	state, err := getAPState(cmd.CloudUuid)
	if err != nil {
		rval = &rpc.CfgPropResponse{
			Time:     ptypes.TimestampNow(),
			Errmsg:   fmt.Sprintf("%v", err),
			Response: rpc.CfgPropResponse_FAILED,
		}
	} else {
		rval = cmdStatus(state, cmd.CmdID)
	}

	return rval, nil
}
