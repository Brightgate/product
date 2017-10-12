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

	"ap_common/network"

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
	macSelf      net.HardwareAddr
)

const (
	idxEth int = iota
	idxIpv4
	idxArp
	idxMAX
)

type auditType int

const (
	foreign auditType = iota
	vetted
	conflict
)

// Our initial network audit strategy is to examine the packet stream for
// Ethernet packets with EthernetTypeIPv4 and EthernetTypeARP. For TypeIPv4 we
// will create a (hwaddr, ipaddr) pair using the MAC address from the Ethernet
// header and the IP address from the IP header. For TypeARP the pair
// (hwaddr, ipaddr) will be extracted from the ARP header. These pairs will be
// inserted into auditRecords and vetted by the lease information coming from
// ap.dhcp4d.
type record struct {
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

	if bytes.Equal(hwaddr, macSelf) || bytes.Equal(hwaddr, network.MacZero) ||
		network.IsMacMulticast(hwaddr) || bytes.Equal(hwaddr, network.MacBcast) ||
		ipaddr.Equal(net.IPv4zero) || ipaddr.IsMulticast() ||
		ipaddr.Equal(net.IPv4bcast) {
		return
	}

	auditMtx.Lock()
	defer auditMtx.Unlock()
	r, ok := auditRecords[layers.NewMACEndpoint(hwaddr)]

	if !ok {
		rec := &record{}
		rec.ipaddr = ipaddr
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

// Decode only the layers we care about:
//   - Look for ARP request and reply to associate MAC and IP
//   - Look for IPv4 to associate MAC and IP
func decodePackets(iface string, decode []gopacket.DecodingLayer) {
	handle, err := pcap.OpenLive(iface, 65536, true, pcap.BlockForever)
	if err != nil {
		log.Fatalln("OpenLive failed:", err)
	}
	defer handle.Close()

	parser := gopacket.NewDecodingLayerParser(layers.LayerTypeEthernet, decode...)
	decoded := []gopacket.LayerType{}

	start := time.Now()
	for time.Since(start) < *capTime {
		var srcMac, dstMac net.HardwareAddr

		data, _, err := handle.ReadPacketData()
		if err != nil {
			log.Println("Error reading packet data:", err)
			continue
		}
		err = parser.DecodeLayers(data, &decoded)

		for _, typ := range decoded {
			switch typ {
			case layers.LayerTypeEthernet:
				// Save the MAC address for reference in IPv4 layer
				eth := decode[idxEth].(*layers.Ethernet)
				srcMac = eth.SrcMAC
				dstMac = eth.DstMAC
				handleEther(eth)

			case layers.LayerTypeIPv4:
				ipv4 := decode[idxIpv4].(*layers.IPv4)
				samplerUpdate(srcMac, ipv4.SrcIP, false)
				samplerUpdate(dstMac, ipv4.DstIP, false)
				handleIpv4(ipv4)

			case layers.LayerTypeARP:
				arp := decode[idxArp].(*layers.ARP)
				samplerUpdate(arp.SourceHwAddress, arp.SourceProtAddress, false)
				samplerUpdate(arp.DstHwAddress, arp.DstProtAddress, false)
				handleArp(arp)
			}
		}
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

func auditor(iface string) {
	auditTicker = time.NewTicker(*auditTime)
	for {
		<-auditTicker.C
		for ep, r := range auditRecords {
			if r.audit == conflict {
				log.Printf("CONFLICT FOUND: %s using %s\n", ep, r.ipaddr)
			} else if r.audit == foreign {
				logUnknown(iface, ep.String(), r.ipaddr.String())
			}
		}
	}
}

func sample(iface string) {
	// These are the layers we wish to decode
	decode := make([]gopacket.DecodingLayer, idxMAX)
	decode[idxEth] = &layers.Ethernet{}
	decode[idxIpv4] = &layers.IPv4{}
	decode[idxArp] = &layers.ARP{}

	for _, layer := range decode {
		capStats[layer.(gopacket.Layer).LayerType()] = &layerStats{
			src: make(map[gopacket.Endpoint]uint64),
			dst: make(map[gopacket.Endpoint]uint64),
		}
	}

	sampleTicker = time.NewTicker(*loopTime)
	for {
		<-sampleTicker.C
		err := network.WaitForDevice(iface, 0)
		if err != nil {
			log.Printf("Not sampling: %s is offline", iface)
		} else {
			if *verbose {
				log.Printf("Sample starting\n")
			}
			decodePackets(iface, decode)
			if *verbose {
				log.Printf("Sample finished\n")
			}
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

	iface, err := config.GetProp("@/network/wifi_nic")
	if err != nil {
		iface, err = config.GetProp("@/network/wired_nic")
		if err != nil {
			return fmt.Errorf("no interfaces defined")
		}
		log.Printf("Sampling wired traffic on %s\n", iface)
	} else {
		log.Printf("Sampling wifi traffic on %s\n", iface)
	}

	self, err := net.InterfaceByName(iface)
	if err != nil {
		return fmt.Errorf("failed to get mac address for %s: %s",
			iface, err)
	}
	macSelf = self.HardwareAddr

	getLeases()
	go auditor(iface)
	go sample(iface)

	return nil
}
