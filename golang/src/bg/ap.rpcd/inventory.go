/*
 * COPYRIGHT 2019 Brightgate Inc. All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package main

import (
	"context"
	"encoding/json"
	"flag"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"
	"time"

	"bg/ap_common/aputil"
	"bg/base_msg"
	"bg/cloud_rpc"
	"bg/common/network"

	"github.com/pkg/errors"

	"github.com/golang/protobuf/proto"
	_ "google.golang.org/grpc/encoding/gzip"
)

// gRPC has a default maximum message size of 4MiB
const msgsize = 2097152

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
	slog.Debugf("getManifest", "contents", manifest)
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
	invDir := plat.ExpandDirPath("__APDATA__", "identifierd/inventory")
	manDir := plat.ExpandDirPath("__APDATA__", "rpcd")
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

	ticker := time.NewTicker(12 * time.Hour)
	defer ticker.Stop()
	for !done {
		err := sendInventory(ctx, client)
		if err != nil {
			slog.Errorf("Failed inventory: %s", err)
		}
		select {
		case done = <-doneChan:
		case <-ticker.C:
		}
	}
	slog.Infof("inventory loop exiting")
	wg.Done()
}
