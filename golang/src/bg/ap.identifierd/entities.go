/*
 * Copyright 2020 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package main

import (
	"bytes"
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
	mac     uint64
	timeout time.Time
	saved   time.Time
	private bool
	info    *base_msg.DeviceInfo
	// scans tracks whether we have seen a scan result for a given type
	// (tcp, udp) for the client.  If we have then we ignore subsequent
	// scans until a reset().  Scans happen frequently enough that if we
	// gathered them all we'd have a result in most intervals.
	scans map[base_msg.ScanType]bool
	// fullResetTime tracks the time after which the entity record should
	// go through a full reset, so that data collection can begin again.
	fullResetTime time.Time
}

func newEntity(hwaddr uint64) *entity {
	return &entity{
		mac:     hwaddr,
		private: false,
		info: &base_msg.DeviceInfo{
			Created:    aputil.NowToProtobuf(),
			MacAddress: proto.Uint64(hwaddr),
		},
		scans:         make(map[base_msg.ScanType]bool),
		fullResetTime: time.Now().Add(resetDuration),
	}
}

func (e *entity) reset() {
	e.info = &base_msg.DeviceInfo{
		Created:    aputil.NowToProtobuf(),
		MacAddress: proto.Uint64(e.mac),
	}

	// Have we crossed a "major reset" interval?  If so, clear
	// data so that we restart the collection of a broader set of
	// information from this client.
	if time.Now().After(e.fullResetTime) {
		e.scans = make(map[base_msg.ScanType]bool)
		e.fullResetTime = time.Now().Add(resetDuration)
		e.timeout = time.Time{}

		hwaddr := network.Uint64ToHWAddr(e.mac)
		slog.Debugf("%s: starting full reset (next at %s)", hwaddr, e.fullResetTime)
	}
}

func (e *entity) addTimeout() {
	if e.timeout.IsZero() {
		e.timeout = time.Now().Add(collectionDuration)
	}
}

func (e *entity) isRecording() bool {
	return time.Now().Before(e.timeout)
}

// addOptions adds the DHCPOptions to the DeviceInfo if it seems to be
// different than a previous entry (differences seem to be very rare).
func (e *entity) addOptions(msg *base_msg.DHCPOptions) {
	// We expect Options to be of length 1 in nearly all cases-- DHCP
	// clients universally post the same ParamReqList and VendorClassId for
	// both DISCOVER and REQUEST.  Even getting two different entries is
	// noteworthy.  Here we limit the total number of Options which can be
	// gathered in order to prevent a runaway use of space by a broken or
	// antagonistic client.
	if len(e.info.Options) > 8 {
		return
	}
	// If this entry is substantially redundant with any previous entry we've
	// seen for this device during this interval, don't bother recording it.
	for _, o := range e.info.Options {
		if bytes.Equal(msg.ParamReqList, o.ParamReqList) &&
			bytes.Equal(msg.VendorClassId, o.VendorClassId) {
			return
		}
	}
	e.info.Options = append(e.info.Options, msg)
	e.info.Updated = aputil.NowToProtobuf()
}

// addScan adds an EventNetScan to the outbound entity; scans come in
// periodically, and may not be aligned with a recording interval.  So we track
// them separately, posting them in the interval when they arrive.
func (e *entity) addScan(msg *base_msg.EventNetScan) {
	t := msg.GetScanType()
	if t != base_msg.ScanType_UNKNOWN {
		if !e.scans[t] {
			e.scans[t] = true
			e.info.Scan = append(e.info.Scan, msg)
			e.info.Updated = aputil.NowToProtobuf()
		}
	}
}

// entities is a vessel to collect data about clients.
type entities struct {
	sync.Mutex
	dataMap map[uint64]*entity
}

func (e *entities) getEntityLocked(hwaddr uint64) *entity {
	_, ok := e.dataMap[hwaddr]
	if !ok {
		e.dataMap[hwaddr] = newEntity(hwaddr)
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
	d.info.DhcpName = proto.String(name)
}

func (e *entities) addMsgEntity(hwaddr uint64, msg *base_msg.EventNetEntity) {
	e.Lock()
	defer e.Unlock()

	d := e.getEntityLocked(hwaddr)
	d.addTimeout()
	if d.isRecording() && d.info.Entity == nil {
		d.info.Entity = msg
		d.info.Updated = aputil.NowToProtobuf()
	}
}

func (e *entities) addMsgOptions(hwaddr uint64, msg *base_msg.DHCPOptions) {
	e.Lock()
	defer e.Unlock()

	d := e.getEntityLocked(hwaddr)
	d.addTimeout()
	d.addOptions(msg)
}

func (e *entities) addMsgScan(hwaddr uint64, msg *base_msg.EventNetScan) {
	e.Lock()
	defer e.Unlock()

	d := e.getEntityLocked(hwaddr)
	d.addTimeout()
	d.addScan(msg)
}

func (e *entities) addMsgRequest(hwaddr uint64, msg *base_msg.EventNetRequest) {
	e.Lock()
	defer e.Unlock()

	d := e.getEntityLocked(hwaddr)
	d.addTimeout()
	if d.isRecording() && !d.private {
		d.info.Request = append(d.info.Request, msg)
		d.info.Updated = aputil.NowToProtobuf()
	}
}

func (e *entities) addMsgListen(hwaddr uint64, msg *base_msg.EventListen) {
	e.Lock()
	defer e.Unlock()

	d := e.getEntityLocked(hwaddr)
	d.addTimeout()
	if d.isRecording() {
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

	for _, d := range e.dataMap {
		updated := aputil.ProtobufToTime(d.info.Updated)
		if updated != nil && updated.After(d.saved) {
			if *trackVPN || !isVPN(d.mac) {
				slog.Debugf("Storing inventory for %x", d.mac)
				inventory.Devices = append(inventory.Devices, d.info)
				d.saved = time.Now()
			} else {
				slog.Debugf("Skipping inventory for %x", d.mac)
			}
		}
		d.reset()
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

