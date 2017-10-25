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

type MCP struct {
	socket *zmq.Socket
	sender string
	daemon string
	sync.Mutex
}

const (
	OK        = base_msg.MCPResponse_OP_OK
	INVALID   = base_msg.MCPResponse_INVALID
	NO_DAEMON = base_msg.MCPResponse_NO_DAEMON

	OP_GET = base_msg.MCPRequest_GET
	OP_SET = base_msg.MCPRequest_SET
	OP_DO  = base_msg.MCPRequest_DO
)

const (
	OFFLINE = iota
	STARTING
	INITING
	ONLINE
	STOPPING
	INACTIVE
	BROKEN
)

var States = map[int]string{
	OFFLINE:  "offline",
	STARTING: "starting",
	INITING:  "initializing",
	ONLINE:   "online",
	STOPPING: "stopping",
	BROKEN:   "broken",
	INACTIVE: "inactive",
}

type DaemonState struct {
	Name  string
	State int
	Since time.Time
	Pid   int
}

func New(name string) (*MCP, error) {
	var handle *MCP

	sender := fmt.Sprintf("%s(%d)", name, os.Getpid())

	socket, err := zmq.NewSocket(zmq.REQ)
	if err != nil {
		err = fmt.Errorf("Failed to create new MCP socket: %v", err)
		return handle, err
	}

	err = socket.SetSndtimeo(time.Duration(base_def.LOCAL_ZMQ_SEND_TIMEOUT * time.Second))
	if err != nil {
		fmt.Printf("Failed to set MCP send timeout: %v\n", err)
		return handle, err
	}

	err = socket.SetRcvtimeo(time.Duration(base_def.LOCAL_ZMQ_RECEIVE_TIMEOUT * time.Second))
	if err != nil {
		fmt.Printf("Failed to set MCP receive timeout: %v\n", err)
		return handle, err
	}

	err = socket.Connect(base_def.MCP_ZMQ_REP_URL)
	if err != nil {
		err = fmt.Errorf("Failed to connect new MCP socket: %v", err)
		return handle, err
	} else {
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
			case NO_DAEMON:
				err = fmt.Errorf("No such daemon")
			default:
				if oc == OP_GET {
					rval = *r.State
				}
			}
		}
	}
	m.Unlock()

	return rval, err
}

func (m MCP) GetState(daemon string) (string, error) {
	return m.msg(OP_GET, daemon, "", -1)
}

func (m MCP) SetState(state int) error {
	var err error

	if _, ok := States[state]; !ok {
		err = fmt.Errorf("invalid state: %d", state)
	} else if m.daemon == "" {
		err = fmt.Errorf("only a daemon can update its state")
	} else {
		_, err = m.msg(OP_SET, m.daemon, "", state)
	}

	return err
}

func (m MCP) Do(daemon, command string) error {
	_, err := m.msg(OP_DO, daemon, command, -1)

	return err
}
