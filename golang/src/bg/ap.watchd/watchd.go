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
	"net"
	"net/http"
	_ "net/http/pprof"
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
	"bg/base_def"
	"bg/base_msg"
	"bg/common/cfgapi"
	"bg/common/network"

	"github.com/golang/protobuf/proto"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
)

const pname = "ap.watchd"

var (
	watchDir = apcfg.String("data_dir", "/var/spool/watchd", false, nil)
	addr     = apcfg.String("diag_port", base_def.WATCHD_DIAG_PORT,
		false, nil)
	nmapVerbose = apcfg.Bool("nmap_verbose", false, true, nil)
	logLevel    = apcfg.String("log_level", "info", true, aputil.LogSetLevel)

	brokerd *broker.Broker
	config  *cfgapi.Handle
	slog    *zap.SugaredLogger

	watchers = make([]*watcher, 0)

	rings   cfgapi.RingMap
	macToIP = make(map[string]string)
	ipToMac = make(map[string]string)
	mapMtx  sync.Mutex

	gateways     map[uint32]bool
	internalMacs map[uint64]bool

	metrics struct {
		lanDrops       prometheus.Counter
		wanDrops       prometheus.Counter
		sampledPkts    prometheus.Counter
		missedPkts     prometheus.Counter
		tcpScans       prometheus.Counter
		tcpScanTime    prometheus.Summary
		udpScans       prometheus.Counter
		udpScanTime    prometheus.Summary
		subnetScans    prometheus.Counter
		subnetScanTime prometheus.Summary
		vulnScans      prometheus.Counter
		vulnScanTime   prometheus.Summary
		blockedIPs     prometheus.Counter
		knownHosts     prometheus.Gauge
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
		slog.Warnf("invalid MAC address %s", mac)
		return
	}

	if ipv4 := net.ParseIP(value); ipv4 != nil {
		registerIPAddr(hwaddr, ipv4.To4())
		scannerRequest(mac, ipv4.String())
		setMacIP(mac, value)
	} else {
		slog.Warnf("invalid IPv4 address %s", value)
	}
}

func configIPv4Delexp(path []string) {
	if hwaddr, err := net.ParseMAC(path[1]); err == nil {
		unregisterIPAddr(hwaddr)
		clearMac(path[1])
	} else {
		slog.Warnf("invalid MAC address %s", path[1])
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
	tmp := make(map[uint64]bool)
	nics, _ := config.GetNics()
	for _, nic := range nics {
		if nic.MacAddr != "" {
			tmp[network.MacToUint64(nic.MacAddr)] = true
		}
	}
	internalMacs = tmp
}

func getLeases() {
	clients := config.GetClients()
	if clients == nil {
		return
	}

	for macaddr, client := range clients {
		hwaddr, err := net.ParseMAC(macaddr)
		if err != nil {
			slog.Warnf("Invalid mac address: %s", macaddr)
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
		slog.Errorf("Couldn't parse IP address: %s", ipstr)
		return false
	}

	hwaddr, err := net.ParseMAC(mac)
	if err != nil {
		slog.Errorf("Couldn't parse MAC: %s", mac)
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

func eventHandler(event []byte) {
	slog.Debugf("got network update event - reevaluting interfaces")
	getGateways()
}

func signalHandler() {
	sig := make(chan os.Signal, 2)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	s := <-sig
	slog.Infof("Signal (%v) received.", s)
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
	metrics.subnetScans = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "watchd_subnet_scans",
		Help: "Number of subnet scans completed",
	})
	metrics.subnetScanTime = prometheus.NewSummary(prometheus.SummaryOpts{
		Name: "watchd_subnet_scan_time",
		Help: "time spent on subnet scans",
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
	prometheus.MustRegister(metrics.subnetScans)
	prometheus.MustRegister(metrics.subnetScanTime)
	prometheus.MustRegister(metrics.vulnScans)
	prometheus.MustRegister(metrics.vulnScanTime)
	prometheus.MustRegister(metrics.blockedIPs)
	prometheus.MustRegister(metrics.knownHosts)

	http.Handle("/metrics", promhttp.Handler())
	go http.ListenAndServe(base_def.WATCHD_DIAG_PORT, nil)
}

func main() {
	// To avoid dropping packets, we need to have extra processes available.
	runtime.GOMAXPROCS(8)

	slog = aputil.NewLogger(pname)
	defer slog.Sync()

	*watchDir = aputil.ExpandDirPath(*watchDir)
	if !aputil.FileExists(*watchDir) {
		if err := os.MkdirAll(*watchDir, 0755); err != nil {
			slog.Fatalf("Error adding directory %s: %v",
				*watchDir, err)
		}
	}
	mcpd, err := mcp.New(pname)
	if err != nil {
		slog.Warnf("failed to connect to mcp")
	}

	prometheusInit()

	brokerd = broker.New(pname)
	defer brokerd.Fini()

	config, err = apcfg.NewConfigd(brokerd, pname, cfgapi.AccessInternal)
	if err != nil {
		slog.Fatalf("cannot connect to configd: %v", err)
	}

	config.HandleChange(`^@/clients/.*/ipv4$`, configIPv4Changed)
	config.HandleDelete(`^@/clients/.*/ipv4$`, configIPv4Delexp)
	config.HandleExpire(`^@/clients/.*/ipv4$`, configIPv4Delexp)
	brokerd.Handle(base_def.TOPIC_UPDATE, eventHandler)

	macToIPInit()
	rings = config.GetRings()
	getGateways()

	mcpd.SetState(mcp.ONLINE)
	slog.Infof("watchd online")
	for _, w := range watchers {
		go w.init(w)
	}

	apiInit()
	signalHandler()

	for _, w := range watchers {
		if w.running {
			slog.Infof("Stopping %s", w.name)
			go w.fini(w)
		}
	}

	for _, w := range watchers {
		logged := false
		for w.running {
			if !logged {
				slog.Infof("Waiting for %s", w.name)
				logged = true
			}
			time.Sleep(time.Millisecond)
		}
	}
}
