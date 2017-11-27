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
	"sync"
	"syscall"
	"time"

	"bg/ap_common/apcfg"
	"bg/ap_common/aputil"
	"bg/ap_common/broker"
	"bg/ap_common/mcp"
	"bg/ap_common/network"
	"bg/base_def"
	"bg/base_msg"

	"github.com/golang/protobuf/proto"
)

const (
	pname = "ap.watchd"
)

var (
	brokerd *broker.Broker
	config  *apcfg.APConfig

	watchDir = flag.String("dir", ".",
		"directory in which the watchd work files should be stored")
	addr = flag.String("prom_address", base_def.WATCHD_PROMETHEUS_PORT,
		"The address to listen on for Prometheus HTTP requests.")
	verbose = flag.Bool("verbose", false, "Log nmap progress")

	watchers = make([]*watcher, 0)
)

var (
	rings apcfg.RingMap

	macToIP = make(map[string]string)
	ipToMac = make(map[string]string)
	mapMtx  sync.Mutex
)

//
// watchd hosts a number of relatively independent monitoring subsystems.  Each
// is defined by the following structure, and plugged into the watchd framework
// at launch time by their init() functions.
//
type watcher struct {
	name string
	init func() error
	fini func()
}

func addWatcher(name string, ini func() error, fini func()) {
	w := watcher{
		name: name,
		init: ini,
		fini: fini,
	}

	watchers = append(watchers, &w)
}

//
// We maintain mappings from MAC to IP Address, and from IP Address to MAC.
// These mappings are populated at startup with call to GetClients().  They are
// updated over time by monitoring changes in @/clients/<macaddr>ipv4
//
func getMacFromIP(ip string) string {
	mapMtx.Lock()
	mac := ipToMac[ip]
	mapMtx.Unlock()
	return mac
}

func getIPFromMac(mac string) string {
	mapMtx.Lock()
	ip := macToIP[mac]
	mapMtx.Unlock()
	return ip
}

func setMacIP(mac, ip string) {
	mapMtx.Lock()
	macToIP[mac] = ip
	ipToMac[ip] = mac
	log.Printf("%s <->%s\n", mac, ip)
	mapMtx.Unlock()
}

func clearMac(mac string) {
	mapMtx.Lock()
	ip := macToIP[mac]
	if ip != "" {
		delete(ipToMac, ip)
	}
	delete(macToIP, mac)
	mapMtx.Unlock()
}

func macToIPInit() {
	clients := config.GetClients()

	for m, c := range clients {
		if c.IPv4 != nil {
			setMacIP(m, c.IPv4.String())
		}
	}
}

func configIPv4Changed(path []string, value string, expires *time.Time) {
	hwaddr, err := net.ParseMAC(path[1])
	if err != nil {
		log.Printf("invalid MAC address %s", path[1])
		return
	}

	if ipv4 := net.ParseIP(value); ipv4 != nil {
		samplerUpdate(hwaddr, ipv4.To4(), true)
		scannerRequest(ipv4.String())
		setMacIP(path[1], value)
	} else {
		log.Printf("invalid IPv4 address %s", value)
	}
}

func configIPv4Delexp(path []string) {
	if hwaddr, err := net.ParseMAC(path[1]); err == nil {
		samplerDelete(hwaddr)
		clearMac(path[1])
	} else {
		log.Printf("invalid MAC address %s", path[1])
	}
}

//
// Send a notification that we have an unknown entity on our network.
func logUnknown(ring, mac, ipstr string) bool {
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

	entity := &base_msg.EventNetEntity{
		Timestamp:   aputil.NowToProtobuf(),
		Sender:      proto.String(brokerd.Name),
		Debug:       proto.String("-"),
		Ring:        proto.String(ring),
		Ipv4Address: proto.Uint32(network.IPAddrToUint32(addr)),
		MacAddress:  proto.Uint64(network.HWAddrToUint64(hwaddr)),
	}

	err = brokerd.Publish(entity, base_def.TOPIC_ENTITY)
	return err == nil
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
	*watchDir = aputil.ExpandDirPath(*watchDir)

	if !aputil.FileExists(*watchDir) {
		if err := os.Mkdir(*watchDir, 0755); err != nil {
			log.Fatalf("Error adding directory %s: %v\n",
				*watchDir, err)
		}
	}
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
	config.HandleDelete(`^@/clients/.*/ipv4$`, configIPv4Delexp)
	config.HandleExpire(`^@/clients/.*/ipv4$`, configIPv4Delexp)
	macToIPInit()
	rings = config.GetRings()

	if mcpd != nil {
		mcpd.SetState(mcp.ONLINE)
	}

	for _, w := range watchers {
		var err error

		if w.init != nil {
			err = w.init()
		}

		if err != nil {
			log.Printf("Failed to start %s: %v\n", w.name, err)
		} else if w.fini != nil {
			defer w.fini()
		}
	}

	signalHandler()
}
