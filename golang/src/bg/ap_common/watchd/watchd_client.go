/*
 * COPYRIGHT 2017 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package watchd

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"bg/ap_common/aputil"
	"bg/base_def"
	"bg/base_msg"

	"github.com/golang/protobuf/proto"

	// Ubuntu: requires libzmq3-dev, which is 0MQ 4.2.1.
	zmq "github.com/pebbe/zmq4"
)

// We maintain some low-level usage statistics for each device.  Periodically
// the 'current' statistics are rolled into an 'aggregate' structure and the
// current stats are reset.  This gives us a view into a device's recent
// activity, as well as its overall activity. (XXX: eventually each snapshot
// will be saved into a database before before cleared, so we will be able to
// examine a device's historical behavior in greater detail.)

// ProtoRecord tracks a device's network statistics for a single protocol
// (currently just TCP and UDP)
type ProtoRecord struct {
	OpenPorts      map[int]bool   `json:"OpenPorts,omitempty"`
	OutPorts       map[int]bool   `json:"OutPorts,omitempty"`
	InPorts        map[int]bool   `json:"InPorts,omitempty"`
	OutgoingBlocks map[string]int `json:"OutgoingBlocks,omitempty"`
	IncomingBlocks map[string]int `json:"IncomingBlocks,omitempty"`
}

// DeviceRecord tracks a device's network statistics for all protocols
type DeviceRecord map[string]*ProtoRecord

// DeviceMap tracks all devices for which we are gathering statistics
type DeviceMap map[string]DeviceRecord

// Watchd is an opaque handle used by clients to communicate with ap.watchd
type Watchd struct {
	socket *zmq.Socket
	sender string
	sync.Mutex
}

const (
	// OK means the watchd operation succeeded
	OK = base_msg.WatchdResponse_OK
	// ERR means the watchd operation failed
	ERR = base_msg.WatchdResponse_Err

	sendTimeout = time.Duration(base_def.LOCAL_ZMQ_SEND_TIMEOUT * time.Second)
	recvTimeout = time.Duration(base_def.LOCAL_ZMQ_RECEIVE_TIMEOUT * time.Second)
)

// New instantiates a new watchd handle and establishes a 0MQ connection to the
// watchd daemon
func New(name string) (*Watchd, error) {

	socket, err := zmq.NewSocket(zmq.REQ)
	if err != nil {
		return nil, fmt.Errorf("failed to create new watchd socket: %v", err)
	}

	if err = socket.SetSndtimeo(sendTimeout); err != nil {
		return nil, fmt.Errorf("failed to set watchd send timeout: %v", err)
	}

	if err = socket.SetRcvtimeo(recvTimeout); err != nil {
		return nil, fmt.Errorf("failed to set watchd receive timeout: %v", err)
	}

	err = socket.Connect(base_def.LOCAL_ZMQ_URL + base_def.WATCHD_ZMQ_REP_PORT)
	if err != nil {
		return nil, fmt.Errorf("failed to connect new watchd socket: %v", err)
	}

	h := Watchd{
		sender: fmt.Sprintf("%s(%d)", name, os.Getpid()),
		socket: socket,
	}

	return &h, nil
}

// GetStats requests the stats for a device (or set of devices) from watchd
// within the specified time range, and returns the results in a DeviceMap
// structure.
func (w *Watchd) GetStats(mac string, start, end *time.Time) (DeviceMap, error) {
	var devs DeviceMap

	op := &base_msg.WatchdRequest{
		Timestamp: aputil.NowToProtobuf(),
		Sender:    proto.String(w.sender),
		Debug:     proto.String("-"),
		Device:    proto.String(mac),
		Command:   proto.String("getstats"),
	}

	if start != nil {
		op.Start = aputil.TimeToProtobuf(start)
	}
	if end != nil {
		op.End = aputil.TimeToProtobuf(end)
	}

	data, err := proto.Marshal(op)
	if err != nil {
		fmt.Println("Failed to marshal watchd arguments: ", err)
		return nil, err
	}

	w.Lock()
	defer w.Unlock()
	if _, err = w.socket.SendBytes(data, 0); err != nil {
		return nil, err
	}

	reply, err := w.socket.RecvMessageBytes(0)
	if err != nil {
		return nil, fmt.Errorf("watchd comm failure: %v", err)
	}
	if len(reply) == 0 {
		return nil, nil
	}

	r := base_msg.WatchdResponse{}
	proto.Unmarshal(reply[0], &r)
	switch *r.Status {
	case OK:
		rval := []byte(*r.Response)
		err = json.Unmarshal(rval, &devs)
		return devs, err
	case ERR:
		return nil, fmt.Errorf("%v", *r.Response)
	default:
		return nil, fmt.Errorf("unrecognized response from watchd")
	}
}

// GetStatsCurrent is a simple wrapper around GetStats that specifically asks
// for the current set of stats
func (w *Watchd) GetStatsCurrent(mac string) (DeviceMap, error) {
	return w.GetStats(mac, nil, nil)
}

// GetStatsAggregate is a simple wrapper around GetStats that specifically asks
// for the aggregated statistics since the system came online
func (w *Watchd) GetStatsAggregate(mac string) (DeviceMap, error) {
	// If the start and end times are equal, we're asking for the aggregate
	// stats since the system came up
	t := time.Now()
	return w.GetStats(mac, &t, &t)
}
