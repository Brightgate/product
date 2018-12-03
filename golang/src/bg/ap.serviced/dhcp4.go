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

package main

import (
	"flag"
	"fmt"
	"math/rand"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"bg/ap_common/aputil"
	"bg/ap_common/network"
	"bg/base_def"
	"bg/base_msg"
	"bg/common/cfgapi"

	"github.com/golang/protobuf/proto"
	dhcp "github.com/krolaw/dhcp4"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/net/ipv4"
)

var (
	verbose = flag.Bool("v", false, "verbose logging")

	handlers   = make(map[string]*ringHandler)
	domainName string

	sharedRouter net.IP     // without vlans, all rings share a
	sharedSubnet *net.IPNet // subnet and a router node

	// Which authentication type is required by each interface
	ifaceToAuthType = make(map[string]string)

	// Track the interface on which each client's DHCP request arrives
	clientRequestOn = make(map[string]string)

	dhcpMetrics struct {
		requests    prometheus.Counter
		provisioned prometheus.Counter
		claimed     prometheus.Counter
		renewed     prometheus.Counter
		released    prometheus.Counter
		declined    prometheus.Counter
		expired     prometheus.Counter
		rejected    prometheus.Counter
		exhausted   prometheus.Counter
	}
)

func getRing(hwaddr string) string {
	var ring string

	if client := clients[hwaddr]; client != nil {
		ring = client.Ring
	}

	return ring
}

/*******************************************************
 *
 * Communication with message broker
 */
func dhcpIPv4Expired(hwaddr string) {
	// Watch for lease expirations in @/clients/<macaddr>/ipv4.  We actually
	// clean up expired leases as a side effect of handing out new ones, so
	// all we do here is log it.
	slog.Infof("Lease for %s expired", hwaddr)
	dhcpMetrics.expired.Inc()
}

func dhcpIPv4Changed(hwaddr string, client *cfgapi.ClientInfo) {
	//
	// In most cases, this change will just be a broadcast notification of a
	// lease change we just registered.  All other cases should be an IP
	// address being statically assigned.  Anything else is user error.
	//

	ipaddr := client.IPv4
	expires := client.Expires

	ring := getRing(hwaddr)
	if ring == "" {
		// While we could assign an address to a client we've never seen
		// before, it's up to somebody else to create the initial client
		// record for us to work with.
		slog.Warnf("Attempted to assign %s to non-existent client %s",
			ipaddr, hwaddr)
		return
	}

	h := handlers[ring]
	if !dhcp.IPInRange(h.rangeStart, h.rangeEnd, ipaddr) {
		slog.Warnf("%s assigned %s, out of its ring range (%v - %v)",
			hwaddr, ipaddr, h.rangeStart, h.rangeEnd)
		return
	}

	h.Lock()
	defer h.Unlock()

	var oldipv4 net.IP
	l := h.leaseSearch(hwaddr)
	if l != nil {
		if ipaddr.Equal(l.ipaddr) {
			new := time.Now()
			lease := new

			if expires != nil {
				new = *expires
			}
			if l.expires != nil {
				lease = *l.expires
			}

			if new.Equal(lease) {
				// The expiration times are missing or the same,
				// so we're resetting the same static IP or
				// seeing our own notificiation
				return
			}
		}

		if expires != nil {
			// Either somebody else explicitly made a dynamic IP
			// assignment (which would be user error), or we've
			// already changed the assignment since this
			// notification was sent.  Either way, we're ignoring
			// it.
			slog.Infof("Rejecting ipv4 assignment %s->%s",
				ipaddr, hwaddr)
			return
		}

		oldipv4 = l.ipaddr
	}

	if !ipaddr.Equal(oldipv4) {
		if oldipv4 != nil {
			notifyRelease(oldipv4)
		}
		notifyProvisioned(ipaddr)
	}
	l = h.getLease(ipaddr)
	h.recordLease(l, hwaddr, "", ipaddr, nil)
}

func dhcpDeleteEvent(hwaddr string) {
	clientMtx.Lock()
	client, ok := clients[hwaddr]
	clientMtx.Unlock()

	if ok {
		slog.Debugf("Handling deletion of client %s", hwaddr)

		if ring := client.Ring; ring != "" {
			h := handlers[ring]
			h.Lock()
			if l := h.leaseSearch(hwaddr); l != nil {
				dhcpMetrics.released.Inc()
				h.releaseLease(l, hwaddr)
			}
			h.Unlock()
		}
	}
	delete(clientRequestOn, hwaddr)
}

func dhcpRingChanged(hwaddr string, client *cfgapi.ClientInfo, old string) {
	slog.Infof("changing %s to %s", hwaddr, client.Ring)
}

func configNodesChanged(path []string, val string, expires *time.Time) {
	initAuthMap()
}

func propPath(hwaddr, prop string) string {
	return fmt.Sprintf("@/clients/%s/%s", hwaddr, prop)
}

/*
 * This is the first time we've seen this device.  Send an ENTITY message with
 * its hardware address, name, and any IP address it's requesting.
 */
func notifyNewEntity(p dhcp.Packet, options dhcp.Options, authType string) {
	ipaddr := p.CIAddr()
	hwaddr := network.HWAddrToUint64(p.CHAddr())
	hostname := string(options[dhcp.OptionHostName])

	slog.Infof("New client %s (name: %q incoming IP address: %s)",
		p.CHAddr().String(), hostname, ipaddr.String())
	entity := &base_msg.EventNetEntity{
		Timestamp:  aputil.NowToProtobuf(),
		Sender:     proto.String(brokerd.Name),
		Debug:      proto.String("-"),
		MacAddress: proto.Uint64(hwaddr),
	}
	if authType != "" {
		entity.Authtype = proto.String(authType)
	}
	if hostname != "" {
		entity.Hostname = proto.String(hostname)
	}
	if ipv4 := network.IPAddrToUint32(ipaddr); ipv4 != 0 {
		entity.Ipv4Address = proto.Uint32(ipv4)
	}

	err := brokerd.Publish(entity, base_def.TOPIC_ENTITY)
	if err != nil {
		slog.Warnf("couldn't publish %s: %v", base_def.TOPIC_ENTITY, err)
	}
}

/*
 * A provisioned IP address has now been claimed by a client.
 */
func notifyClaimed(p dhcp.Packet, ipaddr net.IP, name string,
	dur time.Duration) {

	ttl := uint32(dur.Seconds())

	action := base_msg.EventNetResource_CLAIMED
	resource := &base_msg.EventNetResource{
		Timestamp:   aputil.NowToProtobuf(),
		Sender:      proto.String(brokerd.Name),
		Debug:       proto.String("-"),
		Action:      &action,
		Ipv4Address: proto.Uint32(network.IPAddrToUint32(ipaddr)),
		Hostname:    proto.String(name),
		Duration:    proto.Uint32(ttl),
	}

	err := brokerd.Publish(resource, base_def.TOPIC_RESOURCE)
	if err != nil {
		slog.Warnf("couldn't publish %s: %v", base_def.TOPIC_RESOURCE, err)
	}
}

/*
 * We've have provisionally assigned an IP address to a client.  Send a
 * net.resource message indicating that that address is no longer available.
 */
func notifyProvisioned(ipaddr net.IP) {
	action := base_msg.EventNetResource_PROVISIONED
	resource := &base_msg.EventNetResource{
		Timestamp:   aputil.NowToProtobuf(),
		Sender:      proto.String(brokerd.Name),
		Debug:       proto.String("-"),
		Action:      &action,
		Ipv4Address: proto.Uint32(network.IPAddrToUint32(ipaddr)),
	}

	err := brokerd.Publish(resource, base_def.TOPIC_RESOURCE)
	if err != nil {
		slog.Warnf("couldn't publish %s: %v", base_def.TOPIC_RESOURCE, err)
	}
}

/*
 * An IP address has been released.  It may have been released or declined by
 * the client, or the lease may have expired.
 */
func notifyRelease(ipaddr net.IP) {
	action := base_msg.EventNetResource_RELEASED
	resource := &base_msg.EventNetResource{
		Timestamp:   aputil.NowToProtobuf(),
		Sender:      proto.String(brokerd.Name),
		Debug:       proto.String("-"),
		Action:      &action,
		Ipv4Address: proto.Uint32(network.IPAddrToUint32(ipaddr)),
	}

	err := brokerd.Publish(resource, base_def.TOPIC_RESOURCE)
	if err != nil {
		slog.Warnf("couldn't publish %s: %v", base_def.TOPIC_RESOURCE, err)
	}
}

/*
 * Report on the DHCP options used by the client. Used for DHCP fingerprinting.
 * Send to the cloud for processing, and, with verbose turned on, to the log for
 * human consumption.
 */
func notifyOptions(hwaddr net.HardwareAddr, options dhcp.Options, msgType dhcp.MessageType) {
	msg := &base_msg.DHCPOptions{
		Timestamp:     aputil.NowToProtobuf(),
		Sender:        proto.String(brokerd.Name),
		Debug:         proto.String("-"),
		MacAddress:    proto.Uint64(network.HWAddrToUint64(hwaddr)),
		MsgType:       proto.Uint32(uint32(msgType)),
		ParamReqList:  options[dhcp.OptionParameterRequestList],
		VendorClassId: options[dhcp.OptionVendorClassIdentifier],
	}

	err := brokerd.Publish(msg, base_def.TOPIC_OPTIONS)
	if err != nil {
		slog.Warnf("couldn't publish %s: %v", base_def.TOPIC_OPTIONS, err)
	}

	if !*verbose {
		return
	}

	optionkeys := make([]int, 0)
	for opt := range options {
		optionkeys = append(optionkeys, int(opt))
	}
	sort.Ints(optionkeys)
	slog.Debugf("    Options: %v", optionkeys)
	slog.Debugf("    ParameterRequestList: %v", options[dhcp.OptionParameterRequestList])
	if vendorClassIdentifier, ok := options[dhcp.OptionVendorClassIdentifier]; ok {
		slog.Debugf("    VendorClassIdentifier: %s", string(vendorClassIdentifier))
	}
	if hostName, ok := options[dhcp.OptionHostName]; ok {
		slog.Debugf("    HostName: %s", string(hostName))
	}
	slog.Debugf("    ClientIdentifier: %x", options[dhcp.OptionClientIdentifier])
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

type ringHandler struct {
	ring       string        // Client ring eligible for this server
	subnet     net.IPNet     // Subnet being managed
	serverIP   net.IP        // DHCP server's IP
	options    dhcp.Options  // Options to send to DHCP Clients
	rangeStart net.IP        // Start of IP range to distribute
	rangeEnd   net.IP        // End of IP range to distribute
	rangeSpan  int           // Number of IPs to distribute (starting from start)
	duration   time.Duration // Lease period
	leases     []lease       // Per-lease state

	sync.Mutex
}

/*
 * Construct a DHCP NAK message
 */
func (h *ringHandler) nak(p dhcp.Packet) dhcp.Packet {
	return dhcp.ReplyPacket(p, dhcp.NAK, h.serverIP, nil, 0, nil)
}

/*
 * Handle DISCOVER messages
 */
func (h *ringHandler) discover(p dhcp.Packet, options dhcp.Options) dhcp.Packet {
	hwaddr := p.CHAddr().String()
	slog.Infof("DISCOVER %s", hwaddr)

	notifyOptions(p.CHAddr(), options, dhcp.Discover)

	l := h.leaseAssign(hwaddr)
	if l == nil {
		slog.Warnf("Out of %s leases", h.ring)
		dhcpMetrics.exhausted.Inc()
		return h.nak(p)
	}
	slog.Infof("  OFFER %s to %s", l.ipaddr, l.hwaddr)

	notifyProvisioned(l.ipaddr)
	dhcpMetrics.provisioned.Inc()
	return dhcp.ReplyPacket(p, dhcp.Offer, h.serverIP, l.ipaddr, h.duration,
		h.options.SelectOrderOrAll(options[dhcp.OptionParameterRequestList]))
}

// If the client specifies a hostname, sanitize it and return it to the caller.
// This includes extracting a hostname from a FQDN, truncating the name at the
// first NULL byte (if any), and verifying that the remaining substring is a
// valid DNS name.
func extractHostname(options dhcp.Options) string {
	name := string(options[dhcp.OptionHostName])
	if dot := strings.Index(name, "."); dot >= 0 {
		name = name[:dot]
	}

	if null := strings.IndexByte(name, 0); null >= 0 {
		name = name[:null]
	}

	if !network.ValidDNSName(name) {
		name = ""
	}

	return name
}

/*
 * Handle REQUEST messages
 */
func (h *ringHandler) request(p dhcp.Packet, options dhcp.Options) dhcp.Packet {
	var reqIP net.IP

	hwaddr := p.CHAddr().String()
	slog.Infof("REQUEST for %s on %s", hwaddr, h.ring)
	dhcpMetrics.requests.Inc()

	notifyOptions(p.CHAddr(), options, dhcp.Request)

	server, ok := options[dhcp.OptionServerIdentifier]
	if ok && !net.IP(server).Equal(h.serverIP) {
		return nil // Message not for this dhcp server
	}

	clientMtx.Lock()
	ring := getRing(hwaddr)
	clientMtx.Unlock()
	if ring != h.ring {
		slog.Infof("   '%s' client requesting on '%s' ring",
			ring, h.ring)
		dhcpMetrics.rejected.Inc()
		return h.nak(p)
	}

	requestOption := net.IP(options[dhcp.OptionRequestedIPAddress])

	/*
	 * If this client already has an IP address assigned (either statically,
	 * or a previously assigned dynamic address), that overrides any address
	 * it might ask for.
	 */
	action := ""
	current := h.leaseSearch(hwaddr)
	if current != nil {
		reqIP = current.ipaddr
		if requestOption != nil {
			if reqIP.Equal(requestOption) {
				action = "renewing"
				dhcpMetrics.renewed.Inc()
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
	} else if requestOption != nil {
		reqIP = requestOption
		action = "granting request"
	} else {
		reqIP = net.IP(p.CIAddr())
		action = "CLAIMED"
	}

	if len(reqIP) != 4 || reqIP.Equal(net.IPv4zero) {
		slog.Warnf("Invalid reqIP %s from %s", reqIP.String(), hwaddr)
		dhcpMetrics.rejected.Inc()
		return h.nak(p)
	}

	l := h.getLease(reqIP)
	if l == nil || !l.assigned || l.hwaddr != hwaddr {
		slog.Warnf("Invalid lease of %s for %s", reqIP.String(), hwaddr)
		dhcpMetrics.rejected.Inc()
		return h.nak(p)
	}

	slog.Infof("   REQUEST %s %s", action, reqIP.String())
	if l.static {
		l.expires = nil
	} else {
		expires := time.Now().Add(h.duration)
		l.expires = &expires
	}

	l.name = extractHostname(options)
	slog.Infof("   REQUEST assigned %s to %s (%q) until %s",
		l.ipaddr, hwaddr, l.name, l.expires)

	config.CreateProp(propPath(hwaddr, "ipv4"), l.ipaddr.String(), l.expires)
	config.CreateProp(propPath(hwaddr, "dhcp_name"), l.name, l.expires)
	notifyClaimed(p, l.ipaddr, l.name, h.duration)
	dhcpMetrics.claimed.Inc()

	if h.ring == base_def.RING_INTERNAL {
		// Clients asking for addresses on the internal network are
		// notified that they are expected to operate as satellite nodes
		o := []dhcp.Option{
			{
				Code:  1,
				Value: []byte(base_def.MODE_SATELLITE),
			},
		}

		vendorOpt, _ := network.DHCPEncodeOptions(o)
		h.options[dhcp.OptionVendorSpecificInformation] = vendorOpt
	}

	// Note: even for static IP assignments, we tell the requesting client
	// that it needs to renew at the regular period for the ring.  This lets
	// us revoke a static assignment at some point in the future.
	return dhcp.ReplyPacket(p, dhcp.ACK, h.serverIP, l.ipaddr, h.duration,
		h.options.SelectOrderOrAll(options[dhcp.OptionParameterRequestList]))
}

/*
 * If this lease is unassigned, or assigned to somebody else, return 'false'.
 * Otherwise, release it, update the configuration, send a notification, and
 * return 'true'
 */
func (h *ringHandler) releaseLease(l *lease, hwaddr string) bool {
	if l == nil || !l.assigned || l.hwaddr != hwaddr {
		return false
	}
	if l.expires == nil {
		return false
	}

	l.assigned = false
	notifyRelease(l.ipaddr)
	config.DeleteProp(propPath(l.hwaddr, "ipv4"))
	return true
}

/*
 * Handle RELEASE message for a specific IP address
 */
func (h *ringHandler) release(p dhcp.Packet) {
	hwaddr := p.CHAddr().String()
	ipaddr := p.CIAddr()

	l := h.getLease(ipaddr)
	if l == nil {
		slog.Debugf("Client %s RELEASE unsupported address: %s",
			hwaddr, ipaddr.String())
		return
	}
	if h.releaseLease(l, hwaddr) {
		dhcpMetrics.released.Inc()
		slog.Infof("RELEASE %s", hwaddr)
	}
}

/*
 * Handle DECLINE message.  We only get the client's MAC address, so we have to
 * scan all possible leases to find the one being declined
 */
func (h *ringHandler) decline(p dhcp.Packet) {
	hwaddr := p.CHAddr().String()

	l := h.leaseSearch(hwaddr)
	if h.releaseLease(l, hwaddr) {
		dhcpMetrics.declined.Inc()
		slog.Infof("DECLINE for %s", hwaddr)
	}
}

//
// Based on the client's MAC address, identify its ring and return the
// appropriate DHCP handler.
//
func selectRingHandler(p dhcp.Packet, options dhcp.Options) *ringHandler {
	var handler *ringHandler

	mac := p.CHAddr().String()
	requestIface := clientRequestOn[mac]
	authType := ifaceToAuthType[requestIface]

	clientMtx.Lock()
	ring := getRing(mac)
	clientMtx.Unlock()

	if authType == "" {
		slog.Debugf("Ignoring DHCP request from %s on unsupported "+
			"iface %s", mac, requestIface)
	} else if ring == "" {
		// If we don't have a ring assignment for this client, then
		// there are two possibilities.  First, it's a brand new
		// wireless client, and its DHCP request arrived before configd
		// finished handling the NetEntity event from networkd.  Second,
		// it's a wired client which won't trigger a networkd NetEntity
		// event, since there is no explicit 'join the network' process.
		// With wired and wifi interfaces attached to the same bridge,
		// we can't distinguish between the two.  We'll send a NetEntity
		// event ourselves, which will be harmlessly redundant in the
		// first case.  Because we have no authentication event, we have
		// to reverse-engineer the authentication method from the VLAN
		// the request arrived on.
		slog.Infof("New client %s incoming on %s.  Auth: %s",
			mac, requestIface, authType)
		notifyNewEntity(p, options, authType)
	} else if handler = handlers[ring]; handler == nil {
		slog.Infof("Client %s identified on unknown ring '%s'",
			mac, ring)
	}
	// Once we've handled the DHCP request for this client, we can forget
	// the source interface.  This prevents the map from growing without
	// bound.
	delete(clientRequestOn, mac)

	return handler
}

func (h *ringHandler) ServeDHCP(p dhcp.Packet, msgType dhcp.MessageType,
	options dhcp.Options) (d dhcp.Packet) {

	ringHandler := selectRingHandler(p, options)
	if ringHandler == nil {
		return nil
	}

	ringHandler.Lock()
	defer ringHandler.Unlock()
	switch msgType {

	case dhcp.Discover:
		return ringHandler.discover(p, options)

	case dhcp.Request:
		return ringHandler.request(p, options)

	case dhcp.Release:
		ringHandler.release(p)

	case dhcp.Decline:
		ringHandler.decline(p)
	}
	return nil
}

func (h *ringHandler) recordLease(l *lease, hwaddr, name string, ipv4 net.IP,
	etime *time.Time) {
	l.name = name
	l.hwaddr = hwaddr
	l.ipaddr = ipv4.To4()
	l.expires = etime
	l.static = (etime == nil)
	l.assigned = true
}

/*
 * If this nic already has a live lease, return that.  Otherwise, assign an
 * available lease at random.  A 'nil' response indicates that all leases are
 * currently assigned.
 */
func (h *ringHandler) leaseAssign(hwaddr string) *lease {
	var rval *lease

	now := time.Now()
	target := rand.Intn(h.rangeSpan)
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
		ipv4 := dhcp.IPAdd(h.rangeStart, assigned)
		rval = &h.leases[assigned]
		expires := time.Now().Add(h.duration)
		h.recordLease(rval, hwaddr, "", ipv4, &expires)
	}
	return rval
}

/*
 * Scan all leases in all ranges, looking for an IP address assigned to this
 * NIC.
 */
func (h *ringHandler) leaseSearch(hwaddr string) *lease {
	for i := 0; i < h.rangeSpan; i++ {
		l := &h.leases[i]
		if l.assigned && l.hwaddr == hwaddr {
			return l
		}
	}
	return nil
}

func (h *ringHandler) getLease(ip net.IP) *lease {
	if !dhcp.IPInRange(h.rangeStart, h.rangeEnd, ip) {
		return nil
	}

	slot := dhcp.IPRange(h.rangeStart, ip) - 1
	return &h.leases[slot]
}

func ipRange(ring, subnet string) (start net.IP, ipnet *net.IPNet, span int) {
	var err error

	start, ipnet, err = net.ParseCIDR(subnet)
	if err != nil {
		slog.Fatalf("Invalid subnet %v for ring %s: %v",
			subnet, ring, err)
	}

	ones, bits := ipnet.Mask.Size()
	span = (1<<uint32(bits-ones) - 2)
	return
}

//
// Instantiate a new DHCP handler for the given ring
//
func newHandler(name string, rings cfgapi.RingMap) *ringHandler {
	ring := rings[name]
	duration := time.Duration(ring.LeaseDuration) * time.Minute
	start, subnet, span := ipRange(name, ring.Subnet)

	myip := dhcp.IPAdd(start, 1)
	if name == base_def.RING_INTERNAL {
		// Shrink the range to exclude the router
		span--
		start = dhcp.IPAdd(start, 1)
	} else {
		// Exclude the lower addresses that are reserved for the routers
		// on each of the mesh APs
		iname := base_def.RING_INTERNAL
		isub := rings[base_def.RING_INTERNAL].Subnet
		_, _, reserved := ipRange(iname, isub)
		start = dhcp.IPAdd(start, reserved)
		span -= reserved
	}
	// Exclude the broadcast address
	span--

	h := ringHandler{
		ring:       name,
		subnet:     *subnet,
		serverIP:   myip,
		rangeStart: start,
		rangeEnd:   dhcp.IPAdd(start, span),
		rangeSpan:  span,
		duration:   duration,
		options: dhcp.Options{
			dhcp.OptionSubnetMask:                 subnet.Mask,
			dhcp.OptionRouter:                     myip,
			dhcp.OptionDomainNameServer:           myip,
			dhcp.OptionNetworkTimeProtocolServers: myip,
		},
		leases: make([]lease, span, span),
	}
	h.options[dhcp.OptionDomainName] = []byte(domainName)
	h.options[dhcp.OptionVendorClassIdentifier] = []byte("Brightgate, Inc.")

	return &h
}

func (h *ringHandler) recoverLeases() {
	// Preemptively pull the network and DHCP server from the pool
	h.leases[0].assigned = true
	h.leases[1].assigned = true

	clientMtx.Lock()
	for macaddr, client := range clients {
		if client.IPv4 == nil {
			continue
		}

		if l := h.getLease(client.IPv4); l != nil {
			h.recordLease(l, macaddr, client.DHCPName, client.IPv4,
				client.Expires)
		}
	}
	clientMtx.Unlock()
}

func initAuthMap() {
	newMap := make(map[string]string)
	for _, ring := range config.GetRings() {
		newMap[ring.Bridge] = ring.Auth
	}
	ifaceToAuthType = newMap
}

func initHandlers() {
	// Iterate over the known rings.  For each one, create a DHCP handler to
	// manage its subnet.
	rings := config.GetRings()
	for name := range rings {
		h := newHandler(name, rings)
		h.recoverLeases()
		handlers[h.ring] = h
	}
}

// Extract the requesting client's MAC address from inside a raw DHCP packet
func extractClientMac(b []byte, n int) string {
	var mac string

	p := dhcp.Packet(b[:n])
	if p.HLen() <= 16 {
		mac = p.CHAddr().String()
	}
	return mac
}

type multiConn struct {
	conn *ipv4.PacketConn
	cm   *ipv4.ControlMessage
}

// On errors, we set the 'received bytes' value to 0, which tells the
// library to skip any further parsing of the packet.
func (s *multiConn) ReadFrom(b []byte) (n int, addr net.Addr, err error) {
	var iface *net.Interface
	var clientMac string

	n, s.cm, addr, err = s.conn.ReadFrom(b)
	if err != nil {
		slog.Warnf("ReadFrom() failed: %v", err)
	} else if s.cm == nil {
		slog.Warnf("DHCP read has no ControlMessage")
	} else if n < 240 {
		slog.Warnf("Invalid DHCP packet: only %d bytes", n)
	} else if clientMac = extractClientMac(b, n); clientMac == "" {
		// This looks like an invalid DHCP packet.
		slog.Warnf("Invalid DHCP packet: no mac address found")
		n = 0
	} else if iface, err = net.InterfaceByIndex(s.cm.IfIndex); err != nil {
		slog.Warnf("Failed interface lookup for request from %s: %v",
			clientMac, err)
		n = 0
	} else {
		clientRequestOn[clientMac] = iface.Name
		slog.Debugf("DHCP pkt from %s on %s", clientMac, iface.Name)
	}
	return
}

func (s *multiConn) WriteTo(b []byte, addr net.Addr) (n int, err error) {
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
	serveConn := multiConn{
		conn: p,
	}

	return dhcp.Serve(&serveConn, handler)
}

func dhcpLoop() {
	/*
	 * Even with multiple VLANs and/or address ranges, we still only have a
	 * single UDP broadcast address.  We create a metahandler that receives
	 * all of the requests at that address, and routes them to the correct
	 * per-ring handler.
	 */
	h := ringHandler{
		ring: "_metahandler",
	}
	for {
		err := listenAndServeIf(&h)
		if err != nil {
			slog.Fatalf("DHCP server failed: %v", err)
		} else {
			slog.Infof("%s DHCP server exited", err)
		}
	}
}

func dhcpPrometheusInit() {
	dhcpMetrics.requests = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "dhcp4d_requests",
		Help: "Number of addresses requested",
	})
	dhcpMetrics.provisioned = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "dhcp4d_provisioned",
		Help: "Number of addresses provisioned",
	})
	dhcpMetrics.claimed = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "dhcp4d_claimed",
		Help: "Number of addresses claimed",
	})
	dhcpMetrics.renewed = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "dhcp4d_renewed",
		Help: "Number of addresses renewed",
	})
	dhcpMetrics.released = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "dhcp4d_released",
		Help: "Number of addresses released",
	})
	dhcpMetrics.declined = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "dhcp4d_declined",
		Help: "Number of addresses declined",
	})
	dhcpMetrics.expired = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "dhcp4d_expired",
		Help: "Number of addresses expired",
	})
	dhcpMetrics.rejected = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "dhcp4d_rejected",
		Help: "Number of addresses rejected",
	})
	dhcpMetrics.exhausted = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "dhcp4d_exhausted",
		Help: "Number of exhaustion failures",
	})

	prometheus.MustRegister(dhcpMetrics.requests)
	prometheus.MustRegister(dhcpMetrics.provisioned)
	prometheus.MustRegister(dhcpMetrics.claimed)
	prometheus.MustRegister(dhcpMetrics.released)
	prometheus.MustRegister(dhcpMetrics.declined)
	prometheus.MustRegister(dhcpMetrics.expired)
	prometheus.MustRegister(dhcpMetrics.rejected)
	prometheus.MustRegister(dhcpMetrics.exhausted)
}

func dhcpInit() {
	var err error

	dhcpPrometheusInit()

	domainName, err = config.GetDomain()
	if err != nil {
		slog.Fatalf("failed to fetch gateway domain: %v", err)
	}

	initHandlers()
	initAuthMap()

	go dhcpLoop()

	config.HandleChange(`^@/nodes/.*$`, configNodesChanged)
}
