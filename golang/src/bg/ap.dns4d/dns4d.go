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
 * Requirements can be installed by invoking $SRC/ap-reqs.bash.
 *
 * Local DNS-specific files are kept in <aproot>/var/spool/dns4d/.
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
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"bg/ap_common/apcfg"
	"bg/ap_common/aputil"
	"bg/ap_common/broker"
	"bg/ap_common/mcp"
	"bg/ap_common/network"
	"bg/base_def"
	"bg/base_msg"
	"bg/data/phishtank"

	"github.com/golang/protobuf/proto"
	"github.com/miekg/dns"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	addr = flag.String("listen-address", base_def.DNSD_PROMETHEUS_PORT,
		"The address to listen on for HTTP requests.")
	brokerd *broker.Broker
	config  *apcfg.APConfig

	latencies = prometheus.NewSummary(prometheus.SummaryOpts{
		Name: "dns_resolve_seconds",
		Help: "DNS query resolution time",
	})

	captiveSubnet *net.IPNet

	// If site's phishing score is below this, send to our IP
	phishThreshold = 0
	phishScorer    phishtank.Scorer

	ringRecords  map[string]dnsRecord // per-ring records for the router
	perRingHosts map[string]bool      // hosts with per-ring results

	domainname    string
	brightgateDNS string
	upstreamDNS   = "8.8.8.8:53"
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
	clients   apcfg.ClientMap
	warned    map[string]bool

	hostsMtx sync.Mutex
	hosts    map[string]dnsRecord
)

const pname = "ap.dns4d"

type dnsRecord struct {
	rectype uint16
	recval  string
	expires *time.Time
}

func dnsUpdate(resource *base_msg.EventNetResource) {
	action := resource.GetAction()
	ipv4 := network.Uint32ToIPAddr(*resource.Ipv4Address)
	ttl := time.Duration(resource.GetDuration()) * time.Second
	expires := time.Now().Add(ttl)

	arpa, err := dns.ReverseAddr(ipv4.String())
	if err != nil {
		log.Printf("Invalid IP address %s: %v\n", ipv4, err)
		return
	}

	hostsMtx.Lock()
	if action == base_msg.EventNetResource_CLAIMED {
		hosts[arpa] = dnsRecord{
			rectype: dns.TypePTR,
			recval:  resource.GetHostname() + ".",
			expires: &expires,
		}
	} else if action == base_msg.EventNetResource_RELEASED {
		delete(hosts, arpa)
	}
	hostsMtx.Unlock()
}

func clientUpdateEvent(path []string, val string, expires *time.Time) {
	if len(path) != 3 {
		// All updates should affect /clients/<macaddr>/property
		return
	}

	mac := path[1]
	new := config.GetClient(mac)
	if new == nil {
		log.Printf("Got update for nonexistent client: %s\n", mac)
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
func cnameUpdateEvent(path []string, val string, expires *time.Time) {
	updateOneCname(path[2], val)
}

func cnameDeleteEvent(path []string) {
	log.Printf("cname delete %s\n", path[2])
	deleteOneCname(path[2])
}

func resourceEvent(event []byte) {
	resource := &base_msg.EventNetResource{}
	proto.Unmarshal(event, resource)
	dnsUpdate(resource)
}

func logRequest(handler string, start time.Time, ip net.IP, r, m *dns.Msg) {
	latencies.Observe(time.Since(start).Seconds())

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

	err := brokerd.Publish(entity, base_def.TOPIC_REQUEST)
	if err != nil {
		log.Println(err)
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
func getClient(w dns.ResponseWriter) *apcfg.ClientInfo {
	addr, ok := w.RemoteAddr().(*net.UDPAddr)
	if !ok {
		return nil
	}

	clientMtx.Lock()
	defer clientMtx.Unlock()

	for _, c := range clients {
		if addr.IP.Equal(c.IPv4) {
			return c
		}
	}

	ipStr := addr.IP.String()
	if !warned[ipStr] {
		warned[ipStr] = logUnknown(ipStr)
	}

	return nil
}

// Look through the client table to find the mac address corresponding to this
// client record.
func getMac(record *apcfg.ClientInfo) net.HardwareAddr {
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

func captiveHandler(client *apcfg.ClientInfo, w dns.ResponseWriter, r *dns.Msg) {
	record, _ := ringRecords[client.Ring]

	m := new(dns.Msg)
	m.SetReply(r)
	m.Authoritative = true
	start := time.Now()
	for _, q := range r.Question {
		if q.Qtype == dns.TypeA {
			m.Answer = append(m.Answer, answerA(q, record))
		}
	}
	w.WriteMsg(m)

	logRequest("captiveHandler", start, client.IPv4, r, m)
}

func upstreamRequest(server string, r, m *dns.Msg) {
	c := new(dns.Client)

	r2, _, err := c.Exchange(r, server)
	if err != nil || r2 == nil {
		log.Printf("failed to exchange: %v", err)
		m.Rcode = dns.RcodeServerFailure
		return
	}

	// Copy the flags from the message header
	m.Compress = r2.Compress
	m.Authoritative = r2.Authoritative
	m.Truncated = r2.Truncated
	m.RecursionDesired = r2.RecursionDesired
	m.RecursionAvailable = r2.RecursionAvailable
	m.Rcode = r2.Rcode
	if r2.Rcode != dns.RcodeSuccess {
		log.Printf("failed to get a valid answer to query: %v\n", r)
		log.Printf("  response: %v\n", r2)
	} else {
		// We've received a valid answer from the upstream DNS server.
		// Copy the different 'answer' fields into our forwarded reply
		// message.
		m.Answer = append(m.Answer, r2.Answer...)
		m.Ns = append(m.Ns, r2.Ns...)
		m.Extra = append(m.Extra, r2.Extra...)
	}
}

func localHandler(w dns.ResponseWriter, r *dns.Msg) {
	c := getClient(w)
	if c == nil {
		return
	} else if c.Ring == "setup" {
		captiveHandler(c, w, r)
		return
	}

	m := new(dns.Msg)
	m.SetReply(r)
	m.Authoritative = true

	start := time.Now()
	for _, q := range r.Question {
		var rec dnsRecord
		var ok bool

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
	}
	w.WriteMsg(m)

	logRequest("localHandler", start, c.IPv4, r, m)
}

func notifyBlockEvent(c *apcfg.ClientInfo, hostname string) {
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
		Hostname:    &hostname,
		MacAddress:  proto.Uint64(network.HWAddrToUint64(dev)),
		Ipv4Address: proto.Uint32(network.IPAddrToUint32(c.IPv4)),
	}

	if err := brokerd.Publish(entity, topic); err != nil {
		log.Printf("couldn't publish %s (%v): %v\n", topic, entity, err)
	}
}

func proxyHandler(w dns.ResponseWriter, r *dns.Msg) {
	c := getClient(w)
	if c == nil {
		return
	} else if c.Ring == "setup" {
		captiveHandler(c, w, r)
		return
	}

	m := new(dns.Msg)
	m.SetReply(r)
	m.Authoritative = false

	start := time.Now()
	for _, q := range r.Question {
		hostname := q.Name[:len(q.Name)-1]
		if phishScorer.Score(hostname, phishtank.Dns) < phishThreshold {
			// Are any of the questions in our phishing database?
			// If so, return our IP address; for the HTTP and HTTPS
			// cases, we can display a "no phishing" page.
			//
			// XXX: maybe we should return a CNAME record for our
			// local 'phishing.<siteid>.brightgate.net'?
			localRecord, _ := ringRecords[c.Ring]
			log.Printf("phish threshold crossed, ring %s, returning %v\n",
				c.Ring, localRecord)
			m.Answer = append(m.Answer, answerA(q, localRecord))
			notifyBlockEvent(c, hostname)
		} else if strings.Contains(q.Name, ".in-addr.arpa.") {
			hostsMtx.Lock()
			rec, ok := hosts[q.Name]
			hostsMtx.Unlock()
			if ok && rec.rectype == dns.TypePTR {
				m.Answer = append(m.Answer, answerPTR(q, rec))
			}

		} else {
			upstreamRequest(upstreamDNS, r, m)
		}
	}
	w.WriteMsg(m)
	logRequest("proxyHandler", start, c.IPv4, r, m)
}

func deleteOneClient(c *apcfg.ClientInfo) {
	if c.IPv4 == nil {
		return
	}
	ipv4 := c.IPv4.String()

	delete(warned, ipv4)

	hostsMtx.Lock()
	if arpa, err := dns.ReverseAddr(ipv4); err == nil {
		log.Printf("Deleting PTR record %s->%s\n", arpa, c.DHCPName)
		delete(hosts, arpa)
	}

	for addr, rec := range hosts {
		if rec.rectype == dns.TypeA && rec.recval == ipv4 {
			log.Printf("Deleting A record %s->%s\n", addr, ipv4)
			delete(hosts, addr)
		}
	}
	hostsMtx.Unlock()
}

// Convert a client's configd info into DNS records
func updateOneClient(c *apcfg.ClientInfo) {
	if c.IPv4 == nil {
		return
	}

	ipv4 := c.IPv4.String()
	delete(warned, ipv4)
	hostsMtx.Lock()
	if c.DNSName != "" {
		hostname := c.DNSName + "." + domainname + "."

		log.Printf("Adding A record %s->%s\n", hostname, ipv4)
		hosts[hostname] = dnsRecord{
			rectype: dns.TypeA,
			recval:  ipv4,
			expires: c.Expires,
		}
	}

	if c.DHCPName != "" {
		arpa, err := dns.ReverseAddr(ipv4)
		if err != nil {
			log.Printf("Invalid address %v for %s: %v\n",
				c.IPv4, c.DNSName, err)
		} else {
			log.Printf("Adding PTR record %s->%s\n", arpa,
				c.DHCPName)
			hosts[arpa] = dnsRecord{
				rectype: dns.TypePTR,
				recval:  c.DHCPName,
				expires: c.Expires,
			}
		}
	}
	hostsMtx.Unlock()
}

func updateOneCname(hostname, canonical string) {
	hostname = hostname + "." + domainname + "."
	canonical = canonical + "." + domainname + "."
	log.Printf("cname update %s -> %s\n", hostname, canonical)

	hostsMtx.Lock()
	hosts[hostname] = dnsRecord{
		rectype: dns.TypeCNAME,
		recval:  canonical,
	}
	hostsMtx.Unlock()
}

func deleteOneCname(hostname string) {
	hostname = hostname + "." + domainname + "."
	log.Printf("cname delete %s\n", hostname)

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
		for _, c := range cnames.Children {
			updateOneCname(c.Name, c.Value)
		}
	}

	perRingHosts = make(map[string]bool)
	hostnames := [...]string{"gateway", "phishing", "malware", "captive"}
	for _, name := range hostnames {
		perRingHosts[name+"."+domainname+"."] = true
	}
}

func getNameserver() string {
	// Get the nameserver address from configd
	tmp, _ := config.GetProp("@/network/dnsserver")
	if tmp == "" {
		return ""
	}

	// Attempt to split the address into <ip>:<port>
	comp := strings.Split(tmp, ":")
	if len(comp) < 1 || len(comp) > 2 {
		goto errout
	}

	// Verify that that IP address is legal
	if ip := net.ParseIP(comp[0]); ip == nil {
		goto errout
	}

	// If the address didn't include a port number, append the standard port
	if len(comp) == 1 {
		tmp += ":53"
	}

	return tmp

errout:
	log.Printf("Invalid nameserver: %s\n", tmp)
	return ""
}

func initNetwork() {
	var err error

	warned = make(map[string]bool)

	siteid, err := config.GetProp("@/siteid")
	if err != nil {
		log.Printf("failed to fetch siteid: %v\n", err)
		siteid = "0000"
	}
	domainname = siteid + ".brightgate.net"

	if tmp := getNameserver(); tmp != "" {
		upstreamDNS = tmp
	}
	log.Printf("Using nameserver: %s\n", upstreamDNS)

	rings := config.GetRings()
	if rings == nil {
		log.Fatalf("Can't retrieve ring information\n")
	} else {
		log.Printf("defined rings %v\n", rings)
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
	}
}

// loadPhishtank sets the global phishScorer to score how reliable a domain is
func loadPhishtank() {
	antiphishing := aputil.ExpandDirPath("/var/spool/antiphishing/")

	reader := phishtank.NewReader(
		phishtank.Whitelist(antiphishing+"whitelist.csv"),
		phishtank.Phishtank(antiphishing+"phishtank.csv"),
		phishtank.MDL(antiphishing+"mdl.csv"),
		phishtank.Generic(antiphishing+"example_blacklist.csv", -3, 1))
	phishScorer = reader.Scorer(phishtank.Dns)
}

func init() {
	prometheus.MustRegister(latencies)
}

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	mcpd, err := mcp.New(pname)
	if err != nil {
		log.Printf("cannot connect to mcp\n")
	}

	// Need to have certain network capabilities.
	// priv_net_bind_service = prctl.cap_effective.net_bind_service
	// if not priv_net_bind_service:
	//     logging.warning("require CAP_NET_BIND_SERVICE to bind DHCP server port")
	//     sys.exit(1)

	// XXX configuration retrieval

	flag.Usage = func() {
		flag.PrintDefaults()
	}
	flag.Parse()

	http.Handle("/metrics", promhttp.Handler())
	go http.ListenAndServe(*addr, nil)

	brokerd = broker.New(pname)
	brokerd.Handle(base_def.TOPIC_RESOURCE, resourceEvent)
	defer brokerd.Fini()

	config, err = apcfg.NewConfig(brokerd, pname)
	if err != nil {
		log.Fatalf("cannot connect to configd: %v\n", err)
	}
	config.HandleChange(`^@/clients/.*/(ipv4|dns_name|dhcp_name|ring)$`,
		clientUpdateEvent)
	config.HandleDelete(`^@/clients/.*`, clientDeleteEvent)
	config.HandleExpire(`^@/clients/.*/(ipv4|dns_name)$`, clientDeleteEvent)

	config.HandleChange(`^@/dns/cnames/.*$`, cnameUpdateEvent)
	config.HandleDelete(`^@/dns/cnames/.*$`, cnameDeleteEvent)

	hosts = make(map[string]dnsRecord)
	initNetwork()
	initHostMap()
	loadPhishtank()

	dns.HandleFunc(domainname+".", localHandler)
	dns.HandleFunc(".", proxyHandler)

	if mcpd != nil {
		mcpd.SetState(mcp.ONLINE)
	}

	go func() {
		srv := &dns.Server{Addr: ":53", Net: "udp"}
		if err := srv.ListenAndServe(); err != nil {
			log.Fatalf("Failed to set udp listener %s\n", err.Error())
		}
	}()

	log.Println("udp dns listener routine launched")

	go func() {
		srv := &dns.Server{Addr: ":53", Net: "tcp"}
		if err := srv.ListenAndServe(); err != nil {
			log.Fatalf("Failed to set tcp listener %s\n", err.Error())
		}
	}()

	log.Println("tcp dns listener routine launched")

	sig := make(chan os.Signal)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	s := <-sig
	log.Fatalf("Signal (%v) received, stopping\n", s)
}
