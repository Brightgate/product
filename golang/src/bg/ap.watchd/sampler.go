/*
 * COPYRIGHT 2017 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

// Appliance packet sampler. Logs statistics about captured packets, and keeps
// audit records of (MAC, IP) address pairs.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"bg/ap_common/network"
	"bg/base_def"

	// Requires libpcap
	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcap"
)

const (
	// EthernetTypeLARQ is an EthernetType which we have observed being used by
	// the Raspberry Pi 3 on its wlan interface. These packets are routed to the
	// AP from a SRC MAC addresses which is identical to that of the AP wlan MAC
	// address except in one bit (the U/L bit in the first octet). In addition,
	// this EthernetType is not defined in gopacket and causes a decoding error.
	// These packets may relate to a Broadcomm specific wlan driver, LARQ, or
	// HomePNA.
	EthernetTypeLARQ layers.EthernetType = 0x886c
)

var (
	auditTime = flag.Duration("audit-time", time.Duration(time.Second*120),
		"How often to audit the sample records")

	warnTime = flag.Duration("warn-time", time.Duration(time.Hour),
		"How often to report a device using a bad IP address")

	// Currently granted DHCP leases (maps Mac -> IPv4)
	currentAddrs = make(map[uint64]uint32)
	currentMtx   sync.Mutex

	// Historically granted DHCP leases (maps Mac -> IPv4)
	oldAddrs = make(map[uint64]uint32)
	oldMtx   sync.Mutex

	// Mac -> IPv4 pairs we've observed on the network.
	currentObservations  = make(map[uint64]map[uint32]bool)
	historicObservations = make(map[uint64]map[uint32]time.Time)
	observedMtx          sync.Mutex

	capStats       = make(map[gopacket.LayerType]*layerStats)
	auditTicker    *time.Ticker
	gateways       map[uint32]bool
	samplerRunning bool
)

const (
	idxEth int = iota
	idxIpv4
	idxArp
	idxUDP
	idxTCP
	idxMAX
)

var (
	decodeLayers []gopacket.DecodingLayer
	parser       *gopacket.DecodingLayerParser
)

type auditType int

const (
	foreign auditType = iota
	vetted
	conflict
)

// Our initial network audit strategy is to examine the packet stream for
// Ethernet packets with EthernetTypeIPv4 and EthernetTypeARP.  We will use
// these to construct records for subsequent auditing.  The records include the
// packet's IP address and security ring, and will be stored in a map indexed by
// the mac address extracted from the IP/ARP headers.  These MAC->IP mappings
// will subsequently be compared against the lease information coming from
// ap.dhcp4d.
type record struct {
	ring   string
	ipaddr net.IP
	audit  auditType
}

// XXX What are the interesting bits from the capture? Example stats include:
//   - How many times has an Endpoint been a src? A dst?
//   - Using gopacket.Flow we could keep a count of packets from A->B and B->A
type layerStats struct {
	src map[gopacket.Endpoint]uint64
	dst map[gopacket.Endpoint]uint64
	sync.Mutex
}

func printStats() {
	for typ, stats := range capStats {
		log.Printf("Layer Type: %s\n", typ)
		for ep, count := range stats.src {
			log.Printf("\tSrc: %s (%d)\n", ep, count)
		}

		for ep, count := range stats.dst {
			log.Printf("\tDst: %s (%d)\n", ep, count)
		}
	}
}

func handleEther(eth *layers.Ethernet) {
	if eth.EthernetType == EthernetTypeLARQ {
		return
	}

	stats := capStats[eth.LayerType()]

	stats.Lock()
	stats.src[layers.NewMACEndpoint(eth.SrcMAC)]++
	stats.dst[layers.NewMACEndpoint(eth.DstMAC)]++
	stats.Unlock()
}

func handleIpv4(ipv4 *layers.IPv4) {
	stats := capStats[ipv4.LayerType()]

	stats.Lock()
	stats.src[layers.NewIPEndpoint(ipv4.SrcIP)]++
	stats.dst[layers.NewIPEndpoint(ipv4.DstIP)]++
	stats.Unlock()
}

func handleArp(arp *layers.ARP) {
	stats := capStats[arp.LayerType()]

	stats.Lock()
	stats.src[layers.NewMACEndpoint(arp.SourceHwAddress)]++
	stats.dst[layers.NewMACEndpoint(arp.DstHwAddress)]++
	stats.Unlock()
}

func handlePort(src, dst net.IP, sport, dport int, proto string) {
	// XXX: this is just recording the observed source/destination ports.
	// In many cases, what we really want to know is the service being used.
	// So, we may be recording that the traffic originates from port 55617,
	// but it's really more interesting that it is going to port 443.  For
	// intra-lan traffic, we'll record both ends of the conversation.  For
	// traffic that is lan<->wan, we might be dropping the interesting half.
	// This needs some more thought.

	if rec := getDeviceRecordByIP(src.String()); rec != nil {
		if p := getProtoRecord(rec, proto); p != nil {
			p.OutPorts[sport] = true
		}
		releaseDeviceRecord(rec)
	}
	if rec := getDeviceRecordByIP(dst.String()); rec != nil {
		if p := getProtoRecord(rec, proto); p != nil {
			p.InPorts[dport] = true
		}
		releaseDeviceRecord(rec)
	}
}

func observedIPAddr(self, hwaddr net.HardwareAddr, ipaddr net.IP) {

	// Ignore records for internal routers, or zero, multicast, or broadcast
	// addresses.
	if bytes.Equal(hwaddr, self) ||
		bytes.Equal(hwaddr, network.MacZero) ||
		network.IsMacMulticast(hwaddr) ||
		bytes.Equal(hwaddr, network.MacBcast) ||
		ipaddr.Equal(net.IPv4zero) || ipaddr.IsMulticast() ||
		ipaddr.Equal(net.IPv4bcast) {
		return
	}

	// Convert the addresses into integers for faster processing
	mac := network.HWAddrToUint64(hwaddr)
	ip := network.IPAddrToUint32(ipaddr)

	// If this mac->ip is valid, we're done
	currentMtx.Lock()
	expected, ok := currentAddrs[mac]
	currentMtx.Unlock()
	if ok && expected == ip {
		return
	}

	// Record this observation for the auditor to evaluate
	observedMtx.Lock()
	list, ok := currentObservations[mac]
	if !ok {
		list = make(map[uint32]bool)
		currentObservations[mac] = list
	}
	list[ip] = true
	observedMtx.Unlock()
}

func registerIPAddr(hwaddr net.HardwareAddr, ipaddr net.IP) {
	mac := network.HWAddrToUint64(hwaddr)
	ip := network.IPAddrToUint32(ipaddr)

	currentMtx.Lock()
	current, ok := currentAddrs[mac]
	if !ok || current != ip {
		if ip == 0 {
			delete(currentAddrs, mac)
		} else {
			currentAddrs[mac] = ip
		}
	}
	currentMtx.Unlock()

	if ok && ip != current {
		oldMtx.Lock()
		oldAddrs[mac] = current
		oldMtx.Unlock()
	}
}

func unregisterIPAddr(hwaddr net.HardwareAddr) {
	registerIPAddr(hwaddr, network.Uint32ToIPAddr(0))
}

func decodeOnePacket(self net.HardwareAddr, data []byte) {
	var eth *layers.Ethernet
	var srcIP, dstIP net.IP

	decoded := []gopacket.LayerType{}
	if err := parser.DecodeLayers(data, &decoded); err != nil {
		return
	}

	for _, typ := range decoded {
		switch typ {
		case layers.LayerTypeEthernet:
			eth = decodeLayers[idxEth].(*layers.Ethernet)

		case layers.LayerTypeIPv4:
			ipv4 := decodeLayers[idxIpv4].(*layers.IPv4)
			srcIP = ipv4.SrcIP
			dstIP = ipv4.DstIP

			// We ignore traffic to/from our gateway addresses
			// because the scanner's nmap children are responsible
			// for a ton of traffic we're not interested in.
			if gateways[network.IPAddrToUint32(srcIP)] ||
				gateways[network.IPAddrToUint32(dstIP)] {
				return
			}

			handleEther(eth)
			observedIPAddr(self, eth.SrcMAC, srcIP)
			observedIPAddr(self, eth.DstMAC, dstIP)
			handleIpv4(ipv4)

		case layers.LayerTypeARP:
			arp := decodeLayers[idxArp].(*layers.ARP)
			observedIPAddr(self, arp.SourceHwAddress,
				arp.SourceProtAddress)
			observedIPAddr(self, arp.DstHwAddress,
				arp.DstProtAddress)
			handleArp(arp)

		case layers.LayerTypeUDP:
			udp := decodeLayers[idxUDP].(*layers.UDP)
			sport := int(udp.SrcPort)
			dport := int(udp.DstPort)
			handlePort(srcIP, dstIP, sport, dport, "udp")

		case layers.LayerTypeTCP:
			tcp := decodeLayers[idxTCP].(*layers.TCP)
			sport := int(tcp.SrcPort)
			dport := int(tcp.DstPort)
			handlePort(srcIP, dstIP, sport, dport, "tcp")
		}
	}
}

func getGateways() {
	gateways = make(map[uint32]bool)

	for _, r := range rings {
		router := net.ParseIP(network.SubnetRouter(r.Subnet))
		gateways[network.IPAddrToUint32(router)] = true
	}
}

func getLeases() {
	clients := config.GetClients()
	if clients == nil {
		return
	}

	for macaddr, client := range clients {
		hwaddr, err := net.ParseMAC(macaddr)
		if err != nil {
			log.Printf("Invalid mac address: %s\n", macaddr)
		} else if client.IPv4 != nil {
			registerIPAddr(hwaddr, client.IPv4)
		}
	}
}

func auditRecords(recs map[uint64]map[uint32]bool) {

	// Make a pass over the observed data, filtering out any that
	// are legal in retrospect.  This can happen if we saw traffic
	// to a legal client before we got the DHCP update from configd.
	currentMtx.Lock()
	for mac, addrs := range recs {
		for ip := range addrs {
			if currentAddrs[mac] == ip {
				delete(addrs, ip)
			}
		}
		if len(addrs) == 0 {
			delete(recs, mac)
		}
	}
	currentMtx.Unlock()

	// Repeat a warning if it was at least this long ago
	now := time.Now()
	warnSince := now.Add(-1 * *warnTime)

	// Iterate over all of the remaining illegal addresses we've
	// seen.  If this is the first time we've seen one, report it.  If it's
	// been a while since we mentioned seeing it, repeat the message.
	for mac, addrs := range recs {
		historic, ok := historicObservations[mac]
		if !ok {
			historic = make(map[uint32]time.Time)
			historicObservations[mac] = historic
		}

		for ip := range addrs {
			lastTime, ok := historic[ip]
			if !ok || lastTime.Before(warnSince) {
				// Check to see if this is a stale address, or
				// just bad
				oldstr := ""
				oldMtx.Lock()
				if oldAddrs[mac] == ip {
					oldstr = "stale "
				}
				oldMtx.Unlock()

				hwaddr := network.Uint64ToHWAddr(mac)
				ipaddr := network.Uint32ToIPAddr(ip)
				log.Printf("Found device %v using %saddr %v\n",
					hwaddr, oldstr, ipaddr)
				historic[ip] = now
			}
		}
	}
}

func auditor() {
	auditTicker = time.NewTicker(*auditTime)
	for samplerRunning {
		<-auditTicker.C

		// Make a copy of currentObservations and reset the master, so
		// we can evaluate the list without holding the lock.
		observedMtx.Lock()
		copy := currentObservations
		currentObservations = make(map[uint64]map[uint32]bool)
		observedMtx.Unlock()

		auditRecords(copy)
	}
}

func openOne(ring, iface string) (*pcap.Handle, error) {
	if ring != base_def.RING_INTERNAL {
		// The internal ring is special.  See
		// networkd.go:prepareRingBridge()
		err := network.WaitForDevice(iface, time.Minute)
		if err != nil {
			return nil, fmt.Errorf("%s: %v", iface, err)
		}
	}

	handle, err := pcap.OpenLive(iface, 65536, true, pcap.BlockForever)
	if err != nil {
		err = fmt.Errorf("pcap.OpenLive(%s) failed: %v", iface, err)
	}
	return handle, err
}

func sampleOne(ring, iface string) {
	self, err := net.InterfaceByName(iface)
	if err != nil {
		log.Printf("failed to get mac addr for %s device (%s): %v",
			ring, iface, err)
		return
	}
	log.Printf("Sampling on %s (%s)\n", ring, iface)

	warned := false
	for samplerRunning {
		handle, err := openOne(ring, iface)
		if err != nil {
			if !warned {
				log.Printf("openOne(%s) failed: %v\n",
					iface, err)
				warned = true
			}
			continue
		}
		warned = false

		for {
			data, _, err := handle.ReadPacketData()
			if err != nil {
				log.Println("Error reading packet data:", err)
				break
			}
			decodeOnePacket(self.HardwareAddr, data)
		}
		handle.Close()
	}
}

func sampleFini() {
	log.Printf("Shutting down sampler\n")
	samplerRunning = false
	auditTicker.Stop()
	printStats()
}

func sampleInit() error {
	// These are the layers we wish to decode
	decodeLayers = make([]gopacket.DecodingLayer, idxMAX)
	decodeLayers[idxEth] = &layers.Ethernet{}
	decodeLayers[idxIpv4] = &layers.IPv4{}
	decodeLayers[idxArp] = &layers.ARP{}
	decodeLayers[idxUDP] = &layers.UDP{}
	decodeLayers[idxTCP] = &layers.TCP{}

	parser = gopacket.NewDecodingLayerParser(layers.LayerTypeEthernet,
		decodeLayers...)

	for _, layer := range decodeLayers {
		capStats[layer.(gopacket.Layer).LayerType()] = &layerStats{
			src: make(map[gopacket.Endpoint]uint64),
			dst: make(map[gopacket.Endpoint]uint64),
		}
	}

	getGateways()
	getLeases()

	samplerRunning = true
	for ring, config := range rings {
		if config.Bridge == "" {
			continue
		}
		go sampleOne(ring, config.Bridge)
	}

	go auditor()

	return nil
}

func init() {
	addWatcher("sampler", sampleInit, sampleFini)
}
