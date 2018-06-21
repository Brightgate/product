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
	"fmt"
	"os"
	"path/filepath"
	"time"

	"bg/ap_common/network"
	"bg/base_msg"
	"bg/cloud_models/appliancedb"
	"bg/cloud_rpc"
	"cloud.google.com/go/pubsub"

	"github.com/satori/uuid"

	"github.com/golang/protobuf/proto"
	"google.golang.org/grpc/codes"
	_ "google.golang.org/grpc/encoding/gzip"
	"google.golang.org/grpc/status"
)

func writeInventoryInfo(devInfo *base_msg.DeviceInfo, uuid uuid.UUID) (string, error) {
	hwaddr := network.Uint64ToHWAddr(devInfo.GetMacAddress())
	// We receive only what has recently changed
	hwaddrPath := filepath.Join(inventoryBasePath, uuid.String(), hwaddr.String())
	if err := os.MkdirAll(hwaddrPath, 0755); err != nil {
		return "", status.Errorf(codes.FailedPrecondition, "mkdir failed")
	}

	filename := fmt.Sprintf("device_info.%d.pb", int(time.Now().Unix()))
	tmpFilename := "tmp." + filename
	path := filepath.Join(hwaddrPath, filename)
	tmpPath := filepath.Join(hwaddrPath, tmpFilename)
	f, err := os.OpenFile(
		tmpPath,
		os.O_WRONLY|os.O_CREATE|os.O_TRUNC,
		0644)
	if err != nil {
		return "", status.Errorf(codes.FailedPrecondition, "open failed")
	}
	defer f.Close()

	out, err := proto.Marshal(devInfo)
	if err != nil {
		os.Remove(tmpPath)
		return "", status.Errorf(codes.FailedPrecondition, "marshal failed")
	}

	if _, err := f.Write(out); err != nil {
		os.Remove(tmpPath)
		return "", status.Errorf(codes.FailedPrecondition, "write failed")
	}
	f.Sync()

	// Creating a tmp file and renaming it guarantees that we'll never have
	// a partial record in the file.
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return "", status.Errorf(codes.FailedPrecondition, "rename failed")
	}
	return path, nil
}

func inventoryMessage(ctx context.Context, applianceDB appliancedb.DataStore,
	idmap *appliancedb.ApplianceID, m *pubsub.Message) {
	var err error

	// For now we have nothing we can really do with malformed messages
	defer m.Ack()

	inventory := &cloud_rpc.InventoryReport{}
	err = proto.Unmarshal(m.Data, inventory)
	if err != nil {
		slog.Errorw("failed to decode message", "error", err, "data", string(m.Data))
		return
	}

	for _, devInfo := range inventory.Inventory.Devices {
		path, err := writeInventoryInfo(devInfo, idmap.CloudUUID)
		if err != nil {
			slog.Errorw("failed to write report", "path", path, "error", err)
			return
		}
		slog.Infow("wrote report", "path", path)
	}
}
