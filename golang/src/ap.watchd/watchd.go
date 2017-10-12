/*
 * COPYRIGHT 2017 Brightgate Inc.  All rights reserved.
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
	"flag"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"ap_common/apcfg"
	"ap_common/broker"
	"ap_common/mcp"
	"ap_common/network"
	"base_def"
	"base_msg"

	"github.com/golang/protobuf/proto"
)

var (
	brokerd *broker.Broker
	config  *apcfg.APConfig

	nmapDir = flag.String("scandir", ".",
		"directory in which the nmap scan files should be stored")
	addr = flag.String("prom_address", base_def.SCAND_PROMETHEUS_PORT,
		"The address to listen on for Prometheus HTTP requests.")
	verbose = flag.Bool("verbose", false, "Log nmap progress")
)

const (
	pname = "ap.watchd"
)

// Send a notification that we have an unknown entity on our network.
func logUnknown(iface, mac, ipstr string) bool {
	var addr net.IP

	addr = net.ParseIP(ipstr).To4()
	if addr == nil {
		log.Printf("Couldn't parse IP address: %s\n", ipstr)
		return false
	}

	hwaddr, err := net.ParseMAC(mac)
	if err != nil {
		log.Printf("Couldn't parse MAC: %s\n", mac)
		return false
	}

	t := time.Now()
	entity := &base_msg.EventNetEntity{
		Timestamp: &base_msg.Timestamp{
			Seconds: proto.Int64(t.Unix()),
			Nanos:   proto.Int32(int32(t.Nanosecond())),
		},
		Sender:        proto.String(brokerd.Name),
		Debug:         proto.String("-"),
		InterfaceName: &iface,
		Ipv4Address:   proto.Uint32(network.IPAddrToUint32(addr)),
		MacAddress:    proto.Uint64(network.HWAddrToUint64(hwaddr)),
	}

	err = brokerd.Publish(entity, base_def.TOPIC_ENTITY)
	return err == nil
}

func configIPv4Changed(path []string, value string) {
	hwaddr, err := net.ParseMAC(path[1])
	if err != nil {
		log.Printf("invalid MAC address %s", path[1])
		return
	}

	if ipv4 := net.ParseIP(value); ipv4 != nil {
		samplerUpdate(hwaddr, ipv4.To4(), true)
		scannerRequest(ipv4.String())
	} else {
		log.Printf("invalid IPv4 address %s", value)
	}
}

func configIPv4Delexp(path []string) {
	if hwaddr, err := net.ParseMAC(path[1]); err == nil {
		samplerDelete(hwaddr)
	} else {
		log.Printf("invalid MAC address %s", path[1])
	}
}

func signalHandler() {
	sig := make(chan os.Signal)

	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	received := <-sig

	log.Printf("Signal (%v) received.\n", received)
}

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	flag.Parse()

	mcpd, err := mcp.New(pname)
	if err != nil {
		log.Printf("failed to connect to mcp\n")
	}

	brokerd = broker.New(pname)
	defer brokerd.Fini()

	config, err = apcfg.NewConfig(brokerd, pname)
	if err != nil {
		log.Fatalf("cannot connect to configd: %v\n", err)
	}
	config.HandleChange(`^@/clients/.*/ipv4$`, configIPv4Changed)

	if mcpd != nil {
		if err = mcpd.SetState(mcp.ONLINE); err != nil {
			log.Printf("failed to set status\n")
		}
	}

	metricsInit()

	if err = sampleInit(); err != nil {
		log.Printf("Failed to start sampler: %v\n", err)
	} else {
		defer sampleFini()
	}

	scannerInit()
	defer scannerFini()

	config.HandleChange(`^@/clients/.*/ipv4$`, configIPv4Changed)
	config.HandleDelete(`^@/clients/.*/ipv4$`, configIPv4Delexp)
	config.HandleExpire(`^@/clients/.*/ipv4$`, configIPv4Delexp)

	signalHandler()
}
