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

	"bg/cloud_rpc"

	"github.com/grpc-ecosystem/go-grpc-middleware/util/metautils"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"cloud.google.com/go/pubsub"
)

type eventServer struct {
	topicName    string
	eventTopic   *pubsub.Topic
	pubsubClient *pubsub.Client
}

func newEventServer(pubsubClient *pubsub.Client, topicName string) (*eventServer, error) {
	eventTopic := pubsubClient.Topic(topicName)

	return &eventServer{
		topicName,
		eventTopic,
		pubsubClient,
	}, nil
}

func (ts *eventServer) Put(ctx context.Context, req *cloud_rpc.PutEventRequest) (*cloud_rpc.PutEventResponse, error) {
	_, slog := endpointLogger(ctx)

	uuid := metautils.ExtractIncoming(ctx).Get("clouduuid")
	if uuid == "" {
		return nil, status.Errorf(codes.Internal, "missing clouduuid")
	}
	slog.Infow("incoming event", "SubTopic", req.SubTopic)

	m := &pubsub.Message{
		Attributes: map[string]string{
			"typeURL": req.Payload.TypeUrl,
			"uuid":    uuid,
		},
		Data: req.Payload.Value,
	}
	slog.Infow("outgoing pubsub", "message", m)
	pubsubResult := ts.eventTopic.Publish(ctx, m)
	_, err := pubsubResult.Get(ctx)
	if err != nil {
		slog.Warnw("Publish failed", "message", m, "error", err)
		return nil, status.Errorf(codes.Unavailable, "Publish failed")
	}

	// Formulate a response.
	return &cloud_rpc.PutEventResponse{}, nil
}
