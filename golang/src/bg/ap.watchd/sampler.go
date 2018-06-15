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
	"io"
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

var (
	auditTime = flag.Duration("atime", time.Duration(time.Minute*2),
		"How often to audit the sample records")

	warnTime = flag.Duration("wtime", time.Duration(time.Hour),
		"How often to warn about a device using a bad IP address")

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

	subnets     []*net.IPNet
	subnetBcast []net.IP

	auditTicker *time.Ticker
	auditDone   chan bool

	samplers         []*samplerState
	samplerRunning   bool
	samplerWaitGroup sync.WaitGroup
)

type samplerState struct {
	// Persistent state describing the identity of the sampler
	ring   string
	iface  string
	hwaddr net.HardwareAddr
	handle *pcap.Handle

	// State of the current packet being analyzed
	parser      *gopacket.DecodingLayerParser
	decodedEth  layers.Ethernet
	decodedIPv4 layers.IPv4
	decodedARP  layers.ARP
	decodedUDP  layers.UDP
	decodedTCP  layers.TCP

	sync.Mutex
}

// Does the given IP address fall into one of the address ranges covered by our
// local subnets?
func localIPAddr(ip net.IP) bool {
	for _, s := range subnets {
		if s.Contains(ip) {
			return true
		}
	}
	return false
}

// Is this address used for UDP broadcast/multicast?
func broadcastUDPAddr(ip net.IP) bool {
	if ip.IsMulticast() || ip.Equal(net.IPv4bcast) {
		return true
	}

	for _, b := range subnetBcast {
		if ip.Equal(b) {
			return true
		}
	}
	return false
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
		ip == 0 || gateways[ip] || internalMacs[mac] ||
		ipaddr.IsLinkLocalMulticast() || ipaddr.IsLinkLocalUnicast() ||
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
		if gateways[network.IPAddrToUint32(srcIP)] {
			return
		}
	} else if ipv4 != nil {
		// We ignore traffic to/from the gateway to avoid recording all
		// of our nmap scan noise.
		// XXX: rather than ignoring all of the gateway traffic, we
		// could add a flag that the scanner code could toggle, so we
		// only ignore it while the scanner is running.
		if gateways[network.IPAddrToUint32(ipv4.SrcIP)] ||
			gateways[network.IPAddrToUint32(ipv4.DstIP)] {
			return
		}

		srcIP, dstIP = ipv4.SrcIP, ipv4.DstIP
		srcMac, dstMac = eth.SrcMAC, eth.DstMAC
		proto := ""
		src := endpoint{
			ip:     ipv4.SrcIP,
			hwaddr: eth.SrcMAC,
		}
		dst := endpoint{
			ip:     ipv4.DstIP,
			hwaddr: eth.DstMAC,
		}
		if udp != nil {
			proto = "udp"
			src.port = int(udp.SrcPort)
			dst.port = int(udp.DstPort)
		} else if tcp != nil {
			proto = "tcp"
			src.port = int(tcp.SrcPort)
			dst.port = int(tcp.DstPort)
		}
		updateStats(src, dst, proto, len(data))

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
	defer samplerWaitGroup.Done()

	for samplerRunning {
		select {
		case <-auditDone:
			return
		case <-auditTicker.C:
		}

		// Make a copy of currentObservations and reset the master, so
		// we can evaluate the list without holding the lock.
		observedMtx.Lock()
		copy := currentObservations
		currentObservations = make(map[uint64]map[uint32]bool)
		observedMtx.Unlock()

		auditRecords(copy)
	}
}

func openInterface(ring, iface string) (*pcap.Handle, error) {
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
func sampleLoop(state *samplerState) {
	lastDropped := 0
	packets := 0

	for samplerRunning {
		data, _, err := state.handle.ZeroCopyReadPacketData()
		if err != nil {
			if err != io.EOF || samplerRunning {
				log.Println("Error reading packet data:", err)
			}
			return
		}

		processOnePacket(state, data)
		metrics.sampledPkts.Inc()

		packets++
		if packets%10000 == 0 {
			s, err := state.handle.Stats()
			delta := s.PacketsDropped - lastDropped
			if err == nil && delta != 0 {
				log.Printf("%s: dropped %d of %d packets\n",
					state.iface, s.PacketsDropped,
					s.PacketsReceived)
				lastDropped = s.PacketsDropped
				metrics.missedPkts.Add(float64(delta))
			}
		}
	}
}

func sampleInterface(state *samplerState) {
	var err error

	defer samplerWaitGroup.Done()

	parserInit(state)
	warned := false
	for samplerRunning {
		state.handle, err = openInterface(state.ring, state.iface)
		if err != nil {
			if !warned {
				log.Printf("openInterface(%s) failed: %v\n",
					state.iface, err)
				warned = true
			}
			time.Sleep(time.Second)
			continue
		}
		warned = false

		sampleLoop(state)
		state.handle.Close()
	}
	log.Printf("Sampler for %s (%s) offline\n", state.iface, state.ring)
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
	for _, s := range samplers {
		if s.handle != nil {
			s.handle.Close()
		}
	}

	close(auditDone)
	auditTicker.Stop()

	samplerWaitGroup.Wait()

	w.running = false
}

func subnetBroadcastAddr(n *net.IPNet) net.IP {
	base := network.IPAddrToUint32(n.IP)
	mask := network.IPAddrToUint32(net.IP(n.Mask))
	return network.Uint32ToIPAddr(base | ^mask)
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
			subnetBcast = append(subnetBcast,
				subnetBroadcastAddr(subnet))
			subnets = append(subnets, subnet)
			s := samplerState{
				ring:   ring,
				iface:  config.Bridge,
				hwaddr: hwaddr,
			}
			samplers = append(samplers, &s)
			samplerWaitGroup.Add(1)
			go sampleInterface(&s)
		}
	}

	samplerWaitGroup.Add(1)
	auditTicker = time.NewTicker(*auditTime)
	auditDone = make(chan bool)
	go auditor()

	w.running = true
}

func init() {
	addWatcher("sampler", sampleInit, sampleFini)
}
