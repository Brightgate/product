/*
 * COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 *
 */

/*
 * Elementary DNSv4 server
 *
 * Anti-phishing datafiles are kept in <aproot>/var/spool/antiphishing/.
 *
 * XXX Need to handle RFC 2606 (reserved gTLDs that should be intercepted)
 * and RFC 7686 (.onion TLD that should be logged).
 *
 * XXX This implementation may be suitable to run both IPv4 and IPv6
 * servers in the same process.
 */

package main

import (
	"bytes"
	"container/heap"
	"flag"
	"fmt"
	"hash/crc64"
	"io/ioutil"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"bg/ap_common/apcfg"
	"bg/ap_common/aputil"
	"bg/ap_common/broker"
	"bg/ap_common/data"
	"bg/ap_common/mcp"
	"bg/ap_common/network"
	"bg/base_def"
	"bg/base_msg"
	"bg/common/cfgapi"

	"github.com/golang/protobuf/proto"
	"github.com/miekg/dns"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
)

const (
	pname       = "ap.dns4d"
	maxCacheTTL = uint32(3600)
)

type dnsRecord struct {
	rectype uint16
	recval  string
	expires *time.Time
}

var (
	cacheSize = flag.Int("cache_size", 1024*1024,
		"size of DNS cache (set to 0 to disable caching).")
	dataDir = flag.String("dir", data.DefaultDataDir,
		"antiphishing data directory")

	brokerd *broker.Broker
	config  *cfgapi.Handle
	slog    *zap.SugaredLogger

	ringRecords  map[string]dnsRecord // per-ring records for the router
	perRingHosts map[string]bool      // hosts with per-ring results
	subnets      []*net.IPNet

	domainname    string
	brightgateDNS string
	upstreamDNS   = "8.8.8.8:53"

	dnsHTTPClient *http.Client
)

/*
 * The 'clients' map represents all of the clients that we know about.  In
 * particular, we track which clients have been assigned an IP address either
 * statically or by DHCP.  This map is used to populate our initial DNS dataset
 * and to determine which incoming requests we will answer.

 * The 'hosts' map contains the DNS records we use to answer DNS requests.  The
 * initial data comes from the properties file, via the clients map.  Over time
 * additional PTR records will be added in response to NetEntity events.
 *
 * The two maps are protected by mutexes.  If an operation requires holding both
 * mutexes, the ClientMtx should be taken first.
 *
 */
var (
	clientMtx sync.Mutex
	clients   cfgapi.ClientMap

	hostsMtx sync.Mutex
	hosts    = make(map[string]dnsRecord)

	unknownWarned   = make(map[string]time.Time)
	blockWarned     = make(map[string]time.Time)
	warnedMtx       sync.Mutex
	cachedResponses dnsCache

	metrics struct {
		requests         prometheus.Counter
		blocked          prometheus.Counter
		upstreamCnt      prometheus.Counter
		upstreamFailures prometheus.Counter
		upstreamTimeouts prometheus.Counter
		upstreamLatency  prometheus.Summary
		requestSize      prometheus.Summary
		responseSize     prometheus.Summary
		cacheSize        prometheus.Gauge
		cacheEntries     prometheus.Gauge
		cacheLookups     prometheus.Counter
		cacheCollisions  prometheus.Counter
		cacheHitRate     prometheus.Gauge
	}
)

type cachedResponse struct {
	question string // question that triggered the response
	key      uint64 // hash of the question for fast map lookup

	response  *dns.Msg  // the upstream response to the question
	cachedAt  time.Time // when this cache entry was added
	eol       time.Time // when does the shortest TTL field expire
	size      int       // combined size of question and response
	timeEaten uint32    // used to adjust TTLs when using a cached response
}

type cacheEOLHeap []*cachedResponse

type dnsCache struct {
	responses map[uint64]*cachedResponse // cached data index by question
	eolHeap   cacheEOLHeap               // data ordered by TTL expiration
	size      int                        // total size of all entries
	table     *crc64.Table               // used during hash generation
	lookups   int                        // lookups into the cache
	hits      int                        // successful lookups

	sync.Mutex
}

/***********************************************************
 * utility routines required by the container/heap interface
 */
func (h cacheEOLHeap) Len() int { return len(h) }

func (h cacheEOLHeap) Less(i, j int) bool {
	return (h[i].eol).Before(h[j].eol)
}

func (h cacheEOLHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
}

func (h *cacheEOLHeap) Push(x interface{}) {
	r := x.(*cachedResponse)
	*h = append(*h, r)
}

func (h *cacheEOLHeap) Pop() interface{} {
	old := *h
	n := len(old)
	r := old[n-1]
	*h = old[0 : n-1]
	return r
}

// Remove any entries that have expired.  If the cache is larger than we want,
// remove entries in their expiration order until we shrink to the desired size.
func (d *dnsCache) expire() {
	now := time.Now()

	for len(d.eolHeap) > 0 {
		c := d.eolHeap[0]
		if c.eol.After(now) && d.size < *cacheSize {
			return
		}

		heap.Pop(&d.eolHeap)
		delete(d.responses, c.key)
		d.size -= c.size
		metrics.cacheEntries.Set(float64(len(d.responses)))
		metrics.cacheSize.Set(float64(d.size))
	}
}

// Decrease all TTL fields in all records
func adjustTTL(delta uint32, records []dns.RR) {
	for _, r := range records {
		if hdr := r.Header(); hdr != nil && hdr.Ttl > 0 {
			if delta <= hdr.Ttl {
				hdr.Ttl -= delta
			}
		}
	}
}

func (d *dnsCache) lookup(key uint64, question string) *dns.Msg {
	var r *dns.Msg

	d.lookups++
	metrics.cacheLookups.Inc()
	d.Lock()
	d.expire()
	if c, ok := d.responses[key]; ok && c.question == question {
		r = c.response

		// Each time we use a cached response, adjust any TTL fields to
		// account for time that has elapsed since a) the record was
		// cached, and/or b) time that has elapsed since we last
		// adjusted the TTLs.
		delta := uint32(time.Since(c.cachedAt).Seconds())
		bite := delta - c.timeEaten
		c.timeEaten += bite
		adjustTTL(bite, r.Answer)
		adjustTTL(bite, r.Ns)
		adjustTTL(bite, r.Extra)
		d.hits++
	}
	d.Unlock()
	metrics.cacheHitRate.Set(100.0 * (float64(d.hits) / float64(d.lookups)))
	return r
}

func (d *dnsCache) insert(key uint64, question string, response *dns.Msg) {
	ttl := maxCacheTTL
	for _, answer := range response.Answer {
		hdr := answer.Header()
		if hdr.Ttl < ttl {
			ttl = hdr.Ttl
		}
	}
	if ttl == 0 {
		return
	}

	now := time.Now()
	c := &cachedResponse{
		question: question,
		key:      key,
		response: response,
		cachedAt: now,
		eol:      now.Add(time.Duration(ttl) * time.Second),
		size:     len(question) + response.Len(),
	}

	d.Lock()
	// In the enormously unlikely event that two questions hash to the same
	// 64-bit key, we won't cache the second one.
	if _, ok := d.responses[key]; !ok {
		d.responses[key] = c
		heap.Push(&d.eolHeap, c)
		d.size += c.size
		metrics.cacheEntries.Set(float64(len(d.responses)))
		metrics.cacheSize.Set(float64(d.size))
	} else {
		metrics.cacheCollisions.Inc()
	}
	d.Unlock()
}

func (d *dnsCache) init() {
	metrics.cacheEntries.Set(0.0)
	metrics.cacheSize.Set(0.0)
	d.responses = make(map[uint64]*cachedResponse)
	d.eolHeap = make([]*cachedResponse, 0)
	d.table = crc64.MakeTable(crc64.ISO)
}

// Returns 'true' if we have issued a warning about this key within the past
// hour.
func wasWarned(key string, list map[string]time.Time) bool {
	warnedMtx.Lock()
	defer warnedMtx.Unlock()

	if t, ok := list[key]; ok && time.Since(t) < time.Hour {
		return true
	}
	list[key] = time.Now()
	return false
}

func clearWarned(key string, list map[string]time.Time) {
	warnedMtx.Lock()
	delete(list, key)
	warnedMtx.Unlock()
}

func clientUpdateEvent(path []string, val string, expires *time.Time) {
	if len(path) != 3 {
		// All updates should affect /clients/<macaddr>/property
		return
	}

	mac := path[1]
	new := config.GetClient(mac)
	if new == nil {
		slog.Warnf("Got update for nonexistent client: %s", mac)
		return
	}

	clientMtx.Lock()
	old := clients[mac]
	if old != nil {
		deleteOneClient(old)
	}
	updateOneClient(new)
	clients[mac] = new

	clientMtx.Unlock()
}

func clientDeleteEvent(path []string) {
	ignore := true

	if len(path) == 2 {
		// Handle full client delete (@/clients/<mac>)
		ignore = false
	} else if len(path) == 3 &&
		(path[2] == "dns_name" || path[2] == "ipv4") {
		ignore = false
	}

	if !ignore {
		mac := path[1]

		clientMtx.Lock()
		if old := clients[mac]; old != nil {
			delete(clients, mac)
			deleteOneClient(old)
		}
		clientMtx.Unlock()
	}
}

func blocklistUpdateEvent(path []string, val string, expires *time.Time) {
	data.LoadDNSBlocklist(*dataDir)
}

func cnameUpdateEvent(path []string, val string, expires *time.Time) {
	updateOneCname(path[2], val)
}

func cnameDeleteEvent(path []string) {
	deleteOneCname(path[2])
}

func serverUpdateEvent(path []string, val string, expires *time.Time) {
	setNameserver(val)
}

func logRequest(handler string, start time.Time, ip net.IP, r, m *dns.Msg) {
	protocol := base_msg.Protocol_DNS
	requests := make([]string, 0)
	responses := make([]string, 0)

	for _, question := range r.Question {
		requests = append(requests, question.String())
	}

	for _, answer := range m.Answer {
		responses = append(responses, answer.String())
	}

	entity := &base_msg.EventNetRequest{
		Timestamp:    aputil.NowToProtobuf(),
		Sender:       proto.String(brokerd.Name),
		Debug:        proto.String(handler),
		Requestor:    proto.String(ip.String()),
		IdentityUuid: proto.String(base_def.ZERO_UUID),
		Protocol:     &protocol,
		Request:      requests,
		Response:     responses,
	}

	if err := brokerd.Publish(entity, base_def.TOPIC_REQUEST); err != nil {
		slog.Errorf("failed to publish event: %v", err)
	}
}

// We just got a DNS request from an unknown client.  Send a notification that
// we have an unknown entity on our network.
func logUnknown(ipstr string) bool {
	var addr net.IP

	if host, _, _ := net.SplitHostPort(ipstr); host != "" {
		addr = net.ParseIP(host).To4()
	} else {
		return false
	}

	entity := &base_msg.EventNetEntity{
		Timestamp:   aputil.NowToProtobuf(),
		Sender:      proto.String(brokerd.Name),
		Debug:       proto.String("-"),
		Ipv4Address: proto.Uint32(network.IPAddrToUint32(addr)),
	}

	err := brokerd.Publish(entity, base_def.TOPIC_ENTITY)
	return err == nil
}

// Determine whether the DNS request came from a known client.  If it did,
// return the client record.  If it didn't, raise a warning flag and return nil.
func getClient(w dns.ResponseWriter) (string, *cfgapi.ClientInfo) {
	addr, ok := w.RemoteAddr().(*net.UDPAddr)
	if !ok {
		return "", nil
	}

	clientMtx.Lock()
	defer clientMtx.Unlock()

	for mac, c := range clients {
		if addr.IP.Equal(c.IPv4) {
			return mac, c
		}
	}

	ipStr := addr.IP.String()
	if !wasWarned(ipStr, unknownWarned) {
		slog.Warnf("DNS request from unknown client: %s", ipStr)
	}

	return "", nil
}

// Look through the client table to find the mac address corresponding to this
// client record.
func getMac(record *cfgapi.ClientInfo) net.HardwareAddr {
	clientMtx.Lock()
	defer clientMtx.Unlock()

	for m, c := range clients {
		if c == record {
			mac, _ := net.ParseMAC(m)
			return mac
		}
	}

	return network.MacZero
}

func answerA(q dns.Question, rec dnsRecord) *dns.A {
	a := net.ParseIP(rec.recval)
	rr := dns.A{
		Hdr: dns.RR_Header{
			Name:   q.Name,
			Rrtype: dns.TypeA,
			Class:  dns.ClassINET,
			Ttl:    0},
		A: a.To4(),
	}

	return &rr
}

func answerPTR(q dns.Question, rec dnsRecord) *dns.PTR {
	rr := dns.PTR{
		Hdr: dns.RR_Header{
			Name:   q.Name,
			Rrtype: dns.TypePTR,
			Class:  dns.ClassINET,
			Ttl:    0},
		Ptr: rec.recval,
	}
	return &rr
}

func answerCNAME(q dns.Question, rec dnsRecord) *dns.CNAME {
	rr := dns.CNAME{
		Hdr: dns.RR_Header{
			Name:   q.Name,
			Rrtype: dns.TypeCNAME,
			Class:  dns.ClassINET,
			Ttl:    0},
		Target: rec.recval,
	}
	return &rr
}

func shouldCache(q, r *dns.Msg) bool {
	if *cacheSize == 0 {
		return false
	}

	// Only cache sucessful, complete results
	if r == nil || r.Rcode != dns.RcodeSuccess || r.Truncated {
		return false
	}

	// Only cache results for QUERY operations
	if q.Opcode != dns.OpcodeQuery {
		return false
	}

	// Don't cache results for wildcarded queries
	if strings.Contains(q.Question[0].Name, "*") {
		return false
	}

	// Only cache results that match the single question we've asked
	if len(r.Question) != 1 {
		return false
	}

	a := q.Question[0]
	b := r.Question[0]
	if a.Qtype != b.Qtype || a.Qclass != b.Qclass || a.Name != b.Name {
		return false
	}

	return true
}

func dnsOverHTTPSExchange(m *dns.Msg, server string) (*dns.Msg, error) {
	var rval *dns.Msg

	packed, err := m.Pack()
	if err != nil {
		return nil, fmt.Errorf("pack failed: %v", err)
	}
	r := bytes.NewReader(packed)

	req, err := http.NewRequest("POST", server, r)
	if err != nil {
		return nil, fmt.Errorf("failed to create POST request: %v", err)
	}
	req.Header.Add("content-type", "application/dns-udpwireformat")
	req.Header.Add("accept", "*/*")

	res, err := dnsHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("POST failed: %v", err)
	}
	buf, err := ioutil.ReadAll(res.Body)
	res.Body.Close()

	if res.StatusCode != http.StatusOK {
		details := ""
		if err != nil {
			details = " (" + string(buf) + ")"
		}
		err = fmt.Errorf("DoH server response: %s%s", res.Status,
			details)
	} else {
		rval = &dns.Msg{}
		err = rval.Unpack(buf)
		if err != nil {
			err = fmt.Errorf("unpacking DNS response: %v", err)
			// slog.Debugf("%s", hex.Dump(buf))
			rval = nil
		}
	}

	return rval, err
}

func upstreamRequest(server string, r, m *dns.Msg) {
	var cacheResult bool
	var upstream *dns.Msg
	var err error

	question := r.Question[0].String()
	key := crc64.Checksum([]byte(question), cachedResponses.table)
	if *cacheSize > 0 {
		upstream = cachedResponses.lookup(key, question)
	}

	if upstream == nil {
		c := new(dns.Client)
		start := time.Now()
		metrics.upstreamCnt.Inc()
		if dnsHTTPClient != nil {
			upstream, err = dnsOverHTTPSExchange(r, server)
		} else {
			upstream, _, err = c.Exchange(r, server)
		}
		metrics.upstreamLatency.Observe(time.Since(start).Seconds())
		cacheResult = (err == nil) && shouldCache(r, upstream)
	}

	if err != nil || upstream == nil {
		slog.Warnf("failed to exchange: %v", err)
		metrics.upstreamFailures.Inc()
		if os.IsTimeout(err) {
			metrics.upstreamTimeouts.Inc()
		}
		m.Rcode = dns.RcodeServerFailure
		return
	}

	// Copy the flags from the message header
	m.Compress = upstream.Compress
	m.Authoritative = upstream.Authoritative
	m.Truncated = upstream.Truncated
	m.RecursionDesired = upstream.RecursionDesired
	m.RecursionAvailable = upstream.RecursionAvailable
	m.Rcode = upstream.Rcode
	m.Answer = append(m.Answer, upstream.Answer...)
	m.Ns = append(m.Ns, upstream.Ns...)
	m.Extra = append(m.Extra, upstream.Extra...)

	if upstream.Rcode == dns.RcodeSuccess && cacheResult {
		cachedResponses.insert(key, question, upstream)
	}
}

func localHandler(w dns.ResponseWriter, r *dns.Msg) {
	var rec dnsRecord
	var ok bool

	metrics.requests.Inc()
	metrics.requestSize.Observe(float64(r.Len()))
	_, c := getClient(w)
	if c == nil {
		return
	}

	m := new(dns.Msg)
	m.SetReply(r)
	m.Authoritative = true

	// The protocol technically allows multiple questions, but the major
	// resolvers don't.  With multiple questions, some of the message header
	// bits become ambiguous.
	if len(r.Question) != 1 {
		m.Rcode = dns.RcodeFormatError
		w.WriteMsg(m)
		return
	}

	q := r.Question[0]
	start := time.Now()

	if perRingHosts[q.Name] {
		rec, ok = ringRecords[c.Ring]
	} else {
		hostsMtx.Lock()
		rec, ok = hosts[q.Name]
		hostsMtx.Unlock()
	}

	if ok {
		if rec.rectype == dns.TypeA {
			m.Answer = append(m.Answer, answerA(q, rec))
		} else if rec.rectype == dns.TypeCNAME {
			m.Answer = append(m.Answer, answerCNAME(q, rec))
		}
	} else if brightgateDNS != "" {
		// Proxy needed if we have decided that we are allowing
		// our brightgate domain to be handled upstream as well.
		pq := new(dns.Msg)
		pq.MsgHdr = r.MsgHdr
		pq.Question = append(pq.Question, q)
		upstreamRequest(brightgateDNS, pq, m)
	}
	metrics.responseSize.Observe(float64(m.Len()))
	w.WriteMsg(m)

	logRequest("localHandler", start, c.IPv4, r, m)
}

func notifyBlockEvent(c *cfgapi.ClientInfo, hostname string) {
	protocol := base_msg.Protocol_DNS
	reason := base_msg.EventNetException_PHISHING_ADDRESS
	topic := base_def.TOPIC_EXCEPTION
	dev := getMac(c)

	entity := &base_msg.EventNetException{
		Timestamp:   aputil.NowToProtobuf(),
		Sender:      proto.String(brokerd.Name),
		Debug:       proto.String("-"),
		Protocol:    &protocol,
		Reason:      &reason,
		Details:     []string{hostname},
		MacAddress:  proto.Uint64(network.HWAddrToUint64(dev)),
		Ipv4Address: proto.Uint32(network.IPAddrToUint32(c.IPv4)),
	}

	if err := brokerd.Publish(entity, topic); err != nil {
		slog.Errorf("couldn't publish %s (%v): %v", topic, entity, err)
	}
}

func localAddress(arpa string) bool {
	reversed := strings.TrimSuffix(arpa, ".in-addr.arpa.")
	if ip := net.ParseIP(reversed).To4(); ip != nil {
		ip[0], ip[1], ip[2], ip[3] = ip[3], ip[2], ip[1], ip[0]
		for _, s := range subnets {
			if s.Contains(ip) {
				return true
			}
		}
	}
	return false
}

func proxyHandler(w dns.ResponseWriter, r *dns.Msg) {
	metrics.requests.Inc()
	metrics.requestSize.Observe(float64(r.Len()))

	mac, c := getClient(w)
	if c == nil {
		return
	}

	m := new(dns.Msg)
	m.SetReply(r)
	m.Authoritative = false

	// The protocol technically allows multiple questions, but the major
	// resolvers don't.  With multiple questions, some of the message header
	// bits become ambiguous.
	if len(r.Question) != 1 {
		m.Rcode = dns.RcodeFormatError
		w.WriteMsg(m)
		return
	}

	start := time.Now()
	q := r.Question[0]

	hostname := q.Name[:len(q.Name)-1]
	if data.BlockedHostname(hostname) {
		// XXX: maybe we should return a CNAME record for our
		// local 'phishing.<siteid>.brightgate.net'?
		localRecord, _ := ringRecords[c.Ring]
		m.Answer = append(m.Answer, answerA(q, localRecord))

		// We want to log and Event blocked hostnames for each
		// client that attempts the lookup.
		key := mac + ":" + hostname
		if !wasWarned(key, blockWarned) {
			slog.Infof("Blocking suspected phishing site "+
				"'%s' for %s", hostname, mac)
			notifyBlockEvent(c, hostname)
			metrics.blocked.Inc()
		}
	} else if q.Qtype == dns.TypePTR && localAddress(q.Name) {
		hostsMtx.Lock()
		rec, ok := hosts[q.Name]
		hostsMtx.Unlock()
		if ok && rec.rectype == dns.TypePTR {
			m.Answer = append(m.Answer, answerPTR(q, rec))
		}
	} else {
		upstreamRequest(upstreamDNS, r, m)
	}

	if m.Len() >= 512 {
		// Some clients cannot handle DNS packets larger than 512 bytes,
		// and some firewalls will drop them.  Setting this flag will
		// cause the underlying DNS library to use name compression,
		// shrinking the packet before it gets put on the wire.
		m.Compress = true
	}
	metrics.responseSize.Observe(float64(m.Len()))
	w.WriteMsg(m)
	logRequest("proxyHandler", start, c.IPv4, r, m)
}

func deleteOneClient(c *cfgapi.ClientInfo) {
	if c.IPv4 == nil {
		return
	}
	ipv4 := c.IPv4.String()

	clearWarned(ipv4, unknownWarned)

	hostsMtx.Lock()
	if arpa, err := dns.ReverseAddr(ipv4); err == nil {
		if rec, ok := hosts[arpa]; ok {
			slog.Infof("Deleting PTR record %s->%s", arpa,
				rec.recval)
			delete(hosts, arpa)
		}
	}

	for addr, rec := range hosts {
		if rec.rectype == dns.TypeA && rec.recval == ipv4 {
			slog.Infof("Deleting A record %s->%s", addr, ipv4)
			delete(hosts, addr)
		}
	}
	hostsMtx.Unlock()
}

// Convert a client's configd info into DNS records
func updateOneClient(c *cfgapi.ClientInfo) {
	name := c.DNSName
	if name == "" {
		name = c.DHCPName
	}
	if !network.ValidDNSName(name) || c.IPv4 == nil {
		return
	}

	ipv4 := c.IPv4.String()
	clearWarned(ipv4, unknownWarned)
	hostsMtx.Lock()
	hostname := name + "." + domainname + "."

	slog.Infof("Adding A record %s->%s", hostname, ipv4)
	hosts[hostname] = dnsRecord{
		rectype: dns.TypeA,
		recval:  ipv4,
		expires: c.Expires,
	}

	arpa, err := dns.ReverseAddr(ipv4)
	if err != nil {
		slog.Warnf("Invalid address %v for %s: %v",
			c.IPv4, name, err)
	} else {
		hostname := name + "."
		slog.Infof("Adding PTR record %s->%s", arpa, hostname)
		hosts[arpa] = dnsRecord{
			rectype: dns.TypePTR,
			recval:  hostname,
			expires: c.Expires,
		}
	}
	hostsMtx.Unlock()
}

func updateOneCname(hostname, canonical string) {
	hostname = hostname + "." + domainname + "."
	canonical = canonical + "." + domainname + "."
	slog.Infof("Adding cname %s -> %s", hostname, canonical)

	hostsMtx.Lock()
	hosts[hostname] = dnsRecord{
		rectype: dns.TypeCNAME,
		recval:  canonical,
	}
	hostsMtx.Unlock()
}

func deleteOneCname(hostname string) {
	hostname = hostname + "." + domainname + "."
	slog.Infof("Deleting cname %s", hostname)

	hostsMtx.Lock()
	delete(hosts, hostname)
	hostsMtx.Unlock()
}

func initHostMap() {
	clients = config.GetClients()
	for _, c := range clients {
		if c.Expires == nil || c.Expires.After(time.Now()) {
			updateOneClient(c)
		}
	}

	if cnames, _ := config.GetProps("@/dns/cnames"); cnames != nil {
		for name, c := range cnames.Children {
			updateOneCname(name, c.Value)
		}
	}

	perRingHosts = make(map[string]bool)
	hostnames := [...]string{"gateway", "phishing", "malware", "captive"}
	for _, name := range hostnames {
		perRingHosts[name+"."+domainname+"."] = true
	}
}

func setNameserver(in string) {
	// If the server looks like dns-over-http, accept it as-is.  Otherwise
	// we try to interpret it as an <ip>:<port> tuple.
	if strings.HasPrefix(in, "https://") {
		netTransport := &http.Transport{
			Dial: (&net.Dialer{
				Timeout: 5 * time.Second,
			}).Dial,
			TLSHandshakeTimeout: 5 * time.Second,
			IdleConnTimeout:     300,
		}
		dnsHTTPClient = &http.Client{
			Timeout:   time.Second * 2,
			Transport: netTransport,
		}
	} else {
		comp := strings.Split(in, ":")
		ip := net.ParseIP(comp[0])
		if ip == nil {
			slog.Warnf("Invalid nameserver: %s", in)
			return
		}
		if len(comp) == 1 {
			// If the address didn't include a port number,
			// append the standard port
			in += ":53"
		}
		dnsHTTPClient = nil
	}
	slog.Infof("Using nameserver: %s", in)
	upstreamDNS = in
	cachedResponses.init()
}

func initNetwork() {
	var err error

	unknownWarned = make(map[string]time.Time)
	blockWarned = make(map[string]time.Time)

	domainname, err = config.GetDomain()
	if err != nil {
		slog.Fatalf("failed to fetch gateway domain: %v", err)
	}

	if tmp, _ := config.GetProp("@/network/dnsserver"); tmp != "" {
		setNameserver(tmp)
	}

	rings := config.GetRings()
	if rings == nil {
		slog.Fatalf("Can't retrieve ring information")
	} else {
		slog.Debugf("defined rings %v", rings)
	}

	// Each ring will have an A record for that ring's router.  That
	// record will double as a result for phishing URLs and all captive
	// portal requests.
	ringRecords = make(map[string]dnsRecord)
	for name, ring := range rings {
		srouter := network.SubnetRouter(ring.Subnet)
		ringRecords[name] = dnsRecord{
			rectype: dns.TypeA,
			recval:  srouter,
		}

		if _, subnet, _ := net.ParseCIDR(ring.Subnet); subnet != nil {
			subnets = append(subnets, subnet)
		}
	}
}

func dnsListener(protocol string) {
	srv := &dns.Server{Addr: ":53", Net: protocol}
	if err := srv.ListenAndServe(); err != nil {
		slog.Fatalf("Failed to start %s listener %v", protocol, err)
	}
}

func prometheusInit() {
	metrics.requests = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "dns4d_requests",
		Help: "dns requests handled",
	})
	metrics.blocked = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "dns4d_blocked",
		Help: "suspicious dns requests blocked",
	})
	metrics.upstreamCnt = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "dns4d_upstream_cnt",
		Help: "dns requests forwarded to upstream resolver",
	})
	metrics.upstreamFailures = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "dns4d_upstream_failures",
		Help: "upstream DNS failures",
	})
	metrics.upstreamTimeouts = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "dns4d_upstream_timeouts",
		Help: "upstream DNS timeouts",
	})
	metrics.upstreamLatency = prometheus.NewSummary(prometheus.SummaryOpts{
		Name: "dns4d_upstream_latency",
		Help: "upstream query resolution time",
	})
	metrics.requestSize = prometheus.NewSummary(prometheus.SummaryOpts{
		Name: "dns4d_request_size",
		Help: "dns4d_dns request size (bytes)",
	})
	metrics.responseSize = prometheus.NewSummary(prometheus.SummaryOpts{
		Name: "dns4d_response_size",
		Help: "dns response size (bytes)",
	})
	metrics.cacheSize = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "dns4d_cache_size",
		Help: "data stored in DNS cache (bytes)",
	})
	metrics.cacheEntries = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "dns4d_cache_entries",
		Help: "# of entries in DNS cache",
	})
	metrics.cacheCollisions = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "dns4d_cache_collisions",
		Help: "hash key collisions in the DNS cache map",
	})
	metrics.cacheLookups = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "dns4d_cache_lookups",
		Help: "lookups in the DNS cache",
	})
	metrics.cacheHitRate = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "dns4d_cache_hitrate",
		Help: "success rate of cache lookups",
	})
	prometheus.MustRegister(metrics.requests)
	prometheus.MustRegister(metrics.blocked)
	prometheus.MustRegister(metrics.upstreamCnt)
	prometheus.MustRegister(metrics.upstreamFailures)
	prometheus.MustRegister(metrics.upstreamTimeouts)
	prometheus.MustRegister(metrics.upstreamLatency)
	prometheus.MustRegister(metrics.requestSize)
	prometheus.MustRegister(metrics.responseSize)
	prometheus.MustRegister(metrics.cacheSize)
	prometheus.MustRegister(metrics.cacheEntries)
	prometheus.MustRegister(metrics.cacheLookups)
	prometheus.MustRegister(metrics.cacheCollisions)
	prometheus.MustRegister(metrics.cacheHitRate)

	http.Handle("/metrics", promhttp.Handler())
	go http.ListenAndServe(base_def.DNSD_DIAG_PORT, nil)
}

func main() {
	flag.Usage = func() {
		flag.PrintDefaults()
	}
	flag.Parse()

	slog = aputil.NewLogger(pname)
	defer slog.Sync()
	slog.Infof("Starting")

	mcpd, err := mcp.New(pname)
	if err != nil {
		slog.Errorf("cannot connect to mcp")
	}

	prometheusInit()
	cachedResponses.init()

	brokerd = broker.New(pname)
	defer brokerd.Fini()

	config, err = apcfg.NewConfigd(brokerd, pname, cfgapi.AccessInternal)
	if err != nil {
		slog.Fatalf("cannot connect to configd: %v", err)
	}
	config.HandleChange(`^@/clients/.*/(ipv4|dns_name|dhcp_name|ring)$`,
		clientUpdateEvent)
	config.HandleDelete(`^@/clients/.*`, clientDeleteEvent)
	config.HandleExpire(`^@/clients/.*/(ipv4|dns_name)$`, clientDeleteEvent)
	config.HandleChange(`^@/dns/cnames/.*$`, cnameUpdateEvent)
	config.HandleDelete(`^@/dns/cnames/.*$`, cnameDeleteEvent)
	config.HandleChange(`^@/updates/dns_.*list$`, blocklistUpdateEvent)
	config.HandleChange(`^@/network/dnsserver$`, serverUpdateEvent)

	initNetwork()
	initHostMap()
	data.LoadDNSBlocklist(*dataDir)

	dns.HandleFunc(domainname+".", localHandler)
	dns.HandleFunc(".", proxyHandler)

	go dnsListener("udp")
	go dnsListener("tcp")

	mcpd.SetState(mcp.ONLINE)

	sig := make(chan os.Signal, 2)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	s := <-sig
	slog.Infof("Signal (%v) received, stopping", s)
}
