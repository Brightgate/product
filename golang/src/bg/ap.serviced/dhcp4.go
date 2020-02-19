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

package main

import (
	"fmt"
	"math/rand"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"bg/ap_common/apcfg"
	"bg/ap_common/aputil"
	"bg/ap_common/bgmetrics"
	bgdhcp "bg/ap_common/dhcp"
	"bg/base_def"
	"bg/base_msg"
	"bg/common/cfgapi"
	"bg/common/network"

	"github.com/golang/protobuf/proto"
	dhcp "github.com/krolaw/dhcp4"
	"golang.org/x/net/ipv4"
)

var (
	handlers   = make(map[string]*ringHandler)
	domainName string

	sharedRouter net.IP     // without vlans, all rings share a
	sharedSubnet *net.IPNet // subnet and a router node

	// Track the interface on which each client's DHCP request arrives
	clientRequestOn = make(map[string]*net.Interface)
	badRingRequests = make(map[string]string)

	verbose = apcfg.Bool("verbose", false, true, nil)

	dhcpMetrics struct {
		requests    *bgmetrics.Counter
		provisioned *bgmetrics.Counter
		claimed     *bgmetrics.Counter
		renewed     *bgmetrics.Counter
		released    *bgmetrics.Counter
		declined    *bgmetrics.Counter
		expired     *bgmetrics.Counter
		rejected    *bgmetrics.Counter
		exhausted   *bgmetrics.Counter
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
func leaseDurationChanged(path []string, val string, expires *time.Time) {
	h := handlers[path[1]]
	minutes, _ := strconv.Atoi(val)
	if h != nil && minutes > 0 {
		h.Lock()
		h.duration = time.Minute * time.Duration(minutes)
		h.Unlock()
	}
}

func dhcpIPv4Expired(hwaddr string) {
	// Watch for lease expirations in @/clients/<macaddr>/ipv4.  We actually
	// clean up expired leases as a side effect of handing out new ones, so
	// all we do here is log it.
	slog.Infof("Lease for %s expired", hwaddr)
	dhcpMetrics.expired.Inc()
}

func dhcpIPv4Changed(hwaddr string, client *cfgapi.ClientInfo) {
	var expires time.Time

	if client.Expires != nil {
		expires = *client.Expires
	}

	ipaddr := client.IPv4
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

	l := h.leaseSearch(hwaddr)

	if l != nil && ipaddr.Equal(l.ipaddr) && expires.Equal(l.expires) {
		// If the IP address and expiration time haven't changed, then
		// this is a no-op.  We're probably responding to configd's
		// broadcast notification of the property change we just issued.
		return
	}

	if !expires.IsZero() {
		// Either somebody else explicitly made a dynamic IP
		// assignment (which would be user error), or we've
		// already changed the assignment since this
		// notification was sent.  Either way, we're ignoring
		// it.
		slog.Infof("Rejecting non-static ipv4 assignment %s->%s",
			ipaddr, hwaddr)
		return
	}

	if l != nil && !ipaddr.Equal(l.ipaddr) {
		// The client's address is changing, so release its old lease
		l.assigned = false
		l.confirmed = false
		notifyRelease(l.ipaddr)
	}

	l = h.getLease(ipaddr)
	l.record(hwaddr, expires)
}

func dhcpDeleteEvent(hwaddr string) {
	slog.Infof("Handling deletion of client %s", hwaddr)

	delete(clientRequestOn, hwaddr)

	client, ok := clients[hwaddr]
	if !ok {
		return
	}

	if ring := client.Ring; ring != "" {
		h := handlers[ring]
		h.Lock()
		if l := h.leaseSearch(hwaddr); l != nil {
			dhcpMetrics.released.Inc()
			if l.expires.IsZero() {
				// If this was a statically assigned lease, give
				// the client an expiration time of 'now' to
				// prevent it from being given a static address
				// on its next request.
				l.expires = time.Now()
				l.static = false
			}
			l.assigned = false
			l.confirmed = false
			notifyRelease(l.ipaddr)
		}
		h.Unlock()
	}
}

func dhcpRingChanged(hwaddr string, client *cfgapi.ClientInfo, old string) {
	slog.Infof("changing %s to %s", hwaddr, client.Ring)
}

func propPath(hwaddr, prop string) string {
	return fmt.Sprintf("@/clients/%s/%s", hwaddr, prop)
}

func notifyNetException(mac string, details string,
	reason base_msg.EventNetException_Reason) {

	slog.Debugf("NetException(%s, details: %s, reason: %d)", mac, details, reason)
	hwaddr, _ := net.ParseMAC(mac)
	entity := &base_msg.EventNetException{
		Timestamp:  aputil.NowToProtobuf(),
		Sender:     proto.String(brokerd.Name),
		Debug:      proto.String("-"),
		Reason:     &reason,
		Details:    []string{details},
		MacAddress: proto.Uint64(network.HWAddrToUint64(hwaddr)),
	}

	err := brokerd.Publish(entity, base_def.TOPIC_EXCEPTION)
	if err != nil {
		slog.Warnf("couldn't publish %s: %v", base_def.TOPIC_EXCEPTION, err)
	}
}

/*
 * This is the first time we've seen this device.  Send an ENTITY message with
 * its hardware address, name, and any IP address it's requesting.
 */
func notifyNewEntity(p dhcp.Packet, options dhcp.Options, ring string) {
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
	if ring != "" {
		entity.Ring = proto.String(ring)
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
func notifyClaimed(p dhcp.Packet, ipv4 net.IP, name string, dur time.Duration) {

	ttl := uint32(dur.Seconds())

	action := base_msg.EventNetResource_CLAIMED
	resource := &base_msg.EventNetResource{
		Timestamp:   aputil.NowToProtobuf(),
		Sender:      proto.String(brokerd.Name),
		Debug:       proto.String("-"),
		Action:      &action,
		Ipv4Address: proto.Uint32(network.IPAddrToUint32(ipv4)),
		Hostname:    proto.String(name),
		Duration:    proto.Uint32(ttl),
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

	optionkeys := aputil.SortIntKeys(options)
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
	hwaddr    string    // Client's CHAddr
	ipaddr    net.IP    // Client's IP address
	expires   time.Time // When the lease expires
	static    bool      // Statically assigned?
	assigned  bool      // Lease assigned to a client?
	confirmed bool      // Client accepted lease?
}

type ringHandler struct {
	ring       string        // Client ring eligible for this server
	serverIP   net.IP        // DHCP server's IP
	options    dhcp.Options  // Options to send to DHCP Clients
	rangeStart net.IP        // Start of IP range to distribute
	rangeEnd   net.IP        // End of IP range to distribute
	rangeSpan  int           // Number of IPs to distribute (starting from start)
	duration   time.Duration // Lease period
	leases     []*lease      // Per-lease state

	sync.Mutex
}

func (l *lease) String() string {
	var exp string

	if l.expires.IsZero() {
		exp = " static"
	} else {
		exp = " until " + l.expires.Format(time.Stamp)
	}

	return l.hwaddr + "->" + l.ipaddr.String() + exp

}

// If this lease has expired, clear its assigned/confirmed bits
func (l *lease) expireCheck() {
	if l.assigned && !l.expires.IsZero() && l.expires.Before(time.Now()) {
		slog.Infof("cleaning up expired lease: %s", l)
		l.assigned = false
		l.confirmed = false
	}
}

func (l *lease) record(hwaddr string, etime time.Time) {
	l.hwaddr = hwaddr
	l.expires = etime
	l.static = etime.IsZero()
	if l.static || l.expires.After(time.Now()) {
		slog.Infof("recorded lease: %s", l)
		l.assigned = true
	}
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

	l, err := h.leaseAssign(hwaddr)
	if err != nil {
		slog.Warnf("no lease assigned to %s: %v", hwaddr, err)
		return h.nak(p)
	}
	slog.Infof("  OFFER %s to %s", l.ipaddr, l.hwaddr)

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

	if strings.ToLower(name) == "localhost" || !network.ValidDNSName(name) {
		name = ""
	}

	return name
}

/*
 * Handle REQUEST messages
 */
func (h *ringHandler) request(p dhcp.Packet, options dhcp.Options) dhcp.Packet {
	var reqIP net.IP
	var expiresAt time.Time

	hwaddr := p.CHAddr().String()
	slog.Infof("REQUEST for %s on %s", hwaddr, h.ring)
	dhcpMetrics.requests.Inc()

	notifyOptions(p.CHAddr(), options, dhcp.Request)

	server, ok := options[dhcp.OptionServerIdentifier]
	if ok && !net.IP(server).Equal(h.serverIP) {
		return nil // Message not for this dhcp server
	}
	requestOption := net.IP(options[dhcp.OptionRequestedIPAddress])

	clientMtx.Lock()
	ring := getRing(hwaddr)
	clientMtx.Unlock()
	if ring != h.ring {
		slog.Infof("   '%s' client requesting lease on '%s' ring",
			ring, h.ring)
		dhcpMetrics.rejected.Inc()
		return h.nak(p)
	}

	// If this client already has an IP address assigned (either statically,
	// or a previously assigned dynamic address), that overrides any address
	// it might ask for.
	//
	// Each ring has a designated lease time.  Established clients are
	// assigned leases of that length.  New or migrating clients get a 2
	// minute lease, as we want their ring assignment to be stable before
	// granting them a long-term lease.
	action := ""
	current := h.leaseSearch(hwaddr)
	leaseDuration := 2 * time.Minute
	if current != nil {
		reqIP = current.ipaddr
		if requestOption != nil {
			if !reqIP.Equal(requestOption) {
				action = "overriding client request of " +
					requestOption.String()
			} else if current.confirmed {
				leaseDuration = h.duration
				action = "renewing"
				dhcpMetrics.renewed.Inc()
			} else {
				action = "confirming"
			}
		} else {
			leaseDuration = h.duration
			if current.static {
				// Note: even for static IP assignments, we tell
				// the requesting client that it needs to renew
				// at the regular period for the ring.  This
				// lets us revoke a static assignment at some
				// point in the future.
				action = "using static lease"
			} else {
				action = "found existing lease"
			}
		}

	} else if requestOption != nil {
		reqIP = requestOption
		action = "granting request"

	} else {
		reqIP = net.IP(p.CIAddr())
		action = "CLAIMED"
	}
	slog.Infof("   REQUEST %s %s", action, reqIP.String())

	if len(reqIP) != 4 || reqIP.Equal(net.IPv4zero) {
		slog.Warnf("Invalid reqIP %s from %s", reqIP, hwaddr)
		dhcpMetrics.rejected.Inc()
		return h.nak(p)
	}

	l := h.getLease(reqIP)
	if l == nil || (l.assigned && l.hwaddr != hwaddr) {
		slog.Warnf("Invalid lease of %s for %s", reqIP.String(), hwaddr)
		dhcpMetrics.rejected.Inc()
		return h.nak(p)
	}
	name := extractHostname(options)
	if !l.static {
		expiresAt = time.Now().Add(leaseDuration)
	}
	l.record(hwaddr, expiresAt)

	slog.Infof("   REQUEST assigned %s for %q", l, name)
	l.confirmed = true

	config.CreateProp(propPath(hwaddr, "ipv4"), l.ipaddr.String(), &l.expires)
	config.CreateProp(propPath(hwaddr, "dhcp_name"), name, nil)
	notifyClaimed(p, l.ipaddr, name, leaseDuration)
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

		vendorOpt, _ := bgdhcp.EncodeOptions(o)
		h.options[dhcp.OptionVendorSpecificInformation] = vendorOpt
	}

	return dhcp.ReplyPacket(p, dhcp.ACK, h.serverIP, l.ipaddr, leaseDuration,
		h.options.SelectOrderOrAll(options[dhcp.OptionParameterRequestList]))
}

/*
 * If this lease is unassigned, or assigned to somebody else, return 'false'.
 * Otherwise, release it, update the configuration, send a notification, and
 * return 'true'
 */
func (h *ringHandler) releaseLease(l *lease, hwaddr string) bool {
	if l == nil || !l.assigned || l.hwaddr != hwaddr || l.static {
		return false
	}
	if l.expires.IsZero() {
		return false
	}

	l.assigned = false
	l.confirmed = false
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
		slog.Infof("RELEASE %s", l)
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
	var err error

	mac := p.CHAddr().String()
	requestIface := clientRequestOn[mac]
	requestRing := ifaceToRing[requestIface.Index]

	clientMtx.Lock()
	ring := getRing(mac)
	clientMtx.Unlock()

	if ring == "" {
		// If we don't have a ring assignment for this client, then
		// there are two possibilities.  First, it's a brand new
		// wireless client, and its DHCP request arrived before configd
		// finished handling the NetEntity event from networkd.  Second,
		// it's a wired client which won't trigger a networkd NetEntity
		// event, since there is no explicit 'join the network' step.
		// With wired and wifi interfaces attached to the same bridge,
		// we can't distinguish between the two.  We'll send a NetEntity
		// event ourselves, which will be harmlessly redundant in the
		// first case.  Because we have no authentication event, we have
		// to reverse-engineer the ring from the VLAN the request
		// arrived on.
		if requestRing != "" {
			slog.Infof("New client %s on %d (%s)", mac,
				requestIface.Index, requestIface.Name)
			notifyNewEntity(p, options, requestRing)
		} else {
			slog.Infof("Ignoring request from unknown client %s "+
				"on %d (%s)", mac, requestIface.Index, requestIface.Name)
		}

	} else if ring != requestRing {
		src := requestIface.Name
		if requestRing != "" {
			src += "('" + requestRing + "' ring)"
		}
		txt := "client " + mac + " from " + ring +
			" ring requested address on " + src
		err = fmt.Errorf(txt)

		// Once a client starts requesting on the wrong ring, they may
		// keep doing so every few seconds forever.  We don't want to
		// log all of those requests
		if badRingRequests[mac] != src {
			slog.Warnf("%s", txt)
			badRingRequests[mac] = src
			notifyNetException(mac, txt,
				base_msg.EventNetException_BAD_RING)
		}

	} else if handler = handlers[ring]; handler == nil {
		aputil.ReportError("Client %s on unknown ring '%s'", mac, ring)
	}

	// Once we've handled the DHCP request for this client, we can forget
	// the source interface.  This prevents the map from growing without
	// bound.
	delete(clientRequestOn, mac)
	if err == nil {
		delete(badRingRequests, mac)
	}

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

/*
 * If this nic already has a live lease, return that.  Otherwise, assign an
 * available lease at random.
 */
func (h *ringHandler) leaseAssign(hwaddr string) (*lease, error) {
	var err error
	var assigned *lease

	if ring := getRing(hwaddr); ring != "" && ring != h.ring {
		// This shouldn't be possible, since the hwaddr was used to
		// select this ring hander, but it's still worth checking.
		return nil, fmt.Errorf("%s from '%s' requested address in '%s'",
			hwaddr, ring, h.ring)
	}

	slot := 0
	targetSlot := rand.Intn(h.rangeSpan)

	for i, l := range h.leases {
		if l.assigned && l.hwaddr == hwaddr {
			assigned = l
			break
		}

		l.expireCheck()
		if !l.assigned && (assigned == nil || slot < targetSlot) {
			assigned = l
			slot = i
		}
	}

	if assigned != nil {
		assigned.record(hwaddr, time.Now().Add(h.duration))
	} else {
		dhcpMetrics.exhausted.Inc()
		err = fmt.Errorf("out of leases on '%s' ring", h.ring)
	}

	return assigned, err
}

/*
 * Scan all leases in all ranges, looking for an IP address assigned to this
 * NIC.
 */
func (h *ringHandler) leaseSearch(hwaddr string) *lease {
	var rval *lease

	for _, l := range h.leases {
		l.expireCheck()

		if l.assigned && l.hwaddr == hwaddr {
			if rval != nil {
				slog.Warnf("multiple leases for %s: %v and %v",
					hwaddr, rval.ipaddr, l.ipaddr)
			} else {
				rval = l
			}
		}
	}
	return rval
}

func (h *ringHandler) getLease(ip net.IP) *lease {
	if !dhcp.IPInRange(h.rangeStart, h.rangeEnd, ip) {
		return nil
	}

	slot := dhcp.IPRange(h.rangeStart, ip) - 1
	return h.leases[slot]
}

func ipRange(ring *cfgapi.RingConfig) (net.IP, int) {
	start, ipnet, _ := net.ParseCIDR(ring.Subnet)
	ones, bits := ipnet.Mask.Size()
	span := (1<<uint32(bits-ones) - 2)

	return start, span
}

//
// Instantiate a new DHCP handler for the given ring
//
func newHandler(name string, rings cfgapi.RingMap) *ringHandler {
	ring := rings[name]
	start, span := ipRange(ring)
	if start == nil {
		aputil.ReportError("%s has an illegal subnet: %s", name,
			ring.Subnet)
		return nil
	}

	duration := time.Duration(ring.LeaseDuration) * time.Minute
	myip := dhcp.IPAdd(start, 1)
	if name == base_def.RING_INTERNAL {
		// Shrink the range to exclude the router
		span = base_def.MAX_SATELLITES - 1
		start = dhcp.IPAdd(start, 1)
	} else {
		// Exclude the lower addresses that are reserved for the routers
		// on each of the mesh APs
		span -= base_def.MAX_SATELLITES
		start = dhcp.IPAdd(start, base_def.MAX_SATELLITES)
	}
	// Exclude the broadcast address
	span--

	h := ringHandler{
		ring:       name,
		serverIP:   myip,
		rangeStart: start,
		rangeEnd:   dhcp.IPAdd(start, span),
		rangeSpan:  span,
		duration:   duration,
		options: dhcp.Options{
			dhcp.OptionSubnetMask:                 ring.IPNet.Mask,
			dhcp.OptionRouter:                     myip,
			dhcp.OptionDomainNameServer:           myip,
			dhcp.OptionNetworkTimeProtocolServers: myip,
		},
		leases: make([]*lease, span, span),
	}
	for i := 0; i < span; i++ {
		h.leases[i] = &lease{ipaddr: dhcp.IPAdd(start, i)}
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
			var exp time.Time
			if client.Expires != nil {
				exp = *client.Expires
			}

			l.record(macaddr, exp)
		}
	}
	clientMtx.Unlock()
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
		clientRequestOn[clientMac] = iface
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

func dhcpMetricsInit() {
	dhcpMetrics.requests = bgm.NewCounter("dhcp4d/requests")
	dhcpMetrics.provisioned = bgm.NewCounter("dhcp4d/provisioned")
	dhcpMetrics.claimed = bgm.NewCounter("dhcp4d/claimed")
	dhcpMetrics.renewed = bgm.NewCounter("dhcp4d/renewed")
	dhcpMetrics.released = bgm.NewCounter("dhcp4d/released")
	dhcpMetrics.declined = bgm.NewCounter("dhcp4d/declined")
	dhcpMetrics.expired = bgm.NewCounter("dhcp4d/expired")
	dhcpMetrics.rejected = bgm.NewCounter("dhcp4d/rejected")
	dhcpMetrics.exhausted = bgm.NewCounter("dhcp4d/exhausted")
}

func dhcpInit() {
	var err error

	dhcpMetricsInit()

	domainName, err = config.GetDomain()
	if err != nil {
		slog.Fatalf("failed to fetch gateway domain: %v", err)
	}

	config.HandleChange(`^@/rings/.*/lease_duration$`, leaseDurationChanged)
	initHandlers()

	go dhcpLoop()
}
