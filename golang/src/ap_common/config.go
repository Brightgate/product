/*
 * COPYRIGHT 2017 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package ap_common

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

type PropertyNode struct {
	Name     string
	Value    string          `json:"Value,omitempty"`
	Expires  *time.Time      `json:"Expires,omitempty"`
	Children []*PropertyNode `json:"Children,omitempty"`
}

func dumpSubtree(node *PropertyNode, level int) {
	indent := ""
	for i := 0; i < level; i++ {
		indent += "  "
	}
	e := ""
	if node.Expires != nil {
		e = node.Expires.Format("2006-02-01T15:04:05")
	}
	fmt.Printf("%s%s: %s  %s\n", indent, node.Name, node.Value, e)
	for _, n := range node.Children {
		dumpSubtree(n, level+1)
	}
}

func DumpTree(node *PropertyNode) {
	dumpSubtree(node, 0)
}

type Config struct {
	mutex  sync.Mutex
	socket *zmq.Socket
	sender string
}

func NewConfig(name string) *Config {
	sender := fmt.Sprintf("%s(%d)", name, os.Getpid())
	socket, _ := zmq.NewSocket(zmq.REQ)
	socket.Connect(base_def.CONFIGD_ZMQ_REP_URL)

	return &Config{sender: sender, socket: socket}
}

func (c *Config) msg(oc base_msg.ConfigQuery_Operation, prop, val string,
	expires *time.Time) (string, error) {
	t := time.Now()
	query := &base_msg.ConfigQuery{
		Timestamp: &base_msg.Timestamp{
			Seconds: proto.Int64(t.Unix()),
			Nanos:   proto.Int32(int32(t.Nanosecond())),
		},
		Sender:    proto.String(c.sender),
		Debug:     proto.String("-"),
		Operation: &oc,
		Property:  proto.String(prop),
		Value:     proto.String(val),
	}
	if expires != nil {
		query.Expires = &base_msg.Timestamp{
			Seconds: proto.Int64(expires.Unix()),
			Nanos:   proto.Int32(int32(expires.Nanosecond())),
		}
	}

	data, err := proto.Marshal(query)
	if err != nil {
		fmt.Println("Failed to marshal config update arguments: ", err)
		return "", err
	}

	c.mutex.Lock()
	_, err = c.socket.SendBytes(data, 0)
	rval := ""
	if err != nil {
		fmt.Println("Failed to send config msg: ", err)
	} else {
		var reply [][]byte

		reply, err = c.socket.RecvMessageBytes(0)
		if len(reply) > 0 {
			response := &base_msg.ConfigResponse{}
			proto.Unmarshal(reply[0], response)
			if oc == base_msg.ConfigQuery_GET {
				rval = *response.Value
			}
		}
	}
	c.mutex.Unlock()

	return rval, err
}

func (c Config) GetProp(prop string) (string, error) {
	rval, err := c.msg(base_msg.ConfigQuery_GET, prop, "-", nil)

	return rval, err
}

func (c Config) SetProp(prop, val string, expires *time.Time) error {
	_, err := c.msg(base_msg.ConfigQuery_SET, prop, val, expires)

	return err
}

func (c Config) CreateProp(prop, val string, expires *time.Time) error {
	_, err := c.msg(base_msg.ConfigQuery_CREATE, prop, val, expires)

	return err
}

func (c Config) DeleteProp(prop string) error {
	_, err := c.msg(base_msg.ConfigQuery_DELETE, prop, "-", nil)

	return err
}
