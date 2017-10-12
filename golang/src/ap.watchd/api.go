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

	"ap_common/aputil"
	"base_def"
	"base_msg"

	"github.com/golang/protobuf/proto"
	zmq "github.com/pebbe/zmq4"
)

const (
	API_OK  = int(base_msg.WatchdResponse_OK)
	API_ERR = int(base_msg.WatchdResponse_Err)
)

func apiLoop() {
	incoming, _ := zmq.NewSocket(zmq.REP)
	incoming.Bind(base_def.WATCHD_ZMQ_REP_URL)
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
			rc, rval = GetMetrics(*req.Device, start, end)
		default:
			rc = API_ERR
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

func apiInit() error {
	go apiLoop()
	return nil
}

func init() {
	addWatcher("apiloop", apiInit, nil)
}
