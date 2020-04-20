/*
 * COPYRIGHT 2020 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

// Observe various events and record them to observations log files for
// later cloud upload.
package main

import (
	"fmt"
	"net"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"bg/ap_common/apcfg"
	"bg/ap_common/aputil"
	"bg/ap_common/broker"
	"bg/ap_common/mcp"
	"bg/ap_common/platform"
	"bg/base_def"
	"bg/base_msg"
	"bg/common/cfgapi"
	"bg/common/network"
	"bg/common/vpn"

	"github.com/golang/protobuf/proto"
	"go.uber.org/zap"
)

var (
	logDir   = apcfg.String("logdir", "/var/spool/identifierd", false, nil)
	trackVPN = apcfg.Bool("vpn", false, true, nil)
	_        = apcfg.String("log_level", "info", true, aputil.LogSetLevel)

	brokerd *broker.Broker
	config  *cfgapi.Handle
	slog    *zap.SugaredLogger

	// DNS requests only contain the IP addr, so we maintin a map ipaddr -> hwaddr
	ipMtx      sync.Mutex
	ipMap      = make(map[uint32]uint64)
	vpnClients = make(map[uint64]bool)

	newData = newEntities()
)

const (
	pname = "ap.identifierd"

	observeFile = "observations.pb"

	keepFor            = 2 * 24 * time.Hour
	logInterval        = 15 * time.Minute
	collectionDuration = 30 * time.Minute
	// Just less than every 6 days, so that we stride through different
	// days of the week and different times of day.
	resetDuration = time.Hour * (6*24 - 3)
)

func delHWaddr(hwaddr uint64) {
	ipMtx.Lock()
	defer ipMtx.Unlock()
	for ip, hw := range ipMap {
		if hw == hwaddr {
			delete(ipMap, ip)
			break
		}
	}
	delete(vpnClients, hwaddr)
}

func getHWaddr(ip uint32) (uint64, bool) {
	ipMtx.Lock()
	defer ipMtx.Unlock()
	hwaddr, ok := ipMap[ip]
	return hwaddr, ok
}

func addIP(ip uint32, hwaddr uint64, vpn bool) {
	ipMtx.Lock()
	defer ipMtx.Unlock()
	ipMap[ip] = hwaddr

	if vpn {
		vpnClients[hwaddr] = true
	}
}

func handleEntity(event []byte) {
	msg := &base_msg.EventNetEntity{}
	err := proto.Unmarshal(event, msg)
	if err != nil {
		slog.Warnw("failed to unmarshal to entity", "event", event, "error", err)
		return
	}
	if msg.MacAddress == nil {
		return
	}
	// Strip stuff we don't care about passing along
	msg.Sender = nil
	msg.Debug = nil

	newData.addMsgEntity(*msg.MacAddress, msg)
}

func handleRequest(event []byte) {
	request := &base_msg.EventNetRequest{}
	err := proto.Unmarshal(event, request)
	if err != nil {
		slog.Warnw("failed to unmarshal to NetRequest", "event", event, "error", err)
		return
	}
	slog.Debugw("handleRequest", "request", request)

	if *request.Protocol != base_msg.Protocol_DNS {
		return
	}

	// See record_client() in dns4d
	ip := net.ParseIP(*request.Requestor)
	if ip == nil {
		slog.Warnf("empty Requestor: %v", request)
		return
	}

	if ip.Equal(network.IPLocalhost) {
		return
	}

	hwaddr, ok := getHWaddr(network.IPAddrToUint32(ip))
	if !ok {
		slog.Warnf("unknown entity: %v", ip)
		return
	}

	// Strip stuff we don't care about passing along
	request.Sender = nil
	request.Debug = nil

	newData.addMsgRequest(hwaddr, request)
}

func isVPN(mac uint64) bool {
	ipMtx.Lock()
	defer ipMtx.Unlock()

	return vpnClients[mac]
}

func vpnUpdate(hwaddr net.HardwareAddr, ip net.IP) {
	mac := network.HWAddrToUint64(hwaddr)
	if ip == nil {
		delHWaddr(mac)
	} else {
		ipaddr := network.IPAddrToUint32(ip)
		addIP(ipaddr, mac, true)
	}
}

func configDHCPChanged(path []string, val string, expires *time.Time) {
	slog.Debugf("configDHCPChanged: %s %s %v", path[1], val, expires)
	mac, err := net.ParseMAC(path[1])
	if err != nil {
		slog.Warnf("invalid MAC address %s", path[1])
		return
	}

	hwaddr := network.HWAddrToUint64(mac)
	newData.addDHCPName(hwaddr, val)
}

func configIPv4Changed(path []string, val string, expires *time.Time) {
	slog.Debugf("configIPv4Changed: %s %s %v", path[1], val, expires)
	mac, err := net.ParseMAC(path[1])
	if err != nil {
		slog.Warnf("invalid MAC address %s", path[1])
		return
	}

	ipv4 := net.ParseIP(val)
	if ipv4 == nil {
		slog.Warnf("invalid IPv4 address %s", val)
		return
	}
	ipaddr := network.IPAddrToUint32(ipv4)
	addIP(ipaddr, network.HWAddrToUint64(mac), false)
}

func configIPv4Delexp(path []string) {
	slog.Debugf("configIPv4Delexp: %s", path[1])
	mac, err := net.ParseMAC(path[1])
	if err != nil {
		slog.Warnf("invalid MAC address %s", path[1])
		return
	}

	delHWaddr(network.HWAddrToUint64(mac))
}

func configPrivacyChanged(path []string, val string, expires *time.Time) {
	slog.Debugf("configPrivacyChanged: %s %s %v", path[1], val, expires)
	mac, err := net.ParseMAC(path[1])
	if err != nil {
		slog.Warnf("invalid MAC address %s: %s", path[1], err)
		return
	}

	private, err := strconv.ParseBool(val)
	if err != nil {
		slog.Warnf("invalid bool value %s: %s", val, err)
		return
	}

	newData.setPrivacy(mac, private)
}

func configPrivacyDelete(path []string) {
	slog.Debugf("configPrivacyDelete: %s", path[1])
	mac, err := net.ParseMAC(path[1])
	if err != nil {
		slog.Warnf("invalid MAC address %s", path[1])
		return
	}

	newData.setPrivacy(mac, false)
}

func handleScan(event []byte) {
	scan := &base_msg.EventNetScan{}
	err := proto.Unmarshal(event, scan)
	if err != nil {
		slog.Warnw("failed to unmarshal to NetScan", "event", event, "error", err)
		return
	}
	slog.Debugw("handleScan", "scan", scan)

	hwaddr, ok := getHWaddr(*scan.Ipv4Address)
	if !ok {
		return
	}

	// Strip stuff we don't care about passing along
	scan.Sender = nil
	scan.Debug = nil

	newData.addMsgScan(hwaddr, scan)
}

func handleListen(event []byte) {
	listen := &base_msg.EventListen{}
	err := proto.Unmarshal(event, listen)
	if err != nil {
		slog.Warnw("failed to unmarshal to Listen", "event", event, "error", err)
		return
	}
	slog.Debugw("handleListen", "listen", listen)

	hwaddr, ok := getHWaddr(*listen.Ipv4Address)
	if !ok {
		return
	}

	// Strip stuff we don't care about passing along
	listen.Sender = nil
	listen.Debug = nil

	newData.addMsgListen(hwaddr, listen)
}

func handleOptions(event []byte) {
	options := &base_msg.DHCPOptions{}
	err := proto.Unmarshal(event, options)
	if err != nil {
		slog.Warnw("failed to unmarshal to DHCPOptions", "event", event, "error", err)
		return
	}

	slog.Debugw("handleOptions", "options", options)

	// Strip stuff we don't care about passing along
	options.Sender = nil
	options.Debug = nil

	newData.addMsgOptions(*options.MacAddress, options)
}

func save() {
	n, err := newData.writeInventory(filepath.Join(*logDir, observeFile))
	if err != nil {
		slog.Warnf("could not save observation data:", err)
		return
	}
	if n == 0 {
		return
	}

	inv := base_msg.EventDeviceInventory{
		Timestamp: aputil.NowToProtobuf(),
		Sender:    proto.String(brokerd.Name),
		Debug:     proto.String("-"),
	}
	if err := brokerd.Publish(&inv, base_def.TOPIC_DEVICE_INVENTORY); err != nil {
		slog.Warnf("could not publish to %s: %v", base_def.TOPIC_DEVICE_INVENTORY, err)
	}
}

// clean removes old observation files from logDir
func clean() {
	walkFunc := func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return fmt.Errorf("error walking to %s: %v", path, err)
		}

		if info.IsDir() || !strings.HasPrefix(info.Name(), observeFile) {
			return nil
		}

		old := time.Now().Add(-keepFor)
		if info.ModTime().After(old) {
			return nil
		}

		slog.Debugf("removing %s", path)
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("error removing %s: %v", path, err)
		}

		return nil
	}

	if err := filepath.Walk(*logDir, walkFunc); err != nil {
		slog.Warnf("error walking %s: %v", *logDir, err)
	}
}

// logger periodically saves to disk both data for inference by the trained
// device ID model, and data observed from clients to be sent to the cloud.
// The observed data is kept for keepFor hours until it is removed by clean().
func logger(stop chan bool, wg *sync.WaitGroup) {
	defer wg.Done()
	tick := time.NewTicker(logInterval)
	defer tick.Stop()

	for {
		select {
		case <-tick.C:
			save()
			clean()
		case <-stop:
			save()
			return
		}
	}
}

func recoverClients() {
	clients := config.GetClients()

	for macaddr, client := range clients {
		hwaddr, err := net.ParseMAC(macaddr)
		if err != nil {
			slog.Warnf("Invalid mac address in @/clients: %s", macaddr)
			continue
		}
		hw := network.HWAddrToUint64(hwaddr)

		if client.IPv4 != nil {
			addIP(network.IPAddrToUint32(client.IPv4), hw, false)
		}

		if client.DHCPName != "" {
			newData.addDHCPName(hw, client.DHCPName)
		}

		newData.setPrivacy(hwaddr, client.DNSPrivate)
	}

	vpnClients = make(map[uint64]bool)
	vpn.Init(config)
	keys, _ := vpn.GetKeys("")
	for mac, key := range keys {
		if ip := net.ParseIP(key.WGAssignedIP); ip != nil {
			hwaddr := network.MacToUint64(mac)
			addIP(network.IPAddrToUint32(ip), hwaddr, true)
		}
	}
	vpn.RegisterMacIPHandler(vpnUpdate)
}

func signalHandler() {
	sig := make(chan os.Signal, 2)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	s := <-sig
	slog.Infof("Signal (%v) received.", s)
}

func main() {
	var err error

	slog = aputil.NewLogger(pname)
	defer func() { _ = slog.Sync() }()

	slog.Infof("starting")

	mcpd, err := mcp.New(pname)
	if err != nil {
		slog.Fatalf("failed to connect to mcp")
	}

	if strings.EqualFold(os.Getenv("BG_FAILSAFE"), "true") {
		slog.Infof("Starting in failsafe mode - going idle")
		_ = mcpd.SetState(mcp.FAILSAFE)
		signalHandler()
		os.Exit(0)
	}

	// Use the broker to listen for appropriate messages to create and update
	// our observations. To respect a client's privacy we won't register any
	// handlers until we have recovered each client's privacy configuration.
	brokerd, err = broker.NewBroker(slog, pname)
	if err != nil {
		slog.Fatal(err)
	}
	defer brokerd.Fini()

	config, err = apcfg.NewConfigd(brokerd, pname, cfgapi.AccessInternal)
	if err != nil {
		slog.Fatalf("cannot connect to configd: %v", err)
	}
	go apcfg.HealthMonitor(config, mcpd)
	aputil.ReportInit(slog, pname)

	plat := platform.NewPlatform()

	*logDir = plat.ExpandDirPath(platform.APData, "identifierd")

	recoverClients()

	brokerd.Handle(base_def.TOPIC_ENTITY, handleEntity)
	brokerd.Handle(base_def.TOPIC_REQUEST, handleRequest)
	brokerd.Handle(base_def.TOPIC_SCAN, handleScan)
	brokerd.Handle(base_def.TOPIC_LISTEN, handleListen)
	brokerd.Handle(base_def.TOPIC_OPTIONS, handleOptions)

	_ = config.HandleChange(`^@/clients/.*/ipv4$`, configIPv4Changed)
	_ = config.HandleChange(`^@/clients/.*/dhcp_name$`, configDHCPChanged)
	_ = config.HandleDelete(`^@/clients/.*/ipv4$`, configIPv4Delexp)
	_ = config.HandleExpire(`^@/clients/.*/ipv4$`, configIPv4Delexp)
	_ = config.HandleChange(`^@/clients/.*/dns_private$`, configPrivacyChanged)
	_ = config.HandleDelete(`^@/clients/.*/dns_private$`, configPrivacyDelete)

	if err = os.MkdirAll(*logDir, 0755); err != nil {
		slog.Fatalf("failed to mkdir:", err)
	}

	if err = mcpd.SetState(mcp.ONLINE); err != nil {
		slog.Warnf("failed to set status")
	}

	stop := make(chan bool)
	wg := &sync.WaitGroup{}
	wg.Add(1)
	go logger(stop, wg)

	signalHandler()

	// Tell the logger to stop, and wait for it to flush its output
	stop <- true
	wg.Wait()
}
