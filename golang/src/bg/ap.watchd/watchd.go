/*
 * COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
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
	"net/http"
	"os"
	"os/signal"
	"runtime"
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
	"bg/common/cfgapi"

	"github.com/golang/protobuf/proto"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const pname = "ap.watchd"

var (
	watchDir = flag.String("dir", "/var/spool/watchd",
		"directory in which the watchd work files should be stored")
	addr = flag.String("pport", base_def.WATCHD_PROMETHEUS_PORT,
		"The address to listen on for Prometheus HTTP requests.")
	verbose = flag.Bool("verbose", false, "Log nmap progress")

	brokerd *broker.Broker
	config  *cfgapi.Handle

	profiler *aputil.Profiler
	watchers = make([]*watcher, 0)

	rings   cfgapi.RingMap
	macToIP = make(map[string]string)
	ipToMac = make(map[string]string)
	mapMtx  sync.Mutex

	gateways     map[uint32]bool
	internalMacs map[uint64]bool

	metrics struct {
		lanDrops     prometheus.Counter
		wanDrops     prometheus.Counter
		sampledPkts  prometheus.Counter
		missedPkts   prometheus.Counter
		tcpScans     prometheus.Counter
		tcpScanTime  prometheus.Summary
		udpScans     prometheus.Counter
		udpScanTime  prometheus.Summary
		hostScans    prometheus.Counter
		hostScanTime prometheus.Summary
		vulnScans    prometheus.Counter
		vulnScanTime prometheus.Summary
		blockedIPs   prometheus.Counter
		knownHosts   prometheus.Gauge
	}
)

//
// watchd hosts a number of relatively independent monitoring subsystems.  Each
// is defined by the following structure, and plugged into the watchd framework
// at launch time by their init() functions.
//
type watcher struct {
	name    string
	running bool
	init    func(*watcher)
	fini    func(*watcher)
}

func addWatcher(name string, ini func(*watcher), fini func(*watcher)) {
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
	mac := path[1]

	hwaddr, err := net.ParseMAC(mac)
	if err != nil {
		log.Printf("invalid MAC address %s", mac)
		return
	}

	if ipv4 := net.ParseIP(value); ipv4 != nil {
		registerIPAddr(hwaddr, ipv4.To4())
		scannerRequest(mac, ipv4.String())
		setMacIP(mac, value)
	} else {
		log.Printf("invalid IPv4 address %s", value)
	}
}

func configIPv4Delexp(path []string) {
	if hwaddr, err := net.ParseMAC(path[1]); err == nil {
		unregisterIPAddr(hwaddr)
		clearMac(path[1])
	} else {
		log.Printf("invalid MAC address %s", path[1])
	}
}

func getGateways() {
	gateways = make(map[uint32]bool)

	for _, r := range rings {
		router := net.ParseIP(network.SubnetRouter(r.Subnet))
		gateways[network.IPAddrToUint32(router)] = true
	}

	// Build a set of the MACs belonging to our APs, so we can distinguish
	// between client and internal network traffic
	internalMacs = make(map[uint64]bool)
	nics, _ := config.GetNics("", "")
	for _, nic := range nics {
		if hwaddr := network.MacToUint64(nic); hwaddr != 0 {
			internalMacs[hwaddr] = true
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
			registerIPAddr(hwaddr, client.IPv4)
		}
	}
}

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
	profiling := false

	sig := make(chan os.Signal, 3)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM, syscall.SIGUSR2)
	for {
		s := <-sig
		log.Printf("Signal (%v) received.\n", s)
		switch s {

		case syscall.SIGUSR2:
			if profiler == nil {
				continue
			}

			if !profiling {
				if err := profiler.CPUStart(); err != nil {
					log.Printf("profiler failed: %v\n", err)
				} else {
					profiling = true
				}
			} else {
				profiler.CPUStop()
				profiler.HeapProfile()
				profiling = false
			}
		default:
			return
		}
	}
}

func prometheusInit() {
	metrics.lanDrops = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "watchd_landrops",
		Help: "Number of internal packets dropped by the firewall",
	})
	metrics.wanDrops = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "watchd_wandrops",
		Help: "Number of external packets dropped by the firewall",
	})
	metrics.sampledPkts = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "watchd_sampled_pkts",
		Help: "Number of packets exampined by the sampler",
	})
	metrics.missedPkts = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "watchd_missed_pkts",
		Help: "Number of packets missed by the sampler",
	})
	metrics.tcpScans = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "watchd_tcp_scans",
		Help: "Number of device tcp port scans completed",
	})
	metrics.tcpScanTime = prometheus.NewSummary(prometheus.SummaryOpts{
		Name: "watchd_tcp_scan_time",
		Help: "time spent on tcp port scans",
	})
	metrics.udpScans = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "watchd_udp_scans",
		Help: "Number of device udp port scans completed",
	})
	metrics.udpScanTime = prometheus.NewSummary(prometheus.SummaryOpts{
		Name: "watchd_udp_scan_time",
		Help: "time spent on udp port scans",
	})
	metrics.hostScans = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "watchd_host_scans",
		Help: "Number of host scans completed",
	})
	metrics.hostScanTime = prometheus.NewSummary(prometheus.SummaryOpts{
		Name: "watchd_host_scan_time",
		Help: "time spent on host scans",
	})
	metrics.vulnScans = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "watchd_vuln_scans",
		Help: "Number of device vulnerability scans completed",
	})
	metrics.vulnScanTime = prometheus.NewSummary(prometheus.SummaryOpts{
		Name: "watchd_vuln_scan_time",
		Help: "time spent on vulnerability scans",
	})
	metrics.blockedIPs = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "watchd_blocked_ips",
		Help: "Number of dangerous IPs we've detected and blocked",
	})
	metrics.knownHosts = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "watchd_known_hosts",
		Help: "Number of devices we know about and are monitoring",
	})
	prometheus.MustRegister(metrics.lanDrops)
	prometheus.MustRegister(metrics.wanDrops)
	prometheus.MustRegister(metrics.sampledPkts)
	prometheus.MustRegister(metrics.missedPkts)
	prometheus.MustRegister(metrics.tcpScans)
	prometheus.MustRegister(metrics.tcpScanTime)
	prometheus.MustRegister(metrics.udpScans)
	prometheus.MustRegister(metrics.udpScanTime)
	prometheus.MustRegister(metrics.hostScans)
	prometheus.MustRegister(metrics.hostScanTime)
	prometheus.MustRegister(metrics.vulnScans)
	prometheus.MustRegister(metrics.vulnScanTime)
	prometheus.MustRegister(metrics.blockedIPs)
	prometheus.MustRegister(metrics.knownHosts)

	http.Handle("/metrics", promhttp.Handler())
	go http.ListenAndServe(base_def.WATCHD_PROMETHEUS_PORT, nil)
}

func main() {
	// To avoid dropping packets, we need to have extra processes available.
	runtime.GOMAXPROCS(8)
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	flag.Parse()
	*watchDir = aputil.ExpandDirPath(*watchDir)

	if !aputil.FileExists(*watchDir) {
		if err := os.MkdirAll(*watchDir, 0755); err != nil {
			log.Fatalf("Error adding directory %s: %v\n",
				*watchDir, err)
		}
	}
	mcpd, err := mcp.New(pname)
	if err != nil {
		log.Printf("failed to connect to mcp\n")
	}

	prometheusInit()

	brokerd = broker.New(pname)
	defer brokerd.Fini()

	config, err = apcfg.NewConfigd(brokerd, pname, cfgapi.AccessInternal)
	if err != nil {
		log.Fatalf("cannot connect to configd: %v\n", err)
	}

	config.HandleChange(`^@/clients/.*/ipv4$`, configIPv4Changed)
	config.HandleDelete(`^@/clients/.*/ipv4$`, configIPv4Delexp)
	config.HandleExpire(`^@/clients/.*/ipv4$`, configIPv4Delexp)
	macToIPInit()
	rings = config.GetRings()

	profiler = aputil.NewProfiler(pname)
	defer profiler.CPUStop()

	mcpd.SetState(mcp.ONLINE)
	for _, w := range watchers {
		go w.init(w)
	}

	signalHandler()

	for _, w := range watchers {
		if w.running {
			log.Printf("Stopping %s\n", w.name)
			go w.fini(w)
		}
	}

	for _, w := range watchers {
		logged := false
		for w.running {
			if !logged {
				log.Printf("Waiting for %s\n", w.name)
				logged = true
			}
			time.Sleep(time.Millisecond)
		}
	}
}
