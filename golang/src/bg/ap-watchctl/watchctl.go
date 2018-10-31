/*
 * COPYRIGHT 2018 Brightgate Inc. All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

/*
 * Todo:
 *    Add ability to interrupt scans
 *    Add sort-by-field option for lists
 *    Allow clients to be specified by either mac or IP
 */

package main

import (
	"fmt"
	"net"
	"os"
	"sort"
	"strconv"
	"time"

	"bg/ap_common/aputil"
	"bg/base_def"
	"bg/base_msg"

	"github.com/golang/protobuf/proto"

	// Ubuntu: requires libzmq3-dev, which is 0MQ 4.2.1.
	zmq "github.com/pebbe/zmq4"
)

const (
	pname = "ap-watchctl"
)

// Simple wrapper type, allowing us to sort a list of WatchdScanInfo structs
type scanList []*base_msg.WatchdScanInfo

func (list scanList) Len() int {
	return len(list)
}

func (list scanList) Swap(i, j int) {
	list[i], list[j] = list[j], list[i]
}

func (list scanList) Less(i, j int) bool {
	return *list[i].Id < *list[j].Id
}

// Ask watchd for a list of all scheduled and running scans, sort the returned
// list by ID, and print it to stdout.
func listScans(s *zmq.Socket) error {
	cmd := base_msg.WatchdRequest_SCAN_LIST

	msg := base_msg.WatchdRequest{
		Timestamp: aputil.NowToProtobuf(),
		Sender:    proto.String(pname),
		Cmd:       &cmd,
	}

	rval, err := sendMsg(s, &msg)
	if err != nil {
		return err
	}

	sort.Sort(scanList(rval.Scans))
	fmt.Printf("%5s %-15s %-17s %-4s %-9s %s\n",
		"ID", "IP", "mac", "type", "state", "when")
	for _, scan := range rval.Scans {
		ip := "unknown"
		if scan.Ip != nil {
			ip = *scan.Ip
		}

		mac := "unknown"
		if scan.Mac != nil {
			mac = *scan.Mac
		}

		scanType := "unknown"
		if scan.Type != nil {
			switch *scan.Type {
			case base_msg.WatchdScanInfo_TCP_PORTS:
				scanType = "tcp"
			case base_msg.WatchdScanInfo_UDP_PORTS:
				scanType = "udp"
			case base_msg.WatchdScanInfo_VULN:
				scanType = "vuln"
			}
		}

		state := "unknown"
		if scan.State != nil {
			switch *scan.State {
			case base_msg.WatchdScanInfo_ACTIVE:
				state = "active"
			case base_msg.WatchdScanInfo_SCHEDULED:
				state = "scheduled"
			}
		}

		when := "unknown"
		if scan.When != nil {
			w := aputil.ProtobufToTime(scan.When)
			when = w.Format(time.RFC3339)
		}
		fmt.Printf("%5d %-15s %-17s %-4s %-9s %s\n",
			*scan.Id, ip, mac, scanType, state, when)
	}

	return nil
}

// Ask watchd to schedule a new client scan.  We don't currently allow the user
// to specify a time, so "schedule" really means "run as soon as possible."
func addScan(s *zmq.Socket, args []string) error {
	if len(args) < 1 || len(args) > 2 {
		usage()
	}

	if ip := net.ParseIP(args[0]); ip == nil {
		return fmt.Errorf("invalid IP address: %s", args[0])
	}

	scan := base_msg.WatchdScanInfo{
		Ip: proto.String(args[0]),
	}

	if len(args) == 2 {
		var scanType base_msg.WatchdScanInfo_ScanType
		switch args[1] {
		case "tcp":
			scanType = base_msg.WatchdScanInfo_TCP_PORTS
		case "udp":
			scanType = base_msg.WatchdScanInfo_UDP_PORTS
		case "vuln":
			scanType = base_msg.WatchdScanInfo_VULN
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
	_, err := sendMsg(s, &msg)
	return err
}

// Attempt to delete a scheduled scan.  The scan is identified by its ID.
func delScan(s *zmq.Socket, args []string) error {
	if len(args) != 1 {
		usage()
	}

	id, err := strconv.Atoi(args[0])
	if err != nil {
		return fmt.Errorf("Invalid scan ID: %s", args[0])
	}

	scan := base_msg.WatchdScanInfo{
		Id: proto.Int32(int32(id)),
	}

	cmd := base_msg.WatchdRequest_SCAN_DEL
	msg := base_msg.WatchdRequest{
		Timestamp: aputil.NowToProtobuf(),
		Sender:    proto.String(pname),
		Cmd:       &cmd,
		Scan:      &scan,
	}
	_, err = sendMsg(s, &msg)
	return err
}

// Instruct watchd to reschedule a scan so that it will run ASAP.
func nowScan(s *zmq.Socket, args []string) error {
	if len(args) != 1 {
		usage()
	}

	id, err := strconv.Atoi(args[0])
	if err != nil {
		return fmt.Errorf("invalid scan ID: %s", args[0])
	}

	scan := base_msg.WatchdScanInfo{
		Id:   proto.Int32(int32(id)),
		When: aputil.NowToProtobuf(),
	}

	cmd := base_msg.WatchdRequest_SCAN_RESCHED
	msg := base_msg.WatchdRequest{
		Timestamp: aputil.NowToProtobuf(),
		Sender:    proto.String(pname),
		Cmd:       &cmd,
		Scan:      &scan,
	}

	_, err = sendMsg(s, &msg)
	return err
}

// Send a single 0mq message to watchd.  Return the response from watchd, or an
// error
func sendMsg(s *zmq.Socket, op *base_msg.WatchdRequest) (*base_msg.WatchdResponse, error) {
	data, err := proto.Marshal(op)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal command: %v", err)
	}

	if _, err = s.SendBytes(data, 0); err != nil {
		return nil, fmt.Errorf("failed to send command: %v", err)
	}

	var reply [][]byte
	reply, err = s.RecvMessageBytes(0)
	if err != nil {
		return nil, fmt.Errorf("failed to receive response: %v", err)
	}

	rval := &base_msg.WatchdResponse{}
	if len(reply) > 0 {
		if err = proto.Unmarshal(reply[0], rval); err != nil {
			return nil, fmt.Errorf("failed to unmarshal response: %v", err)
		}
		if rval.Errmsg != nil && len(*rval.Errmsg) > 0 {
			err = fmt.Errorf("%s", *rval.Errmsg)
		}
	}
	return rval, err
}

// Establish a 0mq connection to watchd
func connect() (*zmq.Socket, error) {
	port := base_def.LOCAL_ZMQ_URL + base_def.WATCHD_ZMQ_REP_PORT
	socket, err := zmq.NewSocket(zmq.REQ)
	if err != nil {
		err = fmt.Errorf("failed to create new watchd socket: %v", err)
	} else if err = socket.Connect(port); err != nil {
		err = fmt.Errorf("failed to connect socket %s: %v", port, err)
		socket = nil
	}

	return socket, err
}

func usage() {
	fmt.Printf("usage:\t%s\n", pname)
	fmt.Printf("\tscan list\n")
	fmt.Printf("\tscan add <ip> [<tcp|udp|vuln>]\n")
	fmt.Printf("\tscan now <id>\n")
	fmt.Printf("\tscan del <id>\n")
	os.Exit(2)
}

func main() {
	var err error

	if len(os.Args) < 3 {
		usage()
	}

	cmd := os.Args[1]
	if cmd != "scan" {
		usage()
	}

	socket, err := connect()
	if err != nil {
		fmt.Printf("%s: unable to connect to watchd: %v\n", pname, err)
		os.Exit(1)
	}
	defer socket.Close()

	subcmd := os.Args[2]
	switch subcmd {
	case "list":
		err = listScans(socket)
	case "add":
		err = addScan(socket, os.Args[3:])
	case "del":
		err = delScan(socket, os.Args[3:])
	case "now":
		err = nowScan(socket, os.Args[3:])
	default:
		usage()
	}

	if err != nil {
		fmt.Printf("%s %s failed: %v\n", cmd, subcmd, err)
		os.Exit(1)
	}
}
