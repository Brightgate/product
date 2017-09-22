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

	"ap_common/apcfg"
	"ap_common/broker"
	"ap_common/mcp"
	"ap_common/network"
	"base_def"
	"base_msg"
	"data/phishtank"

	"github.com/golang/protobuf/proto"
	"github.com/miekg/dns"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	aproot = flag.String("root", "proto.armv7l/opt/com.brightgate",
		"Root of AP installation")
	addr = flag.String("listen-address", base_def.DNSD_PROMETHEUS_PORT,
		"The address to listen on for HTTP requests.")
	brokerd broker.Broker
	config  *apcfg.APConfig

	latencies = prometheus.NewSummary(prometheus.SummaryOpts{
		Name: "dns_resolve_seconds",
		Help: "DNS query resolution time",
	})

	captiveSubnet *net.IPNet

	phishScorer phishtank.Scorer

	ringRecords map[string]*dnsRecord

	domainname   string
	upstream_dns = "8.8.8.8:53"
	// If site's phishing score is below this, send to our IP
	phishThreshold = 0
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

func dns_update(resource *base_msg.EventNetResource) {
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

// These are the per-client property changes that affect us
var relevantProperties = map[string]bool{
	"ipv4":      true,
	"dns_name":  true,
	"dhcp_name": true,
	"ring":      true,
}

func configEvent(raw []byte) {
	var old, new *apcfg.ClientInfo

	event := &base_msg.EventConfig{}
	proto.Unmarshal(raw, event)

	// Ignore messages without an explicit type
	if event.Type == nil {
		return
	}
	etype := *event.Type
	property := *event.Property
	path := strings.Split(property[2:], "/")

	// Ignore any config changes unrelated to clients
	if len(path) < 2 || path[0] != "clients" {
		return
	}

	macaddr := path[1]

	// The only whole-client update we would react to (or expect to see, for
	// that matter) is a delete
	if len(path) == 2 {
		if etype != base_msg.EventConfig_DELETE {
			return
		}
	} else {
		// Check to see if this is a property change that we care about
		if r, _ := relevantProperties[path[2]]; !r {
			return
		}

		if etype == base_msg.EventConfig_CHANGE {
			new = config.GetClient(macaddr)
		}
	}

	clientMtx.Lock()
	old = clients[macaddr]
	if old != nil {
		delete(clients, macaddr)
		deleteOneClient(old)
	}
	if new != nil {
		updateOneClient(new)
		clients[macaddr] = new
	}
	clientMtx.Unlock()
}

func resourceEvent(event []byte) {
	resource := &base_msg.EventNetResource{}
	proto.Unmarshal(event, resource)
	dns_update(resource)
}

func logRequest(handler string, start time.Time, ip net.IP, r, m *dns.Msg) {
	latencies.Observe(time.Since(start).Seconds())

	t := time.Now()
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
		Timestamp: &base_msg.Timestamp{
			Seconds: proto.Int64(t.Unix()),
			Nanos:   proto.Int32(int32(t.Nanosecond())),
		},
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

	t := time.Now()
	entity := &base_msg.EventNetEntity{
		Timestamp: &base_msg.Timestamp{
			Seconds: proto.Int64(t.Unix()),
			Nanos:   proto.Int32(int32(t.Nanosecond())),
		},
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
			m.Answer = append(m.Answer, answerA(q, *record))
		}
	}
	w.WriteMsg(m)

	logRequest("captiveHandler", start, client.IPv4, r, m)
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

		hostsMtx.Lock()
		rec, rec_ok := hosts[q.Name]
		hostsMtx.Unlock()

		if rec_ok {
			if rec.rectype == dns.TypeA {
				m.Answer = append(m.Answer, answerA(q, rec))
			} else if rec.rectype == dns.TypeCNAME {
				m.Answer = append(m.Answer, answerCNAME(q, rec))
			}
			continue
		}

		// Proxy needed if we have decided that we are allowing
		// our domain to be handled upstream as well.
		pq := new(dns.Msg)
		pq.MsgHdr = r.MsgHdr
		pq.Question = append(pq.Question, q)

		c := new(dns.Client)
		r2, _, err := c.Exchange(pq, upstream_dns)
		if err != nil {
			log.Printf("failed to exchange: %v", err)
		} else if r2 != nil && r2.Rcode != dns.RcodeSuccess {
			log.Printf("failed to get an valid answer\n%v", r)
		} else {
			log.Printf("%s proxy response %s\n",
				domainname, r2)

			m.Authoritative = false
			m.Answer = append(m.Answer, r2.Answer...)
		}
	}
	w.WriteMsg(m)

	logRequest("localHandler", start, c.IPv4, r, m)
}

func proxyHandler(w dns.ResponseWriter, r *dns.Msg) {
	c := getClient(w)
	if c == nil {
		return
	} else if c.Ring == "setup" {
		captiveHandler(c, w, r)
		return
	}
	record, _ := ringRecords[c.Ring]

	m := new(dns.Msg)
	m.SetReply(r)
	m.Authoritative = false

	start := time.Now()
	for _, q := range r.Question {
		if phishScorer.Score(
			q.Name[:len(q.Name)-1], phishtank.Dns) < phishThreshold {
			// Are any of the questions in our phishing database?
			// If so, return our IP address; for the HTTP and HTTPS
			// cases, we can display a "no phishing" page.
			log.Printf("phish threshold crossed, ring %s, returning %v\n",
				c.Ring, *record)
			m.Answer = append(m.Answer, answerA(q, *record))
		} else if strings.Contains(q.Name, ".in-addr.arpa.") {
			hostsMtx.Lock()
			rec, rec_ok := hosts[q.Name]
			hostsMtx.Unlock()
			if rec_ok && rec.rectype == dns.TypePTR {
				m.Answer = append(m.Answer, answerPTR(q, rec))
			}

		} else {
			c := new(dns.Client)

			r2, _, err := c.Exchange(r, upstream_dns)
			if err != nil {
				log.Printf("failed to exchange: %v", err)
			} else if r2 != nil && r2.Rcode != dns.RcodeSuccess {
				log.Printf("failed to get an valid answer\n%v", r)
			} else {
				m.Answer = append(m.Answer, r2.Answer...)
			}
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
		hostname := c.DNSName + "."
		if domainname != "" {
			hostname += domainname + "."
		}
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

func initHostMap() {
	clients = config.GetClients()
	for _, c := range clients {
		if c.Expires == nil || c.Expires.After(time.Now()) {
			updateOneClient(c)
		}
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
	warned = make(map[string]bool)

	if config = apcfg.NewConfig(pname); config == nil {
		log.Fatalf("Can't connect to the config daemon\n")
	}

	domainname, _ = config.GetProp("@/network/domainname")
	if tmp := getNameserver(); tmp != "" {
		upstream_dns = tmp
	}
	log.Printf("Using nameserver: %s\n", upstream_dns)

	rings := config.GetRings()
	if rings == nil {
		log.Fatalf("Can't retrieve ring information\n")
	} else {
		log.Printf("defined rings %v\n", rings)
	}

	subnets := config.GetSubnets()
	if subnets == nil {
		log.Fatalf("Can't retrieve subnet information\n")
	} else {
		log.Printf("defined subnets %v\n", subnets)
	}

	// Each ring will have an A record for that ring's router.  That
	// record will double as a result for phishing URLs and all captive
	// portal requests.
	ringRecords = make(map[string]*dnsRecord)
	for name, ring := range rings {
		subnet, ok := subnets[ring.Interface]
		if !ok {
			log.Printf("No subnet for %s/%s\n", name,
				ring.Interface)
			continue
		}

		srouter := network.SubnetRouter(subnet)
		log.Printf("add DNS A pointer to %s for %s/%s\n", srouter,
			name, ring.Interface)
		ringRecords[name] = &dnsRecord{
			rectype: dns.TypeA,
			recval:  srouter,
		}
	}
}

// loadPhishtank sets the global phishScorer to score how reliable a domain is
func loadPhishtank() {
	antiphishing := *aproot + "/var/spool/antiphishing/"

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
		log.Printf("Failed to connect to mcp\n")
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

	log.Println("prometheus client launched")

	brokerd.Init(pname)
	brokerd.Handle(base_def.TOPIC_CONFIG, configEvent)
	brokerd.Handle(base_def.TOPIC_RESOURCE, resourceEvent)
	brokerd.Connect()
	defer brokerd.Disconnect()
	brokerd.Ping()

	config = apcfg.NewConfig(pname)
	hosts = make(map[string]dnsRecord)
	initNetwork()
	initHostMap()
	loadPhishtank()

	if domainname != "" {
		dns.HandleFunc(domainname+".", localHandler)
	}
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
