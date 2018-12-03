/*
 * COPYRIGHT 2018 Brightgate Inc. All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or  alteration will be a violation of federal law.
 */

// ap-arpspoof -i <interface> -t <target> -h <host>

// ap-arpspoof will send ARP packets on <interface> to cause the <target> to
// believe this machine is <host>.
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"bg/ap_common/network"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcap"
)

var (
	argIface  string
	argTarget string
	argHost   string
)

func arpFlagInit() {
	flag.StringVar(&argIface, "i", "", "The network interface to use")
	flag.StringVar(&argTarget, "t", "", "The target computer's IP address")
	flag.StringVar(&argHost, "h", "", "The host IP address to impersonate")
}

// ArpData consists of the data necessary to construct an ARP request or reply
type ArpData struct {
	IP     net.IP
	HWAddr net.HardwareAddr
}

func buildArpPacket(src *ArpData, dst *ArpData, op uint16) ([]byte, error) {
	ether := layers.Ethernet{
		EthernetType: layers.EthernetTypeARP,

		SrcMAC: src.HWAddr,
		DstMAC: dst.HWAddr,
	}

	arp := layers.ARP{
		AddrType: layers.LinkTypeEthernet,
		Protocol: layers.EthernetTypeIPv4,

		HwAddressSize:   6,
		ProtAddressSize: 4,
		Operation:       op,

		SourceHwAddress:   []byte(src.HWAddr),
		SourceProtAddress: []byte(src.IP.To4()),

		DstHwAddress:   []byte{0, 0, 0, 0, 0, 0},
		DstProtAddress: []byte(dst.IP.To4()),
	}

	buf := gopacket.NewSerializeBuffer()
	opts := gopacket.SerializeOptions{}
	err := gopacket.SerializeLayers(buf, opts, &ether, &arp)
	if err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// ArpRequest sends an ARP request from src to dst.
func ArpRequest(handle *pcap.Handle, src, dst *ArpData) error {
	packet, err := buildArpPacket(src, dst, layers.ARPRequest)
	if err != nil {
		return err
	}
	return handle.WritePacketData(packet)
}

func replyWait(handle *pcap.Handle, done chan bool, ipv4 net.IP) net.HardwareAddr {
	var eth layers.Ethernet
	var arp layers.ARP

	parser := gopacket.NewDecodingLayerParser(layers.LayerTypeEthernet,
		&eth, &arp)
	decoded := []gopacket.LayerType{}

	for {
		select {
		case <-done:
			return nil
		default:
			data, _, err := handle.ReadPacketData()
			if err != nil {
				fmt.Println("ReadPacketData() failed:", err)
				continue
			}

			err = parser.DecodeLayers(data, &decoded)
			if err != nil {
				continue
			}

			if len(decoded) != 2 {
				continue
			}

			if arp.Operation == layers.ARPReply && net.IP(arp.SourceProtAddress).Equal(ipv4) {
				close(done)
				return net.HardwareAddr(arp.SourceHwAddress)
			}
		}
	}
}

func requestLoop(handle *pcap.Handle, done chan bool, src, dst *ArpData) {
	for {
		time.Sleep(500 * time.Millisecond)
		select {
		case <-done:
			return
		default:
			if err := ArpRequest(handle, src, dst); err != nil {
				close(done)
				return
			}
		}
	}
}

// ArpRequestWait sends ARP requests from src to dst for the IP address dst.IP
// and waits for a reply. If no reply is received ArpRequestWait blocks indefinitely
func ArpRequestWait(handle *pcap.Handle, src, dst *ArpData) net.HardwareAddr {
	done := make(chan bool)

	go requestLoop(handle, done, src, dst)

	return replyWait(handle, done, dst.IP)
}

// ArpBroadcastWait broadcasts ARP requests from src for the IP address ip and
// waits for a reply. If no reply is received ArpBroadcastWait blocks indefinitely
func ArpBroadcastWait(handle *pcap.Handle, src *ArpData, ip net.IP) net.HardwareAddr {
	dst := &ArpData{
		IP:     ip,
		HWAddr: network.MacBcast,
	}

	return ArpRequestWait(handle, src, dst)
}

func arpspoof() {
	flag.Parse()
	if argIface == "" || argTarget == "" || argHost == "" {
		flag.Usage()
		os.Exit(1)
	}

	target := net.ParseIP(argTarget)
	if target == nil {
		log.Fatalf("Unable to parse target IP %s\n", argTarget)
	}

	host := net.ParseIP(argHost)
	if host == nil {
		log.Fatalf("Unable to parse host IP %s\n", argHost)
	}

	iface, err := net.InterfaceByName(argIface)
	if err != nil {
		log.Fatalf("Unable to use interface %s\n", argIface)
	}

	ifaceAddrs, err := iface.Addrs()
	if err != nil {
		log.Fatalln("Unable to get interface unicast addresses:", err)
	}

	var ifaceAddr net.IP
	for _, addr := range ifaceAddrs {
		if ipnet, ok := addr.(*net.IPNet); ok {
			if ip4 := ipnet.IP.To4(); ip4 != nil {
				ifaceAddr = ip4
				break
			}
		}
	}

	if ifaceAddr == nil {
		log.Fatalln("Could not get interface address.")
	}

	ifaceData := &ArpData{IP: ifaceAddr, HWAddr: iface.HardwareAddr}

	handle, err := pcap.OpenLive(iface.Name, 65536, true, pcap.BlockForever)
	if err != nil {
		log.Fatal(err)
	}
	defer handle.Close()

	// Broadcast an ARP request to discover both the host and target MAC addresses.
	// The hosts's MAC address will be saved so that we can correct the spoof on
	// program exit. The target's MAC address will be used in constructing the
	// spoofed ARP packet.
	hostHwAddr := ArpBroadcastWait(handle, ifaceData, host)
	targetHwAddr := ArpBroadcastWait(handle, ifaceData, target)

	log.Printf("Discovered host: %s and target: %s\n", hostHwAddr, targetHwAddr)

	hostData := &ArpData{
		IP:     host,
		HWAddr: hostHwAddr,
	}

	targetData := &ArpData{
		IP:     target,
		HWAddr: targetHwAddr,
	}

	spoofData := &ArpData{
		IP:     hostData.IP,
		HWAddr: ifaceData.HWAddr,
	}

	ArpRequestWait(handle, spoofData, targetData)

	// The spoof has taken effect but it may not be persistent. For example an
	// iPhone has been seen re-ARP-ing itself after ~60 seconds.
	log.Println("Spoof done. Waiting....")

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT)
	<-sig

	log.Println("Restoring target")
	ArpRequest(handle, hostData, targetData)
}

func init() {
	addTool("ap-arpspoof", arpspoof)
}
