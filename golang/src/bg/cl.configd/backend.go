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

	rpc "bg/cloud_rpc"

	"github.com/golang/protobuf/ptypes"
)

type backEndServer struct {
}

func (s *backEndServer) Hello(ctx context.Context,
	req *rpc.CfgBackEndHello) (*rpc.CfgBackEndResponse, error) {

	payload := []byte("hello from cloud configd")
	return &rpc.CfgBackEndResponse{
		Time:     ptypes.TimestampNow(),
		Response: rpc.CfgBackEndResponse_OK,
		Payload:  payload,
	}, nil
}

func (s *backEndServer) Download(ctx context.Context,
	req *rpc.CfgBackEndDownload) (*rpc.CfgBackEndResponse, error) {

	return &rpc.CfgBackEndResponse{
		Time:     ptypes.TimestampNow(),
		Response: rpc.CfgBackEndResponse_OK,
	}, nil
}

func (s *backEndServer) Update(ctx context.Context,
	req *rpc.CfgBackEndUpdate) (*rpc.CfgBackEndResponse, error) {

	return &rpc.CfgBackEndResponse{
		Time:     ptypes.TimestampNow(),
		Response: rpc.CfgBackEndResponse_OK,
	}, nil
}

func (s *backEndServer) FetchCmds(ctx context.Context,
	req *rpc.CfgBackEndFetchCmds) (*rpc.CfgBackEndResponse, error) {

	return &rpc.CfgBackEndResponse{
		Time:     ptypes.TimestampNow(),
		Response: rpc.CfgBackEndResponse_OK,
	}, nil
}
