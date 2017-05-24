/*
 * COPYRIGHT 2017 Brightgate Inc. All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or  alteration will be a violation of federal law.
 */

package main

import (
	"fmt"
	"log"
	"time"

	"base_def"
	"base_msg"

	"github.com/golang/protobuf/proto"

	zmq "github.com/pebbe/zmq4"
)

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	log.Println("start")
	publisher, _ := zmq.NewSocket(zmq.PUB)
	publisher.Connect(base_def.BROKER_ZMQ_PUB_URL)

	//  Ensure subscriber connection has time to complete
	time.Sleep(time.Second)

	t := time.Now()

	// XXX Build a legitimate ping message.
	ping := &base_msg.EventPing{
		Timestamp: &base_msg.Timestamp{
			Seconds: proto.Int64(t.Unix()),
			Nanos:   proto.Int32(int32(t.Nanosecond())),
		},
		Sender:      proto.String("ap-msgping(nnnnn)"),
		Debug:       proto.String("-"),
		PingMessage: proto.String("-"),
	}

	data, err := proto.Marshal(ping)

	_, err = publisher.SendMessage(base_def.TOPIC_PING, data)
	if err != nil {
		fmt.Println(err)
	}

	log.Println("end")
}
