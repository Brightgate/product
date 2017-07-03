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

package main

import (
	"flag"
	"log"
	"math/rand"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"ap_common"
	"ap_common/mcp"
	"ap_common/network"
	"base_def"
	"base_msg"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/net/ipv4"

	"github.com/golang/protobuf/proto"

	dhcp "github.com/krolaw/dhcp4"
)

var (
	addr = flag.String("promhttp-address", base_def.DHCPD_PROMETHEUS_PORT,
		"Prometheus publication HTTP port.")

	handlers       = make(map[string]*DHCPHandler)
	clientClass    = make(map[string]string)
	clientClassMtx sync.Mutex
	broker         ap_common.Broker
	config         *ap_common.Config

	use_vlans    bool
	physical_nic string
	wanMac       string
	wifiMac      string
	sharedRouter net.IP     // without vlans, all classes share a
	sharedSubnet *net.IPNet // subnet and a router node

	lastRequestOn string // last DHCP request arrived on this mac
)

const pname = "ap.dhcp4d"

func getClass(hwaddr string) string {
	clientClassMtx.Lock()
	class := clientClass[hwaddr]
	clientClassMtx.Unlock()

	return class
}

func updateClass(hwaddr, old, new string) bool {
	updated := false

	clientClassMtx.Lock()
	if clientClass[hwaddr] == old {
		clientClass[hwaddr] = new
		updated = true
	}
	clientClassMtx.Unlock()

	return updated
}

/*******************************************************
 *
 * Communication with message broker
 */
func configExpired(path []string) {
	/*
	 * Watch for lease expirations in @/dhcp/leases/<ipaddr>.  We actually
	 * clean up expired leases as a side effect of handing out new ones, so
	 * all we do here is log it.
	 */
	if len(path) == 3 && path[0] == "dhcp" && path[1] == "leases" {
		log.Printf("Lease for %s expired\n", path[2])
	}
}

func configChanged(path []string, val string) {
	/*
	 * Watch for client identifications in @/client/<macaddr>/class
	 */
	if len(path) == 3 && path[0] == "clients" && path[2] == "class" {
		client := path[1]

		old := getClass(client)
		if (old != val) && updateClass(client, old, val) {
			if old == "" {
				log.Printf("configd reports new client %s is %s\n",
					client, val)
			} else {
				log.Printf("configd moves client %s from %s to  %s\n",
					client, old, val)
			}
		}
	} else if len(path) == 2 && path[0] == "network" {
		if path[1] == "wifi_mac" {
			wifiMac = val
		} else if path[1] == "wan_mac" {
			wanMac = val
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
		configExpired(path)
	case base_msg.EventConfig_CHANGE:
		configChanged(path, value)
	}
}

func leaseProperty(ipaddr net.IP) string {
	return "@/dhcp/leases/" + ipaddr.String()
}

/*
 * This is the first time we've seen this device.  Send an ENTITY message with
 * its hardware address, name, and any IP address it's requesting.
 */
func notifyNewEntity(p dhcp.Packet, options dhcp.Options, class string) {
	t := time.Now()
	ipaddr := p.CIAddr()
	hwaddr_u64 := network.HWAddrToUint64(p.CHAddr())
	hostname := string(options[dhcp.OptionHostName])

	log.Printf("New client %s (name: %q incoming IP address: %s)\n",
		p.CHAddr().String(), hostname, ipaddr.String())
	entity := &base_msg.EventNetEntity{
		Timestamp: &base_msg.Timestamp{
			Seconds: proto.Int64(t.Unix()),
			Nanos:   proto.Int32(int32(t.Nanosecond())),
		},
		Sender:      proto.String(broker.Name),
		Debug:       proto.String("-"),
		MacAddress:  proto.Uint64(hwaddr_u64),
		Ipv4Address: proto.Uint32(network.IPAddrToUint32(ipaddr)),
		DnsName:     proto.String(hostname),
		Class:       proto.String(class),
	}

	err := broker.Publish(entity, base_def.TOPIC_ENTITY)
	if err != nil {
		log.Printf("couldn't publish %s: %v\n", base_def.TOPIC_ENTITY, err)
	}
}

/*
 * A provisioned IP address has now been claimed by a client.
 */
func notifyClaimed(p dhcp.Packet, ipaddr net.IP, name string) {
	t := time.Now()

	action := base_msg.EventNetResource_CLAIMED
	resource := &base_msg.EventNetResource{
		Timestamp: &base_msg.Timestamp{
			Seconds: proto.Int64(t.Unix()),
			Nanos:   proto.Int32(int32(t.Nanosecond())),
		},
		Sender:      proto.String(broker.Name),
		Debug:       proto.String("-"),
		Action:      &action,
		Ipv4Address: proto.Uint32(network.IPAddrToUint32(ipaddr)),
		DnsName:     proto.String(name),
	}

	err := broker.Publish(resource, base_def.TOPIC_RESOURCE)
	if err != nil {
		log.Printf("couldn't publish %s: %v\n", base_def.TOPIC_RESOURCE, err)
	}
}

/*
 * We've have provisionally assigned an IP address to a client.  Send a
 * net.resource message indicating that that address is no longer available.
 */
func notifyProvisioned(p dhcp.Packet, ipaddr net.IP) {
	t := time.Now()

	action := base_msg.EventNetResource_PROVISIONED
	resource := &base_msg.EventNetResource{
		Timestamp: &base_msg.Timestamp{
			Seconds: proto.Int64(t.Unix()),
			Nanos:   proto.Int32(int32(t.Nanosecond())),
		},
		Sender:      proto.String(broker.Name),
		Debug:       proto.String("-"),
		Action:      &action,
		Ipv4Address: proto.Uint32(network.IPAddrToUint32(ipaddr)),
	}

	err := broker.Publish(resource, base_def.TOPIC_RESOURCE)
	if err != nil {
		log.Printf("couldn't publish %s: %v\n", base_def.TOPIC_RESOURCE, err)
	}
}

/*
 * An IP address has been released.  It may have been released or declined by
 * the client, or the lease may have expired.
 */
func notifyRelease(ipaddr net.IP) {
	t := time.Now()
	action := base_msg.EventNetResource_RELEASED
	resource := &base_msg.EventNetResource{
		Timestamp: &base_msg.Timestamp{
			Seconds: proto.Int64(t.Unix()),
			Nanos:   proto.Int32(int32(t.Nanosecond())),
		},
		Sender:      proto.String(broker.Name),
		Debug:       proto.String("-"),
		Action:      &action,
		Ipv4Address: proto.Uint32(network.IPAddrToUint32(ipaddr)),
	}

	err := broker.Publish(resource, base_def.TOPIC_RESOURCE)
	if err != nil {
		log.Printf("couldn't publish %s: %v\n", base_def.TOPIC_RESOURCE, err)
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

type DHCPHandler struct {
	iface       string        // Net interface we serve
	class       string        // Client class eligible for this server
	subnet      net.IPNet     // Subnet being managed
	server_ip   net.IP        // DHCP server's IP
	options     dhcp.Options  // Options to send to DHCP Clients
	range_start net.IP        // Start of IP range to distribute
	range_end   net.IP        // End of IP range to distribute
	range_size  int           // Number of IPs to distribute (starting from start)
	duration    time.Duration // Lease period
	leases      []lease       // Per-lease state
}

/*
 * Construct a DHCP NAK message
 */
func (h *DHCPHandler) nak(p dhcp.Packet) dhcp.Packet {
	return dhcp.ReplyPacket(p, dhcp.NAK, h.server_ip, nil, 0, nil)
}

/*
 * Handle DISCOVER messages
 */
func (h *DHCPHandler) discover(p dhcp.Packet, options dhcp.Options) dhcp.Packet {
	hwaddr := p.CHAddr().String()
	log.Printf("DISCOVER %s\n", hwaddr)

	l := h.leaseAssign(hwaddr)
	if l == nil {
		log.Printf("Out of %s leases\n", h.class)
		return h.nak(p)
	}
	log.Printf("  OFFER %s to %s\n", l.ipaddr, l.hwaddr)

	notifyProvisioned(p, l.ipaddr)
	return dhcp.ReplyPacket(p, dhcp.Offer, h.server_ip, l.ipaddr, h.duration,
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
	if ok && !net.IP(server).Equal(h.server_ip) {
		return nil // Message not for this dhcp server
	}
	request_option := net.IP(options[dhcp.OptionRequestedIPAddress])

	current := h.leaseSearch(hwaddr)

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

	l := h.getLease(reqIP)
	if l == nil || !l.assigned || l.hwaddr != hwaddr {
		return h.nak(p)
	}

	l.name = string(options[dhcp.OptionHostName])
	if !l.static {
		expires := time.Now().Add(h.duration)
		l.expires = &expires
	}

	/*
	 * XXX: currently a lease is a single property, which means we will lose
	 * any hostname on restart.  When ap.configd is augmented to accept
	 * property trees, we can fix this.
	 */
	log.Printf("   REQUEST assigned %s to %s (%q) until %s\n", l.ipaddr, hwaddr, l.name, l.expires)
	config.CreateProp(leaseProperty(l.ipaddr), l.hwaddr, l.expires)
	notifyClaimed(p, l.ipaddr, l.name)

	return dhcp.ReplyPacket(p, dhcp.ACK, h.server_ip, l.ipaddr, h.duration,
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

	l := h.getLease(ipaddr)
	if l == nil {
		log.Printf("Client %s RELEASE unsupported address: %s\n",
			hwaddr, ipaddr.String())
		return
	}
	if h.releaseLease(l, hwaddr) {
		log.Printf("RELEASE %s\n", hwaddr)
	}
}

/*
 * Handle DECLINE message.  We only get the client's MAC address, so we have to
 * scan all possible leases to find the one being declined
 */
func (h *DHCPHandler) decline(p dhcp.Packet) {
	hwaddr := p.CHAddr().String()

	l := h.leaseSearch(hwaddr)
	if h.releaseLease(l, hwaddr) {
		log.Printf("DECLINE for %s\n", hwaddr)
	}
}

/*
 * Based on the client's MAC address, identify its class and return the
 * appropriate DHCP handler
 */
func selectClassHandler(p dhcp.Packet, options dhcp.Options) *DHCPHandler {
	hwaddr := p.CHAddr().String()
	class := getClass(hwaddr)
	if class == "" {
		// If the DHCP request arrived on the wifi port, the client is
		// 'unclassified'.  Otherwise, it's 'wired'.  This relies on the
		// knowledge that the DHCP library (krolaw/dhcp4) will call the
		// handler immediately after the call to ReadFrom(), where
		// lastRequestOn gets set.
		//
		if lastRequestOn == wifiMac {
			updateClass(hwaddr, "", "unclassified")
		} else {
			updateClass(hwaddr, "", "wired")
		}

		class = getClass(hwaddr)
		log.Printf("New %s client %s via %s: %s\n", class, hwaddr,
			lastRequestOn)
		notifyNewEntity(p, options, class)
	}

	class_handler, ok := handlers[class]
	if !ok {
		log.Printf("Client %s identified as unknown class '%s'\n",
			hwaddr, class)
	}
	return class_handler
}

func (h *DHCPHandler) ServeDHCP(p dhcp.Packet, msgType dhcp.MessageType,
	options dhcp.Options) (d dhcp.Packet) {

	class_handler := selectClassHandler(p, options)
	if class_handler == nil {
		return nil
	}

	switch msgType {

	case dhcp.Discover:
		return class_handler.discover(p, options)

	case dhcp.Request:
		return class_handler.request(p, options)

	case dhcp.Release:
		class_handler.release(p)

	case dhcp.Decline:
		class_handler.decline(p)
	}
	return nil
}

/*
 * If this nic already has a live lease, return that.  Otherwise, assign an
 * available lease at random.  A 'nil' response indicates that all leases are
 * currently assigned.
 */
func (h *DHCPHandler) leaseAssign(hwaddr string) *lease {
	var rval *lease

	now := time.Now()
	target := rand.Intn(h.range_size)
	assigned := -1

	for i, l := range h.leases {
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
		rval = &h.leases[assigned]
		rval.hwaddr = hwaddr
		rval.ipaddr = dhcp.IPAdd(h.range_start, assigned)
		rval.assigned = true
	}
	return rval
}

/*
 * Scan all leases in all ranges, looking for an IP address assigned to this
 * NIC.
 */
func (h *DHCPHandler) leaseSearch(hwaddr string) *lease {
	for i := 0; i < h.range_size; i++ {
		l := &h.leases[i]
		if l.assigned && l.hwaddr == hwaddr {
			return l
		}
	}
	return nil
}

func (h *DHCPHandler) getLease(ip net.IP) *lease {
	if !dhcp.IPInRange(h.range_start, h.range_end, ip) {
		return nil
	}

	slot := dhcp.IPRange(h.range_start, ip) - 1

	if slot < 0 || slot >= h.range_size {
		return nil
	}

	return &h.leases[slot]
}

//
// Instantiate a new DHCP handler for the given class/subnet.
//
func newHandler(class, network, nameserver string, duration int) *DHCPHandler {
	var range_size int
	var err error

	start, subnet, err := net.ParseCIDR(network)
	if err == nil {
		ones, bits := subnet.Mask.Size()
		range_size = 1<<uint32(bits-ones) - 2
	}

	if range_size == 0 {
		log.Fatal("Invalid DHCP config.  Poorly formed subnet: %s\n",
			network)
	}

	var myip, nsip net.IP
	if use_vlans {
		// When using VLANs, each managed range is a proper subnet with
		// its own routing info.
		myip = dhcp.IPAdd(start, 1)
	} else {
		myip = sharedRouter
		subnet = sharedSubnet
	}

	if len(nameserver) == 0 {
		nsip = myip
	} else {
		nsip = net.ParseIP(nameserver).To4()
	}

	h := DHCPHandler{
		class:       class,
		subnet:      *subnet,
		server_ip:   myip,
		range_start: start,
		range_end:   dhcp.IPAdd(start, range_size),
		range_size:  range_size,
		duration:    time.Duration(duration) * time.Minute,
		options: dhcp.Options{
			dhcp.OptionSubnetMask:       subnet.Mask,
			dhcp.OptionRouter:           myip,
			dhcp.OptionDomainNameServer: nsip,
		},
		leases: make([]lease, range_size, range_size),
	}

	return &h
}

func (h *DHCPHandler) recoverLeases(root *ap_common.PropertyNode) {
	// Preemptively pull the network and DHCP server from the pool
	h.leases[0].assigned = true
	h.leases[1].assigned = true

	if root == nil {
		return
	}

	for _, s := range root.Children {
		ip := net.ParseIP(s.Name).To4()
		if l := h.getLease(ip); l != nil {
			l.name = ""
			l.hwaddr = s.Value
			l.ipaddr = ip
			l.expires = s.Expires
			l.static = (s.Expires == nil)
			l.assigned = true
		}
	}
}

func initPhysical(props *ap_common.PropertyNode) {
	var err error

	physical_nic, err = config.GetProp("@/network/default")
	if err != nil {
		log.Fatalf("No default interface defined\n")
	}

	iface, err := net.InterfaceByName(physical_nic)
	if err != nil {
		log.Fatalf("No such network device: %s\n", iface)
	}
	wanMac, _ = config.GetProp("@/network/wan_mac")
	wifiMac, _ = config.GetProp("@/network/wifi_mac")

	prop, err := config.GetProp("@/network/use_vlans")
	if err == nil && prop == "true" {
		use_vlans = true
	} else {
		// If we aren't using VLANs, all classes share a subnet
		if node := props.GetChild("network"); node != nil {
			start, subnet, err := net.ParseCIDR(node.Value)
			if err == nil {
				sharedRouter = dhcp.IPAdd(start, 1)
				sharedSubnet = subnet
			}
		}

		if sharedSubnet == nil {
			log.Fatalf("Missing shared network info\n")
		}
	}
}

func initHandlers() {
	var err error
	var nameserver string
	var props, leases, node *ap_common.PropertyNode

	/*
	 * Currently assumes a single interface/dhcp configuration.  If we want
	 * to support per-interface configs, then this will need to be a tree of
	 * per-iface configs.  If we want to support multple interfaces with the
	 * same config, then the 'iface' parameter will have to be a []string
	 * rather than a single string.
	 */
	if props, err = config.GetProps("@/dhcp/config"); err != nil {
		log.Fatalf("Failed to get DHCP configuration info: %v", err)
	}

	initPhysical(props)

	if node = props.GetChild("nameserver"); node != nil {
		nameserver = node.Value
	}

	if leases, err = config.GetProps("@/dhcp/leases"); err != nil {
		log.Fatalf("Failed to get DHCP lease info: %v", err)
	}

	// Iterate over the known classes.  For each one, find the VLAN name and
	// subnet, and create a DHCP handler to manage that subnet.
	classes := config.GetClasses()
	subnets := config.GetSubnets()
	for name, class := range classes {
		subnet, ok := subnets[class.Interface]
		if !ok {
			log.Printf("No subnet for %s\n", class.Interface)
			continue
		}
		h := newHandler(name, subnet, nameserver, class.LeaseDuration)
		if class.Interface == "default" {
			h.iface = physical_nic
		} else {
			h.iface = class.Interface
		}

		h.recoverLeases(leases)
		handlers[h.class] = h
	}

}

type MultiConn struct {
	conn *ipv4.PacketConn
	cm   *ipv4.ControlMessage
}

func (s *MultiConn) ReadFrom(b []byte) (n int, addr net.Addr, err error) {
	n, s.cm, addr, err = s.conn.ReadFrom(b)

	if err != nil {
		return
	}

	requestMac := ""
	if s.cm != nil {
		iface, err := net.InterfaceByIndex(s.cm.IfIndex)
		if err == nil {
			requestMac = iface.HardwareAddr.String()
		}
	}

	// If the request arrives on the WAN port, drop it.
	if requestMac == wanMac {
		log.Printf("Request arrived on wan: %s\n", requestMac)
		n = 0
	} else {
		if requestMac == wifiMac {
			log.Printf("Request arrived on wifi: %s\n", requestMac)
		} else {
			log.Printf("Request arrived on wired: %s\n", requestMac)
		}

		lastRequestOn = requestMac
	}
	return
}

func (s *MultiConn) WriteTo(b []byte, addr net.Addr) (n int, err error) {
	s.cm.Src = nil
	return s.conn.WriteTo(b, s.cm, addr)
}

func listenAndServeIf(handler dhcp.Handler) error {
	l, err := net.ListenPacket("udp4", ":67")
	if err != nil {
		return err
	}
	defer l.Close()

	p := ipv4.NewPacketConn(l)
	err = p.SetControlMessage(ipv4.FlagInterface, true)
	if err != nil {
		return err
	}
	serveConn := MultiConn{
		conn: p,
	}

	return dhcp.Serve(&serveConn, handler)
}

func mainLoop() {
	/*
	 * Even with multiple VLANs and/or address ranges, we still only have a
	 * single UDP broadcast address.  We create a metahandler that receives
	 * all of the requests at that address, and routes them to the correct
	 * per-class handler.
	 */
	h := DHCPHandler{
		class: "_metahandler",
	}
	for {
		err := listenAndServeIf(&h)
		if err != nil {
			log.Printf("DHCP server failed: %v\n", err)
		} else {
			log.Printf("%s DHCP server exited\n", err)
		}
	}
}

//
// Determine the initial client -> class mappings
func initClientMap() {
	clients := config.GetClients()
	for client, info := range clients {
		clientClass[client] = info.Class
		log.Printf("%s starts as %s\n", client, info.Class)
	}
}

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	mcp, err := mcp.New(pname)
	if err != nil {
		log.Printf("Failed to connect to mcp\n")
	}

	flag.Parse()

	http.Handle("/metrics", promhttp.Handler())
	go http.ListenAndServe(*addr, nil)

	broker.Init(pname)
	broker.Handle(base_def.TOPIC_CONFIG, config_event)
	broker.Connect()
	defer broker.Disconnect()
	broker.Ping()

	// Interface to configd
	config = ap_common.NewConfig(pname)
	initClientMap()

	initHandlers()
	if mcp != nil {
		mcp.SetStatus("online")
	}
	log.Printf("DHCP server online\n")
	mainLoop()
	log.Printf("Shutting down\n")

	os.Exit(0)
}
