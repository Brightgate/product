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
 * XXX Need to handle RFC 2606 (reserved gTLDs that should be intercepted)
 * and RFC 7686 (.onion TLD that should be logged).
 *
 * XXX This implementation may be suitable to run both IPv4 and IPv6
 * servers in the same process.
 */

// Ubuntu: requires libzmq3-dev, which is 0MQ 4.2.1.
// XXX Exception messages are not displayed.

package main

import (
	"data/phishtank"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"ap_common"
	"ap_common/mcp"
	"ap_common/network"
	"base_def"
	"base_msg"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/golang/protobuf/proto"

	"github.com/miekg/dns"
)

var (
	addr = flag.String("listen-address", base_def.DNSD_PROMETHEUS_PORT,
		"The address to listen on for HTTP requests.")
	port         = flag.Int("port", 53, "port to run on")
	tsig         = flag.String("tsig", "", "use MD5 hmac tsig: keyname:base64")
	upstream_dns = flag.String("upstream-dns", "8.8.8.8:53",
		"The upstream DNS server to use.")
	broker    ap_common.Broker
	phishdata *phishtank.DataSource

	latencies = prometheus.NewSummary(prometheus.SummaryOpts{
		Name: "dns_resolve_seconds",
		Help: "DNS query resolution time",
	})

	client_map_mtx sync.Mutex
	client_map     map[string]int64

	hosts_mtx sync.Mutex
)

const pname = "ap.dns4d"

// Terminate dom with '.'.
const dom = "blueslugs.com."

type dns_record struct {
	rectype uint16
	recval  string
}

/*
 * Although hosts[] is seeded with A and CNAME records from the configuration,
 * PTR records are currently added dynamically as we receive net.resource events
 * from ap.dhcp4d. Access to hosts[] is protected by hosts_mtx, as we need to
 * update the record based on event subscriptions while potentially serving
 * records from either local_handler() or proxy_handler().
 */
var hosts = map[string]dns_record{
	"a-gw.blueslugs.com.":                {dns.TypeA, "192.168.135.1"},
	"s-media.blueslugs.com.":             {dns.TypeA, "192.168.135.4"},
	"w-media.blueslugs.com.":             {dns.TypeCNAME, "s-media.blueslugs.com."},
	"s-cooler.blueslugs.com.":            {dns.TypeA, "192.168.135.5"},
	"p-inky.blueslugs.com.":              {dns.TypeA, "192.168.135.6"},
	"inky.blueslugs.com.":                {dns.TypeCNAME, "p-inky.blueslugs.com."},
	"a-sprinkles.blueslugs.com.":         {dns.TypeA, "192.168.135.19"},
	"a-mfi-outdoor-front.blueslugs.com.": {dns.TypeA, "192.168.135.20"},
	"mfi-outdoor-front.blueslugs.com.":   {dns.TypeCNAME, "a-mfi-outdoor-front.blueslugs.com."},
	"a-mfi-office.blueslugs.com.":        {dns.TypeA, "192.168.135.21"},
	"mfi-office.blueslugs.com.":          {dns.TypeCNAME, "mfi-office.blueslugs.com."},
	"a-berry-clock.blueslugs.com.":       {dns.TypeA, "192.168.135.29"},
	"a-tivo.blueslugs.com.":              {dns.TypeA, "192.168.135.30"},
	"a-tivo-stream.blueslugs.com.":       {dns.TypeA, "192.168.135.31"},
	"pckts2.blueslugs.com.":              {dns.TypeA, "192.168.135.139"},
	"w-tappy.blueslugs.com.":             {dns.TypeA, "192.168.135.248"},
	"w-pi3.blueslugs.com.":               {dns.TypeA, "192.168.135.248"},
	"s-pi.blueslugs.com.":                {dns.TypeA, "192.168.135.249"},
	"pidora.blueslugs.com.":              {dns.TypeCNAME, "pidora.blueslugs.com."},
	"s-deb.blueslugs.com.":               {dns.TypeA, "192.168.135.251"},
	"debian.blueslugs.com.":              {dns.TypeCNAME, "s-deb.blueslugs.com."},
	"i1.blueslugs.com.":                  {dns.TypeCNAME, "s-deb.blueslugs.com."},
	"s-cent.blueslugs.com.":              {dns.TypeA, "192.168.135.252"},
	"centos.blueslugs.com.":              {dns.TypeCNAME, "s-cent.blueslugs.com."},
	"phab.blueslugs.com.":                {dns.TypeCNAME, "s-cent.blueslugs.com."},
	"s-smart.blueslugs.com.":             {dns.TypeA, "192.168.135.253"},
	"smartos.blueslugs.com.":             {dns.TypeCNAME, "s-smart.blueslugs.com."},
	"s-vm.blueslugs.com.":                {dns.TypeA, "192.168.135.254"},
}

func dns_update(resource *base_msg.EventNetResource) {
	action := resource.GetAction()
	ipv4 := network.Uint32ToIPAddr(*resource.Ipv4Address)

	arpa, err := dns.ReverseAddr(ipv4.String())
	if err != nil {
		log.Println(err)
		return
	}

	hosts_mtx.Lock()
	if action == base_msg.EventNetResource_CLAIMED {
		hosts[arpa] = dns_record{
			rectype: dns.TypePTR,
			recval:  resource.GetDnsName() + ".",
		}
	} else if action == base_msg.EventNetResource_RELEASED {
		delete(hosts, arpa)
	}
	hosts_mtx.Unlock()
}

func config_changed(event []byte) {
	config := &base_msg.EventConfig{}
	proto.Unmarshal(event, config)
}

func resource_changed(event []byte) {
	resource := &base_msg.EventNetResource{}
	proto.Unmarshal(event, resource)
	dns_update(resource)
}

// service_config = {
//     "mode": "proxy",      # "passthrough"?
// }
//
// # Site 1
// net_config = {
//     "gw": "192.168.135.1",
//     "domain": "blueslugs.com",
//     # XXX These should become URLs per RFC 4501.
//     "ns": ["208.67.222.222", "208.67.220.220"],
//     # 36 - 48
//     "ranges": {
//         "trusted": ("192.168.135.36", 6),
//         "untrusted": ("192.168.135.42", 3),
//         "quarantined": ("192.168.135.45", 3)
//     }
// }
//
// # Site 2
// # net_config = {
// #         "gw": "192.168.247.1",
// #         "domain": "blueslugs.com",
// #         "ns": ["208.67.222.222", "208.67.220.220"],
// #         "ranges": {
// #             "trusted": ("192.168.247.24", 16),
// #             "untrusted": ("192.168.247.40", 8),
// #             "quarantined": ("192.168.247.48", 4)
// #         }
// # }
// # trusted = IPv4Range("trusted", "192.168.247.64", 32)
// # untrusted = IPv4Range("untrusted", "192.168.247.128", 32)
//
//         # XXX Database schema
//         # XXX Configuration event for table updates

func record_client(ipstr string) {
	host, _, _ := net.SplitHostPort(ipstr)
	if host == "" {
		log.Printf("empty host from '%s'\n", ipstr)
		return
	}

	client_map_mtx.Lock()
	client_map[host] = client_map[host] + 1
	log.Printf("client %s, map[client] %d\n", host, client_map[host])

	if client_map[host] == 1 {
		t := time.Now()

		addr := net.ParseIP(host).To4()

		entity := &base_msg.EventNetEntity{
			Timestamp: &base_msg.Timestamp{
				Seconds: proto.Int64(t.Unix()),
				Nanos:   proto.Int32(int32(t.Nanosecond())),
			},
			Sender:      proto.String(broker.Name),
			Debug:       proto.String("-"),
			Ipv4Address: proto.Uint32(network.IPAddrToUint32(addr)),
		}

		err := broker.Publish(entity, base_def.TOPIC_ENTITY)
		if err != nil {
			log.Println(err)
		}
	}
	client_map_mtx.Unlock()
}

func local_handler(w dns.ResponseWriter, r *dns.Msg) {
	var (
		a  net.IP
		rr dns.RR
	)

	lt := time.Now()

	m := new(dns.Msg)
	m.SetReply(r)

	// XXX We will need the remote client address once we
	// are ready to give different answers to different
	// askers.
	ip, _ := w.RemoteAddr().(*net.UDPAddr)
	record_client(ip.String())

	// Iterate through questions.
	for _, question := range r.Question {
		hosts_mtx.Lock()
		rec, rec_ok := hosts[question.Name]
		hosts_mtx.Unlock()
		if rec_ok {
			if rec.rectype == dns.TypeA {
				a = net.ParseIP(rec.recval)
				rr = &dns.A{
					Hdr: dns.RR_Header{Name: question.Name,
						Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 0},
					A: a.To4(),
				}
			} else if rec.rectype == dns.TypeCNAME {
				rr = &dns.CNAME{
					Hdr: dns.RR_Header{Name: question.Name,
						Rrtype: dns.TypeCNAME,
						Class:  dns.ClassINET, Ttl: 0},
					Target: rec.recval,
				}
			}

			m.Authoritative = true
			m.Answer = append(m.Answer, rr)
		} else {
			// Proxy needed if we have decided that
			// we are allowing our domain to be
			// handled upstream as well.

			// We are assuming we cannot find records from
			// upstream in our phishing data.  Otherwise we
			// would have to perform the phishing check
			// here.
			q := new(dns.Msg)
			q.MsgHdr = r.MsgHdr
			q.Question = append(q.Question, question)

			c := new(dns.Client)
			// XXX Upstream DNS server config.
			r2, _, err := c.Exchange(q, *upstream_dns)
			if err != nil {
				log.Printf("failed to exchange: %v", err)
				// XXX At this point, r2 is empty or
				// bad, because of a network error.
				// If it's an I/O timeout, do we retry?
			} else {
				if r2 != nil && r2.Rcode != dns.RcodeSuccess {
					log.Printf("failed to get an valid answer\n%v", r)
					// XXX At this point, r2 represents a
					// DNS error.
				}

				log.Printf("bs proxy response %s\n", r2)

				m.Authoritative = false
				for _, answer := range r2.Answer {
					m.Answer = append(m.Answer, answer)
				}
			}
		}
	}

	w.WriteMsg(m)

	latencies.Observe(time.Since(lt).Seconds())
	t := time.Now()

	host, _, _ := net.SplitHostPort(ip.String())
	addr := net.ParseIP(host).To4()

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
		Sender:       proto.String(broker.Name),
		Debug:        proto.String("local_handler"),
		Requestor:    proto.String(addr.String()),
		IdentityUuid: proto.String(base_def.ZERO_UUID),
		Protocol:     &protocol,
		Request:      requests,
		Response:     responses,
	}

	err := broker.Publish(entity, base_def.TOPIC_REQUEST)
	if err != nil {
		log.Println(err)
	}

	log.Printf("bs handle complete {} %s\n", m)
}

func proxy_handler(w dns.ResponseWriter, r *dns.Msg) {
	lt := time.Now()

	ip, _ := w.RemoteAddr().(*net.UDPAddr)
	record_client(ip.String())

	m := new(dns.Msg)
	m.SetReply(r)
	m.Authoritative = false

	/*
	 * Are any of the questions in our phishing database?  If so, return
	 * our IP address; for the HTTP and HTTPS cases, we can display a "no
	 * phishing" page.
	 */
	for _, question := range r.Question {
		if phishdata.KnownToDataSource(question.Name[:len(question.Name)-1]) {
			m.Answer = append(m.Answer, &dns.A{
				Hdr: dns.RR_Header{Name: question.Name,
					Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 0},
				// XXX Following is our gateway's IP
				A: net.IP{192, 168, 136, 1}.To4(),
			})
		} else if strings.Contains(question.Name, ".in-addr.arpa.") {
			hosts_mtx.Lock()
			rec, rec_ok := hosts[question.Name]
			hosts_mtx.Unlock()
			if rec_ok && rec.rectype == dns.TypePTR {
				m.Answer = append(m.Answer, &dns.PTR{
					Hdr: dns.RR_Header{Name: question.Name,
						Rrtype: dns.TypePTR, Class: dns.ClassINET, Ttl: 0},
					Ptr: rec.recval,
				})
			} else {
				log.Printf("unhandled arpa DNS query: %v", question)
			}
		} else {

			c := new(dns.Client)
			// XXX Upstream DNS server config.
			r2, _, err := c.Exchange(r, *upstream_dns)
			if err != nil {
				log.Printf("failed to exchange: %v", err)
			} else {
				if r2 != nil && r2.Rcode != dns.RcodeSuccess {
					log.Printf("failed to get an valid answer\n%v", r)
				}

				m.Answer = append(m.Answer, r2.Answer...)
			}
		}
	}

	w.WriteMsg(m)

	latencies.Observe(time.Since(lt).Seconds())
	t := time.Now()

	host, _, _ := net.SplitHostPort(ip.String())
	addr := net.ParseIP(host).To4()

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
		Sender:       proto.String(broker.Name),
		Debug:        proto.String("proxy_handler"),
		Requestor:    proto.String(addr.String()),
		IdentityUuid: proto.String(base_def.ZERO_UUID),
		Protocol:     &protocol,
		Request:      requests,
		Response:     responses,
	}

	err := broker.Publish(entity, base_def.TOPIC_REQUEST)
	if err != nil {
		log.Println(err)
	}
}

func init() {
	prometheus.MustRegister(latencies)
}

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	mcp, err := mcp.New(pname)
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

	log.Println("cli flags parsed")

	// RESOLVE_TIME = promc.Summary("dns_resolve_seconds",
	//                              "DNS query resolution time")

	http.Handle("/metrics", promhttp.Handler())
	go http.ListenAndServe(*addr, nil)

	log.Println("prometheus client launched")

	broker.Init(pname)
	broker.Handle(base_def.TOPIC_CONFIG, config_changed)
	broker.Handle(base_def.TOPIC_RESOURCE, resource_changed)
	broker.Connect()
	defer broker.Disconnect()

	log.Println("message bus listener routine launched")

	time.Sleep(time.Second)
	broker.Ping()

	client_map = make(map[string]int64)

	// load the phishtank
	log.Printf("phishdata %v", phishdata)
	phishdata = &phishtank.DataSource{}
	phishdata.Loader("online-valid-test.csv")
	// phishdata.AutoLoader("online-valid.csv", time.Hour)
	// ^^ uncomment to autoupdate with real phish data, also change in httpd
	log.Printf("phishdata %v", phishdata)

	dns.HandleFunc("blueslugs.com.", local_handler)
	dns.HandleFunc(".", proxy_handler)

	if mcp != nil {
		mcp.SetStatus("online")
	}
	go func() {
		srv := &dns.Server{Addr: ":" + strconv.Itoa(*port), Net: "udp"}
		if err := srv.ListenAndServe(); err != nil {
			log.Fatalf("Failed to set udp listener %s\n", err.Error())
		}
	}()

	log.Println("udp dns listener routine launched")

	go func() {
		srv := &dns.Server{Addr: ":" + strconv.Itoa(*port), Net: "tcp"}
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

// # XXX retrieve my server address
// #   XXX one service instance per separate network?
//
// # XXX network configurations
// # XXX quarantine range
// # XXX trusted dns, untrusted dns
// # XXX event log: host discovery from DNS request
//
//
//     def __init__(self, address_list, event_channels, timeout=120):
//         # address_list is a list of IPAddresses
//         self.address_list = address_list
//         self.address = address_list[0]
//         self.port = 53 # XXX Less general
//         self.timeout = timeout
//
//         self.clients = []
//
//         # event_channels should be the set of channels the app needs
//         self.event_channels = event_channels
//
//         self.st_requests = 0
//         self.st_local_responses = 0
//         self.st_local_not_founds = 0
//         self.st_remote_not_founds = 0
//         # XXX timestamp based statistics?
//
//     @RESOLVE_TIME.time()
//     def resolve(self, request, handler):
