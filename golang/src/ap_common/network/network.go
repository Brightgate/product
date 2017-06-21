/*
 * COPYRIGHT 2017 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

// Package network contains helper functions for reading a writing packets to a
// network interface.
package network

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"net"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcap"
)

// Well known MAC addresses
var (
	MacZero  = net.HardwareAddr([]byte{0, 0, 0, 0, 0, 0})
	MacBcast = net.HardwareAddr([]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF})
	macMcast = net.HardwareAddr([]byte{0x01, 0x00, 0x5E}) // prefix only
)

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
		HWAddr: MacBcast,
	}

	return ArpRequestWait(handle, src, dst)
}

func IsMacMulticast(a net.HardwareAddr) bool {
	return a[3]&0x80 == 0x80 && bytes.HasPrefix(a, macMcast)
}

// HWAddrToUint64 encodes a net.HardwareAddr as a uint64
func HWAddrToUint64(a net.HardwareAddr) uint64 {
	hwaddr := make([]byte, 8)
	hwaddr[0] = 0
	hwaddr[1] = 0
	copy(hwaddr[2:], a)

	return binary.BigEndian.Uint64(hwaddr)
}
