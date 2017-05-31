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
	"os"
	"os/signal"
	"syscall"

	"ap_common"
	"base_def"
	"base_msg"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/golang/protobuf/proto"
)

var addr = flag.String("listen-address", base_def.LOGD_PROMETHEUS_PORT,
	"The address to listen on for HTTP requests.")

func handle_ping(event []byte) {
	// XXX pings were green
	ping := &base_msg.EventPing{}
	proto.Unmarshal(event, ping)
	log.Printf("[sys.ping] %v", ping)
}

func handle_config(event []byte) {
	config := &base_msg.EventConfig{}
	proto.Unmarshal(event, config)
	log.Printf("[sys.config] %v", config)
}

func handle_entity(event []byte) {
	// XXX entities were blue
	entity := &base_msg.EventNetEntity{}
	proto.Unmarshal(event, entity)
	log.Printf("[net.entity] %v", entity)
}

func handle_resource(event []byte) {
	resource := &base_msg.EventNetResource{}
	proto.Unmarshal(event, resource)
	log.Printf("[net.resource] %v", resource)
}

func handle_request(event []byte) {
	// XXX requests were also blue
	request := &base_msg.EventNetRequest{}
	proto.Unmarshal(event, request)
	log.Printf("[net.request] %v", request)
}

func main() {
	var b ap_common.Broker

	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	log.Println("start")

	flag.Parse()

	log.Println("cli flags parsed")

	http.Handle("/metrics", promhttp.Handler())
	go http.ListenAndServe(*addr, nil)

	log.Println("prometheus client launched")

	b.Init("ap.logd")
	b.Handle(base_def.TOPIC_PING, handle_ping)
	b.Handle(base_def.TOPIC_CONFIG, handle_config)
	b.Handle(base_def.TOPIC_ENTITY, handle_entity)
	b.Handle(base_def.TOPIC_RESOURCE, handle_resource)
	b.Handle(base_def.TOPIC_REQUEST, handle_request)
	b.Connect()
	defer b.Disconnect()

	sig := make(chan os.Signal)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	s := <-sig
	log.Fatalf("Signal (%v) received, stopping\n", s)
}
