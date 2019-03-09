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

	"bg/cl_common/daemonutils"
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
	_, slog := daemonutils.EndpointLogger(ctx)
	slog.Infow("incoming event", "SubTopic", req.SubTopic)

	siteUUID, err := getSiteUUID(ctx, false)
	if err != nil {
		return nil, err
	}
	applianceUUID := metautils.ExtractIncoming(ctx).Get("appliance_uuid")
	if applianceUUID == "" {
		return nil, status.Errorf(codes.Internal, "missing appliance_uuid")
	}

	m := &pubsub.Message{
		Attributes: map[string]string{
			"typeURL": req.Payload.TypeUrl,
			"uuid":    applianceUUID,
			"site":    siteUUID.String(),
		},
		Data: req.Payload.Value,
	}
	slog.Infow("outgoing pubsub", "datalen", len(m.Data), "attributes", m.Attributes)
	pubsubResult := ts.eventTopic.Publish(ctx, m)
	_, err = pubsubResult.Get(ctx)
	if err != nil {
		slog.Warnw("Publish failed", "message", m, "error", err)
		return nil, status.Errorf(codes.Unavailable, "Publish failed")
	}

	// Formulate a response.
	return &cloud_rpc.PutEventResponse{}, nil
}
