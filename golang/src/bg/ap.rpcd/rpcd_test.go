/*
 * COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 *
 */

package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"testing"
	"time"

	"bg/ap_common/aptest"
	"bg/ap_common/aputil"
	"bg/ap_common/platform"
	"bg/base_msg"
	"bg/cloud_rpc"
	"bg/cloud_rpc/mocks"
	"bg/common/grpcutils"

	"github.com/golang/protobuf/proto"
	"github.com/stretchr/testify/mock"
	"go.uber.org/zap/zaptest"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

func setupLogging(t *testing.T) {
	// Assign globals
	slog = zaptest.NewLogger(t).Sugar()
}

func TestNetException(t *testing.T) {
	setupLogging(t)
	tMock := &mocks.EventClient{}
	defer tMock.AssertExpectations(t)
	ctx := context.Background()

	tMock.On("Put",
		mock.Anything,
		mock.AnythingOfType("*cloud_rpc.PutEventRequest"),
	).Return(&cloud_rpc.PutEventResponse{Result: 0}, nil)

	protocol := base_msg.Protocol_IP
	reason := base_msg.EventNetException_BLOCKED_IP

	entity := &base_msg.EventNetException{
		Timestamp:   aputil.NowToProtobuf(),
		Sender:      proto.String("testing123"),
		Debug:       proto.String("-"),
		Protocol:    &protocol,
		Reason:      &reason,
		MacAddress:  proto.Uint64(0xfeedfacedeadbeef),
		Ipv4Address: proto.Uint32(0xf00dd00d),
	}
	data, err := proto.Marshal(entity)
	if err != nil {
		panic(err)
	}
	err = handleNetException(ctx, tMock, data)
	if err != nil {
		t.Errorf("expected handleException to succeed: %s", err)
	}
}

func TestSendHeartbeat(t *testing.T) {
	setupLogging(t)
	ctx := context.Background()
	tMock := &mocks.EventClient{}
	defer tMock.AssertExpectations(t)

	tMock.On("Put",
		mock.Anything,
		mock.AnythingOfType("*cloud_rpc.PutEventRequest"),
	).Return(&cloud_rpc.PutEventResponse{Result: 0}, nil)

	err := publishHeartbeat(ctx, tMock)
	if err != nil {
		t.Errorf("expected publishHeartbeat to work")
	}
	tMock.AssertExpectations(t)
}

func TestSendHeartbeatFail(t *testing.T) {
	setupLogging(t)
	ctx := context.Background()
	tMock := &mocks.EventClient{}
	defer tMock.AssertExpectations(t)

	tMock.On("Put",
		mock.Anything,
		mock.AnythingOfType("*cloud_rpc.PutEventRequest"),
	).Return(nil, grpc.Errorf(codes.Unavailable, "failed"))

	err := publishHeartbeat(ctx, tMock)
	if err == nil {
		t.Errorf("expected publishHeartbeat to fail")
	}
	tMock.AssertExpectations(t)
}

func mkInventoryFile(tr *aptest.TestRoot) {
	inv := &base_msg.DeviceInventory{
		Timestamp: aputil.NowToProtobuf(),
	}
	inv.Devices = append(inv.Devices, &base_msg.DeviceInfo{
		Created:    aputil.NowToProtobuf(),
		Updated:    aputil.NowToProtobuf(),
		MacAddress: proto.Uint64(0xb10eb10eb10eb10e),
	})
	pbuf, err := proto.Marshal(inv)
	if err != nil {
		panic(err)
	}

	os.Setenv("APROOT", tr.Root)
	plat := platform.NewPlatform()

	fname := plat.ExpandDirPath("__APDATA__/identifierd", fmt.Sprintf("observations.pb.%d", time.Now().Unix()))

	err = os.MkdirAll(path.Dir(fname), 0755)
	if err != nil {
		panic(err)
	}

	err = ioutil.WriteFile(fname, pbuf, 0755)
	if err != nil {
		panic(err)
	}
}

func TestSendInventory(t *testing.T) {
	setupLogging(t)
	ctx := context.Background()

	tMock := &mocks.EventClient{}
	defer tMock.AssertExpectations(t)

	tMock.On("Put",
		mock.Anything,
		mock.AnythingOfType("*cloud_rpc.PutEventRequest"),
	).Return(&cloud_rpc.PutEventResponse{Result: 0}, nil)

	apr := aptest.NewTestRoot(t)
	apr.Clean()
	defer apr.Fini()

	mkInventoryFile(apr)

	err := sendInventory(ctx, tMock)
	if err != nil {
		t.Errorf("expected sendInventory to work: %s", err)
	}
	tMock.AssertExpectations(t)

	// Try again-- this time we expect nothing to be sent
	tMock = &mocks.EventClient{}
	defer tMock.AssertExpectations(t)

	err = sendInventory(ctx, tMock)
	if err != nil {
		t.Errorf("expected sendInventory to work")
	}
	tMock.AssertExpectations(t)
}

func TestMain(m *testing.M) {
	// Need to setup global
	applianceCred = grpcutils.NewTestCredential()
	prometheusInit()
	os.Exit(m.Run())
}
