/*
 * COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package archive

import (
	"net"
	"sync"
	"time"
)

// Each archive has a Content-Type associated with it.
const (
	DropContentType = "application/vnd.b10e.drop-archive+json"
	StatContentType = "application/vnd.b10e.stat-archive+json"
	DropBinaryType  = "application/vnd.b10e.drop-archive+gob"
	StatBinaryType  = "application/vnd.b10e.stat-archive+gob"
)

// DropRecord contains information about a single packet blocked by the firewall
type DropRecord struct {
	Time  time.Time
	Indev string
	Src   string
	Dst   string
	Smac  string `json:",omitempty"`
	Proto string

	// Used in-core, but not persisted
	SrcIP   net.IP `json:"-"`
	DstIP   net.IP `json:"-"`
	SrcPort int    `json:"-"`
	DstPort int    `json:"-"`
}

// DropArchive lists all of the packets blocked by the firewall within the
// specified period of time.
type DropArchive struct {
	Start    time.Time
	End      time.Time
	LanDrops []*DropRecord `json:",omitempty"`
	WanDrops []*DropRecord `json:",omitempty"`
}

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

	mtx sync.Mutex
}

// Lock locks the device record
func (d *DeviceRecord) Lock() {
	d.mtx.Lock()
}

// Unlock unlocks the device record
func (d *DeviceRecord) Unlock() {
	d.mtx.Unlock()
}

// Snapshot captures the data for one or more devices over a period of time
type Snapshot struct {
	Start time.Time
	End   time.Time
	Data  map[string]*DeviceRecord
}

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
