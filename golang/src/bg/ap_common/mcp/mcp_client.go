/*
 * COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
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
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"bg/ap_common/aputil"
	"bg/ap_common/platform"
	"bg/base_def"
	"bg/base_msg"

	"github.com/golang/protobuf/proto"

	// Ubuntu: requires libzmq3-dev, which is 0MQ 4.2.1.
	zmq "github.com/pebbe/zmq4"
)

// MCP is an opaque handle used by client daemons to communicate with ap.mcp
type MCP struct {
	socket   *zmq.Socket
	sender   string
	daemon   string
	platform *platform.Platform
	sync.Mutex
}

// Version gets incremented whenver the MCP protocol changes incompatibly
const Version = int32(1)

// Shorthand forms of base_msg commands and error codes
const (
	OK       = base_msg.MCPResponse_OP_OK
	INVALID  = base_msg.MCPResponse_INVALID
	NODAEMON = base_msg.MCPResponse_NO_DAEMON
	BADVER   = base_msg.MCPResponse_BADVERSION

	PING   = base_msg.MCPRequest_PING
	GET    = base_msg.MCPRequest_GET
	SET    = base_msg.MCPRequest_SET
	DO     = base_msg.MCPRequest_DO
	UPDATE = base_msg.MCPRequest_UPDATE
)

// Daemons must be in one of the following states
const (
	BROKEN = iota
	OFFLINE
	BLOCKED
	STARTING
	INITING
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

func newConnection(name, url string) (*MCP, error) {
	var handle *MCP

	plat := platform.NewPlatform()
	port := url + base_def.MCP_ZMQ_REP_PORT
	sendTO := base_def.LOCAL_ZMQ_SEND_TIMEOUT * time.Second
	recvTO := base_def.LOCAL_ZMQ_RECEIVE_TIMEOUT * time.Second

	socket, err := zmq.NewSocket(zmq.REQ)
	if err != nil {
		err = fmt.Errorf("failed to create new MCP socket: %v", err)
		return handle, err
	}

	if err = socket.SetSndtimeo(time.Duration(sendTO)); err != nil {
		fmt.Printf("failed to set MCP send timeout: %v\n", err)
		return handle, err
	}

	if err = socket.SetRcvtimeo(time.Duration(recvTO)); err != nil {
		fmt.Printf("failed to set MCP receive timeout: %v\n", err)
		return handle, err
	}

	err = socket.Connect(port)
	if err != nil {
		err = fmt.Errorf("failed to connect new socket %s: %v", port, err)
	} else {
		sender := fmt.Sprintf("%s(%d)", name, os.Getpid())
		handle = &MCP{
			sender:   sender,
			socket:   socket,
			platform: plat,
		}
		if name[0:3] == "ap." {
			handle.daemon = name[3:]
			handle.SetState(INITING)
		}
	}

	if err = handle.Ping(); err != nil {
		handle = nil
	}

	return handle, err
}

// New connects to ap.mcp, and returns an opaque handle that can be used for
// subsequent communication with the daemon.
func New(name string) (*MCP, error) {
	return newConnection(name, base_def.LOCAL_ZMQ_URL)
}

// NewPeer connects to ap.mcp running on a gateway node, and returns an opaque
// handle that can be used for subsequent communication with the daemon.
func NewPeer(name string) (*MCP, error) {
	if name != "ap.mcp" {
		log.Printf("Warning: NewPeer() is only intended to be called " +
			"by ap.mcp on satellite nodes.")
	}
	return newConnection(name, base_def.GATEWAY_ZMQ_URL)
}

// Close closes the connection to the mcp daemon
func (m *MCP) Close() {
	if m != nil {
		m.socket.Close()
	}
}

func (m *MCP) msg(op *base_msg.MCPRequest) (string, error) {
	data, err := proto.Marshal(op)
	if err != nil {
		fmt.Println("Failed to marshal mcp arguments: ", err)
		return "", err
	}

	rval := ""
	m.Lock()
	_, err = m.socket.SendBytes(data, 0)
	if err == nil {
		var reply [][]byte

		reply, err = m.socket.RecvMessageBytes(0)
		if err != nil {
			err = fmt.Errorf("Failed to receive mcp response: %v",
				err)
		} else if len(reply) > 0 {
			r := base_msg.MCPResponse{}
			proto.Unmarshal(reply[0], &r)
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
	}
	m.Unlock()

	return rval, err
}

// Ping sends a no-op command to the daemon as a liveness check and as a
// protocol version check.
func (m *MCP) Ping() error {
	oc := PING
	version := base_msg.Version{Major: proto.Int32(Version)}
	op := &base_msg.MCPRequest{
		Timestamp: aputil.NowToProtobuf(),
		Sender:    proto.String(m.sender),
		Version:   &version,
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
		version := base_msg.Version{Major: proto.Int32(Version)}
		op := &base_msg.MCPRequest{
			Timestamp: aputil.NowToProtobuf(),
			Sender:    proto.String(m.sender),
			Version:   &version,
			Debug:     proto.String("-"),
			State:     proto.String(string(b)),
			Node:      proto.String(nodeID),
			Lifetime:  proto.Int32(int32(lifetime.Seconds())),
			Operation: &oc,
		}
		s, rerr := m.msg(op)
		b = []byte(s)
		if rerr != nil {
			err = fmt.Errorf("mcp request failed: %v", err)
		} else if rerr := json.Unmarshal(b, &rval); rerr != nil {
			err = fmt.Errorf("failed to unmarshal reply: %v", err)
		}
	}

	return rval, err
}

func (m *MCP) daemonMsg(oc base_msg.MCPRequest_Operation,
	daemon, command string, state int) (string, error) {

	version := base_msg.Version{Major: proto.Int32(Version)}
	op := &base_msg.MCPRequest{
		Timestamp: aputil.NowToProtobuf(),
		Sender:    proto.String(m.sender),
		Version:   &version,
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
