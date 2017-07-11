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
	"bufio"
	"bytes"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"ap_common"
	"ap_common/mcp"
	"base_def"
	"base_msg"

	"github.com/golang/protobuf/proto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	addr = flag.String("prom_address", base_def.SCAND_SSDP_PROMETHEUS_PORT,
		"The address to listen on for Prometheus HTTP requests.")
	broker ap_common.Broker
	config *ap_common.Config
	pname  = "ap.scand-ssdp"
)

// listen listens on conn for UDP packets, and documents them.
func listen(conn *net.UDPConn) {
	buf := make([]byte, 1024)

	for {
		blen, addr, err := conn.ReadFromUDP(buf)

		if err != nil {
			log.Fatalf("Error reading from connection: ", err)
		}

		document(addr, buf[:blen])
	}
}

// document documents the received SSDP request via the message bus.
// Drops malformed http.
func document(addr *net.UDPAddr, rs []byte) {
	req, err := http.ReadRequest(bufio.NewReader(bytes.NewReader(rs)))
	// ^^ how to get request and also keep address? conn is a io.Reader,
	//    but using http.ReadRequest on it would make it forget addr.

	if err != nil {
		// prints this a lot, as malformed http is common in these messages
		// log.Printf("Error reading received requests: %v\n", err)
		return
	}

	t := time.Now()
	msg := new(base_msg.EventSSDP)

	msg.Timestamp = &base_msg.Timestamp{
		Seconds: proto.Int64(t.Unix()),
		Nanos:   proto.Int32(int32(t.Nanosecond())),
	}
	msg.Sender = proto.String(broker.Name)
	msg.Debug = proto.String("-")

	msg.Address = proto.String(addr.String())

	// only stores first value for each header
	msg.Server = proto.String(req.Header.Get("Server"))
	req.Header.Del("Server")
	msg.UniqueServiceName = proto.String(req.Header.Get("Usn"))
	req.Header.Del("Usn")
	msg.Location = proto.String(req.Header.Get("Location"))
	req.Header.Del("Location")
	msg.SearchTarget = proto.String(req.Header.Get("St"))
	req.Header.Del("St")
	msg.SearchTarget = proto.String(req.Header.Get("Nt"))
	req.Header.Del("Nt")

	headers := map[string][]string(req.Header)
	hs := make([]*base_msg.Pair, 0)
	for k, v := range headers {
		if len(v) > 0 {
			p := &base_msg.Pair{
				Header: proto.String(k),
				Value:  proto.String(v[0]),
			}
			hs = append(hs, p)
		}
	}
	msg.ExtraHeaders = hs

	err = broker.Publish(msg, base_def.TOPIC_SCAN_SSDP)
	if err != nil {
		log.Printf("Error sending scan: %v\n", err)
	}
}

// echo echos back a recieved EventNetScan.
// Add broker.Handle(base_def.TOPIC_SCAN_SSDP, echo) in main to run.
func echo(event []byte) {
	msg := &base_msg.EventSSDP{}
	proto.Unmarshal(event, msg)
	log.Println(msg)
}

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	flag.Parse()

	mcp, err := mcp.New(pname)
	if err != nil {
		log.Println("failed to connect to mcp")
	}

	http.Handle("/metrics", promhttp.Handler())
	go http.ListenAndServe(*addr, nil)

	config = ap_common.NewConfig(pname)

	broker.Init(pname)
	broker.Connect()
	defer broker.Disconnect()
	broker.Ping()

	if mcp != nil {
		if err = mcp.SetStatus("online"); err != nil {
			log.Println("failed to set status")
		}
	}

	sig := make(chan os.Signal)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	// skipping IPv6 multicast for now
	muaddrs := "239.255.255.250"
	muaddr := net.UDPAddr{net.ParseIP(muaddrs), 1900, ""}

	// nil here is *Interface
	conn, err := net.ListenMulticastUDP("udp", nil, &muaddr)
	if err != nil {
		log.Printf("Error with multicast listening: %v\n", err)
	}
	defer conn.Close()

	go listen(conn)

	s := <-sig
	log.Fatalf("Signal (%v) received, stopping scan\n", s)
}
