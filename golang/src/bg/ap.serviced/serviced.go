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
	"strings"
	"sync"
	"syscall"
	"time"

	"bg/ap_common/apcfg"
	"bg/ap_common/aputil"
	"bg/ap_common/broker"
	"bg/ap_common/mcp"
	"bg/base_def"
	"bg/common/cfgapi"
	"bg/common/network"

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

	ifaceToRing map[int]string
	ringToIface map[string]*net.Interface
	ipv4ToIface map[string]*net.Interface

	exitChan = make(chan struct{})
)

func clientUpdateEvent(path []string, val string, expires *time.Time) {
	var ipv4 net.IP

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

	dnsChanged := false
	switch path[2] {
	case "ipv4":
		if ipv4 = net.ParseIP(val); ipv4 == nil {
			slog.Warnf("Invalid IP address %s for %s", val, mac)
			return
		}
		dnsChanged = !ipv4.Equal(client.IPv4)
	case "dns_name":
		dnsChanged = (val != client.DNSName)
	case "dhcp_name":
		dnsChanged = (val != client.DHCPName)
	case "ring":
		dnsChanged = (val != client.Ring)
	}

	if dnsChanged {
		dnsDeleteClient(client)
	}
	switch path[2] {
	case "ipv4":
		client.IPv4 = ipv4
		client.Expires = expires
		dhcpIPv4Changed(mac, client)
	case "dns_name":
		client.DNSName = val
	case "dhcp_name":
		client.DHCPName = val
	case "ring":
		if client.Ring == "" {
			slog.Infof("added %s to %s", mac, val)
		} else if client.Ring != val {
			slog.Infof("moved %s from %s to %s", mac, client.Ring, val)
		}
		client.Ring = val
	}
	if dnsChanged {
		dnsUpdateClient(client)
	}
}

func clientDeleteEvent(path []string) {
	mac := path[1]
	clientMtx.Lock()
	defer clientMtx.Unlock()

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
			dhcpDeleteEvent(mac)
			dnsDeleteClient(client)
			client.IPv4 = nil
		}
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
		dnsDeleteClient(client)
		client.IPv4 = nil
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

func eventHandler(event []byte) {
	slog.Debugf("got network update event - reevaluting interfaces")
	initInterfaces()
}

func configSiteChanged(path []string, val string, expires *time.Time) {
	slog.Infof("%s changed - restarting to reset DHCP configuration",
		strings.Join(path, "/"))
	close(exitChan)
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
	brokerd.Handle(base_def.TOPIC_UPDATE, eventHandler)

	config, err = apcfg.NewConfigd(brokerd, pname, cfgapi.AccessInternal)
	if err != nil {
		slog.Fatalf("cannot connect to configd: %v", err)
	}
	clients = config.GetClients()
	rings = config.GetRings()

	initInterfaces()
	dnsInit()
	dhcpInit()
	relayInit()

	config.HandleChange(`^@/clients/.*`, clientUpdateEvent)
	config.HandleDelete(`^@/clients/.*`, clientDeleteEvent)
	config.HandleExpire(`^@/clients/.*`, clientExpireEvent)
	config.HandleChange(`^@/nodes/.*$`, configNodesChanged)
	config.HandleChange(`^@/site_index$`, configSiteChanged)
	config.HandleChange(`^@/network/base_address$`, configSiteChanged)

	mcpd.SetState(mcp.ONLINE)

	sig := make(chan os.Signal, 2)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	select {
	case s := <-sig:
		slog.Fatalf("Signal (%v) received, stopping", s)
	case <-exitChan:
		slog.Infof("stopping")
	}
}
