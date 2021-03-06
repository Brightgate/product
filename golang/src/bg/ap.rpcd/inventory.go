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
	"encoding/json"
	"flag"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"bg/ap_common/aputil"
	"bg/ap_common/platform"
	"bg/base_def"
	"bg/base_msg"
	"bg/cloud_rpc"
	"bg/common/network"

	"github.com/pkg/errors"

	"github.com/golang/protobuf/proto"
	_ "google.golang.org/grpc/encoding/gzip"
)

// gRPC has a default maximum message size of 4MiB
const msgsize = 524288

var (
	forceInventory = flag.Bool("force-inventory", false, "always send all inventory")
)

type diskManifest map[string]time.Time

func getManifest(manPath string) (diskManifest, error) {
	manifest := make(diskManifest)
	// When forceInventory is passed, we just nuke the manifest on
	// disk, forcing everything to start over.
	if *forceInventory {
		os.Remove(manPath)
		return manifest, nil
	}
	// Read manifest from disk
	m, err := ioutil.ReadFile(manPath)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read manifest")
	}
	err = json.Unmarshal(m, &manifest)
	if err != nil {
		return nil, errors.Wrap(err, "failed to import manifest")
	}
	slog.Debugw("getManifest", "contents", manifest)
	return manifest, nil
}

func putManifest(manPath string, manifest diskManifest) error {
	// Write manifest
	slog.Debugw("Writing manifest", "manifest", manifest)
	s, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return errors.Wrapf(err, "failed to make manifest JSON")
	}

	tmpPath := manPath + ".tmp"
	err = ioutil.WriteFile(tmpPath, s, 0644)
	if err != nil {
		return errors.Wrapf(err, "failed to write file %s", tmpPath)
	}

	err = os.Rename(tmpPath, manPath)
	if err != nil {
		return errors.Wrapf(err, "failed to rename %s -> %s", tmpPath, manPath)
	}
	return nil
}

func sendChanged(ctx context.Context, client cloud_rpc.EventClient, changed *base_msg.DeviceInventory, manifest diskManifest) error {
	// Build InventoryReport
	report := &cloud_rpc.InventoryReport{
		Inventory: changed,
	}
	err := publishEvent(ctx, client, "inventory", report)
	if err == nil {
		for _, sentDev := range changed.Devices {
			mac := network.Uint64ToMac(sentDev.GetMacAddress())
			manifest[mac] = time.Now().UTC()
		}
	}
	return err
}

func sendInventory(ctx context.Context, client cloud_rpc.EventClient) error {
	var err error
	invDir := plat.ExpandDirPath(platform.APData, "identifierd")
	manDir := plat.ExpandDirPath(platform.APData, "rpcd")
	manPath := filepath.Join(manDir, "identifierd.json.v1")

	if err = os.MkdirAll(manDir, 0755); err != nil {
		return errors.Wrapf(err, "failed to make manifest dir %s", manPath)
	}

	// Read device inventories from disk
	files, err := ioutil.ReadDir(invDir)
	if err != nil {
		return errors.Wrapf(err, "Could not read dir %s", invDir)
	}
	if len(files) == 0 {
		slog.Infof("no files found in %s", invDir)
		return nil
	}

	manifest, err := getManifest(manPath)
	if err != nil {
		slog.Warnf("failed to import manifest %s: %s", manPath, err)
		manifest = make(diskManifest)
	}

	changed := &base_msg.DeviceInventory{
		Timestamp: aputil.NowToProtobuf(),
	}

	for _, file := range files {
		var in []byte

		path := filepath.Join(invDir, file.Name())
		if in, err = ioutil.ReadFile(path); err != nil {
			slog.Warnf("failed to read device inventory %s: %s", path, err)
			continue
		}
		inventory := &base_msg.DeviceInventory{}
		err = proto.Unmarshal(in, inventory)
		in = nil
		runtime.GC()
		if err != nil {
			slog.Warnf("failed to unmarshal device inventory %s: %s", path, err)
			continue
		}

		for _, devInfo := range inventory.Devices {
			macaddr := devInfo.GetMacAddress()
			if macaddr == 0 || devInfo.Updated == nil {
				continue
			}
			mac := network.Uint64ToMac(macaddr)
			updated := aputil.ProtobufToTime(devInfo.Updated)
			sent, ok := manifest[mac]
			if !ok {
				sent = time.Time{}
			}
			if *forceInventory || updated.After(sent) {
				slog.Infof("Reporting %s > %s", file.Name(), mac)
				changed.Devices = append(changed.Devices, devInfo)
			} else {
				slog.Debugf("Skipping %s > %s", file.Name(), mac)
			}

			if proto.Size(changed) >= msgsize {
				err = sendChanged(ctx, client, changed, manifest)
				if err != nil {
					slog.Warnf("failed to sendChanged(): %v", err)
				}
				changed = &base_msg.DeviceInventory{
					Timestamp: aputil.NowToProtobuf(),
				}
			}
		}
	}

	if len(changed.Devices) != 0 {
		err = sendChanged(ctx, client, changed, manifest)
		if err != nil {
			slog.Warnf("failed final sendChanged(): %v", err)
		}
	}

	err = putManifest(manPath, manifest)
	if err != nil {
		return err
	}
	return nil
}

func inventoryLoop(ctx context.Context, client cloud_rpc.EventClient, wg *sync.WaitGroup, doneChan chan bool) {
	var done bool

	invChan := make(chan bool, 1)

	brokerd.Handle(base_def.TOPIC_DEVICE_INVENTORY, func(event []byte) {
		slog.Debugf("new inventory indicated on %s", base_def.TOPIC_DEVICE_INVENTORY)
		invChan <- true
	})
	slog.Infof("inventory loop starting")

	ticker := time.NewTicker(12 * time.Hour)
	defer ticker.Stop()
	for !done {
		err := sendInventory(ctx, client)
		if err != nil {
			slog.Errorf("Failed inventory: %s", err)
		}
		select {
		case done = <-doneChan:
		case <-invChan:
		case <-ticker.C:
		}
	}
	slog.Infof("inventory loop done")
	wg.Done()
}

