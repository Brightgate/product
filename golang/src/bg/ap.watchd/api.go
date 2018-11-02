/*
 * COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package main

import (
	"fmt"
	"net"

	"bg/ap_common/aputil"
	"bg/base_def"
	"bg/base_msg"

	"github.com/golang/protobuf/proto"
	zmq "github.com/pebbe/zmq4"
)

func reschedScan(req *base_msg.WatchdScanInfo) *base_msg.WatchdResponse {
	resp := &base_msg.WatchdResponse{}

	if req.Id == nil {
		resp.Errmsg = proto.String("missing scanID")
	} else if req.When == nil {
		resp.Errmsg = proto.String("missing scan time")
	} else {
		id := int(*req.Id)
		when := aputil.ProtobufToTime(req.When)

		if err := rescheduleScan(id, when); err != nil {
			resp.Errmsg = proto.String(fmt.Sprintf("%v", err))
		}
	}

	return resp
}

func delScan(req *base_msg.WatchdScanInfo) *base_msg.WatchdResponse {
	resp := &base_msg.WatchdResponse{}

	if req.Id == nil {
		resp.Errmsg = proto.String("missing scanID")
	} else if err := cancelScan(int(*req.Id)); err != nil {
		resp.Errmsg = proto.String(fmt.Sprintf("%v", err))
	}

	return resp
}
func addScan(req *base_msg.WatchdScanInfo) *base_msg.WatchdResponse {
	var scan *ScanRequest

	resp := &base_msg.WatchdResponse{}

	if req.Ip == nil {
		resp.Errmsg = proto.String("missing IP")
	} else if ip := net.ParseIP(*req.Ip); ip == nil {
		resp.Errmsg = proto.String("invalid IP")
	} else if req.Type == nil {
		resp.Errmsg = proto.String("missing scan type")
	} else {
		switch *req.Type {
		case base_msg.WatchdScanInfo_TCP_PORTS:
			scan = newTCPScan("", *req.Ip)
		case base_msg.WatchdScanInfo_UDP_PORTS:
			scan = newUDPScan("", *req.Ip)
		case base_msg.WatchdScanInfo_VULN:
			scan = newVulnScan("", *req.Ip)
		default:
			resp.Errmsg = proto.String("illegal scan type")
		}
	}
	if scan != nil {
		scan.Period = 0
		scheduleScan(scan, 0, true)
	}

	return resp
}

// Given a ScanRequest structure, construct an equivalent WatchScanInfo
// structure which can be returned to a 0mq client.
func convertScan(in *ScanRequest, active bool) *base_msg.WatchdScanInfo {
	var scanType base_msg.WatchdScanInfo_ScanType
	var state base_msg.WatchdScanInfo_ScanState

	if active {
		state = base_msg.WatchdScanInfo_ACTIVE
	} else {
		state = base_msg.WatchdScanInfo_SCHEDULED
	}

	switch in.ScanType {
	case "tcp_ports":
		scanType = base_msg.WatchdScanInfo_TCP_PORTS
	case "udp_ports":
		scanType = base_msg.WatchdScanInfo_UDP_PORTS
	case "vulnerability":
		scanType = base_msg.WatchdScanInfo_VULN
	}
	out := base_msg.WatchdScanInfo{
		Id:    proto.Int32(int32(in.ID)),
		Ip:    proto.String(in.IP),
		Mac:   proto.String(in.Mac),
		Type:  &scanType,
		State: &state,
		When:  aputil.TimeToProtobuf(&in.When),
	}
	return &out
}

func listScans() *base_msg.WatchdResponse {
	scans := make([]*base_msg.WatchdScanInfo, 0)

	scheduled, active := scanGetLists()
	for _, scan := range scheduled {
		scans = append(scans, convertScan(&scan, false))
	}
	for _, scan := range active {
		scans = append(scans, convertScan(&scan, true))
	}

	return &base_msg.WatchdResponse{Scans: scans}
}

func apiLoop(incoming *zmq.Socket) {
	errs := 0
	for {
		var resp *base_msg.WatchdResponse

		msg, err := incoming.RecvMessageBytes(0)
		if err != nil {
			slog.Warnf("Error receiving message: %v", err)
			errs++
			if errs > 10 {
				slog.Fatalf("Too many errors - giving up")
			}
			continue
		}

		errs = 0
		req := &base_msg.WatchdRequest{}
		proto.Unmarshal(msg[0], req)

		switch *req.Cmd {
		case base_msg.WatchdRequest_SCAN_LIST:
			resp = listScans()
		case base_msg.WatchdRequest_SCAN_ADD:
			resp = addScan(req.Scan)
		case base_msg.WatchdRequest_SCAN_DEL:
			resp = delScan(req.Scan)
		case base_msg.WatchdRequest_SCAN_RESCHED:
			resp = reschedScan(req.Scan)
		default:
			resp = &base_msg.WatchdResponse{
				Errmsg: proto.String("unknown command"),
			}
		}

		resp.Timestamp = aputil.NowToProtobuf()
		data, err := proto.Marshal(resp)
		if err != nil {
			slog.Warnf("Failed to marshal response to %v: %v",
				*req, err)
		} else {
			incoming.SendBytes(data, 0)
		}
	}
}

func apiInit() error {
	var err error

	incoming, _ := zmq.NewSocket(zmq.REP)
	port := base_def.INCOMING_ZMQ_URL + base_def.WATCHD_ZMQ_REP_PORT
	if err = incoming.Bind(port); err != nil {
		slog.Warnf("Failed to bind incoming port %s: %v", port, err)
	} else {
		slog.Infof("Listening on %s", port)
		go apiLoop(incoming)
	}

	return err
}
