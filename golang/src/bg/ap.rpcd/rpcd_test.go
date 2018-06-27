/*
 * COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
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
	"os"
	"testing"

	"bg/ap_common/aputil"
	"bg/base_msg"
	"bg/cloud_rpc"
	"bg/cloud_rpc/mocks"

	"github.com/golang/protobuf/proto"
	"github.com/stretchr/testify/mock"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

func setupLogging(t *testing.T) (*zap.Logger, *zap.SugaredLogger) {
	// Assign globals
	logger = zaptest.NewLogger(t)
	slogger = logger.Sugar()
	return logger, slogger
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

	publishHeartbeat(ctx, tMock)
	tMock.AssertExpectations(t)
}

func TestMain(m *testing.M) {
	prometheusInit()
	os.Exit(m.Run())
}
