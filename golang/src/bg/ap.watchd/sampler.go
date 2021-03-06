/*
 * Copyright 2020 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


// Appliance packet sampler. Logs statistics about captured packets, and keeps
// audit records of (MAC, IP) address pairs.
package main

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"os/exec"
	"sync"
	"time"

	"bg/ap_common/apcfg"
	"bg/ap_common/aputil"
	"bg/base_def"
	"bg/common/cfgapi"
	"bg/common/network"

	// Requires libpcap
	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcap"
	"github.com/google/gopacket/pfring"
)

var (
	auditTime = apcfg.Duration("audit_freq", time.Duration(time.Minute*2),
		false, nil)
	warnTime = apcfg.Duration("warn_time", time.Duration(time.Hour), true,
		nil)

	// use pfring or pcap to sample packets
	samplerType = apcfg.String("sampler", "pcap", false, nil)

	// bytes per packet to sample
	samplerSize = apcfg.Int("sampler_size", 1536, true, nil)

	// how frequently to update the sampler drop stats
	samplerStatPeriod = apcfg.Duration("sampler_stat_period",
		10*time.Second, true, nil)

	// report dropped samples if the dropped rate exceeds <n> / 1000
	samplerDropRate = apcfg.Int("sampler_drop_rate", 5, true, nil)

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

type samplerHandle interface {
	ZeroCopyReadPacketData() (data []byte, ci gopacket.CaptureInfo, err error)
	Close()
}

type samplerStats struct {
	total   uint64
	dropped uint64
}

type samplerState struct {
	// Persistent state describing the identity of the sampler
	layer2  bool
	ring    string
	iface   string
	hwaddr  net.HardwareAddr
	sampler samplerHandle
	sz      uint32

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
		network.IsMacMulticast(hwaddr) || internalMacs[mac] ||
		ip == 0 || gateways[ip] ||
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

func registerIPAddr(macStr string, ipaddr net.IP) {
	mac := network.MacToUint64(macStr)
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

func unregisterIPAddr(macStr string) {
	registerIPAddr(macStr, nil)
}

func wgGetMacs(srcIP, dstIP net.IP) (net.HardwareAddr, net.HardwareAddr) {
	srcMAC, _ := net.ParseMAC(ipToMac[srcIP.String()])
	dstMAC, _ := net.ParseMAC(ipToMac[dstIP.String()])

	return srcMAC, dstMAC
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

	if state.layer2 && eth == nil {
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
		if state.layer2 {
			srcMac, dstMac = eth.SrcMAC, eth.DstMAC
		} else {
			srcMac, dstMac = wgGetMacs(srcIP, dstIP)
		}

		proto := ""
		src := endpoint{
			ip:     ipv4.SrcIP,
			hwaddr: srcMac,
		}
		dst := endpoint{
			ip:     ipv4.DstIP,
			hwaddr: dstMac,
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
		updateStats(src, dst, proto, int(ipv4.Length))

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
				slog.Infof("Found device %v using %saddr %v",
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

func findInterface(ring, bridge string) (net.HardwareAddr, error) {
	var hwaddr net.HardwareAddr

	iface, err := net.InterfaceByName(bridge)
	if err != nil {
		err = fmt.Errorf("InterfaceByName(%s) failed: %v",
			bridge, err)
	} else {
		hwaddr = iface.HardwareAddr

		if !cfgapi.SystemRings[ring] {
			if err = network.WaitForDevice(bridge, time.Minute); err != nil {
				err = fmt.Errorf("WaitForDevice(%s) failed: %v",
					bridge, err)
			}
		}
	}
	return hwaddr, err
}

func openInterface(s *samplerState) (samplerHandle, error) {
	var hdl samplerHandle
	var err error

	if s.layer2 && *samplerType == "pfring" {
		var ring *pfring.Ring
		ring, err = pfring.NewRing(s.iface, s.sz, pfring.FlagPromisc)
		if err != nil {
			err = fmt.Errorf("pfring.NewRing(%s) failed: %v",
				s.iface, err)
		} else if err = ring.Enable(); err != nil {
			err = fmt.Errorf("pfring.Enable(%s) failed: %v",
				s.iface, err)
		} else {
			slog.Debugf("sampling %s with pf_ring", s.iface)
			ring.SetSocketMode(pfring.ReadOnly)
			hdl = ring
		}
	} else {
		hdl, err = pcap.OpenLive(s.iface, 65536, true, pcap.BlockForever)
		if err != nil {
			err = fmt.Errorf("pcap.OpenLive(%s) failed: %v",
				s.iface, err)
		} else {
			slog.Debugf("sampling %s with pcap", s.iface)
		}
	}

	return hdl, err
}

//
// Set up the GoPacket parser for this interface's packet stream
//
func parserInit(state *samplerState) {
	if state.layer2 {
		state.parser = gopacket.NewDecodingLayerParser(
			layers.LayerTypeEthernet,
			&state.decodedEth,
			&state.decodedIPv4,
			&state.decodedARP,
			&state.decodedUDP,
			&state.decodedTCP)
	} else {
		state.parser = gopacket.NewDecodingLayerParser(
			layers.LayerTypeIPv4,
			&state.decodedIPv4,
			&state.decodedARP,
			&state.decodedUDP,
			&state.decodedTCP)
	}
}

// Periodically check the count of dropped packets.  Log a message whenever we
// exceed some interesting threshold.
func packetStatsUpdate(state *samplerState, old *samplerStats, when time.Time) {
	var stats samplerStats

	switch sampler := state.sampler.(type) {
	case *pfring.Ring:
		x, _ := sampler.Stats()
		stats.dropped = x.Dropped
		stats.total = x.Received + x.Dropped
	case *pcap.Handle:
		x, _ := sampler.Stats()
		stats.dropped = uint64(x.PacketsDropped)
		stats.total = uint64(x.PacketsReceived + x.PacketsDropped)
	default:
		return
	}

	dropped := stats.dropped - old.dropped
	total := stats.total - old.total
	if total > 0 {
		rate := (dropped * 1000) / total
		if rate >= uint64(*samplerDropRate) {
			slog.Infof("%s dropped %d of %d packets (%1.2f%%) in %v",
				state.iface, dropped, total, float64(rate)/10,
				time.Since(when))

		}
	}
	metrics.droppedPkts.Add(int64(dropped))
	*old = stats
}

// Per-interface loop that endlessly consumes and processes packets
func sampleLoop(state *samplerState) {
	stats := samplerStats{}
	lastCheck := time.Now()

	for samplerRunning {
		if time.Since(lastCheck) > *samplerStatPeriod {
			packetStatsUpdate(state, &stats, lastCheck)
			lastCheck = time.Now()
		}

		if state.sz != uint32(*samplerSize) {
			slog.Infof("sampler size changed from %d to %d - resetting",
				state.sz, *samplerSize)
			return
		}

		data, _, err := state.sampler.ZeroCopyReadPacketData()
		if err != nil {
			slog.Warnf("Error reading packet data: %v", err)
			if err != io.EOF || samplerRunning {
				slog.Warnf("Error reading packet data: %v", err)
			}
			return
		}

		processOnePacket(state, data)
		metrics.sampledPkts.Inc()
	}
}

func sampleInterface(state *samplerState) {
	var lastErrMsg string

	defer samplerWaitGroup.Done()
	tlog := aputil.GetThrottledLogger(slog, time.Second, 10*time.Minute)

	bridge := state.iface
	ring := state.ring

	parserInit(state)
	for samplerRunning {
		var hdl samplerHandle

		hwaddr, err := findInterface(ring, bridge)
		if err == nil {
			state.sz = uint32(*samplerSize)
			hdl, err = openInterface(state)
		}

		if err != nil {
			errMsg := fmt.Sprintf("%v", err)
			if errMsg != lastErrMsg {
				tlog.Clear()
				lastErrMsg = errMsg
			}

			tlog.Warnf("%s sampler: %v", ring, err)
			time.Sleep(time.Second)
			continue
		}
		tlog.Clear()

		slog.Infof("Sampler for %s (%s) online", bridge, ring)
		state.hwaddr = hwaddr
		state.sampler = hdl
		sampleLoop(state)
		hdl.Close()
		slog.Infof("Sampler for %s (%s) offline", bridge, ring)
	}
}

func sampleFini(w *watcher) {
	slog.Infof("Shutting down sampler")
	samplerRunning = false
	for _, s := range samplers {
		if s.sampler != nil {
			switch sampler := s.sampler.(type) {
			case *pfring.Ring:
				sampler.Disable()
			case *pcap.Handle:
				sampler.Close()
			}
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

	if *samplerType != "pfring" {
		cmd := exec.Command("/sbin/rmmod", "pf_ring")
		if out, err := cmd.CombinedOutput(); err != nil {
			slog.Warnf("failed to unload pf_ring: %v", string(out))
		}
	}
	samplerRunning = true
	for ring, config := range rings {
		if ring == base_def.RING_INTERNAL {
			continue
		}

		bcastAddr := subnetBroadcastAddr(config.IPNet)
		subnetBcast = append(subnetBcast, bcastAddr)
		subnets = append(subnets, config.IPNet)

		s := &samplerState{ring: ring}

		if ring == base_def.RING_VPN {
			s.iface = "wgs0"
			s.layer2 = false
		} else {
			s.iface = config.Bridge
			s.layer2 = true
		}

		samplers = append(samplers, s)
		samplerWaitGroup.Add(1)
		go sampleInterface(s)
	}

	samplerWaitGroup.Add(1)
	auditTicker = time.NewTicker(*auditTime) // stopped in sampleFini
	auditDone = make(chan bool)
	go auditor()

	w.running = true
}

func init() {
	addWatcher("sampler", sampleInit, sampleFini)
}

