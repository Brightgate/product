/*
 * COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package mcp

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"bg/ap_common/aputil"
	"bg/ap_common/comms"
	"bg/ap_common/platform"
	"bg/base_def"
	"bg/base_msg"

	"github.com/golang/protobuf/proto"
)

// MCP is an opaque handle used by client daemons to communicate with ap.mcp
type MCP struct {
	comm     *comms.APComm
	sender   string
	daemon   string
	platform *platform.Platform
	sync.Mutex
}

// Version gets incremented whenver the MCP protocol changes incompatibly
const Version = int32(1)

var (
	msgVersion = base_msg.Version{Major: proto.Int32(Version)}
)

// Shorthand forms of base_msg commands and error codes
const (
	OK       = base_msg.MCPResponse_OP_OK
	INVALID  = base_msg.MCPResponse_INVALID
	NODAEMON = base_msg.MCPResponse_NO_DAEMON
	BADVER   = base_msg.MCPResponse_BADVERSION

	PING    = base_msg.MCPRequest_PING
	GET     = base_msg.MCPRequest_GET
	SET     = base_msg.MCPRequest_SET
	DO      = base_msg.MCPRequest_DO
	UPDATE  = base_msg.MCPRequest_UPDATE
	REBOOT  = base_msg.MCPRequest_REBOOT
	GATEWAY = base_msg.MCPRequest_GATEWAY
)

// Daemons must be in one of the following states
const (
	BROKEN = iota
	OFFLINE
	BLOCKED
	STARTING
	INITING
	FAILSAFE
	ONLINE
	STOPPING
)

// States maps the integral value of a daemon's state to a human-readable ascii
// value
var States = map[int]string{
	BROKEN:   "broken",
	OFFLINE:  "offline",
	BLOCKED:  "blocked",
	STARTING: "starting",
	INITING:  "initializing",
	FAILSAFE: "failsafe",
	ONLINE:   "online",
	STOPPING: "stopping",
}

// DaemonState describes the current state of a daemon
type DaemonState struct {
	Name  string
	Node  string
	State int
	Since time.Time
	Pid   int

	VMSize  uint64 // Current vm size in bytes
	VMSwap  uint64 // Current swapped-out memory in bytes
	RssSize uint64 // Current in-core memory in bytes
	Utime   uint64 // CPU consumed in user mode, in ticks
	Stime   uint64 // CPI consumed in kernel mode, in ticks
}

// DaemonList is a slice containing the states for multiple daemons
type DaemonList []*DaemonState

func (l DaemonList) Len() int {
	return len(l)
}

func (l DaemonList) Less(i, j int) bool {
	return strings.Compare(l[i].Name, l[j].Name) < 0
}

func (l DaemonList) Swap(i, j int) {
	l[i], l[j] = l[j], l[i]
}

// Sort will sort the list of daemons into alphabetical order
func (l DaemonList) Sort() {
	sort.Sort(l)
}

func newConnection(daemon, host string) (*MCP, error) {
	var handle *MCP

	url := "tcp://" + host + base_def.MCP_COMM_REP_PORT
	comm, err := comms.NewAPClient(daemon, url)
	if err != nil {
		err = fmt.Errorf("creating APClient: %v", err)
	} else {
		sender := fmt.Sprintf("%s(%d)", daemon, os.Getpid())
		handle = &MCP{
			sender:   sender,
			comm:     comm,
			platform: platform.NewPlatform(),
		}
		if daemon[0:3] == "ap." {
			handle.daemon = daemon[3:]
			handle.SetState(INITING)
		}
		if err = handle.Ping(); err != nil {
			handle = nil
		}
	}

	return handle, err
}

// New connects to ap.mcp on this node, and returns an opaque handle that can be
// used for subsequent communication with the daemon.
func New(name string) (*MCP, error) {
	return newConnection(name, "127.0.0.1")
}

// NewPeer connects to ap.mcp running on a gateway node, and returns an opaque
// handle that can be used for subsequent communication with the daemon.
func NewPeer(name string, ip net.IP) (*MCP, error) {
	if name != "ap.mcp" {
		log.Printf("Warning: NewPeer() is only intended to be called " +
			"by ap.mcp on satellite nodes.")
	}
	c, err := newConnection(name, ip.String())
	if err == nil {
		c.comm.SetRecvTimeout(time.Second)
		c.comm.SetSendTimeout(time.Second)
		c.comm.SetOpenTimeout(time.Second)
	}

	return c, err
}

// Close closes the connection to the mcp daemon
func (m *MCP) Close() {
	if m != nil {
		m.comm.Close()
	}
}

func (m *MCP) msg(op *base_msg.MCPRequest) (string, error) {
	var rval string

	data, err := proto.Marshal(op)
	if err != nil {
		return "", fmt.Errorf("marshaling mcp arguments: %v", err)
	}

	reply, err := m.comm.ReqRepl(data)
	if err != nil {
		err = fmt.Errorf("communicating with mcp: %v", err)

	} else if len(reply) > 0 {
		r := base_msg.MCPResponse{}
		proto.Unmarshal(reply, &r)

		switch *r.Response {
		case INVALID:
			err = fmt.Errorf("invalid command")
		case NODAEMON:
			err = fmt.Errorf("no such daemon")
		case BADVER:
			var version string
			if r.MinVersion != nil {
				version = fmt.Sprintf("%d or greater",
					*r.MinVersion.Major)
			} else {
				version = fmt.Sprintf("%d",
					*r.Version.Major)
			}
			err = fmt.Errorf("requires version %s", version)
		default:
			if r.State != nil {
				rval = *r.State
			}
		}
	}

	return rval, err
}

// Ping sends a no-op command to the daemon as a liveness check and as a
// protocol version check.
func (m *MCP) Ping() error {
	oc := PING
	op := &base_msg.MCPRequest{
		Timestamp: aputil.NowToProtobuf(),
		Sender:    proto.String(m.sender),
		Version:   &msgVersion,
		Debug:     proto.String("-"),
		Operation: &oc,
	}

	_, err := m.msg(op)
	return err
}

// PeerUpdate sends a slice of DaemonState structures representing the states of
// one or more daemons running on a satellite node.  It returns a different
// slice of DaemonState structures representing the states of all daemons on the
// gateway node.
func (m *MCP) PeerUpdate(lifetime time.Duration,
	states []*DaemonState) ([]*DaemonState, error) {

	var rval []*DaemonState

	nodeID, _ := m.platform.GetNodeID()
	b, err := json.Marshal(states)
	if err != nil {
		err = fmt.Errorf("failed to marshal daemon state: %v", err)
	} else {
		oc := UPDATE
		op := &base_msg.MCPRequest{
			Timestamp: aputil.NowToProtobuf(),
			Sender:    proto.String(m.sender),
			Version:   &msgVersion,
			Debug:     proto.String("-"),
			State:     proto.String(string(b)),
			Node:      proto.String(nodeID),
			Lifetime:  proto.Int32(int32(lifetime.Seconds())),
			Operation: &oc,
		}
		s, rerr := m.msg(op)
		b = []byte(s)
		if rerr != nil {
			err = fmt.Errorf("mcp request failed: %v", rerr)
		} else if rerr := json.Unmarshal(b, &rval); rerr != nil {
			err = fmt.Errorf("failed to unmarshal reply: %v", rerr)
		}
	}

	return rval, err
}

func (m *MCP) daemonMsg(oc base_msg.MCPRequest_Operation,
	daemon, command string, state int) (string, error) {

	op := &base_msg.MCPRequest{
		Timestamp: aputil.NowToProtobuf(),
		Sender:    proto.String(m.sender),
		Version:   &msgVersion,
		Debug:     proto.String("-"),
		Operation: &oc,
		Daemon:    proto.String(daemon),
		State:     proto.String(States[state]),
		Command:   proto.String(command),
	}

	return m.msg(op)
}

// GetState will query ap.mcp for the current state of a single daemon.
func (m *MCP) GetState(daemon string) (string, error) {
	return m.daemonMsg(GET, daemon, "", -1)
}

// SetState is used by a daemon to notify ap.mcp of a change in its state
func (m *MCP) SetState(state int) error {
	var err error

	if m == nil {
		return nil
	}

	if _, ok := States[state]; !ok {
		err = fmt.Errorf("invalid state: %d", state)
	} else if m.daemon == "" {
		err = fmt.Errorf("only a daemon can update its state")
	} else {
		_, err = m.daemonMsg(SET, m.daemon, "", state)
	}

	return err
}

// Do is used to instruct ap.mcp to initiate an operation on a daemon
func (m *MCP) Do(daemon, command string) error {
	_, err := m.daemonMsg(DO, daemon, command, -1)

	return err
}

// Reboot is used to instruct ap.mcp to reboot the platform
func (m *MCP) Reboot() error {
	cmd := base_msg.MCPRequest_REBOOT

	op := &base_msg.MCPRequest{
		Timestamp: aputil.NowToProtobuf(),
		Sender:    proto.String(m.sender),
		Version:   &msgVersion,
		Debug:     proto.String("-"),
		Operation: &cmd,
	}

	_, err := m.msg(op)
	return err
}

// Gateway returns the IP address of the gateway node
func (m *MCP) Gateway() (net.IP, error) {
	var ip net.IP

	cmd := base_msg.MCPRequest_GATEWAY
	op := &base_msg.MCPRequest{
		Timestamp: aputil.NowToProtobuf(),
		Sender:    proto.String(m.sender),
		Version:   &msgVersion,
		Debug:     proto.String("-"),
		Operation: &cmd,
	}

	rval, err := m.msg(op)
	if err == nil {
		ip = net.ParseIP(rval)
		if ip == nil {
			err = fmt.Errorf("bad IP address: %s", rval)
		}
	}
	return ip, err
}

// GetComm returns the APComm handle used to communicate with the mcp daemon
func (m *MCP) GetComm() *comms.APComm {
	return m.comm
}
