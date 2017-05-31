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

func (c Config) GetProp(prop string) (string, error) {
	var rval string
	var err error

	t := time.Now()
	oc := base_msg.ConfigQuery_GET

	query := &base_msg.ConfigQuery{
		Timestamp: &base_msg.Timestamp{
			Seconds: proto.Int64(t.Unix()),
			Nanos:   proto.Int32(int32(t.Nanosecond())),
		},
		Sender:    proto.String(c.sender),
		Debug:     proto.String("-"),
		Operation: &oc,
		Property:  proto.String(prop),
		Value:     proto.String("-"),
	}

	data, err := proto.Marshal(query)
	if err != nil {
		fmt.Println("Failed to marshal config update arguments: ", err)
		return "", err
	}

	c.mutex.Lock()
	_, err = c.socket.SendBytes(data, 0)
	if err == nil {
		var reply [][]byte

		reply, err = c.socket.RecvMessageBytes(0)
		if len(reply) > 0 {
			response := &base_msg.ConfigResponse{}
			proto.Unmarshal(reply[0], response)
			rval = *response.Value
		}
	} else {
		fmt.Println("Failed to send config request: ", err)
	}
	c.mutex.Unlock()

	return rval, err
}

func (c Config) SetProp(prop, val string) error {
	t := time.Now()
	oc := base_msg.ConfigQuery_SET

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

	data, err := proto.Marshal(query)
	if err != nil {
		fmt.Println("Failed to marshal config update arguments: ", err)
		return err
	}

	c.mutex.Lock()
	_, err = c.socket.SendBytes(data, 0)
	if err != nil {
		fmt.Println("Failed to send config update msg: ", err)
	} else {
		var reply [][]byte

		reply, err = c.socket.RecvMessageBytes(0)
		if len(reply) > 0 {
			response := &base_msg.ConfigResponse{}
			proto.Unmarshal(reply[0], response)
		}
	}
	c.mutex.Unlock()

	return err
}
