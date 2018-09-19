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

func (s *frontEndServer) Submit(ctx context.Context,
	ops *rpc.CfgPropOps) (*rpc.CfgPropResponse, error) {

	rval := &rpc.CfgPropResponse{
		Time:     ptypes.TimestampNow(),
		Response: rpc.CfgPropResponse_FAILED,
	}

	state, err := getAPState(ops.CloudUuid)
	if err != nil {
		rval.Errmsg = fmt.Sprintf("no such appliance: %s",
			ops.CloudUuid)
	} else if len(ops.Ops) == 0 {
		// no-op
		rval.Response = rpc.CfgPropResponse_OK
	} else if len(ops.Ops) > 1 {
		rval.Response = rpc.CfgPropResponse_UNSUPPORTED
		rval.Errmsg = fmt.Sprintf("compound ops not supported")
	} else if ops.Ops[0].Operation != rpc.CfgPropOps_CfgPropOp_GET {
		rval.Response = rpc.CfgPropResponse_UNSUPPORTED
		rval.Errmsg = fmt.Sprintf("only GETs supported")
	} else {
		rval = execGet(state, ops.Ops[0].Property)
	}

	return rval, nil
}

func (s *frontEndServer) Cancel(ctx context.Context,
	ops *rpc.CfgCmdID) (*rpc.CfgPropResponse, error) {

	return &rpc.CfgPropResponse{
		Response: rpc.CfgPropResponse_OK,
	}, nil
}

func (s *frontEndServer) Status(ctx context.Context,
	ops *rpc.CfgCmdID) (*rpc.CfgPropResponse, error) {

	return &rpc.CfgPropResponse{
		Response: rpc.CfgPropResponse_OK,
	}, nil
}
