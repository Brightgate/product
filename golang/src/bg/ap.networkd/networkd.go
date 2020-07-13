/*
 * COPYRIGHT 2020 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package main

import (
	"fmt"
	"math/rand"
	"net"
	"net/http"
	_ "net/http/pprof"
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
	"bg/ap_common/platform"
	"bg/ap_common/wificaps"
	"bg/base_def"
	"bg/common/cfgapi"
	"bg/common/network"

	"go.uber.org/zap"
)

var (
	templateDir = apcfg.String("template_dir", "/etc/templates/ap.networkd",
		true, nil)
	rulesDir = apcfg.String("rules_dir", "/etc/filter.rules.d",
		true, nil)
	_ = apcfg.String("log_level", "info", true,
		aputil.LogSetLevel)

	wiredNics map[string]*physDevice

	mcpd    *mcp.MCP
	brokerd *broker.Broker
	config  *cfgapi.Handle

	clients    cfgapi.ClientMap // macaddr -> ClientInfo
	clientsMtx sync.Mutex
	rings      cfgapi.RingMap // ring -> config

	nodeID string
	slog   *zap.SugaredLogger

	plat           *platform.Platform
	satellite      bool
	networkNodeIdx byte

	cleanup struct {
		chans []chan bool
		wg    sync.WaitGroup
	}
)

const (
	pname = "ap.networkd"
)

type physDevice struct {
	name     string // Linux device name
	hwaddr   string // mac address
	ring     string // configured ring
	disabled bool
	wireless bool

	cap *wificaps.WifiCapabilities
}

func list(slice []string) string {
	return strings.Join(slice, ",")
}

func slice(list string) []string {
	return strings.Split(list, ",")
}

func sanityCheckSubnets() error {
	for name, ring := range rings {
		if ring.IPNet.Contains(wan.addr) {
			return fmt.Errorf("collision between our IP (%v) and "+
				"%s subnet: %v", wan.addr, name, ring.Subnet)
		}
	}
	return nil
}

// Find the network device being used for internal traffic, and return the IP
// address assigned to it.
func getInternalAddr() net.IP {
	for _, dev := range wiredNics {
		if dev.ring != base_def.RING_INTERNAL {
			continue
		}

		iface, err := net.InterfaceByName(dev.name)
		if err != nil {
			slog.Warnf("Failed to get interface for %s: %v",
				dev.name, err)
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			slog.Warnf("Failed to get address for %s: %v",
				iface.Name, err)
			continue
		}

		for _, addr := range addrs {
			var ipv4 net.IP

			switch v := addr.(type) {
			case *net.IPNet:
				ipv4 = v.IP.To4()

			case *net.IPAddr:
				ipv4 = v.IP.To4()
			}

			if ipv4 != nil {
				return ipv4
			}
		}
	}

	return nil
}

func getGatewayIP() string {
	internal := rings[base_def.RING_INTERNAL]
	gateway := network.SubnetRouter(internal.Subnet)
	return gateway
}

func getNodeID() (string, error) {
	nodeID, err := plat.GetNodeID()
	if err != nil {
		return "", err
	}

	prop := "@/nodes/" + nodeID + "/platform"
	oldName, _ := config.GetProp(prop)
	newName := plat.GetPlatform()

	if oldName != newName {
		if oldName == "" {
			slog.Debugf("Setting %s to %s", prop, newName)
		} else {
			slog.Debugf("Changing %s from %s to %s", prop,
				oldName, newName)
		}
		if err := config.CreateProp(prop, newName, nil); err != nil {
			slog.Warnf("failed to update %s: %v", prop, err)
		}
	}
	return nodeID, nil
}

// Connect to all of the other brightgate daemons and construct our initial model
// of the system
func daemonInit() error {
	var err error

	if mcpd, err = mcp.New(pname); err != nil {
		slog.Warnf("cannot connect to mcp: %v", err)
	} else {
		mcpd.SetState(mcp.INITING)
	}
	satellite = aputil.IsSatelliteMode()

	brokerd, err = broker.NewBroker(slog, pname)
	if err != nil {
		slog.Fatal(err)
	}

	config, err = apcfg.NewConfigd(brokerd, pname, cfgapi.AccessInternal)
	if err != nil {
		return fmt.Errorf("cannot connect to configd: %v", err)
	}
	go apcfg.HealthMonitor(config, mcpd)
	aputil.ReportInit(slog, pname)

	if nodeID, err = getNodeID(); err != nil {
		return err
	}

	*templateDir = plat.ExpandDirPath("__APPACKAGE__", *templateDir)
	*rulesDir = plat.ExpandDirPath("__APPACKAGE__", *rulesDir)

	clients = make(cfgapi.ClientMap)
	rings = make(cfgapi.RingMap)

	config.HandleChange(`^@/site_index`, configSiteIndexChanged)
	config.HandleChange(`^@/clients/.*$`, configClientChanged)
	config.HandleDelete(`^@/clients/.*$`, configClientDeleted)
	config.HandleChange(`^@/nodes/`+nodeID+`/nics/.*$`, configNicChanged)
	config.HandleDelete(`^@/nodes/`+nodeID+`/nics/.*$`, configNicDeleted)
	config.HandleChange(`^@/rings/.*`, configRingChanged)
	config.HandleDelete(`^@/rings/.*/subnet$`, configRingSubnetDeleted)
	config.HandleChange(`^@/network/.*`, configNetworkUpdated)
	config.HandleDelExp(`^@/network/.*`, configNetworkDeleted)
	config.HandleChange(`^@/firewall/rules/`, configRuleChanged)
	config.HandleDelExp(`^@/firewall/rules/`, configRuleDeleted)
	config.HandleChange(`^@/firewall/blocked/`, configBlocklistChanged)
	config.HandleDelExp(`^@/firewall/blocked/`, configBlocklistExpired)
	config.HandleChange(`^@/users/.*/vpn/.*`, configUserChanged)
	config.HandleDelExp(`^@/users/.*`, configUserDeleted)
	config.HandleChange(`^@/policy/site/vpn/client/.*/enabled`, vpnClientUpdateEnabled)
	config.HandleDelExp(`^@/policy/site/vpn/client/.*/enabled`, vpnClientDeleteEnabled)
	config.HandleChange(`^@/policy/site/vpn/server/.*/enabled`, vpnServerUpdateEnabled)
	config.HandleDelExp(`^@/policy/site/vpn/server/.*/enabled`, vpnServerDeleteEnabled)
	config.HandleChange(`^@/policy/.*/vpn/client/.*/allowed`, vpnClientUpdateAllowed)
	config.HandleDelExp(`^@/policy/.*/vpn/client/.*/allowed`, vpnClientDeleteAllowed)
	config.HandleChange(`^@/policy/.*/vpn/server/.*/rings`, vpnUpdateRings)
	config.HandleDelExp(`^@/policy/.*/vpn/server/.*/rings`, vpnDeleteRings)
	config.HandleChange(`^@/policy/.*/vpn/server/.*/subnets`, vpnUpdateRings)
	config.HandleDelExp(`^@/policy/.*/vpn/server/.*/subnets`, vpnDeleteRings)
	config.HandleChange(`^@/policy/site/network/forward/.*/tgt$`, forwardUpdated)
	config.HandleDelExp(`^@/policy/site/network/forward/.*/tgt$`, forwardDeleted)

	rings = config.GetRings()
	clients = config.GetClients()

	discoverDevices()
	wanInit(config.GetWanInfo())

	// All wired devices that haven't yet been assigned to a ring will be
	// put into "standard" by default
	for id, dev := range wiredNics {
		if dev.ring == "" {
			slog.Infof("defaulting %s into standard", id)
			dev.ring = base_def.RING_STANDARD
			configUpdateRing(dev)
		}
	}

	if err = loadFilterRules(); err != nil {
		return fmt.Errorf("unable to load filter rules: %v", err)
	}

	// We use the lowest byte of our internal IP address as a transient,
	// local node index.  For the gateway node, that will always be 1.  For
	// the satellite nodes, we need pull it from the address the gateway's
	// DHCP server gave us.
	networkNodeIdx = 1
	if satellite {
		ip := getInternalAddr()
		if ip == nil {
			return fmt.Errorf("satellite node has no gateway " +
				"connection")
		}
		networkNodeIdx = ip[3]
	}

	if !satellite {
		// The VPN server only runs on the gateway node
		if err := vpnServerInit(); err != nil {
			slog.Errorf("vpnServerInit failed: %v", err)
		} else {
			go vpnServerLoop(&cleanup.wg, addDoneChan())
		}
		if err := vpnClientInit(); err != nil {
			slog.Errorf("vpnClientInit failed: %v", err)
		} else {
			go vpnClientLoop(&cleanup.wg, addDoneChan())
		}
	}

	ntpdSetup()

	return nil
}

func signalHandler(wg *sync.WaitGroup, doneChan chan bool) {
	defer wg.Done()

	sig := make(chan os.Signal)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	select {
	case s := <-sig:
		networkdStop(fmt.Sprintf("Received signal %v", s))
	case <-doneChan:
	}
}

func addDoneChan() chan bool {
	dc := make(chan bool, 1)

	if cleanup.chans == nil {
		cleanup.chans = make([]chan bool, 0)
	}
	cleanup.chans = append(cleanup.chans, dc)
	cleanup.wg.Add(1)

	return dc
}

func networkdStop(msg string) {
	if msg != "" {
		slog.Infof("%s", msg)
	}

	for _, c := range cleanup.chans {
		c <- true
	}
}

func main() {
	rand.Seed(time.Now().Unix())

	slog = aputil.NewLogger(pname)
	defer slog.Sync()
	slog.Infof("starting")

	plat = platform.NewPlatform()
	networkCleanup()
	if err := daemonInit(); err != nil {
		if mcpd != nil {
			mcpd.SetState(mcp.BROKEN)
		}
		slog.Fatalf("networkd failed to start: %v", err)
	}

	applyFilters()

	mcpd.SetState(mcp.ONLINE)
	go signalHandler(&cleanup.wg, addDoneChan())

	resetInterfaces()

	if !aputil.IsCloudAppMode() {
		if !satellite {
			// We are currently a gateway.  Monitor the DHCP info on
			// the wan port to see if that changes
			go wan.monitorLoop(&cleanup.wg, addDoneChan())
		}
	}

	// for pprof
	go http.ListenAndServe(base_def.NETWORKD_DIAG_PORT, nil)

	cleanup.wg.Wait()
	slog.Infof("Cleaning up")

	networkCleanup()
	os.Exit(0)
}
