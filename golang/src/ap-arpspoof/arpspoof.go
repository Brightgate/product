/*
 * COPYRIGHT 2017 Brightgate Inc. All rights reserved.
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
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"ap_common/network"

	"github.com/google/gopacket/pcap"
)

var (
	argIface  = flag.String("i", "", "The network interface to use")
	argTarget = flag.String("t", "", "The target computer's IP address")
	argHost   = flag.String("h", "", "The host IP address to impersonate")
)

func main() {
	flag.Parse()
	if *argIface == "" || *argTarget == "" || *argHost == "" {
		flag.Usage()
		os.Exit(1)
	}

	target := net.ParseIP(*argTarget)
	if target == nil {
		log.Fatalf("Unable to parse target IP %s\n", *argTarget)
	}

	host := net.ParseIP(*argHost)
	if host == nil {
		log.Fatalf("Unable to parse host IP %s\n", *argHost)
	}

	iface, err := net.InterfaceByName(*argIface)
	if err != nil {
		log.Fatalf("Unable to use interface %s\n", *argIface)
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

	ifaceData := &network.ArpData{IP: ifaceAddr, HWAddr: iface.HardwareAddr}

	handle, err := pcap.OpenLive(iface.Name, 65536, true, pcap.BlockForever)
	if err != nil {
		log.Fatal(err)
	}
	defer handle.Close()

	// Broadcast an ARP request to discover both the host and target MAC addresses.
	// The hosts's MAC address will be saved so that we can correct the spoof on
	// program exit. The target's MAC address will be used in constructing the
	// spoofed ARP packet.
	hostHwAddr := network.ArpBroadcastWait(handle, ifaceData, host)
	targetHwAddr := network.ArpBroadcastWait(handle, ifaceData, target)

	log.Printf("Discovered host: %s and target: %s\n", hostHwAddr, targetHwAddr)

	hostData := &network.ArpData{
		IP:     host,
		HWAddr: hostHwAddr,
	}

	targetData := &network.ArpData{
		IP:     target,
		HWAddr: targetHwAddr,
	}

	spoofData := &network.ArpData{
		IP:     hostData.IP,
		HWAddr: ifaceData.HWAddr,
	}

	network.ArpRequestWait(handle, spoofData, targetData)

	// The spoof has taken effect but it may not be persistent. For example an
	// iPhone has been seen re-ARP-ing itself after ~60 seconds.
	log.Println("Spoof done. Waiting....")

	sig := make(chan os.Signal)
	signal.Notify(sig, syscall.SIGINT)
	<-sig

	log.Println("Restoring target")
	network.ArpRequest(handle, hostData, targetData)
}
