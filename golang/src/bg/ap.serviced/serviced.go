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
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"bg/ap_common/apcfg"
	"bg/ap_common/aputil"
	"bg/ap_common/bgmetrics"
	"bg/ap_common/broker"
	"bg/ap_common/mcp"
	"bg/base_def"
	"bg/common/cfgapi"
	"bg/common/network"
	"bg/common/vpn"

	"go.uber.org/zap"
)

const pname = "ap.serviced"

var (
	brokerd *broker.Broker
	config  *cfgapi.Handle
	slog    *zap.SugaredLogger
	bgm     *bgmetrics.Metrics

	clientMtx  sync.Mutex
	clients    cfgapi.ClientMap
	vpnClients map[string]net.IP

	_ = apcfg.String("log_level", "info", true, aputil.LogSetLevel)

	ifaceToRing map[int]string
	ringToIface map[string]*net.Interface
	ipv4ToIface map[string]*net.Interface

	exitChan = make(chan struct{})
)

func pathStr(path []string) string {
	return strings.Join(path, "/")
}

func vpnUpdate(hwaddr net.HardwareAddr, ip net.IP) {
	clientMtx.Lock()
	defer clientMtx.Unlock()

	mac := hwaddr.String()
	if ip == nil {
		delete(vpnClients, mac)
	} else {
		vpnClients[mac] = ip
	}
}

func clientUpdateEvent(path []string, val string, expires *time.Time) {
	clientMtx.Lock()
	defer clientMtx.Unlock()

	mac := path[1]
	client := clients[mac]
	if client == nil {
		if client = config.GetClient(mac); client == nil {
			slog.Warnf("Got update for nonexistent client: %s", mac)
			return
		}
		clients[mac] = client
	}

	update := false
	if path[2] == "ipv4" {
		ipv4 := net.ParseIP(val)
		if ipv4 == nil {
			slog.Warnf("Invalid addr %s for %s", val, mac)
			client.IPv4 = nil
			client.Expires = nil
		} else if !ipv4.Equal(client.IPv4) || expires != client.Expires {
			client.IPv4 = ipv4
			client.Expires = expires
		}
		dhcpIPv4Changed(mac, client)
		update = true

	} else if path[2] == "dns_name" && client.DNSName != val {
		client.DNSName = val
		update = true

	} else if path[2] == "friendly_name" && client.FriendlyName != val {
		client.FriendlyName = val
		go updateFriendlyNames()

	} else if path[2] == "friendly_dns" && client.FriendlyDNS != val {
		client.FriendlyDNS = val
		go updateFriendlyNames()
		update = true

	} else if path[2] == "ring" && client.Ring != val {
		if client.Ring == "" {
			slog.Infof("added %s to %s", mac, val)
		} else {
			slog.Infof("moved %s from %s to %s", mac, client.Ring, val)
		}
		client.Ring = val
		update = true
	}
	if update {
		dnsUpdateClient(mac, client)
	}
}

func clientDeleteEvent(path []string) {
	var update bool

	if len(path) < 2 {
		slog.Warnf("clientDeleteEvent: bad path: @/%s", pathStr(path))
		return
	}

	// e.g. delete @/clients/<mac>/classification/oui_mfg; we don't care
	if len(path) > 3 {
		return
	}

	clientMtx.Lock()
	defer clientMtx.Unlock()

	mac := path[1]
	client := clients[mac]
	if client == nil {
		return
	}

	if len(path) == 2 {
		dhcpDeleteEvent(mac)
		update = true
		client.IPv4 = nil
		delete(clients, mac)

	} else if path[2] == "dns_name" && client.DNSName != "" {
		client.DNSName = ""
		update = true

	} else if path[2] == "friendly_name" && client.FriendlyName != "" {
		client.FriendlyName = ""
		go updateFriendlyNames()

	} else if path[2] == "friendly_dns" && client.FriendlyDNS != "" {
		client.FriendlyDNS = ""
		go updateFriendlyNames()
		update = true

	} else if path[2] == "ipv4" && client.IPv4 != nil {
		dhcpDeleteEvent(mac)
		client.IPv4 = nil
	}

	if update {
		dnsUpdateClient(mac, client)
	}
}

func clientExpireEvent(path []string) {
	if len(path) != 3 || path[2] != "ipv4" {
		// For anything other than a DHCP lease, an 'expiration' is
		// identical to a deletion.
		clientDeleteEvent(path)
	} else {
		clientMtx.Lock()
		defer clientMtx.Unlock()

		mac := path[1]
		client := clients[mac]
		dhcpIPv4Expired(mac)
		if client != nil {
			client.IPv4 = nil
			dnsUpdateClient(mac, client)
		}
	}
}

func initInterfaces() {
	i2r := make(map[int]string)
	r2i := make(map[string]*net.Interface)
	i2i := make(map[string]*net.Interface)

	slog.Debugf("Initializing interfaces")
	slog.Debugf("%10s  %7s  %3s (%s)", "ring", "bridge", "idx", "name")

	//
	// Iterate over all of the rings to/which we will relay UDP broadcasts.
	// Find the interface that serves that ring and the IP address of the
	// router for that subnet.
	//
	for ring, conf := range rings {
		if conf.Bridge == "" {
			continue
		}
		iface, err := net.InterfaceByName(conf.Bridge)
		if iface == nil || err != nil {
			slog.Warnf("No interface %s: %v", conf.Bridge, err)
			continue
		}

		i2i[network.SubnetRouter(conf.Subnet)] = iface
		r2i[ring] = iface
		i2r[iface.Index] = ring
		slog.Debugf("%10s  %7s  %3d (%s)", ring, conf.Bridge,
			iface.Index, iface.Name)
	}

	ifaceToRing = i2r
	ringToIface = r2i
	ipv4ToIface = i2i
}

func getVPNClients() {
	vpnClients = make(map[string]net.IP)

	hdl, err := vpn.NewVpn(config)
	if err != nil {
		slog.Warnf("vpn.NewVpn() failed: %v", err)
	} else {
		keys, _ := hdl.GetKeys("")
		for _, key := range keys {
			if ip := net.ParseIP(key.WGAssignedIP); ip != nil {
				vpnClients[key.GetMac()] = ip
			}
		}
		hdl.RegisterMacIPHandler(vpnUpdate)
	}
}

func eventHandler(event []byte) {
	slog.Debugf("got network update event - reevaluting interfaces")
	initInterfaces()
	relayRestart()
}

func configSiteChanged(path []string, val string, expires *time.Time) {
	slog.Infof("%s changed - restarting to reset DHCP configuration",
		pathStr(path))
	close(exitChan)
}

func configNodesChanged(path []string, val string, expires *time.Time) {
	initInterfaces()
}

func main() {
	slog = aputil.NewLogger(pname)
	defer slog.Sync()
	slog.Infof("starting")

	aputil.ReportInit(slog, pname)

	mcpd, err := mcp.New(pname)
	if err != nil {
		slog.Warnf("cannot connect to mcp: %v", err)
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
	bgm = bgmetrics.NewMetrics(pname, config)

	go http.ListenAndServe(base_def.SERVICED_DIAG_PORT, nil)
	go apcfg.HealthMonitor(config, mcpd)
	aputil.ReportInit(slog, pname)

	clients = config.GetClients()
	getVPNClients()
	rings = config.GetRings()

	brokerd.Handle(base_def.TOPIC_UPDATE, eventHandler)
	initInterfaces()
	dnsInit()
	mcpState := mcp.ONLINE
	if aputil.IsGatewayMode() {
		dhcpInit()
		if strings.EqualFold(os.Getenv("BG_FAILSAFE"), "true") {
			slog.Infof("failsafe mode - disabling relay")
			mcpState = mcp.FAILSAFE
		} else {
			relayInit()
		}
	}

	config.HandleChange(`^@/clients/.*`, clientUpdateEvent)
	config.HandleDelExp(`^@/clients/.*`, clientDeleteEvent)
	config.HandleChange(`^@/nodes/.*$`, configNodesChanged)
	config.HandleChange(`^@/site_index$`, configSiteChanged)
	config.HandleChange(`^@/network/base_address$`, configSiteChanged)

	mcpd.SetState(mcpState)

	sig := make(chan os.Signal, 2)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	select {
	case s := <-sig:
		slog.Infof("Signal (%v) received, stopping", s)
	case <-exitChan:
		slog.Infof("stopping")
	}

	os.Exit(0)
}
