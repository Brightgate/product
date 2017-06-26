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

	"base_def"
	"base_msg"

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
	MCP_OK        = base_msg.MCPResponse_OP_OK
	MCP_INVALID   = base_msg.MCPResponse_INVALID
	MCP_NO_DAEMON = base_msg.MCPResponse_NO_DAEMON

	MCP_OP_GET = base_msg.MCPRequest_GET
	MCP_OP_SET = base_msg.MCPRequest_SET
	MCP_OP_DO  = base_msg.MCPRequest_DO
)

type DaemonStatus struct {
	Name   string
	Status string
	Since  time.Time
	Pid    int
}

func New(name string) (*MCP, error) {
	var handle *MCP

	sender := fmt.Sprintf("%s(%d)", name, os.Getpid())

	socket, err := zmq.NewSocket(zmq.REQ)
	if err != nil {
		err = fmt.Errorf("Failed to create new MCP socket: %v", err)
		return handle, err
	}

	err = socket.SetRcvtimeo(time.Duration(30 * time.Second))
	if err != nil {
		fmt.Printf("Failed to set MCP receive timeout: %v\n", err)
	}

	err = socket.Connect(base_def.MCP_ZMQ_REP_URL)
	if err != nil {
		err = fmt.Errorf("Failed to connect new MCP socket: %v", err)
	} else {
		handle = &MCP{sender: sender, socket: socket}
		if name[0:3] == "ap." {
			handle.daemon = name[3:]
			handle.SetStatus("initializing")
		}
	}

	return handle, err
}

func (m *MCP) msg(oc base_msg.MCPRequest_Operation,
	daemon, status, command string) (string, error) {

	t := time.Now()
	op := &base_msg.MCPRequest{
		Timestamp: &base_msg.Timestamp{
			Seconds: proto.Int64(t.Unix()),
			Nanos:   proto.Int32(int32(t.Nanosecond())),
		},
		Sender:    proto.String(m.sender),
		Debug:     proto.String("-"),
		Operation: &oc,
		Daemon:    proto.String(daemon),
	}

	if len(status) > 0 {
		op.Status = proto.String(status)
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
			case MCP_INVALID:
				err = fmt.Errorf("Invalid command")
			case MCP_NO_DAEMON:
				err = fmt.Errorf("No such daemon")
			default:
				if oc == MCP_OP_GET {
					rval = *r.Status
				}
			}
		}
	}
	m.Unlock()

	return rval, err
}

func (m MCP) GetStatus(daemon string) (string, error) {
	rval, err := m.msg(MCP_OP_GET, daemon, "", "")

	return rval, err
}

func (m MCP) SetStatus(status string) error {
	var err error

	if m.daemon == "" {
		err = fmt.Errorf("Only a daemon can update its status")
	} else {
		_, err = m.msg(MCP_OP_SET, m.daemon, status, "")
	}

	return err
}

func (m MCP) Do(daemon, command string) error {
	_, err := m.msg(MCP_OP_DO, daemon, "", command)

	return err
}
