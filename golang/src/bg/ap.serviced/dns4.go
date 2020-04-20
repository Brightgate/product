/*
 * COPYRIGHT 2020 Brightgate Inc.  All rights reserved.
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
	"fmt"
	"hash/crc64"
	"io/ioutil"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"bg/ap_common/apcfg"
	"bg/ap_common/aputil"
	"bg/ap_common/bgmetrics"
	"bg/ap_common/data"
	"bg/base_def"
	"bg/base_msg"
	"bg/common/cfgapi"
	"bg/common/network"

	"github.com/golang/protobuf/proto"
	"github.com/miekg/dns"
)

const (
	maxCacheTTL = uint32(3600)
)

type dnsRecord struct {
	name     string
	mac      string
	hostRing string
	rectype  uint16
	recval   string
}

var (
	cacheSize = apcfg.Int("cache_size", 1024*1024, false, nil)
	dataDir   = apcfg.String("dir", data.DefaultDataDir, false, nil)
	localTTL  = apcfg.Duration("local_ttl", 5*time.Minute, true, nil)

	ringRecords  map[string]*dnsRecord // per-ring records for the router
	perRingHosts map[string]bool       // hosts with per-ring results
	subnets      []*net.IPNet

	// rings subject to anti-phishing rules
	phishingRings = map[string]bool{
		base_def.RING_DEVICES:    true,
		base_def.RING_UNENROLLED: true,
		base_def.RING_QUARANTINE: true,
	}

	// Limit the ability of clients in one ring to perform DNS lookups (or
	// reverse lookups) of clients in a more secure ring.  The following map
	// describes which rings each ring may look into.
	dnsVisibility = map[string]map[string]bool{
		base_def.RING_CORE: {
			base_def.RING_INTERNAL:   true,
			base_def.RING_UNENROLLED: true,
			base_def.RING_CORE:       true,
			base_def.RING_STANDARD:   true,
			base_def.RING_DEVICES:    true,
			base_def.RING_GUEST:      true,
			base_def.RING_QUARANTINE: true,
		},
		base_def.RING_STANDARD: {
			base_def.RING_STANDARD: true,
			base_def.RING_DEVICES:  true,
			base_def.RING_GUEST:    true,
		},
		base_def.RING_VPN: {
			base_def.RING_STANDARD: true,
			base_def.RING_DEVICES:  true,
			base_def.RING_GUEST:    true,
		},
		base_def.RING_DEVICES: {
			base_def.RING_DEVICES: true,
			base_def.RING_GUEST:   true,
		},
		base_def.RING_GUEST: {
			base_def.RING_GUEST: true,
		},
	}

	clientSelf = &cfgapi.ClientInfo{
		Ring: base_def.RING_CORE,
		IPv4: network.IPLocalhost,
	}

	domainname    string
	brightgateDNS string
	upstreamDNS   = "8.8.8.8:53"

	dnsHTTPClient *http.Client
)

// The 'hosts' map contains the DNS records we use to answer DNS requests.  The
// initial data comes from the properties file, via the clients map.  Over time
// additional PTR records will be added in response to NetEntity events.
var (
	hostsMtx sync.Mutex
	hosts    = make(map[string]*dnsRecord)

	unknownWarned   = make(map[string]time.Time)
	blockWarned     = make(map[string]time.Time)
	warnedMtx       sync.Mutex
	cachedResponses dnsCache

	dnsMetrics struct {
		requests         *bgmetrics.Counter
		blocked          *bgmetrics.Counter
		upstreamCnt      *bgmetrics.Counter
		upstreamFailures *bgmetrics.Counter
		upstreamTimeouts *bgmetrics.Counter
		upstreamLatency  *bgmetrics.Summary
		requestSize      *bgmetrics.Summary
		responseSize     *bgmetrics.Summary
		cacheSize        *bgmetrics.Gauge
		cacheEntries     *bgmetrics.Gauge
		cacheLookups     *bgmetrics.Counter
		cacheCollisions  *bgmetrics.Counter
		cacheHitRate     *bgmetrics.Gauge
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

func (r *dnsRecord) String() string {
	var rval string

	if r.rectype == dns.TypeA {
		rval = "A record"
	} else if r.rectype == dns.TypePTR {
		rval = "PTR record"
	}
	rval += " name=" + r.name + " mac=" + r.mac +
		" value=" + r.recval
	return rval
}

func (r *dnsRecord) Equal(s *dnsRecord) bool {
	return r.mac == s.mac && r.name == s.name &&
		r.hostRing == s.hostRing &&
		r.rectype == s.rectype && r.recval == s.recval
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
		dnsMetrics.cacheEntries.Set(float64(len(d.responses)))
		dnsMetrics.cacheSize.Set(float64(d.size))
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
	dnsMetrics.cacheLookups.Inc()
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
	dnsMetrics.cacheHitRate.Set(100.0 * (float64(d.hits) / float64(d.lookups)))
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
		dnsMetrics.cacheEntries.Set(float64(len(d.responses)))
		dnsMetrics.cacheSize.Set(float64(d.size))
	} else {
		dnsMetrics.cacheCollisions.Inc()
	}
	d.Unlock()
}

func (d *dnsCache) init() {
	dnsMetrics.cacheEntries.Set(0.0)
	dnsMetrics.cacheSize.Set(0.0)
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
		IdentityUuid: nil,
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
func getClient(w dns.ResponseWriter) (string, net.IP, string) {
	addr, ok := w.RemoteAddr().(*net.UDPAddr)
	if !ok {
		return "", nil, ""
	}

	if addr.IP.Equal(clientSelf.IPv4) {
		return network.MacZero.String(), clientSelf.IPv4, clientSelf.Ring
	}

	clientMtx.Lock()
	defer clientMtx.Unlock()

	for mac, c := range clients {
		if addr.IP.Equal(c.IPv4) {
			return mac, c.IPv4, c.Ring
		}
	}

	for mac, ip := range vpnClients {
		if addr.IP.Equal(ip) {
			return mac, ip, base_def.RING_VPN
		}
	}

	ipStr := addr.IP.String()
	if !wasWarned(ipStr, unknownWarned) {
		logUnknown(ipStr)
		slog.Warnf("DNS request from unknown client: %s", ipStr)
	}

	return "", nil, ""
}

func answerA(q dns.Question, rec *dnsRecord) *dns.A {
	a := net.ParseIP(rec.recval)
	rr := dns.A{
		Hdr: dns.RR_Header{
			Name:   q.Name,
			Rrtype: dns.TypeA,
			Class:  dns.ClassINET,
			Ttl:    uint32(localTTL.Seconds())},
		A: a.To4(),
	}

	return &rr
}

func answerPTR(q dns.Question, rec *dnsRecord) *dns.PTR {
	rr := dns.PTR{
		Hdr: dns.RR_Header{
			Name:   q.Name,
			Rrtype: dns.TypePTR,
			Class:  dns.ClassINET,
			Ttl:    uint32(localTTL.Seconds())},
		Ptr: rec.recval,
	}
	return &rr
}

func answerCNAME(q dns.Question, rec *dnsRecord) *dns.CNAME {
	rr := dns.CNAME{
		Hdr: dns.RR_Header{
			Name:   q.Name,
			Rrtype: dns.TypeCNAME,
			Class:  dns.ClassINET,
			Ttl:    uint32(localTTL.Seconds())},
		Target: rec.recval,
	}
	return &rr
}

func shouldCache(q, r *dns.Msg) bool {
	if *cacheSize == 0 {
		return false
	}

	// Only cache successful, complete results
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

	question := strings.ToLower(r.Question[0].String())
	key := crc64.Checksum([]byte(question), cachedResponses.table)
	if *cacheSize > 0 {
		upstream = cachedResponses.lookup(key, question)
	}

	if upstream == nil {
		c := new(dns.Client)
		start := time.Now()
		dnsMetrics.upstreamCnt.Inc()
		if dnsHTTPClient != nil {
			upstream, err = dnsOverHTTPSExchange(r, server)
		} else {
			upstream, _, err = c.Exchange(r, server)
		}
		dnsMetrics.upstreamLatency.Observe(time.Since(start).Seconds())
		cacheResult = (err == nil) && shouldCache(r, upstream)
	}

	tlog := aputil.GetThrottledLogger(slog, time.Second, 10*time.Minute)
	if err != nil || upstream == nil {
		tlog.Warnf("failed to exchange: %v", err)
		dnsMetrics.upstreamFailures.Inc()
		if os.IsTimeout(err) {
			dnsMetrics.upstreamTimeouts.Inc()
		}
		m.Rcode = dns.RcodeServerFailure
		return
	}
	tlog.Clear()

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
	var rec *dnsRecord
	var ok bool

	dnsMetrics.requests.Inc()
	dnsMetrics.requestSize.Observe(float64(r.Len()))
	_, ipv4, ring := getClient(w)
	if ipv4 == nil {
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
	name := strings.ToLower(q.Name)
	start := time.Now()

	if perRingHosts[name] {
		rec, ok = ringRecords[ring]
	} else {
		hostsMtx.Lock()
		rec, ok = hosts[name]
		hostsMtx.Unlock()
	}

	if ok {
		if dnsVisibility[ring][rec.hostRing] {
			if rec.rectype == dns.TypeA {
				m.Answer = append(m.Answer, answerA(q, rec))
				m.RecursionAvailable = true
			} else if rec.rectype == dns.TypeCNAME {
				m.Answer = append(m.Answer, answerCNAME(q, rec))
				m.RecursionAvailable = true
			}
		}
	} else if brightgateDNS != "" {
		// Proxy needed if we have decided that we are allowing
		// our brightgate domain to be handled upstream as well.
		pq := new(dns.Msg)
		pq.MsgHdr = r.MsgHdr
		pq.Question = append(pq.Question, q)
		upstreamRequest(brightgateDNS, pq, m)
	}
	dnsMetrics.responseSize.Observe(float64(m.Len()))
	w.WriteMsg(m)

	logRequest("localHandler", start, ipv4, r, m)
}

func notifyBlockEvent(mac string, ipv4 net.IP, hostname string) {
	protocol := base_msg.Protocol_DNS
	reason := base_msg.EventNetException_PHISHING_ADDRESS
	topic := base_def.TOPIC_EXCEPTION

	hwaddr := network.MacZero
	if mac != "" {
		if x, err := net.ParseMAC(mac); err == nil {
			hwaddr = x
		}
	}

	entity := &base_msg.EventNetException{
		Timestamp:   aputil.NowToProtobuf(),
		Sender:      proto.String(brokerd.Name),
		Debug:       proto.String("-"),
		Protocol:    &protocol,
		Reason:      &reason,
		Details:     []string{hostname},
		MacAddress:  proto.Uint64(network.HWAddrToUint64(hwaddr)),
		Ipv4Address: proto.Uint32(network.IPAddrToUint32(ipv4)),
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
	dnsMetrics.requests.Inc()
	dnsMetrics.requestSize.Observe(float64(r.Len()))

	mac, ipv4, ring := getClient(w)
	if ipv4 == nil {
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
	name := strings.ToLower(q.Name)

	hostname := name[:len(name)-1]
	if phishingRings[ring] && data.BlockedHostname(hostname) {
		// XXX: maybe we should return a CNAME record for our
		// local 'phishing.<siteid>.brightgate.net'?
		localRecord, _ := ringRecords[ring]
		m.Answer = append(m.Answer, answerA(q, localRecord))

		// We want to log and Event blocked hostnames for each
		// client that attempts the lookup.
		key := mac + ":" + hostname
		if !wasWarned(key, blockWarned) {
			slog.Infof("Blocking suspected phishing site "+
				"'%s' for %s", hostname, mac)
			notifyBlockEvent(mac, ipv4, hostname)
			dnsMetrics.blocked.Inc()
		}
	} else if q.Qtype == dns.TypePTR && localAddress(q.Name) {
		hostsMtx.Lock()
		rec, ok := hosts[name]
		hostsMtx.Unlock()

		if ok && rec.rectype == dns.TypePTR &&
			dnsVisibility[ring][rec.hostRing] {

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
	dnsMetrics.responseSize.Observe(float64(m.Len()))
	w.WriteMsg(m)
	logRequest("proxyHandler", start, ipv4, r, m)
}

func dnsUpdateRecord(name, mac, ring, val string, rectype uint16) {
	var newRec *dnsRecord

	if name != "" && val != "" {
		newRec = &dnsRecord{
			name:     name,
			mac:      mac,
			hostRing: ring,
			rectype:  rectype,
			recval:   val,
		}
	}

	// Start by cleaning up any old records for this mac address that don't
	// match the new hostname.
	for x, rec := range hosts {
		if rec.rectype != rectype {
			continue
		}

		if (x == name && newRec == nil) || (x != name && rec.mac == mac) {
			slog.Infof("Deleting %v", hosts[x])
			delete(hosts, x)
		}
	}

	if newRec != nil {
		oldRec := hosts[name]
		if oldRec == nil {
			slog.Infof("Adding %v", newRec)
		} else if !oldRec.Equal(newRec) {
			slog.Infof("Updating %v to %v", oldRec, newRec)
		}
		hosts[name] = newRec
	}
}

// Convert a client's configd info into DNS records
func dnsUpdateClient(mac string, c *cfgapi.ClientInfo) {
	var configName, hostname, ipv4, arpa string
	var err error

	if c.DNSName != "" {
		configName = c.DNSName
	} else {
		configName = c.FriendlyDNS
	}
	name := strings.ToLower(configName)

	if network.ValidDNSName(name) && name != "localhost" && c.IPv4 != nil {
		hostname = name + "." + domainname + "."
		ipv4 = c.IPv4.String()
		clearWarned(ipv4, unknownWarned)

		if arpa, err = dns.ReverseAddr(ipv4); err != nil {
			slog.Warnf("Invalid address %v for %s: %v",
				c.IPv4, name, err)
		}
	}

	hostsMtx.Lock()

	dnsUpdateRecord(hostname, mac, c.Ring, ipv4, dns.TypeA)
	dnsUpdateRecord(arpa, mac, c.Ring, configName+".", dns.TypePTR)

	hostsMtx.Unlock()
}

func updateOneCname(hostname, canonical string) {
	hostname = strings.ToLower(hostname)
	if hostname == "localhost" {
		return
	}

	hostname += "." + domainname + "."
	canonical = canonical + "." + domainname + "."
	slog.Infof("Adding cname %s -> %s", hostname, canonical)

	hostsMtx.Lock()
	hosts[hostname] = &dnsRecord{
		rectype: dns.TypeCNAME,
		recval:  canonical,
	}
	hostsMtx.Unlock()
}

func deleteOneCname(hostname string) {
	hostname = strings.ToLower(hostname) + "." + domainname + "."
	slog.Infof("Deleting cname %s", hostname)

	hostsMtx.Lock()
	delete(hosts, hostname)
	hostsMtx.Unlock()
}

// Iterate over all of the clients.  If a client has a friendly name without a
// matching "DNS-friendly" name, generate a unique DNS name and add it to the
// config tree.  Clear the derived DNS names for any clients that no longer have
// friendly names.
func updateFriendlyNames() {
	updates := make(map[string]string)
	existing := make(map[string]string)
	assigned := make(map[string]string)

	clientMtx.Lock()

	// Build a list of the existing friendly DNS names, verifying that they
	// are all unique and current.
	for mac, c := range clients {
		if c.DNSName != "" {
			assigned[c.DNSName] = mac
		}

		// Make sure any DNS name matches the current friendly name
		if fdns := c.FriendlyDNS; fdns != "" {
			if c.FriendlyName == "" {
				// No friendly name, so the DNS name must be stale
				fdns = ""
			} else {
				// Trim any uniquifying suffix before comparing
				end := len(fdns)
				if dash := strings.Index(fdns, "_"); dash > 0 {
					end = dash
				}

				// If the names don't match, the derived DNS
				// name is stale
				gen := network.GenerateDNSName(c.FriendlyName)
				if gen != fdns[:end] {
					fdns = ""
				}
			}

			if other, ok := existing[fdns]; ok {
				// Two devices derived the same DNS name.  This
				// should never happen.
				slog.Warnf("%s and %s both resolve to %s "+
					"- clearing %s", mac, other, fdns, mac)
				fdns = ""
			}

			if fdns != "" {
				existing[fdns] = mac
			} else {
				updates[mac] = ""
				c.FriendlyDNS = ""
			}
		}
	}

	// Avoid deriving 'localhost', which is our one disallowed DNS name
	assigned["localhost"] = "true"

	// Generate DNS friendly names for clients that need them
	for mac, c := range clients {
		if c.FriendlyName != "" && c.FriendlyDNS == "" {
			base := network.GenerateDNSName(c.FriendlyName)
			fdns := base

			// If the derived name collides with either a derived or
			// manually assigned name, we add a numeric suffix.  We
			// continue incrementing that suffix until we find one
			// that doesn't collide.
			for i := 1; ; i++ {
				if existing[fdns] == "" && assigned[fdns] == "" {
					break
				}
				fdns = base + "_" + strconv.Itoa(i)
			}
			updates[mac] = fdns
			existing[fdns] = mac
		}
	}
	clientMtx.Unlock()

	// Push any changes into the config tree
	for mac, fdns := range updates {
		var err error

		prop := "@/clients/" + mac + "/friendly_dns"
		if fdns == "" {
			slog.Infof("Cleared DNS friendly name for %s", mac)
			err = config.DeleteProp(prop)
		} else {
			slog.Infof("Derived DNS friendly name for %s: %s", mac,
				fdns)
			err = config.CreateProp(prop, fdns, nil)
		}
		if err != nil {
			slog.Warnf("failed to update: %v", err)
		}
	}
}

func initHostMap() {
	clientMtx.Lock()
	for mac, c := range clients {
		if c.Expires == nil || c.Expires.After(time.Now()) {
			dnsUpdateClient(mac, c)
		}
	}
	clientMtx.Unlock()

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

func siteIDChange(path []string, val string, expires *time.Time) {
	slog.Info("restarting due to changed domain")
	os.Exit(0)
}

func initNetwork() {
	var err error

	unknownWarned = make(map[string]time.Time)
	blockWarned = make(map[string]time.Time)

	domainname, err = config.GetDomain()
	if err != nil {
		slog.Fatalf("failed to fetch gateway domain: %v", err)
	}
	domainname = strings.ToLower(domainname)
	config.HandleChange(`^@/siteid`, siteIDChange)

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
	ringRecords = make(map[string]*dnsRecord)
	for name, ring := range rings {
		srouter := network.SubnetRouter(ring.Subnet)
		ringRecords[name] = &dnsRecord{
			hostRing: name,
			rectype:  dns.TypeA,
			recval:   srouter,
		}
		subnets = append(subnets, ring.IPNet)
	}
}

func dnsListener(protocol string) {
	srv := &dns.Server{Addr: ":53", Net: protocol}
	if err := srv.ListenAndServe(); err != nil {
		slog.Fatalf("Failed to start %s listener %v", protocol, err)
	}
}

func dnsMetricsInit() {
	dnsMetrics.requests = bgm.NewCounter("dns4d/requests")
	dnsMetrics.blocked = bgm.NewCounter("dns4d/blocked")
	dnsMetrics.upstreamCnt = bgm.NewCounter("dns4d/upstream_cnt")
	dnsMetrics.upstreamFailures = bgm.NewCounter("dns4d/upstream_failures")
	dnsMetrics.upstreamTimeouts = bgm.NewCounter("dns4d/upstream_timeouts")
	dnsMetrics.upstreamLatency = bgm.NewSummary("dns4d/upstream_latency")
	dnsMetrics.requestSize = bgm.NewSummary("dns4d/request_size")
	dnsMetrics.responseSize = bgm.NewSummary("dns4d/response_size")
	dnsMetrics.cacheSize = bgm.NewGauge("dns4d/cache_size")
	dnsMetrics.cacheEntries = bgm.NewGauge("dns4d/cache_entries")
	dnsMetrics.cacheCollisions = bgm.NewCounter("dns4d/cache_collisions")
	dnsMetrics.cacheLookups = bgm.NewCounter("dns4d/cache_lookups")
	dnsMetrics.cacheHitRate = bgm.NewGauge("dns4d/cache_hitrate")
}

func dnsInit() {
	slog.Info("dns init")
	dnsMetricsInit()

	cachedResponses.init()
	initNetwork()
	initHostMap()
	data.LoadDNSBlocklist(*dataDir)

	dns.HandleFunc(domainname+".", localHandler)
	dns.HandleFunc(".", proxyHandler)

	go updateFriendlyNames()
	go dnsListener("udp")
	go dnsListener("tcp")

	config.HandleChange(`^@/dns/cnames/.*$`, cnameUpdateEvent)
	config.HandleDelete(`^@/dns/cnames/.*$`, cnameDeleteEvent)
	config.HandleChange(`^@/updates/dns_.*list$`, blocklistUpdateEvent)
	config.HandleChange(`^@/network/dnsserver$`, serverUpdateEvent)
}
