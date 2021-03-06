/*
 * Copyright 2020 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"bg/ap_common/apcfg"
	"bg/ap_common/aputil"
	"bg/ap_common/bgmetrics"
	"bg/base_def"
	"bg/base_msg"
	"bg/common/cfgapi"
	"bg/common/network"

	"github.com/golang/protobuf/proto"
	"github.com/miekg/dns"
	"golang.org/x/net/ipv4"
)

// Version 2 of the uPNP spec limits the MX field of an SSDP search request to 5
// seconds, but older clients may use up to 120.  This value represents the
// length of time we need to hold open an SSDP reply port, which is a limited
// resource.  To avoid runnning out of ports, we modify request packets with a
// larger value.
const mxMax = 5

type endpoint struct {
	conn  *ipv4.PacketConn
	iface *net.Interface
	ip    net.IP
	port  int
	ring  string
}

type service struct {
	name    string
	address net.IP
	port    int
	init    func()
	handler func(*endpoint, []byte, int) ([]byte, int, error)
}

var multicastServices = []service{
	{"mDNS", net.IPv4(224, 0, 0, 251), 5353, nil, mDNSHandler},
	{"SSDP", net.IPv4(239, 255, 255, 250), 1900, ssdpInit, ssdpHandler},
}

type relayer struct {
	service service
	conn    *ipv4.PacketConn
	done    bool
	exited  chan struct{}
}

var ringLevel = map[string]int{
	base_def.RING_CORE:     0,
	base_def.RING_STANDARD: 1,
	base_def.RING_DEVICES:  2,
	base_def.RING_GUEST:    3,
}

var (
	ssdpBase = apcfg.Int("ssdp_base", 31000, false, nil)
	ssdpMax  = apcfg.Int("ssdp_max", 20, false, nil)

	rings cfgapi.RingMap

	ssdpInited     bool
	ssdpSearches   *ssdpSearchState
	ssdpSearchLock sync.Mutex

	relayers   []*relayer
	relayerMtx sync.Mutex

	relayMetric struct {
		mdnsRequests  *bgmetrics.Counter
		mdnsReplies   *bgmetrics.Counter
		ssdpSearches  *bgmetrics.Counter
		ssdpTimeouts  *bgmetrics.Counter
		ssdpNotifies  *bgmetrics.Counter
		ssdpResponses *bgmetrics.Counter
	}
)

type ssdpSearchState struct {
	buf       []byte
	port      int
	addr      *net.UDPAddr
	requestor *ipv4.PacketConn
	listener  *ipv4.PacketConn
	next      *ssdpSearchState
}

func (r *relayer) initListener() error {
	s := r.service
	if s.init != nil {
		s.init()
	}

	portStr := ":" + strconv.Itoa(s.port)
	c, err := net.ListenPacket("udp4", portStr)
	if err != nil {
		return fmt.Errorf("failed to listen on port %d: %v",
			s.port, err)
	}

	p := ipv4.NewPacketConn(c)
	if p == nil {
		return fmt.Errorf("couldn't create PacketConn")
	}

	if err = p.SetControlMessage(ipv4.FlagSrc, true); err != nil {
		return fmt.Errorf("couldn't set ControlMessage: %v", err)
	}

	udpaddr := &net.UDPAddr{IP: s.address}
	for _, iface := range ringToIface {
		if err = p.JoinGroup(iface, udpaddr); err != nil {
			return fmt.Errorf("failed to join multicast group: %v",
				err)
		}
	}
	r.conn = p

	return nil
}

func mDNSEvent(addr net.IP, requests, responses []string) {
	event := &base_msg.EventmDNS{
		Request:  requests,
		Response: responses,
	}

	listenType := base_msg.EventListen_mDNS
	listen := &base_msg.EventListen{
		Timestamp:   aputil.NowToProtobuf(),
		Sender:      proto.String(brokerd.Name),
		Debug:       proto.String("-"),
		Ipv4Address: proto.Uint32(network.IPAddrToUint32(addr)),
		Type:        &listenType,
		Mdns:        event,
	}

	if err := brokerd.Publish(listen, base_def.TOPIC_LISTEN); err != nil {
		slog.Warnf("Error sending mDNS listen event: %v", err)
	}
}

func mDNSHandler(source *endpoint, b []byte, n int) ([]byte, int, error) {
	msg := new(dns.Msg)
	if err := msg.Unpack(b); err != nil {
		return nil, 0, fmt.Errorf("malformed mDNS packet from %v: %v",
			source.ip, err)
	}

	requests := make([]string, 0)
	responses := make([]string, 0)

	if len(msg.Question) > 0 {
		relayMetric.mdnsRequests.Inc()
		slog.Debugf("mDNS request from %v", source.ip)
		for _, question := range msg.Question {
			slog.Debugf("   %s", question.String())
			requests = append(requests, question.String())
		}
	}

	if len(msg.Answer) > 0 {
		relayMetric.mdnsReplies.Inc()
		slog.Debugf("mDNS reply from %v", source.ip)
		for _, answer := range msg.Answer {
			slog.Debugf("   %s", answer.String())
			responses = append(responses, answer.String())
		}
	}

	mDNSEvent(source.ip, requests, responses)

	return b, n, nil
}

func ssdpEvent(addr net.IP, mtype base_msg.EventSSDP_MessageType,
	req *http.Request) {

	msg := &base_msg.EventSSDP{}
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

	listenType := base_msg.EventListen_SSDP
	listen := &base_msg.EventListen{
		Timestamp:   aputil.NowToProtobuf(),
		Sender:      proto.String(brokerd.Name),
		Debug:       proto.String("-"),
		Ipv4Address: proto.Uint32(network.IPAddrToUint32(addr)),
		Type:        &listenType,
		Ssdp:        msg,
	}

	if err := brokerd.Publish(listen, base_def.TOPIC_LISTEN); err != nil {
		slog.Warnf("Error sending SSDP listen event: %v", err)
	}
}

func ssdpSearchAlloc(source *endpoint, mx int) (*ssdpSearchState, error) {
	ssdpSearchLock.Lock()
	defer ssdpSearchLock.Unlock()

	sss := ssdpSearches
	if sss == nil {
		return nil, fmt.Errorf("too many outstanding M-SEARCH requests")
	}

	// MX is the maximum time the device should wait before responding.  We
	// will leave our port open for 2x that long.
	deadline := time.Now().Add(time.Duration(mx*2) * time.Second)
	if err := sss.listener.SetDeadline(deadline); err != nil {
		return nil, fmt.Errorf("unable to set UDP deadline: %v", err)
	}

	ssdpSearches = sss.next
	sss.requestor = source.conn
	sss.addr = &net.UDPAddr{IP: source.ip, Port: source.port}

	return sss, nil
}

func ssdpSearchFree(sss *ssdpSearchState) {
	ssdpSearchLock.Lock()
	defer ssdpSearchLock.Unlock()

	sss.requestor = nil
	sss.addr = nil
	sss.next = ssdpSearches
	ssdpSearches = sss
}

// Currently we just check an SSDP packet to be sure that it's a correctly
// structured HTTP response.  We don't examine its contents, but an OK may
// contain information that would be useful to identifierd.
func ssdpResponseCheck(rdr io.Reader) error {
	resp, err := http.ReadResponse(bufio.NewReader(rdr), nil)
	if err != nil {
		return fmt.Errorf("malformed HTTP: %v", err)
	}

	// As per http.Client.Do: body must be read and closed. This is about
	// lint-cleanliness, since the underlying packet is UDP, and already
	// completely read in.
	_, _ = io.Copy(ioutil.Discard, resp.Body)
	resp.Body.Close()
	return nil
}

func ssdpResponseRelay(sss *ssdpSearchState) {
	defer ssdpSearchFree(sss)

	buf := sss.buf
	addr := sss.addr

	for {
		n, _, src, err := sss.listener.ReadFrom(buf)
		if err != nil {
			// This port has a deadline set, so we expect to hit a
			// timeout.  Any other error is worth noting.
			e, _ := err.(net.Error)
			if !e.Timeout() {
				slog.Warnf("Failed to read from %v: %v",
					sss.listener.LocalAddr(), err)
			}
			relayMetric.ssdpTimeouts.Inc()
			return
		}
		if err = ssdpResponseCheck(bytes.NewReader(buf)); err != nil {
			slog.Warnf("Bad SSDP response from %v: %v", src, err)
			return
		}

		slog.Debugf("Forwarding SSDP response from/to %v/%v", src, addr)
		relayMetric.ssdpResponses.Inc()
		l, err := sss.requestor.WriteTo(buf[:n], nil, addr)
		if err != nil {
			slog.Warnf("    Forward to %v failed: %v", addr, err)
			return
		} else if l != n {
			slog.Warnf("    Forwarded %d of %d to %v", l, n, addr)
			return
		}
	}
}

// The response to an SSDP M-SEARCH request is a unicast packet back to the
// originating port.  We create a new local UDP port from which to forward the
// SEARCH packet, and on which we will listen for responses.  We also make a
// static copy of the originating endpoint structure, so we know where the
// response packet needs to be forwarded.
func ssdpSearchHandler(source *endpoint, mx int) error {
	sss, err := ssdpSearchAlloc(source, mx)
	if err != nil {
		return err
	}
	slog.Debugf("Forwarding SSDP M-SEARCH from %v", sss.addr)

	// Replace the original PacketConn in the source structure with our new
	// PacketConn, causing the SEARCH request to be forwarded from our newly
	// opened UDP port instead of the standard SSDP port (1900).
	source.conn = sss.listener

	go ssdpResponseRelay(sss)
	return nil
}

func ssdpHandler(source *endpoint, buf []byte, n int) ([]byte, int, error) {
	var req *http.Request

	outBuf := buf
	outSz := n

	rdr := bytes.NewReader(buf)
	req, err := http.ReadRequest(bufio.NewReader(rdr))
	if err != nil {
		// If we failed to parse the packet as a request, attempt it as
		// a response.
		rdr.Seek(0, io.SeekStart)
		return nil, 0, ssdpResponseCheck(rdr)

	}

	id := fmt.Sprintf("SSDP %s from %v", req.Method, source.ip)
	var mtype base_msg.EventSSDP_MessageType
	if req.Method == "M-SEARCH" {
		uri := req.Header.Get("Man")
		if uri == "\"ssdp:discover\"" {
			mtype = base_msg.EventSSDP_DISCOVER
			mxHdr := req.Header.Get("MX")
			mx, _ := strconv.Atoi(mxHdr)
			if mxHdr == "" {
				err = fmt.Errorf("%s: missing MX header", id)

			} else if mx < 1 || mx > 120 {
				err = fmt.Errorf("%s: bad MX header: %s",
					id, mxHdr)

			} else {
				if mx > mxMax {
					slog.Debugf("%s: reducing MX from %d to %d",
						id, mx, mxMax)

					mx = mxMax
					req.Header.Set("MX", strconv.Itoa(mxMax))

					n := new(bytes.Buffer)
					req.Write(n)
					outBuf = n.Bytes()
					outSz = n.Len()
				}
				err = ssdpSearchHandler(source, mx)
			}
		} else if uri == "" {
			err = fmt.Errorf("%s: missing uri", id)
		} else {
			err = fmt.Errorf("%s: unrecognized uri: %s", id, uri)
		}
		if err == nil {
			relayMetric.ssdpSearches.Inc()
		}
	} else if req.Method == "NOTIFY" {
		nts := req.Header.Get("NTS")
		if nts == "ssdp:alive" {
			mtype = base_msg.EventSSDP_ALIVE
			slog.Debugf("%s: forwarding ALIVE", id)
		} else if nts == "ssdp:byebye" {
			mtype = base_msg.EventSSDP_BYEBYE
			slog.Debugf("%s: forwarding BYEBYE", id)
		} else if nts == "" {
			err = fmt.Errorf("%s: missing NOTIFY nts", id)
		} else {
			err = fmt.Errorf("%s: unrecognized NOTIFY nts: %s",
				id, nts)

		}
		if err == nil {
			relayMetric.ssdpNotifies.Inc()
		}
	} else {
		err = fmt.Errorf("%s: invalid method", id)
	}

	if err == nil {
		ssdpEvent(source.ip, mtype, req)
	}

	return outBuf, outSz, err
}

func ssdpInit() {
	if ssdpInited {
		return
	}

	low := *ssdpBase
	high := *ssdpBase + *ssdpMax

	for port := low; port < high; port++ {
		p, err := net.ListenPacket("udp4", ":"+strconv.Itoa(port))
		if err != nil {
			slog.Warnf("unable to init SEARCH handler on %d: %v",
				port, err)
		} else {
			ssdpSearches = &ssdpSearchState{
				buf:      make([]byte, 4096),
				port:     port,
				next:     ssdpSearches,
				listener: ipv4.NewPacketConn(p),
			}
		}
	}

	propBase := "@/firewall/rules/ssdp/"
	rule := fmt.Sprintf("ACCEPT UDP FROM IFACE NOT wan TO AP DPORTS %d:%d",
		low, high-1)
	ops := []cfgapi.PropertyOp{
		{
			Op:    cfgapi.PropCreate,
			Name:  propBase + "rule",
			Value: rule,
		},
		{
			Op:    cfgapi.PropCreate,
			Name:  propBase + "active",
			Value: "true",
		},
	}

	config.Execute(nil, ops)
	ssdpInited = true
}

//
// Read the next message for this protocol.  Return the length and the interface
// on which it arrived.
func (r *relayer) getPacket(buf []byte) (int, *endpoint) {
	for done := false; !done; {
		var ip net.IP
		var portno int

		n, cm, src, err := r.conn.ReadFrom(buf)
		if n == 0 || err != nil {
			if err != nil {
				if done = r.done; !done {
					slog.Warnf("Read failed: %v", err)
				}
			}
			continue
		}

		ipv4 := ""
		if host, port, serr := net.SplitHostPort(src.String()); serr == nil {
			if ip = net.ParseIP(host); ip != nil {
				ipv4 = ip.To4().String()
			}
			portno, _ = strconv.Atoi(port)
		}
		if ipv4 == "" {
			slog.Warnf("Not an valid source: %s", src.String())
			continue
		}
		if _, ok := ipv4ToIface[ipv4]; ok {
			// If this came from one of our addresses, it's a packet
			// we just forwarded.  Ignore it.
			continue
		}

		iface, ierr := net.InterfaceByIndex(cm.IfIndex)
		if ierr != nil {
			slog.Warnf("Receive error from %s: %v", ipv4, err)
			continue
		}

		ring, ok := ifaceToRing[iface.Index]
		if !ok {
			// This packet isn't from a ring we relay UDP to/from
			continue
		}

		source := endpoint{
			conn:  r.conn,
			iface: iface,
			ip:    ip,
			port:  portno,
			ring:  ring,
		}
		return n, &source
	}

	return 0, nil
}

//
// Process all the multicast messages for a single service.  Each message is
// read, parsed, possibly evented to identifierd, and then forwarded to each
// ring allowed to receive it.
func (r *relayer) run() {
	defer close(r.exited)

	s := r.service

	if err := r.initListener(); err != nil {
		slog.Warnf("Unable to init relay for %s: %v", s.name, err)
		return
	}
	slog.Infof("initted %s", s.name)

	fw := &net.UDPAddr{IP: s.address, Port: s.port}
	inBuf := make([]byte, 4096)
	for {
		n, source := r.getPacket(inBuf)
		if source == nil {
			break
		}
		if source.iface == nil {
			slog.Warnf("multicast packet arrived on bad source: %v",
				source)
			continue
		}

		//
		// Currently we relay all messages up and down the rings.  It
		// may make sense for this to be a per-device and/or
		// per-protocol policy.
		relayUp := true
		relayDown := true

		outBuf, sz, err := s.handler(source, inBuf, n)
		if err != nil {
			slog.Warnf("Bad %s packet: %v", s.name, err)
			continue
		}

		srcLevel, ok := ringLevel[source.ring]
		if !ok {
			slog.Debugf("No relaying from %s", source.ring)
			continue
		}

		for dstRing, dstLevel := range ringLevel {
			dstIface := ringToIface[dstRing]
			if dstIface == nil {
				slog.Fatalf("missing interface for ring %s",
					dstRing)
			}
			if dstIface.Index == source.iface.Index {
				// Don't repeat the message on the interface it
				// arrived on
				continue
			}

			if !relayDown && (srcLevel > dstLevel) {
				continue
			}

			if !relayUp && (srcLevel < dstLevel) {
				continue
			}

			source.conn.SetMulticastInterface(dstIface)
			source.conn.SetMulticastTTL(255)
			l, err := source.conn.WriteTo(outBuf[:sz], nil, fw)
			if err != nil {
				slog.Warnf("    Forward to %s failed: %v",
					dstIface.Name, err)
			} else if l != sz {
				slog.Warnf("    Forwarded %d of %d to %s",
					l, n, dstIface.Name)
			} else {
				slog.Debugf("    Forwarded %d bytes to %s",
					n, dstIface.Name)
			}
		}
	}
	r.conn.Close()
}

func relayMetricsInit() {
	relayMetric.mdnsRequests = bgm.NewCounter("relay/mdns_requests")
	relayMetric.mdnsReplies = bgm.NewCounter("relay/mdns_replies")
	relayMetric.ssdpSearches = bgm.NewCounter("relay/ssdp_searches")
	relayMetric.ssdpTimeouts = bgm.NewCounter("relay/ssdp_timeouts")
	relayMetric.ssdpNotifies = bgm.NewCounter("relay/ssdp_notifies")
	relayMetric.ssdpResponses = bgm.NewCounter("relay/ssdp_requests")
}

func launchRelayers() {
	slog.Infof("Launching relay goroutines")

	relayerMtx.Lock()
	relayers = make([]*relayer, 0)
	for _, s := range multicastServices {
		r := &relayer{
			service: s,
			exited:  make(chan struct{}),
		}
		relayers = append(relayers, r)
		go r.run()
	}
	relayerMtx.Unlock()
}

func relayRestart() {
	slog.Infof("Stopping relay goroutines")
	relayerMtx.Lock()

	// close the relayers' sockets, causing any blocking reads to fail so
	// the routine can examine its 'r.done' flag.
	for _, r := range relayers {
		r.done = true
		if r.conn != nil {
			r.conn.Close()
		}
	}

	// wait for them to exit
	for _, r := range relayers {
		<-r.exited
	}
	relayers = nil
	relayerMtx.Unlock()
	launchRelayers()
}

func relayInit() {
	relayMetricsInit()
	launchRelayers()
}

