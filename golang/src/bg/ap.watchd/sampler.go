/*
 * COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
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

	"bg/ap_common/apcfg"
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
	currentMtx   sync.RWMutex

	// Historically granted DHCP leases (maps Mac -> IPv4)
	oldAddrs = make(map[uint64]uint32)
	oldMtx   sync.Mutex

	// Mac -> IPv4 pairs we've observed on the network.
	currentObservations  = make(map[uint64]map[uint32]bool)
	historicObservations = make(map[uint64]map[uint32]time.Time)
	observedMtx          sync.Mutex

	allStats    = make(map[uint64]*perDeviceStats)
	allStatsMtx sync.Mutex

	gateways map[uint32]bool
	subnets  []*net.IPNet

	auditTicker    *time.Ticker
	samplerRunning bool
)

type samplerState struct {
	ring   string
	iface  string
	hwaddr net.HardwareAddr

	parser *gopacket.DecodingLayerParser

	decodedEth  layers.Ethernet
	decodedIPv4 layers.IPv4
	decodedARP  layers.ARP
	decodedUDP  layers.UDP
	decodedTCP  layers.TCP
}

type xferStats struct {
	pktsSent  uint64
	pktsRcvd  uint64
	bytesSent uint64
	bytesRcvd uint64
}

type perDeviceStats struct {
	aggregate xferStats

	// traffic broken out by the source/target endpoint
	localStats  map[uint32]*xferStats // LAN traffic
	remoteStats map[uint32]*xferStats // WAN traffic

	sync.Mutex
}

func printLine(label string, x *xferStats) {
	fmt.Printf("%30s%9d%12d%8s%9d%12d\n",
		label, x.pktsSent, x.bytesSent, " ", x.pktsRcvd, x.bytesRcvd)
}

func printStats() {
	allStatsMtx.Lock()
	fmt.Printf("%-30s%9s%12s%8s%9s%12s\n", "Device",
		"Pkts Sent", "Bytes Sent", " ", "Pkts Rcvd", "Bytes Rcvd")
	for d, s := range allStats {
		fmt.Printf("%v\n", network.Uint64ToHWAddr(d))
		fmt.Printf("%12s:\n", "local")
		for l, x := range s.localStats {
			printLine(network.Uint32ToIPAddr(l).String(), x)
		}
		fmt.Printf("%12s:\n", "remote")
		for l, x := range s.remoteStats {
			printLine(network.Uint32ToIPAddr(l).String(), x)
		}
		fmt.Printf("%12s:\n", "aggregate")
		printLine(" ", &s.aggregate)
	}

	allStatsMtx.Unlock()
}

// Find the perDeviceStats structure for the identified device.  If it doesn't
// already exist, create a structure and insert it into the allStats map
func getDeviceStats(devid uint64) *perDeviceStats {
	allStatsMtx.Lock()
	s := allStats[devid]
	if s == nil {
		s = &perDeviceStats{
			localStats:  make(map[uint32]*xferStats),
			remoteStats: make(map[uint32]*xferStats),
		}
		allStats[devid] = s
	}
	allStatsMtx.Unlock()

	return s
}

// Extract the stats structure for the given (device, ip) tuple.  Create
// the structure if it doesn't already exist
func getEndpointStats(stats *perDeviceStats, ip uint32, local bool) *xferStats {
	var statsMap map[uint32]*xferStats

	if local {
		statsMap = stats.localStats
	} else {
		statsMap = stats.remoteStats
	}

	x := statsMap[ip]
	if x == nil {
		x = &xferStats{}
		statsMap[ip] = x
	}
	return x
}

// Does the given IP address fall into one of the address ranges covered by our
// local subnets?
func localIPAddr(ipaddr net.IP) bool {
	for _, s := range subnets {
		if s.Contains(ipaddr) {
			return true
		}
	}
	return false
}

func collectStats(state *samplerState, eth *layers.Ethernet, ipv4 *layers.IPv4,
	len uint64) {

	srcLocal := localIPAddr(ipv4.SrcIP)
	dstLocal := localIPAddr(ipv4.DstIP)

	if srcLocal {
		srcDev := network.HWAddrToUint64(eth.SrcMAC)
		stats := getDeviceStats(srcDev)
		stats.Lock()

		xfer := &stats.aggregate
		xfer.pktsSent++
		xfer.bytesSent += len

		dstIP := network.IPAddrToUint32(ipv4.DstIP)
		xfer = getEndpointStats(stats, dstIP, dstLocal)
		xfer.pktsSent++
		xfer.bytesSent += len

		stats.Unlock()
	}

	if dstLocal {
		dstDev := network.HWAddrToUint64(eth.DstMAC)
		stats := getDeviceStats(dstDev)
		stats.Lock()

		xfer := &stats.aggregate
		xfer.pktsRcvd++
		xfer.bytesRcvd += len

		srcIP := network.IPAddrToUint32(ipv4.SrcIP)
		xfer = getEndpointStats(stats, srcIP, srcLocal)
		xfer.pktsRcvd++
		xfer.bytesRcvd += len

		stats.Unlock()
	}
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

func observedIPAddr(state *samplerState, hwaddr net.HardwareAddr, ipaddr net.IP) {

	// Convert the addresses into integers for faster processing
	mac := network.HWAddrToUint64(hwaddr)
	ip := network.IPAddrToUint32(ipaddr)

	// Ignore records for internal routers, or zero, multicast, or broadcast
	// addresses.
	if mac == network.MacZeroInt || mac == network.MacBcastInt ||
		bytes.Equal(hwaddr, state.hwaddr) ||
		network.IsMacMulticast(hwaddr) ||
		ip == 0 || gateways[ip] ||
		ipaddr.IsMulticast() || ipaddr.Equal(net.IPv4bcast) {
		return
	}

	// If this mac->ip is valid, we're done
	currentMtx.RLock()
	expected, ok := currentAddrs[mac]
	currentMtx.RUnlock()
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

func processOnePacket(state *samplerState, data []byte) {
	var (
		eth            *layers.Ethernet
		ipv4           *layers.IPv4
		arp            *layers.ARP
		tcp            *layers.TCP
		udp            *layers.UDP
		srcMac, dstMac net.HardwareAddr
		srcIP, dstIP   net.IP
	)

	decodedLayers := make([]gopacket.LayerType, 0, 10)
	state.parser.DecodeLayers(data, &decodedLayers)
	for _, typ := range decodedLayers {
		switch typ {
		case layers.LayerTypeEthernet:
			eth = &state.decodedEth
		case layers.LayerTypeIPv4:
			ipv4 = &state.decodedIPv4
		case layers.LayerTypeARP:
			arp = &state.decodedARP
		case layers.LayerTypeUDP:
			udp = &state.decodedUDP
		case layers.LayerTypeTCP:
			tcp = &state.decodedTCP
		}
	}
	if eth == nil {
		return
	}

	if arp != nil {
		srcIP, dstIP = arp.SourceProtAddress, arp.DstProtAddress
		srcMac, dstMac = arp.SourceHwAddress, arp.DstHwAddress
	} else if ipv4 != nil {
		srcIP, dstIP = ipv4.SrcIP, ipv4.DstIP
		srcMac, dstMac = eth.SrcMAC, eth.DstMAC
		if udp != nil {
			handlePort(srcIP, dstIP, int(udp.SrcPort),
				int(udp.DstPort), "udp")
		} else if tcp != nil {
			handlePort(srcIP, dstIP, int(tcp.SrcPort),
				int(tcp.DstPort), "tcp")
		}
		collectStats(state, eth, ipv4, uint64(len(data)))
		if !localIPAddr(srcIP) {
			checkBlock(dstMac, srcIP)
		}
		if !localIPAddr(dstIP) {
			checkBlock(srcMac, dstIP)
		}
	}
	observedIPAddr(state, srcMac, srcIP)
	observedIPAddr(state, dstMac, dstIP)
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

//
// Set up the GoPacket parser for this interface's packet stream
//
func parserInit(state *samplerState) {
	state.parser = gopacket.NewDecodingLayerParser(
		layers.LayerTypeEthernet,
		&state.decodedEth,
		&state.decodedIPv4,
		&state.decodedARP,
		&state.decodedUDP,
		&state.decodedTCP)
}

//
// Per-interface loop that endlessly consumes and processes packets
//
func sampleOne(state *samplerState) {
	parserInit(state)

	warned := false
	for samplerRunning {
		handle, err := openOne(state.ring, state.iface)
		if err != nil {
			if !warned {
				log.Printf("openOne(%s) failed: %v\n",
					state.iface, err)
				warned = true
			}
			continue
		}
		warned = false

		for samplerRunning {
			data, _, err := handle.ZeroCopyReadPacketData()
			if err != nil {
				log.Println("Error reading packet data:", err)
				break
			}

			processOnePacket(state, data)
		}
		handle.Close()
	}
}

func getRingSubnet(config *apcfg.RingConfig) (net.HardwareAddr, *net.IPNet, error) {
	var hwaddr net.HardwareAddr
	var subnet *net.IPNet
	var err error

	if _, subnet, _ = net.ParseCIDR(config.Subnet); subnet == nil {
		err = fmt.Errorf("invalid subnet '%s'", config.Subnet)
	} else {
		iface, x := net.InterfaceByName(config.Bridge)
		if x != nil {
			err = fmt.Errorf("InterfaceByName(%s) failed on: %v",
				config.Bridge, err)
		} else {
			hwaddr = iface.HardwareAddr
		}
	}

	return hwaddr, subnet, err
}

func sampleFini(w *watcher) {
	log.Printf("Shutting down sampler\n")
	samplerRunning = false
	auditTicker.Stop()
	printStats()

	w.running = false
}

func sampleInit(w *watcher) {
	blocklistInit()
	getGateways()
	getLeases()

	samplerRunning = true
	for ring, config := range rings {
		if config.Bridge == "" {
			continue
		}

		hwaddr, subnet, err := getRingSubnet(config)
		if err != nil {
			log.Printf("Failed to sample on ring '%s': %v\n",
				ring, err)
		} else {
			subnets = append(subnets, subnet)
			s := samplerState{
				ring:   ring,
				iface:  config.Bridge,
				hwaddr: hwaddr,
			}
			go sampleOne(&s)
		}
	}

	go auditor()
	w.running = true
}

func init() {
	addWatcher("sampler", sampleInit, sampleFini)
}
