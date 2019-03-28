/*
 * COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package main

import (
	"encoding/csv"
	"fmt"
	"io"
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

type client struct {
	attrs []string
}

// observations contains a subset of the data we observe from a client.
type observations struct {
	// hwaddr -> client
	clients    map[uint64]*client
	clientLock sync.Mutex

	// attribute name -> column index
	attrMap map[string]int
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

func (e *entities) writeInventory(path string) error {
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
		return nil
	}

	out, err := proto.Marshal(inventory)
	if err != nil {
		return fmt.Errorf("failed to encode device inventory: %s", err)
	}

	newPath := fmt.Sprintf("%s.%d", path, int(time.Now().Unix()))
	f, err := os.OpenFile(newPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := f.Write(out); err != nil {
		return fmt.Errorf("failed to write device inventory: %s", err)
	}

	return nil
}

func (o *observations) saveTestData(testPath string) error {
	tmpPath := testPath + ".tmp"
	f, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("failed to open %s: %s", tmpPath, err)
	}
	defer f.Close()
	w := csv.NewWriter(f)

	header := make([]string, len(o.attrMap)+1)
	header[0] = "MAC Address"
	for a, i := range o.attrMap {
		header[i+1] = a
	}
	w.Write(header)

	o.clientLock.Lock()
	for h, c := range o.clients {
		row := make([]string, 0)
		row = append(row, network.Uint64ToHWAddr(h).String())
		row = append(row, c.attrs...)
		w.Write(row)
	}
	o.clientLock.Unlock()

	w.Flush()
	if err := w.Error(); err != nil {
		if err = os.Remove(tmpPath); err != nil {
			slog.Warnf("failed to remove tmp file %s: %s\n", tmpPath, err)
		}
		return fmt.Errorf("failed to write %s: %s", tmpPath, err)
	}
	return os.Rename(tmpPath, testPath)
}

func (o *observations) loadTestData(testPath string) error {
	f, err := os.Open(testPath)
	if err != nil {
		return fmt.Errorf("failed to open %s: %s", testPath, err)
	}
	defer f.Close()
	r := csv.NewReader(f)

	header, err := r.Read()
	if err != nil {
		return fmt.Errorf("failed to read header from %s: %s", testPath, err)
	}

	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		} else if err != nil {
			return fmt.Errorf("failed to read from %s: %s", testPath, err)
		}

		hwaddr, err := net.ParseMAC(row[0])
		if err != nil {
			slog.Warnf("invalid MAC address %s: %s\n", row[0], err)
			continue
		}

		for i, v := range row[1:] {
			if v == "1" {
				o.setByName(network.HWAddrToUint64(hwaddr), header[i+1])
			}
		}
	}
	return nil
}

func (o *observations) setByName(hwaddr uint64, attr string) {
	o.clientLock.Lock()
	defer o.clientLock.Unlock()

	if _, ok := o.clients[hwaddr]; !ok {
		c := &client{
			attrs: make([]string, len(o.attrMap)),
		}

		for i := 0; i < len(c.attrs); i++ {
			c.attrs[i] = "0"
		}
		o.clients[hwaddr] = c
	}

	if col, ok := o.attrMap[attr]; ok {
		o.clients[hwaddr].attrs[col] = "1"
	}
}

// NewEntities creates an empty Entities
func newEntities() *entities {
	ret := &entities{
		dataMap: make(map[uint64]*entity),
	}
	return ret
}

// NewObservations creates an empty Observations
func newObservations() *observations {
	ret := &observations{
		clients: make(map[uint64]*client),
		attrMap: make(map[string]int),
	}
	return ret
}
