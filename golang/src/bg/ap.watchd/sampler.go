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
	// XXX These Duration flags should be combined into a single "percentage"
	// flag which indicates how many packets, or how much time, we spend capturing.
	// Ideally, the auditor and sampler go routines could communicate their
	// impact on resources (CPU, memory, time, etc...) and then self-tune to keep
	// the overall impact on the appliance's resources under an accepatable level.
	auditTime = flag.Duration("audit-time", time.Duration(time.Second*120),
		"How often to audit the sample records")
	capTime = flag.Duration("cap-time", time.Duration(time.Second*30),
		"How long to capture packets in each capture interval")
	loopTime = flag.Duration("loop-time", time.Duration(time.Second*60),
		"Loop interval duration (should be greater than cap-time)")

	auditMtx     sync.Mutex
	auditRecords = make(map[gopacket.Endpoint]*record)
	capStats     = make(map[gopacket.LayerType]*layerStats)

	sampleTicker *time.Ticker
	auditTicker  *time.Ticker

	scanningAddr  net.HardwareAddr
	scanningIface string
	scanningRing  string
	gateways      map[uint32]bool
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

	stats.src[layers.NewMACEndpoint(eth.SrcMAC)]++
	stats.dst[layers.NewMACEndpoint(eth.DstMAC)]++
}

func handleIpv4(ipv4 *layers.IPv4) {
	stats := capStats[ipv4.LayerType()]

	stats.src[layers.NewIPEndpoint(ipv4.SrcIP)]++
	stats.dst[layers.NewIPEndpoint(ipv4.DstIP)]++
}

func handleArp(arp *layers.ARP) {
	stats := capStats[arp.LayerType()]

	stats.src[layers.NewMACEndpoint(arp.SourceHwAddress)]++
	stats.dst[layers.NewMACEndpoint(arp.DstHwAddress)]++
}

func handlePort(src, dst net.IP, sport, dport int, proto string) {
	// XXX: this is just recording the observed source/destination ports.
	// In many cases, what we really want to know is the service being used.
	// So, we may be recording that the traffic originates from port 55617,
	// but it's really more interesting that it is going to port 443.  For
	// intra-lan traffic, we'll record both ends of the conversation.  For
	// traffic that is lan<->wan, we might be dropping the interesting half.
	// This needs some more thought.

	if rec := GetDeviceRecordByIP(src.String()); rec != nil {
		if p := GetProtoRecord(rec, proto); p != nil {
			p.OutPorts[sport] = true
		}
		ReleaseDeviceRecord(rec)
	}
	if rec := GetDeviceRecordByIP(dst.String()); rec != nil {
		if p := GetProtoRecord(rec, proto); p != nil {
			p.InPorts[dport] = true
		}
		ReleaseDeviceRecord(rec)
	}
}

// Look up the record for this hwaddr:
//   0) Ignore well known MAC and IP addresses
//   1) If no record exists, create one. If we are authoritative the record is
//      vetted. Else the record is foreign.
//   2) If a 'foreign' or 'vetted' record exists but the record's ipaddr differs
//      from the observed ipaddr, then we save the new ipaddr. If we are
//      authoritative the new address represents a new DHCP lease and the record
//      is vetted. If we are not authoritative and the record was previously
//      vetted we are in conflict.
//   3) If the two IP addresses match and we are authoritative the record is
//      vetted.
func samplerUpdate(hwaddr net.HardwareAddr, ipaddr net.IP, auth bool) {

	if bytes.Equal(hwaddr, scanningAddr) ||
		bytes.Equal(hwaddr, network.MacZero) ||
		network.IsMacMulticast(hwaddr) ||
		bytes.Equal(hwaddr, network.MacBcast) ||
		ipaddr.Equal(net.IPv4zero) || ipaddr.IsMulticast() ||
		ipaddr.Equal(net.IPv4bcast) {
		return
	}

	auditMtx.Lock()
	defer auditMtx.Unlock()
	r, ok := auditRecords[layers.NewMACEndpoint(hwaddr)]

	if !ok {
		rec := &record{
			ipaddr: ipaddr,
			ring:   scanningRing,
		}
		if auth {
			rec.audit = vetted
		} else {
			rec.audit = foreign
		}
		auditRecords[layers.NewMACEndpoint(hwaddr)] = rec
		return
	}

	if r.audit == conflict {
		return
	}

	if !r.ipaddr.Equal(ipaddr) {
		r.ipaddr = ipaddr
		if auth {
			r.audit = vetted
		} else if r.audit == vetted {
			r.audit = conflict
		}
	} else if auth {
		r.audit = vetted
	}
}

func samplerDelete(hwaddr net.HardwareAddr) {
	auditMtx.Lock()
	delete(auditRecords, layers.NewMACEndpoint(hwaddr))
	auditMtx.Unlock()
}

func decodeOnePacket(data []byte) {
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
			samplerUpdate(eth.SrcMAC, srcIP, false)
			samplerUpdate(eth.DstMAC, dstIP, false)
			handleIpv4(ipv4)

		case layers.LayerTypeARP:
			arp := decodeLayers[idxArp].(*layers.ARP)
			samplerUpdate(arp.SourceHwAddress, arp.SourceProtAddress, false)
			samplerUpdate(arp.DstHwAddress, arp.DstProtAddress, false)
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

// Decode only the layers we care about:
//   - Look for ARP request and reply to associate MAC and IP
//   - Look for IPv4 to associate MAC and IP
func decodePackets(iface string) {
	handle, err := pcap.OpenLive(iface, 65536, true, pcap.BlockForever)
	if err != nil {
		log.Fatalln("OpenLive failed:", err)
	}
	defer handle.Close()

	start := time.Now()
	for time.Since(start) < *capTime {
		data, _, err := handle.ReadPacketData()
		if err != nil {
			log.Println("Error reading packet data:", err)
			continue
		}
		decodeOnePacket(data)
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
			samplerUpdate(hwaddr, client.IPv4, true)
		}
	}
}

func auditor() {
	auditTicker = time.NewTicker(*auditTime)
	for {
		<-auditTicker.C
		for ep, r := range auditRecords {
			if r.audit == conflict {
				log.Printf("CONFLICT FOUND: %s using %s\n", ep, r.ipaddr)
			} else if r.audit == foreign {
				logUnknown(r.ring, ep.String(), r.ipaddr.String())
			}
		}
	}
}

func sampleOne(ring, iface string) error {
	self, err := net.InterfaceByName(iface)
	if err != nil {
		return fmt.Errorf("failed to get mac addr: %s", err)
	}
	scanningAddr = self.HardwareAddr
	scanningIface = iface
	scanningRing = ring

	if err := network.WaitForDevice(iface, 0); err != nil {
		return fmt.Errorf("device is offline")
	}

	if *verbose {
		log.Printf("Sample start: %s\n", iface)
	}
	decodePackets(iface)
	if *verbose {
		log.Printf("Sample done: %s\n", iface)
	}
	return nil
}

func sampleLoop() {
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

	sampleTicker = time.NewTicker(*loopTime)
	for {
		for ring, config := range rings {
			if config.Bridge == "" {
				continue
			}
			err := sampleOne(ring, config.Bridge)
			if err != nil {
				log.Printf("Sample of %s on %s failed: %v\n",
					ring, config.Bridge, err)
				continue
			}
			<-sampleTicker.C
		}
	}
}

func sampleFini() {
	log.Printf("Shutting down sampler\n")
	sampleTicker.Stop()
	auditTicker.Stop()
	printStats()
}

func sampleInit() error {
	if *loopTime < *capTime {
		return fmt.Errorf("loop-time should be greater than cap-time")
	}

	getGateways()
	getLeases()

	go sampleLoop()
	go auditor()

	return nil
}

func init() {
	addWatcher("sampler", sampleInit, sampleFini)
}
