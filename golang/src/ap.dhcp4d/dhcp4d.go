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
 * DHCPv4 daemon
 */

// XXX Exception messages are not displayed.
// XXX Hardcoded to wlan0.

package main

import (
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"ap_common"
	"base_def"
	"base_msg"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/golang/protobuf/proto"

	dhcp "github.com/krolaw/dhcp4"
)

var (
	addr = flag.String("promhttp-address", base_def.DHCPD_PROMETHEUS_PORT,
		"Prometheus publication HTTP port.")

	client_map_mtx sync.Mutex
	client_map     map[string]int64
	broker         ap_common.Broker
	config         *ap_common.Config
)

/*******************************************************
 *
 * Communication with message broker
 */
func config_changed(event []byte) {
	config := &base_msg.EventConfig{}
	proto.Unmarshal(event, config)
	property := *config.Property
	path := strings.Split(property[2:], "/")

	/*
	 * We're just looking for lease expirations, so ignore all properties
	 * other than "@/dhcp/leases/*"
	 */
	if len(path) != 3 || path[0] != "dhcp" || path[1] != "leases" {
		return
	}

	/*
	 * Find the matching lease.
	 * Delete it
	 */
}

func hwaddr_to_uint64(ha net.HardwareAddr) uint64 {
	ext_hwaddr := make([]byte, 8)
	ext_hwaddr[0] = 0
	ext_hwaddr[1] = 0
	copy(ext_hwaddr[2:], ha)

	return binary.BigEndian.Uint64(ext_hwaddr)
}

func leaseProperty(ipaddr net.IP) string {
	return "@/dhcp/leases/" + ipaddr.String()
}

/*
 * This is the first time we've seen this device.  Send an ENTITY message with
 * its hardware address and any IP address it's requesting.
 */
func notifyNewEntity(p dhcp.Packet) {
	t := time.Now()
	ipaddr := p.CIAddr()
	hwaddr_u64 := hwaddr_to_uint64(p.CHAddr())

	log.Printf("New client %s (incoming IP address: %s)\n",
		p.CHAddr().String(), ipaddr.String())
	entity := &base_msg.EventNetEntity{
		Timestamp: &base_msg.Timestamp{
			Seconds: proto.Int64(t.Unix()),
			Nanos:   proto.Int32(int32(t.Nanosecond())),
		},
		Sender:      proto.String(fmt.Sprintf("ap.dhcp4d(%d)", os.Getpid())),
		Debug:       proto.String("-"),
		MacAddress:  proto.Uint64(hwaddr_u64),
		Ipv4Address: proto.Uint32(binary.BigEndian.Uint32(ipaddr)),
	}

	data, err := proto.Marshal(entity)
	if err != nil {
		log.Printf("entity couldn't marshal: %v", err)
	} else {
		err = broker.Publish(base_def.TOPIC_ENTITY, data)
		if err != nil {
			log.Printf("couldn't send %v", err)
		}
	}
}

func notifyDHCPRequest(p dhcp.Packet) {
	t := time.Now()
	protocol := base_msg.Protocol_DHCP
	entity := &base_msg.EventNetRequest{
		Timestamp: &base_msg.Timestamp{
			Seconds: proto.Int64(t.Unix()),
			Nanos:   proto.Int32(int32(t.Nanosecond())),
		},
		Sender:    proto.String(fmt.Sprintf("ap.dhcp4d(%d)", os.Getpid())),
		Debug:     proto.String("-"),
		Protocol:  &protocol,
		Requestor: proto.String(p.CHAddr().String()),
	}

	data, err := proto.Marshal(entity)
	if err != nil {
		log.Printf("entity couldn't marshal: %v", err)
	} else {
		err = broker.Publish(base_def.TOPIC_REQUEST, data)
		if err != nil {
			log.Printf("couldn't send %v", err)
		}
	}
}

/*
 * We've have provisionally assigned an IP address to a client.  Send a CLAIM
 * message indicating that that address is no longer available.
 */
func notifyClaimed(p dhcp.Packet, ipaddr net.IP) {
	t := time.Now()

	action := base_msg.EventNetResource_CLAIMED
	entity := &base_msg.EventNetResource{
		Timestamp: &base_msg.Timestamp{
			Seconds: proto.Int64(t.Unix()),
			Nanos:   proto.Int32(int32(t.Nanosecond())),
		},
		Sender:      proto.String(fmt.Sprintf("ap.dhcp4d(%d)", os.Getpid())),
		Debug:       proto.String("-"),
		Action:      &action,
		Ipv4Address: proto.Uint32(binary.BigEndian.Uint32(ipaddr)),
	}

	data, err := proto.Marshal(entity)
	if err != nil {
		log.Printf("entity couldn't marshal: %v", err)
	} else {
		err = broker.Publish(base_def.TOPIC_RESOURCE, data)
		if err != nil {
			log.Printf("couldn't send %v", err)
		}
	}
}

/*
 * An IP address has been released.  It may have been released or declined by
 * the client, or the lease may have expired.
 */
func notifyRelease(ipaddr net.IP) {
	t := time.Now()
	action := base_msg.EventNetResource_RELEASED
	entity := &base_msg.EventNetResource{
		Timestamp: &base_msg.Timestamp{
			Seconds: proto.Int64(t.Unix()),
			Nanos:   proto.Int32(int32(t.Nanosecond())),
		},
		Sender:      proto.String(fmt.Sprintf("ap.dhcp4d(%d)", os.Getpid())),
		Debug:       proto.String("-"),
		Action:      &action,
		Ipv4Address: proto.Uint32(binary.BigEndian.Uint32(ipaddr)),
	}

	data, err := proto.Marshal(entity)
	if err != nil {
		log.Printf("entity couldn't marshal: %v", err)
	} else {
		err = broker.Publish(base_def.TOPIC_RESOURCE, data)
		if err != nil {
			log.Printf("couldn't send %v", err)
		}
	}
}

/*******************************************************
 *
 * Implementing the DHCP protocol
 */

type lease struct {
	name    string     // Client's name from DHCP packet
	nic     string     // Client's CHAddr
	ipaddr  net.IP     // Client's IP address
	expires *time.Time // When the lease expires
	static  bool       // Statically assigned?
}

type DHCPHandler struct {
	iface         string        // Net interface we serve
	ip            net.IP        // Server IP to use
	options       dhcp.Options  // Options to send to DHCP Clients
	start         net.IP        // Start of IP range to distribute
	leaseRange    int           // Number of IPs to distribute (starting from start)
	leaseDuration time.Duration // Lease period
	leases        []*lease      // Per-lease state
}

/*
 * Construct a DHCP NAK message
 */
func (h *DHCPHandler) nak(p dhcp.Packet) dhcp.Packet {
	return dhcp.ReplyPacket(p, dhcp.NAK, h.ip, nil, 0, nil)
}

/*
 * Handle DISCOVER messages
 */
func (h *DHCPHandler) discover(p dhcp.Packet, options dhcp.Options) dhcp.Packet {
	nic := p.CHAddr().String()
	log.Printf("DISCOVER %s\n", nic)

	l := h.leaseSearch(nic, true)
	if l == nil {
		log.Printf("Out of leases\n")
		return h.nak(p)
	}
	log.Printf("  OFFER %s to %s\n", l.ipaddr, l.nic)

	notifyClaimed(p, l.ipaddr)
	return dhcp.ReplyPacket(p, dhcp.Offer, h.ip, l.ipaddr, h.leaseDuration,
		h.options.SelectOrderOrAll(options[dhcp.OptionParameterRequestList]))
}

/*
 * Handle REQUEST messages
 */
func (h *DHCPHandler) request(p dhcp.Packet, options dhcp.Options) dhcp.Packet {
	var reqIP net.IP
	var l *lease

	hwaddr := p.CHAddr().String()
	log.Printf("REQUEST for %s\n", hwaddr)

	server, ok := options[dhcp.OptionServerIdentifier]
	if ok && !net.IP(server).Equal(h.ip) {
		return nil // Message not for this dhcp server
	}
	request_option := net.IP(options[dhcp.OptionRequestedIPAddress])

	/*
	 * If this client already has an IP address assigned (either statically,
	 * or a previously assigned dynamic address), that overrides any address
	 * it might ask for.
	 */
	action := ""
	if current := h.leaseSearch(hwaddr, false); current != nil {
		reqIP = current.ipaddr
		if request_option != nil {
			if reqIP.Equal(request_option) {
				action = "renewing"
			} else {
				/*
				 * XXX: this is potentially worth of a
				 * NetException message
				 */
				action = "overriding client"
			}
		} else if current.static {
			action = "using static lease"
		} else {
			action = "found existing lease"
		}
	} else if request_option != nil {
		reqIP = request_option
		action = "granting request"
	} else {
		reqIP = net.IP(p.CIAddr())
		action = "CLAIMed"
	}
	log.Printf("   REQUEST %s %s\n", action, reqIP.String())

	if len(reqIP) != 4 || reqIP.Equal(net.IPv4zero) {
		return h.nak(p)
	}

	leaseNum := dhcp.IPRange(h.start, reqIP) - 1
	if leaseNum >= 0 && leaseNum < h.leaseRange {
		l = h.leases[leaseNum]
	}
	if l == nil || l.nic != hwaddr {
		return h.nak(p)
	}
	l.name = string(options[dhcp.OptionHostName])
	if !l.static {
		expires := time.Now().Add(h.leaseDuration)
		l.expires = &expires
	}

	/*
	 * XXX: currently a lease is a single property, which means we will lose
	 * any hostname on restart.  When ap.configd is augmented to accept
	 * property trees, we can fix this.
	 */
	log.Printf("   REQUEST assigned %s to %s\n", l.ipaddr, hwaddr)
	config.CreateProp(leaseProperty(l.ipaddr), l.nic, l.expires)
	notifyDHCPRequest(p)

	return dhcp.ReplyPacket(p, dhcp.ACK, h.ip, l.ipaddr, h.leaseDuration,
		h.options.SelectOrderOrAll(options[dhcp.OptionParameterRequestList]))
}

/*
 * Handle RELEASE and DECLINE messages
 */
func (h *DHCPHandler) release(p dhcp.Packet, msgType dhcp.MessageType) {
	nic := p.CHAddr().String()

	for i := 0; i < h.leaseRange; i++ {
		l := h.leases[i]
		if l != nil && l.nic == nic {
			h.leases[i] = nil
			notifyRelease(l.ipaddr)
			config.DeleteProp(leaseProperty(l.ipaddr))

			if msgType == dhcp.Release {
				log.Printf("RELEASE %d for %s\n", l.ipaddr, nic)
			} else {
				log.Printf("DECLINE %d for %s\n", l.ipaddr, nic)
			}
			break
		}
	}
}

/*
 * Master DHCP handler.  Routes packets to message-specific handlers
 */
func (h *DHCPHandler) ServeDHCP(p dhcp.Packet, msgType dhcp.MessageType,
	options dhcp.Options) (d dhcp.Packet) {
	hwaddr := p.CHAddr().String()

	client_map_mtx.Lock()
	client_map[hwaddr] = client_map[hwaddr] + 1
	if client_map[hwaddr] == 1 {
		notifyNewEntity(p)
	}
	client_map_mtx.Unlock()

	switch msgType {

	case dhcp.Discover:
		return h.discover(p, options)

	case dhcp.Request:
		return h.request(p, options)

	case dhcp.Release, dhcp.Decline:
		h.release(p, msgType)
	}
	return nil
}

/*
 * Scan the array of possible leases.  If this nic already has a live lease,
 * return that.  Otherwise, optionally assign an available lease at random.  A
 * 'nil' response indicates that all leases are currently assigned.
 */
func (h *DHCPHandler) leaseSearch(nic string, assign bool) *lease {
	now := time.Now()
	target := rand.Intn(h.leaseRange)
	assigned := -1

	for i := 0; i < h.leaseRange; i++ {
		l := h.leases[i]

		if (l != nil) && l.expires != nil && l.expires.Before(now) {
			/*
			 * Shouldn't happen, but is possible if
			 * ap.configd is offline for a while
			 */
			h.leases[i] = nil
			l = nil
		}

		if l == nil && assigned < target {
			assigned = i
		}

		if l != nil && l.nic == nic {
			return l
		}
	}

	if !assign || assigned < 0 {
		return nil
	}
	l := lease{
		nic:    nic,
		ipaddr: dhcp.IPAdd(h.start, assigned),
	}
	h.leases[assigned] = &l
	return &l
}

/*******************************************************
 *
 * Interaction with ap.configd
 */
type property_node struct {
	Name     string
	Value    string           `json:"Value,omitempty"`
	Expires  *time.Time       `json:"Expires,omitempty"`
	Children []*property_node `json:"Children,omitempty"`
}

func configDHCPParams(params map[string]string) {
	var root property_node

	/*
	 * Currently assumes a single interface/dhcp configuration.  If we want
	 * to support per-interface configs, then this will need to be a tree of
	 * per-iface configs.  If we want to support multple interfaces with the
	 * same config, then the 'iface' parameter will have to be a []string
	 * rather than a single string.
	 */
	tree, err := config.GetProp("@/dhcp/config")
	if err != nil {
		log.Fatalf("Failed to get DHCP configuration info: %v", err)
	}
	err = json.Unmarshal([]byte(tree), &root)
	if err != nil {
		log.Fatalf("Failed to decode configuration info: %v", err)
	}

	/*
	 * Populate the params map, and verify that all required parameters are
	 * present
	 */
	for _, s := range root.Children {
		params[s.Name] = s.Value
	}
	errmsg := ""
	for i, s := range params {
		if s == "" {
			if len(errmsg) > 0 {
				errmsg = errmsg + ", "
			}
			errmsg = errmsg + i
		}
	}
	if len(errmsg) != 0 {
		log.Fatalf("Invalid DHCP config. Missing: %s\n", errmsg)
	}

	/*
	 * If the router and nameserver weren't specified, fill in sensible
	 * defaults
	 */
	if _, ok := params["router"]; !ok {
		params["router"] = params["server_ip"]
	}
	if _, ok := params["name_server"]; !ok {
		params["name_server"] = params["server_ip"]
	}
}

func newHandler() *DHCPHandler {
	var err error
	var range_size, duration int

	params := map[string]string{
		"iface":          "",
		"server_ip":      "",
		"subnet_mask":    "",
		"range_start":    "",
		"range_size":     "",
		"lease_duration": "",
	}
	configDHCPParams(params)

	server_ip := net.ParseIP(params["server_ip"]).To4()
	range_start := net.ParseIP(params["range_start"]).To4()
	subnet_mask := net.ParseIP(params["subnet_mask"]).To4()
	router := net.ParseIP(params["router"]).To4()
	name_server := net.ParseIP(params["name_server"]).To4()

	if range_size, err = strconv.Atoi(params["range_size"]); err != nil {
		log.Fatal("Invalid DHCP config.  Illegal range_size: %s\n",
			params["range_size"])
	}
	if duration, err = strconv.Atoi(params["lease_duration"]); err != nil {
		log.Fatal("Invalid DHCP config.  Illegal lease_duration: %s\n",
			params["lease_duration"])
	}

	return &DHCPHandler{
		iface:         params["iface"],
		ip:            server_ip,
		leaseDuration: time.Duration(duration) * time.Minute,
		start:         range_start,
		leaseRange:    range_size,
		leases:        make([]*lease, range_size, range_size),
		options: dhcp.Options{
			dhcp.OptionSubnetMask:       subnet_mask,
			dhcp.OptionRouter:           router,
			dhcp.OptionDomainNameServer: name_server,
		},
	}
}

func (h *DHCPHandler) recoverLeases() {
	var root property_node

	tree, err := config.GetProp("@/dhcp/leases")
	if err != nil {
		log.Printf("Failed to fetch lease info: %v\n", err)
		return
	}
	err = json.Unmarshal([]byte(tree), &root)
	if err != nil {
		log.Printf("Failed to decode lease info: %v\n", err)
	}

	for _, s := range root.Children {
		ip := net.ParseIP(s.Name).To4()
		n := dhcp.IPRange(h.start, ip) - 1
		if n < 0 || n >= h.leaseRange {
			log.Printf("Out of range IP address %s for %s\n",
				s.Name, s.Value)
			continue
		}
		l := lease{
			name:    "",
			nic:     s.Value,
			ipaddr:  ip,
			expires: s.Expires,
		}
		if s.Expires == nil {
			l.static = true
		}

		h.leases[n] = &l
	}
}

func InitServeDHCP() {
	handler := newHandler()
	handler.recoverLeases()

	log.Fatal(dhcp.ListenAndServeIf(handler.iface, handler))
}

// # XXX quarantine range
// # XXX trusted dns, untrusted dns
//
// # XXX intent: siphon/monitor or active
//
// # XXX allocator statistics
// st_allocated = attr.ib(init=False, default=0)
// st_freed = attr.ib(init=False, default=0)
// st_request_fail_range = attr.ib(init=False, default=0)
// st_request_fail_busy = attr.ib(init=False, default=0)

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	log.Println("start")

	//     # Need to have certain network capabilities.
	//     priv_net_bind_service = prctl.cap_effective.net_bind_service
	//     priv_net_broadcast = prctl.cap_effective.net_broadcast
	//
	//     if not priv_net_bind_service:
	//         logging.warning("require CAP_NET_BIND_SERVICE to bind DHCP server port")
	//         sys.exit(1)
	//     if not priv_net_broadcast:
	//         logging.warning("require CAP_NET_BROADCAST to acquire broadcast packets")
	//         sys.exit(1)

	//     # XXX configuration retrieval

	flag.Parse()

	log.Println("cli flags parsed")

	http.Handle("/metrics", promhttp.Handler())
	go http.ListenAndServe(*addr, nil)

	log.Println("prometheus client launched")

	broker.Init("ap.dhcp4d")
	broker.Handle(base_def.TOPIC_CONFIG, config_changed)
	broker.Connect()
	defer broker.Disconnect()

	log.Println("message bus listener routine launched")
	broker.Ping()

	// Interface to configd
	config = ap_common.NewConfig("ap.dhcp4d")

	client_map = make(map[string]int64)

	InitServeDHCP()
}
