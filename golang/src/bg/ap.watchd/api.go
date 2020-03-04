/*
 * COPYRIGHT 2020 Brightgate Inc.  All rights reserved.
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
	"time"

	"bg/ap_common/aputil"
	"bg/ap_common/comms"
	"bg/base_def"
	"bg/base_msg"

	"github.com/golang/protobuf/proto"
)

func reschedScan(req *base_msg.WatchdScanInfo) *base_msg.WatchdResponse {
	resp := &base_msg.WatchdResponse{}

	if req.Id == nil {
		resp.Errmsg = proto.String("missing scanID")
	} else if req.Period == nil {
		resp.Errmsg = proto.String("missing scan time")
	} else {
		period := time.Duration(req.GetPeriod()) * time.Second

		if err := rescheduleScan(*req.Id, &period); err != nil {
			resp.Errmsg = proto.String(fmt.Sprintf("%v", err))
		}
	}

	return resp
}

func delScan(req *base_msg.WatchdScanInfo) *base_msg.WatchdResponse {
	resp := &base_msg.WatchdResponse{}

	if req.Id == nil {
		resp.Errmsg = proto.String("missing scanID")

	} else if err := rescheduleScan(*req.Id, nil); err != nil {
		resp.Errmsg = proto.String(fmt.Sprintf("%v", err))
	}

	return resp
}

func addScan(req *base_msg.WatchdScanInfo) *base_msg.WatchdResponse {
	var scan *ScanRequest
	var ip, mac, ring string

	if req.Ip != nil {
		ip = *req.Ip
		mac = getMacFromIP(ip)
	}

	resp := &base_msg.WatchdResponse{}

	if req.Ip == nil {
		resp.Errmsg = proto.String("missing IP")
	} else if net.ParseIP(ip) == nil {
		resp.Errmsg = proto.String("invalid IP")
	} else if req.Type == nil {
		resp.Errmsg = proto.String("missing scan type")
	} else if ring = ipToRing(ip); ring == "" {
		resp.Errmsg = proto.String("not a managed subnet")
	} else {
		switch *req.Type {
		case base_msg.ScanType_TCP_PORTS:
			scan = newTCPScan(mac, ip, ring)
		case base_msg.ScanType_UDP_PORTS:
			scan = newUDPScan(mac, ip, ring)
		case base_msg.ScanType_VULN:
			scan = newVulnScan(mac, ip, ring)
		case base_msg.ScanType_PASSWD:
			scan = newPasswdScan(mac, ip, ring)
		case base_msg.ScanType_SUBNET:
			scan = newSubnetScan(ring, ip)
		default:
			resp.Errmsg = proto.String("illegal scan type")
		}
	}
	if scan != nil {
		scheduleScan(scan, 0, true)
	}

	return resp
}

// Given a ScanRequest structure, construct an equivalent WatchdScanInfo
// structure which can be returned to a 0mq client.
func convertScan(in *ScanRequest, active bool) *base_msg.WatchdScanInfo {
	var scanType base_msg.ScanType
	var state base_msg.WatchdScanInfo_ScanState

	if active {
		state = base_msg.WatchdScanInfo_ACTIVE
	} else {
		state = base_msg.WatchdScanInfo_SCHEDULED
	}

	switch in.ScanType {
	case "tcp":
		scanType = base_msg.ScanType_TCP_PORTS
	case "udp":
		scanType = base_msg.ScanType_UDP_PORTS
	case "vuln":
		scanType = base_msg.ScanType_VULN
	case "passwd":
		scanType = base_msg.ScanType_PASSWD
	case "subnet":
		scanType = base_msg.ScanType_SUBNET
	}
	period := uint32(in.Period.Seconds())
	out := base_msg.WatchdScanInfo{
		Id:     proto.Uint32(uint32(in.ID)),
		Ip:     proto.String(in.IP),
		Mac:    proto.String(in.Mac),
		Type:   &scanType,
		State:  &state,
		When:   aputil.TimeToProtobuf(&in.When),
		Period: &period,
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

func apiHandle(msg []byte) []byte {
	var resp *base_msg.WatchdResponse

	req := &base_msg.WatchdRequest{}
	err := proto.Unmarshal(msg, req)

	if req.Cmd == nil {
		msg := "failed to unmarshal command"
		if err != nil {
			msg += fmt.Sprintf(": %v", err)
		}

		resp = &base_msg.WatchdResponse{
			Errmsg: proto.String(msg),
		}
	} else {
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
	}

	resp.Timestamp = aputil.NowToProtobuf()
	data, err := proto.Marshal(resp)
	if err != nil {
		slog.Warnf("Failed to marshal response to %v: %v",
			*req, err)
	}

	return data
}

func apiInit() error {
	url := base_def.INCOMING_COMM_URL + base_def.WATCHD_COMM_REP_PORT

	server, err := comms.NewAPServer(pname, url)
	if err != nil {
		slog.Warnf("creating API endpoint: %v", err)
	} else {
		go server.Serve(apiHandle)
	}

	return err
}
