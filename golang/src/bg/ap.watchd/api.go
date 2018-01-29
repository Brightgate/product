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

package main

import (
	"log"
	"os"
	"strconv"

	"bg/ap_common/aputil"
	"bg/base_def"
	"bg/base_msg"

	"github.com/golang/protobuf/proto"
	zmq "github.com/pebbe/zmq4"
)

// shorthand aliases for constants imported from base_msg
const (
	OK  = int(base_msg.WatchdResponse_OK)
	ERR = int(base_msg.WatchdResponse_Err)
)

func apiLoop() {
	incoming, _ := zmq.NewSocket(zmq.REP)
	port := base_def.LOCAL_ZMQ_URL + base_def.WATCHD_ZMQ_REP_PORT
	if err := incoming.Bind(port); err != nil {
		log.Fatalf("failed to open incoming port %s: %v\n", port, err)
	}
	log.Printf("Listening on %s\n", port)

	me := pname + "." + strconv.Itoa(os.Getpid())

	for {
		var rc int
		var rval string

		msg, err := incoming.RecvMessageBytes(0)
		if err != nil {
			continue
		}

		req := &base_msg.WatchdRequest{}
		proto.Unmarshal(msg[0], req)
		start := aputil.ProtobufToTime(req.Start)
		end := aputil.ProtobufToTime(req.End)

		switch *req.Command {
		case "getstats":
			rc, rval = getMetrics(*req.Device, start, end)
		default:
			rc = ERR
			rval = "Invalid command: " + *req.Command
		}

		prc := base_msg.WatchdResponse_WatchdResponse(rc)
		response := &base_msg.WatchdResponse{
			Timestamp: aputil.NowToProtobuf(),
			Sender:    proto.String(me),
			Debug:     proto.String("-"),
			Status:    &prc,
			Response:  proto.String(rval),
		}

		data, err := proto.Marshal(response)
		if err != nil {
			log.Printf("Failed to marshal response: %v\n", err)
		} else {
			incoming.SendBytes(data, 0)
		}
	}
}

func apiFini(w *watcher) {
	w.running = false
}

func apiInit(w *watcher) {
	go apiLoop()
	w.running = true
}

func init() {
	addWatcher("apiloop", apiInit, apiFini)
}
