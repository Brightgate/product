/*
 * Copyright 2020 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package main

import (
	"context"
	"fmt"

	"bg/cl_common/daemonutils"
	"bg/cl_common/vaulttokensource"
	"bg/cloud_rpc"

	"github.com/pkg/errors"
	"golang.org/x/oauth2"
	"google.golang.org/api/option"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"cloud.google.com/go/pubsub"
)

type eventServer struct {
	topicName    string
	eventTopic   *pubsub.Topic
	pubsubClient *pubsub.Client
	tokenSource  *vaulttokensource.VaultTokenSource
}

func newEventServer(vts *vaulttokensource.VaultTokenSource, topicName string) (*eventServer, error) {
	var opts []option.ClientOption
	if vts != nil {
		opts = append(opts, option.WithTokenSource(oauth2.ReuseTokenSource(nil, vts)))
	}
	pubsubClient, err := pubsub.NewClient(context.Background(), environ.PubsubProject, opts...)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to make pubsub client")
	}

	eventTopic := pubsubClient.Topic(topicName)

	return &eventServer{
		topicName,
		eventTopic,
		pubsubClient,
		vts,
	}, nil
}

func (ts *eventServer) Put(ctx context.Context, req *cloud_rpc.PutEventRequest) (*cloud_rpc.PutEventResponse, error) {
	_, slog := daemonutils.EndpointLogger(ctx)
	slog.Infow("incoming event", "SubTopic", req.SubTopic)

	siteUUID, err := getSiteUUID(ctx, false)
	if err != nil {
		return nil, err
	}
	applianceUUID, err := getApplianceUUID(ctx, false)
	if err != nil {
		return nil, err
	}

	m := &pubsub.Message{
		Attributes: map[string]string{
			"typeURL":        req.Payload.TypeUrl,
			"appliance_uuid": applianceUUID.String(),
			"site_uuid":      siteUUID.String(),
		},
		Data: req.Payload.Value,
	}
	slog.Infow("outgoing pubsub", "datalen", len(m.Data), "attributes", m.Attributes)
	op := func() (err error) {
		pubsubResult := ts.eventTopic.Publish(ctx, m)
		_, err = pubsubResult.Get(ctx)
		return
	}
	err = vaulttokensource.Retry(op, ts.tokenSource.UpdateMetadata,
		func(msg string, e error) {
			slog.Warnw(fmt.Sprintf("Put: %s", msg), "error", e)
		})
	if err != nil {
		slog.Warnw("Publish failed", "message", m, "error", err)
		return nil, status.Errorf(codes.Unavailable, "Publish failed")
	}

	// Formulate a response.
	return &cloud_rpc.PutEventResponse{}, nil
}

