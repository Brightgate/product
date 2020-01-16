/*
 * COPYRIGHT 2020 Brightgate Inc.  All rights reserved.
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
	"encoding/binary"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"bg/ap_common/apcfg"
	"bg/ap_common/aputil"
	"bg/ap_common/bgmetrics"
	"bg/ap_common/broker"
	"bg/ap_common/mcp"
	"bg/ap_common/platform"
	"bg/base_def"
	"bg/base_msg"
	"bg/common/cfgapi"
	"bg/common/network"

	"github.com/golang/protobuf/proto"
	"go.uber.org/zap"
)

const pname = "ap.watchd"

var (
	watchDir = apcfg.String("data_dir", "watchd", false, nil)
	addr     = apcfg.String("diag_port", base_def.WATCHD_DIAG_PORT,
		false, nil)
	nmapVerbose = apcfg.Bool("nmap_verbose", false, true, nil)
	logLevel    = apcfg.String("log_level", "info", true, aputil.LogSetLevel)

	brokerd *broker.Broker
	config  *cfgapi.Handle
	slog    *zap.SugaredLogger
	plat    *platform.Platform

	watchers = make([]*watcher, 0)

	rings   cfgapi.RingMap
	macToIP = make(map[string]string)
	ipToMac = make(map[string]string)
	mapMtx  sync.Mutex

	gateways     map[uint32]bool
	internalMacs map[uint64]bool

	bgm     *bgmetrics.Metrics
	metrics struct {
		lanDrops       *bgmetrics.Counter
		wanDrops       *bgmetrics.Counter
		sampledPkts    *bgmetrics.Counter
		missedPkts     *bgmetrics.Counter
		tcpScans       *bgmetrics.Counter
		tcpScanTime    *bgmetrics.Summary
		udpScans       *bgmetrics.Counter
		udpScanTime    *bgmetrics.Summary
		subnetScans    *bgmetrics.Counter
		subnetScanTime *bgmetrics.Summary
		vulnScans      *bgmetrics.Counter
		vulnScanTime   *bgmetrics.Summary
		blockedIPs     *bgmetrics.Counter
		knownHosts     *bgmetrics.Gauge
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

func setMacIP(mac string, ipv4 net.IP) {
	ip := ipv4.String()
	registerIPAddr(mac, ipv4)

	mapMtx.Lock()
	macToIP[mac] = ip
	ipToMac[ip] = mac
	mapMtx.Unlock()
}

func clearMac(mac string) {
	unregisterIPAddr(mac)

	mapMtx.Lock()
	if ip, ok := macToIP[mac]; ok {
		delete(ipToMac, ip)
	}
	delete(macToIP, mac)
	mapMtx.Unlock()
}

func configIPv4Changed(path []string, value string, expires *time.Time) {
	mac := path[1]

	if _, err := net.ParseMAC(mac); err != nil {
		slog.Warnf("invalid MAC address %s", mac)
		return
	}

	if expires != nil && expires.Before(time.Now()) {
		return
	}

	if ipv4 := net.ParseIP(value); ipv4 != nil {
		scannerRequest(mac, value, 30*time.Second)
		setMacIP(mac, ipv4)
	} else {
		slog.Warnf("invalid IPv4 address %s", value)
	}
}

// Handle the deletion of a full client record, or just its ipv4 address
func configClientDelete(path []string) {
	if len(path) == 2 || (len(path) == 3 && path[2] == "ipv4") {
		mac := path[1]
		if _, err := net.ParseMAC(mac); err == nil {
			ip := getIPFromMac(mac)
			cancelAllScans(mac, ip)
			clearMac(mac)
		} else {
			slog.Warnf("invalid MAC address %s", mac)
		}
	}
}

func getGateways() {
	// Build a map with all possible gateway IPs.  This is used as a fast
	// way to determine whether a packet source/destination is one of our
	// nodes rather than a client device.
	// XXX: we could reduce the size of the map by populating it with only
	// those addresses that belong to active nodes rather than all nodes.
	newGateways := make(map[uint32]bool)
	for _, r := range rings {
		_, ipnet, _ := net.ParseCIDR(r.Subnet)
		base := ipnet.IP.To4()
		for i := 1; i < base_def.MAX_SATELLITES; i++ {
			addr := make(net.IP, 4)
			binary.BigEndian.PutUint32(addr,
				binary.BigEndian.Uint32(base)+uint32(i))
			newGateways[network.IPAddrToUint32(addr)] = true
		}
	}
	gateways = newGateways

	// Build a set of the MACs belonging to our APs, so we can distinguish
	// between client and internal network traffic
	tmp := make(map[uint64]bool)
	nics, _ := config.GetNics()
	for _, nic := range nics {
		if mac := strings.ToLower(nic.MacAddr); mac != "" {
			macKey := network.MacToUint64(mac)
			tmp[macKey] = true
		}
	}
	internalMacs = tmp
}

func getLeases() {
	clients := config.GetClients()
	if clients == nil {
		return
	}

	now := time.Now()
	for macaddr, client := range clients {
		var expired bool
		var action, when string

		if client.IPv4 == nil {
			continue
		}

		if _, err := net.ParseMAC(macaddr); err != nil {
			slog.Warnf("Invalid mac address: %s", macaddr)
			continue
		}

		if client.Expires == nil {
			action = "importing"
			when = "static"
		} else if client.Expires.Before(now) {
			action = "ignoring"
			when = "expired"
			expired = true
		} else {
			action = "importing"
			when = "expires " + client.Expires.Format(time.Stamp)
		}
		slog.Debugf("%s %v -> %v (%s)", action, macaddr, client.IPv4,
			when)

		if !expired {
			setMacIP(macaddr, client.IPv4)
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
		Ipv4Address: proto.Uint32(network.IPAddrToUint32(addr)),
		MacAddress:  proto.Uint64(network.HWAddrToUint64(hwaddr)),
	}

	err = brokerd.Publish(entity, base_def.TOPIC_ENTITY)
	return err == nil
}

func netEventHandler(event []byte) {
	slog.Debugf("got network update event - reevaluting interfaces")
	getGateways()
}

func entityEventHandler(event []byte) {
	entity := &base_msg.EventNetEntity{}
	if err := proto.Unmarshal(event, entity); err != nil {
		slog.Warnf("Unmarshaling NET.ENTITY event: %v", err)
		return
	}

	if entity.MacAddress == nil || entity.Disconnect == nil {
		slog.Warnf("Received incomplete NET.ENTITY event: %v",
			entity)
		return
	}

	if *entity.Disconnect {
		mac := network.Uint64ToMac(*entity.MacAddress)
		mapMtx.Lock()
		ip, ok := macToIP[mac]
		mapMtx.Unlock()

		if ok {
			slog.Infof("Cancelling scans for disconnected client %s", mac)
			cancelAllScans(mac, ip)
		}
	}
}

func signalHandler() {
	sig := make(chan os.Signal, 2)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	s := <-sig
	slog.Infof("Signal (%v) received.", s)
}

func bgmetricsInit() {
	bgm = bgmetrics.NewMetrics(pname, config)
	metrics.lanDrops = bgm.NewCounter("landrops")
	metrics.wanDrops = bgm.NewCounter("wandrops")
	metrics.sampledPkts = bgm.NewCounter("sampled_pkts")
	metrics.missedPkts = bgm.NewCounter("missed_pkts")
	metrics.tcpScans = bgm.NewCounter("tcp_scans")
	metrics.tcpScanTime = bgm.NewSummary("tcp_scan_time")
	metrics.udpScans = bgm.NewCounter("udp_scans")
	metrics.udpScanTime = bgm.NewSummary("udp_scan_time")
	metrics.subnetScans = bgm.NewCounter("subnet_scans")
	metrics.subnetScanTime = bgm.NewSummary("subnet_scan_time")
	metrics.vulnScans = bgm.NewCounter("vuln_scans")
	metrics.vulnScanTime = bgm.NewSummary("vuln_scan_time")
	metrics.blockedIPs = bgm.NewCounter("blocked_ips")
	metrics.knownHosts = bgm.NewGauge("known_hosts")
}

func main() {
	// To avoid dropping packets, we need to have extra processes available.
	runtime.GOMAXPROCS(8)

	slog = aputil.NewLogger(pname)
	defer slog.Sync()

	plat = platform.NewPlatform()

	*watchDir = plat.ExpandDirPath("__APDATA__", *watchDir)
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

	if strings.EqualFold(os.Getenv("BG_FAILSAFE"), "true") {
		slog.Infof("Starting in failsafe mode - going idle")
		err = mcpd.SetState(mcp.FAILSAFE)
		signalHandler()
		os.Exit(0)
	}

	brokerd, err = broker.NewBroker(slog, pname)
	if err != nil {
		slog.Fatal(err)
	}
	defer brokerd.Fini()

	config, err = apcfg.NewConfigd(brokerd, pname, cfgapi.AccessInternal)
	if err != nil {
		slog.Fatalf("cannot connect to configd: %v", err)
	}

	go http.ListenAndServe(base_def.WATCHD_DIAG_PORT, nil)
	go apcfg.HealthMonitor(config, mcpd)
	aputil.ReportInit(slog, pname)
	bgmetricsInit()

	config.HandleDelete(`^@/clients/.*`, configClientDelete)
	config.HandleExpire(`^@/clients/.*/ipv4$`, configClientDelete)
	config.HandleChange(`^@/clients/.*/ipv4$`, configIPv4Changed)
	brokerd.Handle(base_def.TOPIC_UPDATE, netEventHandler)
	brokerd.Handle(base_def.TOPIC_ENTITY, entityEventHandler)

	rings = config.GetRings()
	getGateways()
	getLeases()

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

	os.Exit(0)
}
