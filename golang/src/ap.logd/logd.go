/*
 * COPYRIGHT 2017 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 *
 */

/*
 * message logger
 */

// XXX Exception messages are not displayed.

package main

import (
	"flag"
	"log"
	"net/http"
	"strconv"

	"base_def"
	"base_msg"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/golang/protobuf/proto"

	// Ubuntu: requires libzmq3-dev, which is 0MQ 4.2.1.
	zmq "github.com/pebbe/zmq4"
)

var addr = flag.String("listen-address",
	":"+strconv.Itoa(base_def.LOGD_PROMETHEUS_PORT),
	"The address to listen on for HTTP requests.")

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	log.Println("start")

	flag.Parse()

	log.Println("cli flags parsed")

	http.Handle("/metrics", promhttp.Handler())
	go http.ListenAndServe(*addr, nil)

	log.Println("prometheus client launched")

	//  First, connect our subscriber socket
	subscriber, _ := zmq.NewSocket(zmq.SUB)
	defer subscriber.Close()
	subscriber.Connect("tcp://localhost:" + strconv.Itoa(base_def.BROKER_ZMQ_SUB_PORT))
	subscriber.SetSubscribe("")

	for {
		log.Println("receive message bytes")

		msg, err := subscriber.RecvMessageBytes(0)
		if err != nil {
			log.Println(err)
			break
		}

		topic := string(msg[0])

		switch topic {
		case base_def.TOPIC_PING:
			// XXX pings were green
			ping := &base_msg.EventPing{}
			proto.Unmarshal(msg[1], ping)
			log.Printf("[sys.ping] ")
			log.Println(ping)

		case base_def.TOPIC_CONFIG:
			config := &base_msg.EventConfig{}
			proto.Unmarshal(msg[1], config)
			log.Printf("[sys.config] ")
			log.Println(config)

		case base_def.TOPIC_ENTITY:
			// XXX entities were blue
			entity := &base_msg.EventNetEntity{}
			proto.Unmarshal(msg[1], entity)
			log.Printf("[net.entity] ")
			log.Println(entity)

		case base_def.TOPIC_RESOURCE:
			resource := &base_msg.EventNetResource{}
			proto.Unmarshal(msg[1], resource)
			log.Printf("[net.resource] ")
			log.Println(resource)

		case base_def.TOPIC_REQUEST:
			// XXX requests were also blue
			request := &base_msg.EventNetRequest{}
			proto.Unmarshal(msg[1], request)
			log.Printf("[net.request] ")
			log.Println(request)

		default:
			log.Println("unknown topic " + topic + "; ignoring message")
		}
	}
}
