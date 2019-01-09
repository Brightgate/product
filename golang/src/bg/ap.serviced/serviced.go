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
	"bg/common/cfgapi"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
)

const pname = "ap.serviced"

var (
	brokerd *broker.Broker
	config  *cfgapi.Handle
	slog    *zap.SugaredLogger

	clientMtx sync.Mutex
	clients   cfgapi.ClientMap

	_ = apcfg.String("log_level", "info", true, aputil.LogSetLevel)

	ifaceToRing    map[int]string
	ringToIface    map[string]*net.Interface
	ipv4ToIface    map[string]*net.Interface
	ifaceBroadcast map[string]net.IP
)

func clientUpdateEvent(path []string, val string, expires *time.Time) {

	if len(path) < 3 || (path[2] != "ipv4" && path[2] != "dns_name" &&
		path[2] != "dhcp_name" && path[2] != "ring") {
		return
	}

	mac := path[1]
	clientMtx.Lock()
	defer clientMtx.Unlock()

	client := clients[mac]
	if client == nil {
		client = config.GetClient(mac)
		clients[mac] = client
	}
	if client == nil {
		slog.Warnf("Got update for nonexistent client: %s", mac)
		return
	}

	dnsDeleteClient(client)
	switch path[2] {
	case "ipv4":
		if ipv4 := net.ParseIP(val); ipv4 != nil {
			client.IPv4 = ipv4
			client.Expires = expires
			dhcpIPv4Changed(mac, client)
		} else {
			slog.Warnf("Invalid IP address %s for %s", val, mac)
		}
	case "dns_name":
		client.DNSName = val
	case "dhcp_name":
		client.DHCPName = val
	case "ring":
		if client.Ring == "" {
			slog.Infof("config reports new client %s is %s",
				mac, val)
		} else if client.Ring != val {
			slog.Infof("config moves client %s from %s to %s",
				mac, client.Ring, val)
		}
		client.Ring = val
	}
	dnsUpdateClient(client)
}

func clientDeleteEvent(path []string) {
	mac := path[1]
	clientMtx.Lock()
	client := clients[mac]
	if len(path) == 2 {
		dhcpDeleteEvent(mac)
		if client != nil {
			dnsDeleteClient(client)
			delete(clients, mac)
		}
	} else if len(path) == 3 {
		if path[2] == "dns_name" {
			dnsDeleteClient(client)
			client.DNSName = ""
		} else if path[2] == "ipv4" {
			dhcpIPv4Expired(mac)
			dnsDeleteClient(client)
			client.IPv4 = nil
		}
	}
	clientMtx.Unlock()
}

func initInterfaces() {
	rings = config.GetRings()

	ifaceToRing = make(map[int]string)
	ringToIface = make(map[string]*net.Interface)
	ipv4ToIface = make(map[string]*net.Interface)
	ifaceBroadcast = make(map[string]net.IP)

	//
	// Iterate over all of the rings to/which we will relay UDP broadcasts.
	// Find the interface that serves that ring and the IP address of the
	// router for that subnet.
	//
	for ring, conf := range rings {
		var name string

		// Find the interface that serves this ring, so we can add the
		// interface to the multicast groups on which we listen.
		if _, ok := ringLevel[ring]; !ok {
			slog.Debugf("No relaying from %s", ring)
			continue
		}

		bridge := vlanBridge(conf.Vlan)
		iface, err := net.InterfaceByName(bridge)
		if iface == nil || err != nil {
			slog.Warnf("No interface %s: %v", bridge, err)
			continue
		}

		ifaceBroadcast[name] = network.SubnetBroadcast(conf.Subnet)
		ipv4ToIface[network.SubnetRouter(conf.Subnet)] = iface
		ringToIface[ring] = iface
		ifaceToRing[iface.Index] = ring
	}
}

func configNodesChanged(path []string, val string, expires *time.Time) {
	initInterfaces()
}

func prometheusInit() {
	http.Handle("/metrics", promhttp.Handler())
	go http.ListenAndServe(base_def.SERVICED_DIAG_PORT, nil)
}

func main() {
	slog = aputil.NewLogger(pname)
	defer slog.Sync()
	slog.Infof("starting")

	mcpd, err := mcp.New(pname)
	if err != nil {
		slog.Warnf("cannot connect to mcp: %v", err)
	}

	prometheusInit()
	brokerd = broker.New(pname)
	defer brokerd.Fini()

	config, err = apcfg.NewConfigd(brokerd, pname, cfgapi.AccessInternal)
	if err != nil {
		slog.Fatalf("cannot connect to configd: %v", err)
	}
	clients = config.GetClients()

	initInterfaces()
	dnsInit()
	dhcpInit()
	relayInit()

	config.HandleChange(`^@/clients/.*`, clientUpdateEvent)
	config.HandleDelete(`^@/clients/.*`, clientDeleteEvent)
	config.HandleExpire(`^@/clients/.*`, clientDeleteEvent)
	config.HandleChange(`^@/nodes/.*$`, configNodesChanged)

	mcpd.SetState(mcp.ONLINE)

	sig := make(chan os.Signal, 2)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	s := <-sig
	slog.Fatalf("Signal (%v) received, stopping", s)
}
