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

	"ap_common/apcfg"
	"ap_common/broker"
	"ap_common/mcp"
	"ap_common/network"
	"base_def"
	"base_msg"

	"github.com/golang/protobuf/proto"
	"github.com/miekg/dns"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	promAddr = flag.String("prom_address", base_def.LISTEND_PROMETHEUS_PORT,
		"The address to listen on for Prometheus HTTP requests.")
	brokerd broker.Broker
	config  *apcfg.APConfig
	pname   = "ap.listend"

	ssdpAddrs = []*net.UDPAddr{
		&net.UDPAddr{IP: network.IpSSDPv4, Port: 1900},
		&net.UDPAddr{IP: network.IpSSDPv6Link, Port: 1900},
		&net.UDPAddr{IP: network.IpSSDPv6Site, Port: 1900},
		&net.UDPAddr{IP: network.IpSSDPv6Org, Port: 1900},
		&net.UDPAddr{IP: network.IpSSDPv6Global, Port: 1900},
	}

	mdnsAddrs = []*net.UDPAddr{
		&net.UDPAddr{IP: network.IpmDNSv4, Port: 5353},
		&net.UDPAddr{IP: network.IpmDNSv6, Port: 5353},
	}
)

type publishFunc func(addr net.IP, buf []byte)

func listener(conn *net.UDPConn, pub publishFunc) {
	buf := make([]byte, 2048)
	for {
		blen, addr, err := conn.ReadFrom(buf)
		if err != nil {
			log.Fatalln("Error reading from connection:", err)
		}

		pub(addr.(*net.UDPAddr).IP, buf[:blen])
	}
}

// publishSSDP documents the received SSDP request via the message bus.
// Drops malformed http.
func publishSSDP(addr net.IP, buf []byte) {
	req, err := http.ReadRequest(bufio.NewReader(bytes.NewReader(buf)))
	if err != nil {
		return
	}

	msg := &base_msg.EventSSDP{
		Address: proto.String(addr.String()),
	}

	var mtype base_msg.EventSSDP_MessageType
	if nts := req.Header.Get("Nts"); nts != "" {
		if nts == "ssdp:alive" {
			mtype = base_msg.EventSSDP_ALIVE
		} else if nts == "ssdp:byebye" {
			mtype = base_msg.EventSSDP_BYEBYE
		}
	} else if req.Header.Get("Man") == "\"ssdp:discover\"" {
		mtype = base_msg.EventSSDP_DISCOVER
	}
	msg.Type = &mtype

	// only stores first value for each header
	msg.Server = proto.String(req.Header.Get("Server"))
	req.Header.Del("Server")
	msg.UniqueServiceName = proto.String(req.Header.Get("Usn"))
	req.Header.Del("Usn")
	msg.Location = proto.String(req.Header.Get("Location"))
	req.Header.Del("Location")
	msg.SearchTarget = proto.String(req.Header.Get("St"))
	req.Header.Del("St")
	msg.NotificationType = proto.String(req.Header.Get("Nt"))
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

	t := time.Now()
	listenType := base_msg.EventListen_SSDP
	listen := &base_msg.EventListen{
		Timestamp: &base_msg.Timestamp{
			Seconds: proto.Int64(t.Unix()),
			Nanos:   proto.Int32(int32(t.Nanosecond())),
		},
		Sender: proto.String(brokerd.Name),
		Debug:  proto.String("-"),
		Type:   &listenType,
		Ssdp:   msg,
	}

	err = brokerd.Publish(listen, base_def.TOPIC_LISTEN)
	if err != nil {
		log.Printf("Error sending SSDP listen event: %v\n", err)
	}
}

func publishmDNS(addr net.IP, buf []byte) {
	// mDNS message format is nearly identical to that of DNS, except in
	// both the Question and Answer (Resource Record) sections one bit is
	// taken from the Class and repurposed for mDNS. Because mDNS and DNS
	// messages are the same size, we unpack into dns.Msg.
	var msg dns.Msg
	if err := msg.Unpack(buf); err != nil {
		log.Fatalln("Error unpacking mDNS message:", err)
	}

	requests := make([]string, 0)
	responses := make([]string, 0)

	for _, question := range msg.Question {
		requests = append(requests, question.String())
	}

	for _, answer := range msg.Answer {
		responses = append(responses, answer.String())
	}

	event := &base_msg.EventmDNS{
		Address:  proto.String(addr.String()),
		Request:  requests,
		Response: responses,
	}

	t := time.Now()
	listenType := base_msg.EventListen_mDNS
	listen := &base_msg.EventListen{
		Timestamp: &base_msg.Timestamp{
			Seconds: proto.Int64(t.Unix()),
			Nanos:   proto.Int32(int32(t.Nanosecond())),
		},
		Sender: proto.String(brokerd.Name),
		Debug:  proto.String("-"),
		Type:   &listenType,
		Mdns:   event,
	}

	err := brokerd.Publish(listen, base_def.TOPIC_LISTEN)
	if err != nil {
		log.Printf("Error sending mDNS listen event: %v\n", err)
	}
}

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	flag.Parse()

	mcpd, err := mcp.New(pname)
	if err != nil {
		log.Println("failed to connect to mcp")
	}

	http.Handle("/metrics", promhttp.Handler())
	go http.ListenAndServe(*promAddr, nil)

	config = apcfg.NewConfig(pname)

	ifName, err := config.GetProp("@/network/wifi_nic")
	if err != nil {
		log.Fatalln("No wifi interface defined.")
	}

	iface, err := net.InterfaceByName(ifName)
	if err != nil {
		log.Fatalf("Failed to get interface %s: %s\n", ifName, err)
	}

	brokerd.Init(pname)
	brokerd.Connect()
	defer brokerd.Disconnect()
	brokerd.Ping()

	if mcpd != nil {
		if err = mcpd.SetState(mcp.ONLINE); err != nil {
			log.Println("failed to set status")
		}
	}

	if err := network.WaitForDevice(ifName, 30*time.Second); err != nil {
		log.Fatalf("%s is offline\n", ifName)
	}

	// SSDP listeners
	for _, a := range ssdpAddrs {
		c, err := net.ListenMulticastUDP("udp", iface, a)
		if err != nil {
			log.Fatalf("SSDP listen on %s failed: %s\n", a.String(), err)
		}
		defer c.Close()
		go listener(c, publishSSDP)
	}

	// mDNS listeners
	for _, a := range mdnsAddrs {
		c, err := net.ListenMulticastUDP("udp", iface, a)
		if err != nil {
			log.Fatalf("mDNS listen on %s failed: %s\n", a, err)
		}
		defer c.Close()
		go listener(c, publishmDNS)
	}

	sig := make(chan os.Signal)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	s := <-sig
	log.Fatalf("Signal (%v) received, stopping scan\n", s)
}
