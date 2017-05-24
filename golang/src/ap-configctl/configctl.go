/*
 * COPYRIGHT 2017 Brightgate Inc. All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or  alteration will be a violation of federal law.
 */

/*
 * ap-configctl [-value] property_or_value
 */

package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"base_def"
	"base_msg"

	"github.com/golang/protobuf/proto"

	zmq "github.com/pebbe/zmq4"
)

const (
	sender_fmt = "ap-configctl(%d)"
)

var query_value = flag.Bool("value", false, "Query values")
var set_value = flag.Bool("set", false, "Set one property to the given value")

func main() {
	flag.Parse()

	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	log.Println("start")

	config, _ := zmq.NewSocket(zmq.REQ)
	config.Connect(base_def.CONFIGD_ZMQ_REP_URL)

	//  Ensure subscriber connection has time to complete
	time.Sleep(time.Second)

	sender := fmt.Sprintf(sender_fmt, os.Getpid())

	if *set_value {
		log.Println("set")

		if len(flag.Args()) != 2 {
			log.Fatal("wrong set invocation")
		}

		t := time.Now()
		oc := base_msg.ConfigQuery_SET

		query := &base_msg.ConfigQuery{
			Timestamp: &base_msg.Timestamp{
				Seconds: proto.Int64(t.Unix()),
				Nanos:   proto.Int32(int32(t.Nanosecond())),
			},
			Sender:    proto.String(sender),
			Debug:     proto.String("-"),
			Operation: &oc,
			Property:  proto.String(flag.Arg(0)),
			Value:     proto.String(flag.Arg(1)),
		}

		data, err := proto.Marshal(query)

		_, err = config.SendBytes(data, 0)
		if err != nil {
			fmt.Println(err)
		}

		// XXX Read back response.
		reply, _ := config.RecvMessageBytes(0)
		log.Printf("Received reply [%s]\n", reply)

		response := &base_msg.ConfigResponse{}
		proto.Unmarshal(reply[0], response)

		log.Println(response)

		log.Println("end set")

		os.Exit(0)
	}

	for _, arg := range flag.Args() {
		log.Println(arg)

		t := time.Now()
		oc := base_msg.ConfigQuery_GET

		query := &base_msg.ConfigQuery{
			Timestamp: &base_msg.Timestamp{
				Seconds: proto.Int64(t.Unix()),
				Nanos:   proto.Int32(int32(t.Nanosecond())),
			},
			Sender:    proto.String(sender),
			Debug:     proto.String("-"),
			Operation: &oc,
			Property:  proto.String(arg),
		}

		data, err := proto.Marshal(query)

		_, err = config.SendBytes(data, 0)
		if err != nil {
			fmt.Println(err)
		}

		// XXX Read back response.
		reply, _ := config.RecvMessageBytes(0)
		log.Printf("Received reply [%s]\n", reply)

		response := &base_msg.ConfigResponse{}
		proto.Unmarshal(reply[0], response)

		log.Println(response)
	}

	log.Println("end")
}
