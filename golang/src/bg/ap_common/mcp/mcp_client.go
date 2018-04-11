/*
 * COPYRIGHT 2017 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package mcp

import (
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

// MCP is an opaque handle used by client daemons to communicate with ap.mcp
type MCP struct {
	socket *zmq.Socket
	sender string
	daemon string
	sync.Mutex
}

// Shorthand forms of base_msg commands and error codes
const (
	OK       = base_msg.MCPResponse_OP_OK
	INVALID  = base_msg.MCPResponse_INVALID
	NODAEMON = base_msg.MCPResponse_NO_DAEMON

	GET = base_msg.MCPRequest_GET
	SET = base_msg.MCPRequest_SET
	DO  = base_msg.MCPRequest_DO
)

// Daemons must be in one of the following states
const (
	OFFLINE = iota
	STARTING
	INITING
	ONLINE
	STOPPING
	INACTIVE
	BROKEN
)

// States maps the integral value of a daemon's state to a human-readable ascii
// value
var States = map[int]string{
	OFFLINE:  "offline",
	STARTING: "starting",
	INITING:  "initializing",
	ONLINE:   "online",
	STOPPING: "stopping",
	BROKEN:   "broken",
	INACTIVE: "inactive",
}

// DaemonState describes the current state of a daemon
type DaemonState struct {
	Name  string
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

// New connects to ap.mcp, and returns an opaque handle that can be used for
// subsequent communication with the daemon.
func New(name string) (*MCP, error) {
	var handle *MCP

	port := base_def.LOCAL_ZMQ_URL + base_def.MCP_ZMQ_REP_PORT
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
		handle = &MCP{sender: sender, socket: socket}
		if name[0:3] == "ap." {
			handle.daemon = name[3:]
			handle.SetState(INITING)
		}
	}

	return handle, err
}

func (m *MCP) msg(oc base_msg.MCPRequest_Operation,
	daemon, command string, state int) (string, error) {

	op := &base_msg.MCPRequest{
		Timestamp: aputil.NowToProtobuf(),
		Sender:    proto.String(m.sender),
		Debug:     proto.String("-"),
		Operation: &oc,
		State:     proto.Int32(int32(state)),
		Daemon:    proto.String(daemon),
	}

	if len(command) > 0 {
		op.Command = proto.String(command)
	}

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
				err = fmt.Errorf("Invalid command")
			case NODAEMON:
				err = fmt.Errorf("No such daemon")
			default:
				if oc == GET {
					rval = *r.State
				}
			}
		}
	}
	m.Unlock()

	return rval, err
}

// GetState will query ap.mcp for the current state of a single daemon.
func (m *MCP) GetState(daemon string) (string, error) {
	return m.msg(GET, daemon, "", -1)
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
		_, err = m.msg(SET, m.daemon, "", state)
	}

	return err
}

// Do is used to instruct ap.mcp to initiate an operation on a daemon
func (m *MCP) Do(daemon, command string) error {
	_, err := m.msg(DO, daemon, command, -1)

	return err
}
