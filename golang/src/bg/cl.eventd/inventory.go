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
	"os"
	"path"
	"path/filepath"
	"time"

	"bg/base_msg"
	"bg/cloud_models/appliancedb"
	"bg/cloud_rpc"
	"bg/common/network"

	"cloud.google.com/go/pubsub"

	"github.com/pkg/errors"
	"github.com/satori/uuid"

	"github.com/golang/protobuf/proto"
	_ "google.golang.org/grpc/encoding/gzip"
)

const gcsBaseURL = "https://storage.cloud.google.com/"

func writeInventoryCS(ctx context.Context, applianceDB appliancedb.DataStore,
	uuid uuid.UUID, devInfo *base_msg.DeviceInfo, now time.Time) (string, error) {

	hwaddr := network.Uint64ToHWAddr(devInfo.GetMacAddress())
	filename := fmt.Sprintf("device_info.%d.pb", int(now.Unix()))
	filePath := path.Join("obs", hwaddr.String(), filename)

	out, err := proto.Marshal(devInfo)
	if err != nil {
		return "", errors.Wrap(err, "marshal failed")
	}

	return writeCSObject(ctx, applianceDB, uuid, filePath, out)
}

func writeInventoryFile(uuid uuid.UUID, devInfo *base_msg.DeviceInfo, now time.Time) (string, error) {
	hwaddr := network.Uint64ToHWAddr(devInfo.GetMacAddress())
	// We receive only what has recently changed
	hwaddrPath := filepath.Join(reportBasePath, uuid.String(), hwaddr.String())
	if err := os.MkdirAll(hwaddrPath, 0755); err != nil {
		return "", errors.Wrap(err, "inventory mkdir failed")
	}

	filename := fmt.Sprintf("device_info.%d.pb", int(now.Unix()))
	tmpFilename := "tmp." + filename
	path := filepath.Join(hwaddrPath, filename)
	tmpPath := filepath.Join(hwaddrPath, tmpFilename)
	f, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return "", err
	}
	defer f.Close()

	out, err := proto.Marshal(devInfo)
	if err != nil {
		_ = os.Remove(tmpPath)
		return "", errors.Wrap(err, "marshal failed")
	}

	if _, err := f.Write(out); err != nil {
		_ = os.Remove(tmpPath)
		return "", errors.Wrap(err, "write failed")
	}
	_ = f.Sync()

	// Creating a tmp file and renaming it guarantees that we'll never have
	// a partial record in the file.
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return "", errors.Wrap(err, "rename failed")
	}
	return path, nil
}

func inventoryMessage(ctx context.Context, applianceDB appliancedb.DataStore,
	siteUUID uuid.UUID, m *pubsub.Message) {
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
	// XXX in the future, pass the whole inventory to each writer, allowing
	// reuse of e.g.  http.Clients.
	for _, devInfo := range inventory.Inventory.Devices {
		path, err := writeInventoryFile(siteUUID, devInfo, now)
		if err != nil {
			slog.Errorw("failed to write DeviceInfo to file", "path", path, "error", err)
		} else {
			slog.Infow("wrote DeviceInfo to file", "path", path)
		}
		path, err = writeInventoryCS(ctx, applianceDB, siteUUID, devInfo, now)
		if err != nil {
			slog.Errorw("failed to write DeviceInfo to cloud storage", "path", path, "error", err)
		} else {
			slog.Infow("wrote DeviceInfo to cloud storage", "path", path)
		}
	}
}
