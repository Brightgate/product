/*
 * COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
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
	"net"
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

// Session represents a connection between two devices.  The local address is
// implicit, as a session struct is always found within a device record.
type Session struct {
	RAddr net.IP // Remote address
	RPort int    // Remote port
	LPort int    // Local port
}

// XferStats contains a single set of send/recv statistics
type XferStats struct {
	PktsSent  uint64
	PktsRcvd  uint64
	BytesSent uint64
	BytesRcvd uint64
}

// DeviceRecord tracks a device's network statistics
type DeviceRecord struct {
	Addr net.IP

	// Open ports found during an nmap scan
	OpenTCP []int `json:"OpenTCP,omitempty"`
	OpenUDP []int `json:"OpenUDP,omitempty"`

	// Per-IP counts of packets blocked by the firewall
	BlockedOut map[uint64]int `json:"BlockedOut,omitempty"`
	BlockedIn  map[uint64]int `json:"BlockedIn,omitempty"`

	// Data transfer stats, both total and per-session
	Aggregate XferStats
	LANStats  map[uint64]XferStats `json:"Local,omitempty"`
	WANStats  map[uint64]XferStats `json:"Remote,omitempty"`

	sync.Mutex
}

// Snapshot captures the data for one or more devices over a period of time
type Snapshot struct {
	Start time.Time
	End   time.Time
	Data  map[string]*DeviceRecord
}

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

// SessionToKey Session struct into a 64-bit key, which can be used as a map
// index
func SessionToKey(s Session) uint64 {
	addr := s.RAddr.To4()

	rval := uint64(addr[0])
	rval |= uint64(addr[1]) << 8
	rval |= uint64(addr[2]) << 16
	rval |= uint64(addr[3]) << 24
	rval |= uint64(s.RPort) << 32
	rval |= uint64(s.LPort) << 48
	return rval
}

// KeyToSession Convert a 64-bit map key back into the original Session struct
func KeyToSession(key uint64) Session {
	a := uint8(key & 0xff)
	b := uint8((key >> 8) & 0xff)
	c := uint8((key >> 16) & 0xff)
	d := uint8((key >> 24) & 0xff)
	s := Session{
		RAddr: net.IPv4(a, b, c, d),
		RPort: int((key >> 32) & 0xffff),
		LPort: int((key >> 48) & 0xffff),
	}
	return s
}

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
func (w *Watchd) GetStats(mac string, start, end *time.Time) ([]Snapshot, error) {
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
		var s []Snapshot
		rval := []byte(*r.Response)
		err = json.Unmarshal(rval, &s)
		return s, err
	case ERR:
		return nil, fmt.Errorf("%v", *r.Response)
	default:
		return nil, fmt.Errorf("unrecognized response from watchd")
	}
}

// GetStatsCurrent is a simple wrapper around GetStats that specifically asks
// for the current set of stats
func (w *Watchd) GetStatsCurrent(mac string) (Snapshot, error) {
	now := time.Now()
	slice, err := w.GetStats(mac, &now, nil)
	if len(slice) == 0 {
		return Snapshot{}, err
	}

	return slice[0], nil
}

// GetStatsAll is a simple wrapper around GetStats that specifically asks
// for all of the snapshots still available locally.
func (w *Watchd) GetStatsAll(mac string) ([]Snapshot, error) {
	return w.GetStats(mac, nil, nil)
}
