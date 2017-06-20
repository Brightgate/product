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

	handlers       map[string]*DHCPHandler
	client_map_mtx sync.Mutex
	client_map     map[string]string
	broker         ap_common.Broker
	config         *ap_common.Config
)

/*******************************************************
 *
 * Communication with message broker
 */
func config_expired(path []string) {
	/*
	 * Watch for lease expirations in @/dhcp/leases/<ipaddr>.  We actually
	 * clean up expired leases as a side effect of handing out new ones, so
	 * all we do here is log it.
	 */
	if len(path) == 3 && path[0] == "dhcp" && path[1] == "leases" {
		log.Printf("Lease for %s expired\n", path[2])
	}
}

func config_changed(path []string, val string) {
	/*
	 * Watch for client identifications in @/client/<macaddr>/class
	 */
	if len(path) == 3 && path[0] == "clients" && path[2] == "class" {
		client := path[1]

		client_map_mtx.Lock()
		old := client_map[client]
		client_map[client] = val
		client_map_mtx.Unlock()

		if old == "" {
			log.Printf("configd reports new client %s is %s\n",
				client, val)
		} else {
			log.Printf("configd moves client %s from %s to  %s\n",
				client, old, val)
		}
	}
}

func config_event(event []byte) {
	config := &base_msg.EventConfig{}
	proto.Unmarshal(event, config)

	// Ignore messages without an explicit type
	if config.Type == nil {
		return
	}

	etype := *config.Type
	property := *config.Property
	path := strings.Split(property[2:], "/")
	value := *config.NewValue

	switch etype {
	case base_msg.EventConfig_EXPIRE:
		config_expired(path)
	case base_msg.EventConfig_CHANGE:
		config_changed(path, value)
	}
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
 * its hardware address, name, and any IP address it's requesting.
 */
func notifyNewEntity(p dhcp.Packet, options dhcp.Options) {
	t := time.Now()
	ipaddr := p.CIAddr()
	hwaddr_u64 := hwaddr_to_uint64(p.CHAddr())
	hostname := string(options[dhcp.OptionHostName])

	log.Printf("New client %s (name: %q incoming IP address: %s)\n",
		p.CHAddr().String(), hostname, ipaddr.String())
	entity := &base_msg.EventNetEntity{
		Timestamp: &base_msg.Timestamp{
			Seconds: proto.Int64(t.Unix()),
			Nanos:   proto.Int32(int32(t.Nanosecond())),
		},
		Sender:      proto.String(fmt.Sprintf("ap.dhcp4d(%d)", os.Getpid())),
		Debug:       proto.String("-"),
		MacAddress:  proto.Uint64(hwaddr_u64),
		Ipv4Address: proto.Uint32(binary.BigEndian.Uint32(ipaddr)),
		DnsName:     proto.String(hostname),
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

/*
 * A provisioned IP address has now been claimed by a client.
 */
func notifyClaimed(p dhcp.Packet, ipaddr net.IP, name string) {
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
		DnsName:     proto.String(name),
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
 * We've have provisionally assigned an IP address to a client.  Send a
 * net.resource message indicating that that address is no longer available.
 */
func notifyProvisioned(p dhcp.Packet, ipaddr net.IP) {
	t := time.Now()

	action := base_msg.EventNetResource_PROVISIONED
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
	name     string     // Client's name from DHCP packet
	hwaddr   string     // Client's CHAddr
	ipaddr   net.IP     // Client's IP address
	expires  *time.Time // When the lease expires
	static   bool       // Statically assigned?
	assigned bool       // Lease assigned to a client?
}

type leaseRange struct {
	start    net.IP        // Start of IP range to distribute
	end      net.IP        // End of IP range to distribute
	size     int           // Number of IPs to distribute (starting from start)
	duration time.Duration // Lease period
	leases   []lease       // Per-lease state
}

type DHCPHandler struct {
	iface   string       // Net interface we serve
	ip      net.IP       // Server IP to use
	network net.IPNet    // Full network from which subranges are defined
	options dhcp.Options // Options to send to DHCP Clients
	ranges  map[string]*leaseRange
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
	hwaddr := p.CHAddr().String()
	log.Printf("DISCOVER %s\n", hwaddr)

	r := h.classRange(hwaddr)
	if r == nil {
		log.Printf("%s is in an unconfigured class", hwaddr)
		return h.nak(p)
	}

	l := h.leaseAssign(r, hwaddr)
	if l == nil {
		log.Printf("Out of leases for this class\n")
		return h.nak(p)
	}
	log.Printf("  OFFER %s to %s\n", l.ipaddr, l.hwaddr)

	notifyProvisioned(p, l.ipaddr)
	return dhcp.ReplyPacket(p, dhcp.Offer, h.ip, l.ipaddr, r.duration,
		h.options.SelectOrderOrAll(options[dhcp.OptionParameterRequestList]))
}

/*
 * Handle REQUEST messages
 */
func (h *DHCPHandler) request(p dhcp.Packet, options dhcp.Options) dhcp.Packet {
	var reqIP net.IP

	hwaddr := p.CHAddr().String()
	log.Printf("REQUEST for %s\n", hwaddr)

	server, ok := options[dhcp.OptionServerIdentifier]
	if ok && !net.IP(server).Equal(h.ip) {
		return nil // Message not for this dhcp server
	}
	request_option := net.IP(options[dhcp.OptionRequestedIPAddress])

	/*
	 * Before we grant an IP request, make sure this client belongs to a
	 * known class and that any existing IP address is appropriate for that
	 * class.
	 */
	r := h.classRange(hwaddr)
	current := h.leaseSearch(hwaddr)
	if r == nil {
		log.Printf("%s is in an unconfigured class")
		h.releaseLease(current, hwaddr)
		return h.nak(p)
	}
	if current != nil && !dhcp.IPInRange(r.start, r.end, current.ipaddr) {
		log.Printf("%s already has IP %s in the wrong class\n",
			hwaddr, current.ipaddr.String())
		h.releaseLease(current, hwaddr)
		return h.nak(p)
	}

	/*
	 * If this client already has an IP address assigned (either statically,
	 * or a previously assigned dynamic address), that overrides any address
	 * it might ask for.
	 */
	action := ""
	if current != nil {
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
		action = "CLAIMED"
	}
	log.Printf("   REQUEST %s %s\n", action, reqIP.String())

	if len(reqIP) != 4 || reqIP.Equal(net.IPv4zero) {
		return h.nak(p)
	}

	slot := dhcp.IPRange(r.start, reqIP) - 1
	if slot < 0 || slot >= r.size {
		return h.nak(p)
	}

	l := &r.leases[slot]
	if !l.assigned || l.hwaddr != hwaddr {
		return h.nak(p)
	}

	l.name = string(options[dhcp.OptionHostName])
	if !l.static {
		expires := time.Now().Add(r.duration)
		l.expires = &expires
	}

	/*
	 * XXX: currently a lease is a single property, which means we will lose
	 * any hostname on restart.  When ap.configd is augmented to accept
	 * property trees, we can fix this.
	 */
	config.CreateProp(leaseProperty(l.ipaddr), l.hwaddr, l.expires)
	notifyClaimed(p, l.ipaddr, l.name)

	return dhcp.ReplyPacket(p, dhcp.ACK, h.ip, l.ipaddr, r.duration,
		h.options.SelectOrderOrAll(options[dhcp.OptionParameterRequestList]))
}

/*
 * If this lease is unassigned, or assigned to somebody else, return 'false'.
 * Otherwise, release it, update the configuration, send a notification, and
 * return 'true'
 */
func (h *DHCPHandler) releaseLease(l *lease, hwaddr string) bool {
	if l == nil || !l.assigned || l.hwaddr != hwaddr {
		return false
	}

	l.assigned = false
	notifyRelease(l.ipaddr)
	config.DeleteProp(leaseProperty(l.ipaddr))
	return true
}

/*
 * Handle RELEASE message for a specific IP address
 */
func (h *DHCPHandler) release(p dhcp.Packet) {
	hwaddr := p.CHAddr().String()
	ipaddr := p.CIAddr()

	r := h.addrRange(ipaddr)
	if r == nil {
		log.Printf("Client %s RELEASE unsupported address: %s\n",
			hwaddr, ipaddr.String())
		return
	}

	slot := dhcp.IPRange(r.start, ipaddr) - 1
	lease := &r.leases[slot]
	if h.releaseLease(lease, hwaddr) {
		log.Printf("RELEASE %s\n", hwaddr)
	}
}

/*
 * Handle DECLINE message.  We only get the client's MAC address, so we have to
 * scan all possible leases to find the one being declined
 */
func (h *DHCPHandler) decline(p dhcp.Packet) {
	hwaddr := p.CHAddr().String()

	for _, r := range h.ranges {
		for slot := 0; slot < r.size; slot++ {
			lease := &r.leases[slot]
			if h.releaseLease(lease, hwaddr) {
				log.Printf("DECLINE for %s\n", hwaddr)
				return
			}
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
	if client_map[hwaddr] == "" {
		notifyNewEntity(p, options)
		client_map[hwaddr] = "unclassified"
	}
	client_map_mtx.Unlock()

	switch msgType {

	case dhcp.Discover:
		return h.discover(p, options)

	case dhcp.Request:
		return h.request(p, options)

	case dhcp.Release:
		h.release(p)

	case dhcp.Decline:
		h.decline(p)
	}
	return nil
}

/*
 * Find the lease range responsible for a given IP address
 */
func (h *DHCPHandler) addrRange(ipaddr net.IP) *leaseRange {
	for _, r := range h.ranges {
		if dhcp.IPInRange(r.start, r.end, ipaddr) {
			return r
		}
	}
	return nil
}

/*
 * Select a lease range based on the client's currently assigned class
 */
func (h *DHCPHandler) classRange(hwaddr string) *leaseRange {
	var rval *leaseRange

	client_map_mtx.Lock()
	class := client_map[hwaddr]
	if class == "" {
		class = "unclassified"
	}
	client_map_mtx.Unlock()

	if r, ok := h.ranges[class]; ok {
		rval = r
	} else {
		log.Printf("Client %s in unconfigured class: %s\n",
			hwaddr, class)
	}
	return rval
}

/*
 * Scan the array of leases in this range.  If this nic already has a live
 * lease, return that.  Otherwise, assign an available lease at random.  A 'nil'
 * response indicates that all leases are currently assigned.
 */
func (h *DHCPHandler) leaseAssign(r *leaseRange, hwaddr string) *lease {
	var rval *lease

	now := time.Now()
	target := rand.Intn(r.size)
	assigned := -1

	for i, l := range r.leases {
		if l.assigned && l.expires != nil && l.expires.Before(now) {
			/*
			 * We don't actively handle lease expiration messages;
			 * they get cleaned up lazily here.
			 */
			l.assigned = false
		}

		if l.assigned && l.hwaddr == hwaddr {
			rval = &l
			break
		}

		if !l.assigned && assigned < target {
			assigned = i
		}
	}

	if rval == nil && assigned >= 0 {
		rval = &r.leases[assigned]
		rval.hwaddr = hwaddr
		rval.ipaddr = dhcp.IPAdd(r.start, assigned)
		rval.assigned = true
	}
	return rval
}

/*
 * Scan all leases in all ranges, looking for an IP address assigned to this
 * NIC.
 */
func (h *DHCPHandler) leaseSearch(hwaddr string) *lease {
	for _, r := range h.ranges {
		for i := 0; i < r.size; i++ {
			l := &r.leases[i]
			if l.assigned && l.hwaddr == hwaddr {
				return l
			}
		}
	}
	return nil
}

/*******************************************************
 *
 * Interaction with ap.configd
 */
func getParams(root *ap_common.PropertyNode, params map[string]string) {
	/*
	 * Populate the params map, and verify that all required parameters are
	 * present
	 */
	for _, s := range root.Children {
		if len(s.Value) > 0 {
			params[s.Name] = s.Value
		}
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
}

func newRange(props *ap_common.PropertyNode, ranges map[string]*leaseRange) {
	var range_size, duration int
	var err error

	params := map[string]string{
		"subnet":         "",
		"lease_duration": "",
	}
	getParams(props, params)

	if duration, err = strconv.Atoi(params["lease_duration"]); err != nil {
		log.Fatal("Invalid DHCP config.  Illegal lease_duration: %s\n",
			params["lease_duration"])
	}

	ones, bits := 0, 0
	ip, subnet, err := net.ParseCIDR(params["subnet"])
	if err == nil {
		ones, bits = subnet.Mask.Size()
	}
	if ones == 0 || bits == 0 {
		log.Fatal("Invalid DHCP config.  Poorly formed subnet:: %s\n",
			params["subnet"])
	}
	range_size = 1 << uint32((bits - ones))

	r := leaseRange{
		start:    ip,
		end:      dhcp.IPAdd(ip, range_size),
		size:     range_size,
		duration: time.Duration(duration) * time.Minute,
		leases:   make([]lease, range_size, range_size),
	}
	ranges[props.Name] = &r
}

func newHandler() *DHCPHandler {
	var err error
	var root ap_common.PropertyNode

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
	params := map[string]string{
		"iface":     "",
		"server_ip": "",
		"network":   "",
	}
	getParams(&root, params)
	_, network, err := net.ParseCIDR(params["network"])

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

	server_ip := net.ParseIP(params["server_ip"]).To4()
	router := net.ParseIP(params["router"]).To4()
	name_server := net.ParseIP(params["name_server"]).To4()

	ranges := make(map[string]*leaseRange)
	for _, c := range root.Children {
		if c.Name == "ranges" {
			for _, r := range c.Children {
				newRange(r, ranges)
			}
		}
	}

	return &DHCPHandler{
		iface:   params["iface"],
		ip:      server_ip,
		network: *network,
		ranges:  ranges,
		options: dhcp.Options{
			dhcp.OptionSubnetMask:       network.Mask,
			dhcp.OptionRouter:           router,
			dhcp.OptionDomainNameServer: name_server,
		},
	}
}

func (h *DHCPHandler) recoverLeases() {
	var root ap_common.PropertyNode

	tree, err := config.GetProp("@/dhcp/leases")
	if len(tree) == 0 {
		if err != nil {
			log.Printf("Failed to fetch lease info: %v\n", err)
		}
		return
	}
	if err = json.Unmarshal([]byte(tree), &root); err != nil {
		log.Printf("Failed to decode lease info: %v\n", err)
	}

	for _, s := range root.Children {
		ip := net.ParseIP(s.Name).To4()
		r := h.addrRange(ip)
		slot := dhcp.IPRange(r.start, ip) - 1
		if slot >= 0 && slot < r.size {
			l := &r.leases[slot]
			l.name = ""
			l.hwaddr = s.Value
			l.ipaddr = ip
			l.expires = s.Expires
			l.static = (s.Expires == nil)
			l.assigned = true
		} else {
			log.Printf("Out of range IP address %s for %s\n",
				s.Name, s.Value)
		}
	}
}

func initServeDHCP() {
	handler := newHandler()
	handler.recoverLeases()

	log.Printf("DHCP server online\n")
	err := dhcp.ListenAndServeIf(handler.iface, handler)
	if err != nil {
		log.Printf("DHCP server failed: %v\n", err)
		os.Exit(1)
	}
}

func initClientMap() {
	var clients ap_common.PropertyNode

	client_map = make(map[string]string)
	tree, err := config.GetProp("@/clients")
	if len(tree) == 0 {
		if err != nil {
			log.Printf("Failed to get initial client list: %v", err)
		}
		return
	}

	if err = json.Unmarshal([]byte(tree), &clients); err != nil {
		log.Printf("Failed to decode configuration info: %v", err)
		return
	}

	for _, client := range clients.Children {
		for _, field := range client.Children {
			if field.Name == "class" {
				client_map[client.Name] = field.Value
				log.Printf("%s starts as %s\n", client.Name,
					field.Value)
				break
			}
		}
	}
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
	log.Printf("Starting\n")

	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

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

	http.Handle("/metrics", promhttp.Handler())
	go http.ListenAndServe(*addr, nil)

	broker.Init("ap.dhcp4d")
	broker.Handle(base_def.TOPIC_CONFIG, config_event)
	broker.Connect()
	defer broker.Disconnect()

	broker.Ping()

	// Interface to configd
	config = ap_common.NewConfig("ap.dhcp4d")

	initClientMap()
	initServeDHCP()
	log.Printf("Shutting down\n")
}
