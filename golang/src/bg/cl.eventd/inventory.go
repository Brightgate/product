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
	"time"

	"bg/cl_common/deviceinfo"
	"bg/cloud_rpc"

	"cloud.google.com/go/pubsub"

	"github.com/satori/uuid"

	"github.com/golang/protobuf/proto"
	_ "google.golang.org/grpc/encoding/gzip"
)

func (i *inventoryWriter) inventoryMessage(ctx context.Context, siteUUID uuid.UUID, m *pubsub.Message) {
	var err error

	// For now we have nothing we can really do with malformed messages
	defer m.Ack()

	slog := slog.With("appliance_uuid", m.Attributes["appliance_uuid"],
		"site_uuid", m.Attributes["site_uuid"])

	inventory := &cloud_rpc.InventoryReport{}
	err = proto.Unmarshal(m.Data, inventory)
	if err != nil {
		slog.Errorw("failed to decode message", "error", err, "data", string(m.Data))
		return
	}

	now := time.Now()
	for _, devInfo := range inventory.Inventory.Devices {
		for _, store := range i.stores {
			path, err := store.Write(ctx, siteUUID, devInfo, now)
			if err != nil {
				slog.Errorf("failed to write DeviceInfo to %s: %v", store.Name(), err)
			} else {
				slog.Infof("wrote DeviceInfo to %s %s", store.Name(), path)
			}
		}
	}
}

type inventoryWriter struct {
	stores []deviceinfo.Store
}

