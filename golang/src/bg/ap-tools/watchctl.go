/*
 * Copyright 2020 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


/*
 * Todo:
 *    Add ability to interrupt scans
 *    Add sort-by-field option for lists
 *    Allow clients to be specified by either mac or IP
 */

package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"bg/ap_common/aputil"
	"bg/ap_common/comms"
	"bg/base_def"
	"bg/base_msg"

	"github.com/golang/protobuf/proto"
)

type watchCmd struct {
	fn    func(*comms.APComm, []string) error
	usage string
}

var (
	sortKeys  []string
	validKeys = []string{"id", "ip", "mac", "type", "state", "when", "period"}

	watchCmds map[string]watchCmd
)

// Simple wrapper type, allowing us to sort a list of WatchdScanInfo structs
type scanList []*base_msg.WatchdScanInfo

func (list scanList) Len() int {
	return len(list)
}

func (list scanList) Swap(i, j int) {
	list[i], list[j] = list[j], list[i]
}

const (
	sLess = iota
	sEqual
	sGreater
)

func stringCompare(a, b string) int {
	if a < b {
		return sLess
	}
	if a > b {
		return sGreater
	}
	return sEqual
}

func (list scanList) Less(i, j int) bool {
	comp := sEqual
	for _, key := range sortKeys {
		switch key {
		case "id":
			if *list[i].Id < *list[j].Id {
				comp = sLess
			} else if *list[i].Id > *list[j].Id {
				comp = sGreater
			}
		case "ip":
			comp = stringCompare(*list[i].Ip, *list[j].Ip)
		case "mac":
			comp = stringCompare(*list[i].Mac, *list[j].Mac)
		case "type":
			typeI := typeToString(list[i].Type)
			typeJ := typeToString(list[j].Type)
			comp = stringCompare(typeI, typeJ)
		case "state":
			stateI := stateToString(list[i].State)
			stateJ := stateToString(list[j].State)
			comp = stringCompare(stateI, stateJ)
		case "when":
			whenI := aputil.ProtobufToTime(list[i].When)
			whenJ := aputil.ProtobufToTime(list[j].When)
			if whenI.Before(*whenJ) {
				comp = sLess
			} else if whenJ.Before(*whenI) {
				comp = sGreater
			}
		case "period":
			periodI := *list[i].Period
			periodJ := *list[j].Period
			if periodI < periodJ {
				comp = sLess
			} else if periodJ < periodI {
				comp = sGreater
			}
		}
		if comp != sEqual {
			break
		}
	}

	return comp == sLess
}

func typeToString(scanType *base_msg.ScanType) string {
	s := "unknown"

	if scanType != nil {
		switch *scanType {
		case base_msg.ScanType_TCP_PORTS:
			s = "tcp"
		case base_msg.ScanType_UDP_PORTS:
			s = "udp"
		case base_msg.ScanType_VULN:
			s = "vuln"
		case base_msg.ScanType_PASSWD:
			s = "passwd"
		case base_msg.ScanType_SUBNET:
			s = "subnet"
		}
	}
	return s
}

func stateToString(scanState *base_msg.WatchdScanInfo_ScanState) string {
	s := "unknown"
	if scanState != nil {
		switch *scanState {
		case base_msg.WatchdScanInfo_ACTIVE:
			s = "active"
		case base_msg.WatchdScanInfo_SCHEDULED:
			s = "scheduled"
		}
	}
	return s
}

func periodToString(period *uint32) string {
	var rval string

	if period == nil {
		rval = "-"
	} else {
		p := *period
		h := p / (60 * 60)
		p -= (h * (60 * 60))
		m := p / 60
		s := p - (m * 60)

		if h > 0 {
			rval = fmt.Sprintf("%dh%02dm", h, m)
		} else if m > 0 && s > 0 {
			rval = fmt.Sprintf("%dm%02ds", m, s)
		} else if m > 0 {
			rval = fmt.Sprintf("%dm", m)
		} else {
			rval = fmt.Sprintf("%ds", s)
		}
	}
	return rval
}

func parseListArgs(args []string) {
	fs := flag.NewFlagSet("scan list", flag.ExitOnError)

	sortKeyFlag := fs.String("k", "id", "key(s) to sort on")
	fs.Parse(args)

	sortKeys = strings.Split(*sortKeyFlag, ",")
	for _, key := range sortKeys {
		ok := false
		for _, valid := range validKeys {
			if key == valid {
				ok = true
			}
		}
		if !ok {
			fmt.Printf("Invalid key: %s\n", key)
			watchUsage()
		}
	}
}

// Ask watchd for a list of all scheduled and running scans, sort the returned
// list by whichever key(s) the user requests, and print it to stdout.
func listScans(c *comms.APComm, args []string) error {
	parseListArgs(args)

	cmd := base_msg.WatchdRequest_SCAN_LIST
	msg := base_msg.WatchdRequest{
		Timestamp: aputil.NowToProtobuf(),
		Sender:    proto.String(pname),
		Cmd:       &cmd,
	}

	rval, err := sendMsg(c, &msg)
	if err != nil {
		return err
	}

	sort.Sort(scanList(rval.Scans))
	fmt.Printf("%5s %-17s %-17s %-6s %-9s %6s %s\n",
		"ID", "IP", "mac", "type", "state", "period", "when")
	for _, scan := range rval.Scans {
		ip := "unknown"
		if scan.Ip != nil {
			ip = *scan.Ip
		}

		mac := "unknown"
		if scan.Mac != nil {
			mac = *scan.Mac
		}

		scanType := typeToString(scan.Type)
		state := stateToString(scan.State)
		period := periodToString(scan.Period)

		when := "unknown"
		if scan.When != nil {
			w := aputil.ProtobufToTime(scan.When)
			when = w.Format(time.RFC3339)
		}
		fmt.Printf("%5d %-17s %-17s %-6s %-9s %6s %s\n",
			*scan.Id, ip, mac, scanType, state, period, when)
	}

	return nil
}

// Ask watchd to schedule a new client scan.  We don't currently allow the user
// to specify a time, so "schedule" really means "run as soon as possible."
func addScan(c *comms.APComm, args []string) error {
	if len(args) < 1 || len(args) > 2 {
		watchUsage()
	}

	if ip := net.ParseIP(args[0]); ip == nil {
		return fmt.Errorf("invalid IP address: %s", args[0])
	}

	scan := base_msg.WatchdScanInfo{
		Ip: proto.String(args[0]),
	}

	if len(args) == 2 {
		var scanType base_msg.ScanType
		switch args[1] {
		case "tcp":
			scanType = base_msg.ScanType_TCP_PORTS
		case "udp":
			scanType = base_msg.ScanType_UDP_PORTS
		case "vuln":
			scanType = base_msg.ScanType_VULN
		case "passwd":
			scanType = base_msg.ScanType_PASSWD
		default:
			return fmt.Errorf("Invalid scan type: %s", args[1])
		}
		scan.Type = &scanType
	}

	cmd := base_msg.WatchdRequest_SCAN_ADD
	msg := base_msg.WatchdRequest{
		Timestamp: aputil.NowToProtobuf(),
		Sender:    proto.String(pname),
		Cmd:       &cmd,
		Scan:      &scan,
	}
	_, err := sendMsg(c, &msg)
	return err
}

// Attempt to delete a scheduled scan.  The scan is identified by its ID.
func delScan(c *comms.APComm, args []string) error {
	if len(args) != 1 {
		watchUsage()
	}

	id, err := strconv.Atoi(args[0])
	if err != nil {
		return fmt.Errorf("Invalid scan ID: %s", args[0])
	}

	scan := base_msg.WatchdScanInfo{
		Id: proto.Uint32(uint32(id)),
	}

	cmd := base_msg.WatchdRequest_SCAN_DEL
	msg := base_msg.WatchdRequest{
		Timestamp: aputil.NowToProtobuf(),
		Sender:    proto.String(pname),
		Cmd:       &cmd,
		Scan:      &scan,
	}
	_, err = sendMsg(c, &msg)
	return err
}

// Instruct watchd to reschedule a scan so that it will run ASAP.
func nowScan(c *comms.APComm, args []string) error {
	var period uint32

	if len(args) != 1 {
		watchUsage()
	}

	id, err := strconv.Atoi(args[0])
	if err != nil {
		return fmt.Errorf("invalid scan ID: %s", args[0])
	}

	scan := base_msg.WatchdScanInfo{
		Id:     proto.Uint32(uint32(id)),
		Period: &period,
	}

	cmd := base_msg.WatchdRequest_SCAN_RESCHED
	msg := base_msg.WatchdRequest{
		Timestamp: aputil.NowToProtobuf(),
		Sender:    proto.String(pname),
		Cmd:       &cmd,
		Scan:      &scan,
	}

	_, err = sendMsg(c, &msg)
	return err
}

// Send a single 0mq message to watchd.  Return the response from watchd, or an
// error
func sendMsg(c *comms.APComm, op *base_msg.WatchdRequest) (*base_msg.WatchdResponse, error) {
	data, err := proto.Marshal(op)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal command: %v", err)
	}

	reply, err := c.ReqRepl(data)
	if err != nil {
		return nil, fmt.Errorf("failed to send command: %v", err)
	}

	rval := &base_msg.WatchdResponse{}
	if len(reply) > 0 {
		if err = proto.Unmarshal(reply, rval); err != nil {
			return nil, fmt.Errorf("failed to unmarshal response: %v", err)
		}
		if rval.Errmsg != nil && len(*rval.Errmsg) > 0 {
			err = fmt.Errorf("%s", *rval.Errmsg)
		}
	}
	return rval, err
}

func watchUsage() {
	fmt.Printf("usage:\t%s\n", pname)
	for name, cmd := range watchCmds {
		fmt.Printf("\tscan %s %s\n", name, cmd.usage)
	}

	os.Exit(2)
}

func watchctl() {
	var err error

	if len(os.Args) < 3 {
		watchUsage()
	}

	cmd := os.Args[1]
	if cmd != "scan" {
		watchUsage()
	}

	findGateway()
	url := aputil.GatewayURL(base_def.WATCHD_COMM_REP_PORT)
	comm, err := comms.NewAPClient(os.Args[0], url)
	if err != nil {
		fmt.Printf("%s: unable to connect to watchd: %v\n", pname, err)
		os.Exit(1)
	}
	defer comm.Close()

	subcmd := os.Args[2]
	if wcmd, ok := watchCmds[subcmd]; ok {
		err = wcmd.fn(comm, os.Args[3:])
	} else {
		watchUsage()
	}

	if err != nil {
		fmt.Printf("%s %s failed: %v\n", cmd, subcmd, err)
		os.Exit(1)
	}
}

func init() {
	addTool("ap-watchctl", watchctl)

	watchCmds = map[string]watchCmd{
		"list": {listScans, "[-k <" + strings.Join(validKeys, "|") + ">]"},
		"add":  {addScan, "<ip> [<tcp|udp|vuln|passwd>]"},
		"del":  {delScan, "<id>"},
		"now":  {nowScan, "<id>"},
	}
}

