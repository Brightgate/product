/*
 * COPYRIGHT 2020 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package main

import (
	"fmt"
	"net"
	"os"
	"runtime/debug"
	"sync"
	"time"

	"bg/ap_common/aputil"
	"bg/base_msg"
	"bg/common/network"

	"github.com/golang/protobuf/proto"
)

// 'entity' contains data about a client. The data is sent to the cloud for
// later use as training data. Most data is collected for only 30 minutes after
// seeing the client is active. A client is deemed active if we receive any of:
//   1) EventNetEntity
//   2) DHCPOptions
//   3) EventNetScan
//   4) EventNetRequest
//   5) EventListen
// The 30 minute timeout is reset if identiferd restarts.
// XXX Add config option to reset the timeout on a specific client.
type entity struct {
	timeout time.Time
	saved   time.Time
	private bool
	info    *base_msg.DeviceInfo
}

// entities is a vessel to collect data about clients.
type entities struct {
	sync.Mutex
	dataMap map[uint64]*entity
}

func (e *entities) getEntityLocked(hwaddr uint64) *entity {
	_, ok := e.dataMap[hwaddr]
	if !ok {
		e.dataMap[hwaddr] = &entity{
			private: false,
			info: &base_msg.DeviceInfo{
				Created:    aputil.NowToProtobuf(),
				MacAddress: proto.Uint64(hwaddr),
			},
		}
	}
	return e.dataMap[hwaddr]
}

func (e *entities) setPrivacy(mac net.HardwareAddr, private bool) {
	e.Lock()
	defer e.Unlock()

	d := e.getEntityLocked(network.HWAddrToUint64(mac))
	d.private = private
}

func (e *entities) addDHCPName(hwaddr uint64, name string) {
	e.Lock()
	defer e.Unlock()

	d := e.getEntityLocked(hwaddr)
	if d.info.DhcpName == nil {
		d.info.DhcpName = proto.String(name)
	}
}

func addTimeout(d *entity) {
	if d.timeout.IsZero() {
		d.timeout = time.Now().Add(collectionDuration)
	}
}

func (e *entities) addMsgEntity(hwaddr uint64, msg *base_msg.EventNetEntity) {
	e.Lock()
	defer e.Unlock()

	d := e.getEntityLocked(hwaddr)
	addTimeout(d)
	if d.info.Entity == nil {
		d.info.Entity = msg
		d.info.Updated = aputil.NowToProtobuf()
	}
}

func (e *entities) addMsgOptions(hwaddr uint64, msg *base_msg.DHCPOptions) {
	e.Lock()
	defer e.Unlock()

	d := e.getEntityLocked(hwaddr)
	addTimeout(d)
	d.info.Options = append(d.info.Options, msg)
	d.info.Updated = aputil.NowToProtobuf()
}

func (e *entities) addMsgScan(hwaddr uint64, msg *base_msg.EventNetScan) {
	e.Lock()
	defer e.Unlock()

	d := e.getEntityLocked(hwaddr)
	addTimeout(d)
	if time.Now().Before(d.timeout) {
		d.info.Scan = append(d.info.Scan, msg)
		d.info.Updated = aputil.NowToProtobuf()
	}
}

func (e *entities) addMsgRequest(hwaddr uint64, msg *base_msg.EventNetRequest) {
	e.Lock()
	defer e.Unlock()

	d := e.getEntityLocked(hwaddr)
	addTimeout(d)
	if time.Now().Before(d.timeout) && !d.private {
		d.info.Request = append(d.info.Request, msg)
		d.info.Updated = aputil.NowToProtobuf()
	}
}

func (e *entities) addMsgListen(hwaddr uint64, msg *base_msg.EventListen) {
	e.Lock()
	defer e.Unlock()

	d := e.getEntityLocked(hwaddr)
	addTimeout(d)
	if time.Now().Before(d.timeout) {
		d.info.Listen = append(d.info.Listen, msg)
		d.info.Updated = aputil.NowToProtobuf()
	}
}

func (e *entities) writeInventory(path string) (int, error) {
	e.Lock()
	defer e.Unlock()
	defer debug.FreeOSMemory()

	inventory := &base_msg.DeviceInventory{
		Timestamp: aputil.NowToProtobuf(),
	}

	for h, d := range e.dataMap {
		updated := aputil.ProtobufToTime(d.info.Updated)
		if updated == nil || updated.Before(d.saved) {
			continue
		}

		inventory.Devices = append(inventory.Devices, d.info)
		d.saved = time.Now()
		d.info = &base_msg.DeviceInfo{
			Created:    aputil.NowToProtobuf(),
			MacAddress: proto.Uint64(h),
		}
	}

	if len(inventory.Devices) == 0 {
		return 0, nil
	}

	out, err := proto.Marshal(inventory)
	if err != nil {
		return 0, fmt.Errorf("failed to encode device inventory: %s", err)
	}

	newPath := fmt.Sprintf("%s.%d", path, int(time.Now().Unix()))
	slog.Debugf("writing Inventory %s", newPath)
	f, err := os.OpenFile(newPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	if _, err := f.Write(out); err != nil {
		return 0, fmt.Errorf("failed to write device inventory: %s", err)
	}

	return len(inventory.Devices), nil
}

// NewEntities creates an empty Entities
func newEntities() *entities {
	ret := &entities{
		dataMap: make(map[uint64]*entity),
	}
	return ret
}
