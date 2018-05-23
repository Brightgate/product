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
	"os"
	"testing"

	"bg/ap_common/aputil"
	"bg/ap_common/iotcore/mocks"
	"bg/base_msg"

	"github.com/eclipse/paho.mqtt.golang"
	"github.com/fgrosse/zaptest"
	"github.com/golang/protobuf/proto"
	"github.com/stretchr/testify/mock"
	"go.uber.org/zap"
	"golang.org/x/net/context"
)

func setupLogging(t *testing.T) (*zap.Logger, *zap.SugaredLogger) {
	// Assign globals
	logger = zaptest.Logger(t)
	slogger = logger.Sugar()
	return logger, slogger
}

func TestNetException(t *testing.T) {
	setupLogging(t)
	iotcMock := &mocks.IoTMQTTClient{}
	defer iotcMock.AssertExpectations(t)
	ctx := context.Background()

	iotcMock.On("PublishEvent",
		"exception",
		mock.AnythingOfType("string"),
	).Return(&mqtt.DummyToken{})

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
	err = handleNetException(ctx, iotcMock, data)
	if err != nil {
		t.Errorf("expected handleException to succeed: %s", err)
	}
}

func TestSendUpbeat(t *testing.T) {
	setupLogging(t)
	iotcMock := &mocks.IoTMQTTClient{}

	iotcMock.On("PublishEvent",
		"upbeat",
		mock.AnythingOfType("string"),
	).Return(&mqtt.DummyToken{})

	publishUpbeat(iotcMock)
	iotcMock.AssertExpectations(t)
}

func TestMain(m *testing.M) {
	prometheusInit()
	os.Exit(m.Run())
}
