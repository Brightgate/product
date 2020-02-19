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
	"fmt"
	"math/rand"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"bg/ap_common/apcfg"
	"bg/ap_common/aputil"
	"bg/ap_common/broker"
	"bg/ap_common/mcp"
	"bg/ap_common/platform"
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
	hostapdLatency = apcfg.Int("hostapd_latency", 60, true, nil)
	hostapdDebug   = apcfg.Bool("hostapd_debug", false, true,
		hostapdReset)
	hostapdVerbose = apcfg.Bool("hostapd_verbose", false, true,
		hostapdReset)
	deadmanTimeout      = apcfg.Duration("deadman", 5*time.Second, true, nil)
	retransmitSoftLimit = apcfg.Int("retransmit_soft", 3, true, nil)
	retransmitHardLimit = apcfg.Int("retransmit_hard", 6, true, nil)
	retransmitTimeout   = apcfg.Duration("retransmit_timeout",
		5*time.Minute, true, nil)
	apScanFreq   = apcfg.Duration("ap_scan_freq", 3*time.Minute, true, nil)
	apStale      = apcfg.Duration("ap_stale", 10*time.Minute, true, nil)
	chanEvalFreq = apcfg.Duration("chan_eval_freq", 6*time.Hour, true, nil)
	_            = apcfg.String("log_level", "info", true,
		aputil.LogSetLevel)

	physDevices = make(map[string]*physDevice)

	mcpd    *mcp.MCP
	brokerd *broker.Broker
	config  *cfgapi.Handle
	clients cfgapi.ClientMap // macaddr -> ClientInfo
	rings   cfgapi.RingMap   // ring -> config
	nodeID  string
	slog    *zap.SugaredLogger

	plat           *platform.Platform
	hostapd        *hostapdHdl
	satellite      bool
	networkNodeIdx byte

	cleanup struct {
		chans []chan bool
		wg    sync.WaitGroup
	}
)

const (
	// Allow up to 4 failures in a 1 minute period before giving up
	failuresAllowed  = 4
	maxSSIDs         = 4
	period           = time.Duration(time.Minute)
	hotplugBlockFile = "/tmp/bg-skip-hotplug"

	pname = "ap.networkd"
)

type physDevice struct {
	name     string // Linux device name
	hwaddr   string // mac address
	ring     string // configured ring
	pseudo   bool
	disabled bool

	wifi *wifiInfo
}

func hostapdReset(name, val string) error {
	if hostapd != nil {
		hostapd.reset()
	}
	return nil
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

// Create a sentinel file, which prevents the hotplug scripts from being
// executed
func hotplugBlock() {
	slog.Debugf("blocking hotplug scripts")
	f, err := os.Create(hotplugBlockFile)
	if err != nil {
		slog.Warnf("creating %s: %v", hotplugBlockFile, err)
	} else {
		f.Close()
	}
}

// Remove the hotplug-blocking sentinel file.  Create and remove a dummy bridge
// device, which will cause the hotplug scripts to be run once.
func hotplugUnblock() {
	hotplugTrigger := "trigger"

	if aputil.FileExists(hotplugBlockFile) {
		slog.Debugf("unblocking hotplug scripts")
		err := os.Remove(hotplugBlockFile)
		if err != nil {
			slog.Warnf("removing %s: %v", hotplugBlockFile, err)
		}

		slog.Debugf("triggering hotplug refresh")
		err = exec.Command(plat.BrctlCmd, "addbr", hotplugTrigger).Run()
		if err != nil {
			slog.Warnf("addbr %s failed: %v", hotplugTrigger, err)
		} else {
			time.Sleep(100 * time.Millisecond)
		}
		err = exec.Command(plat.BrctlCmd, "delbr", hotplugTrigger).Run()
		if err != nil {
			slog.Warnf("delbr %s failed: %v", hotplugTrigger, err)
		}
	}
}

// Find the network device being used for internal traffic, and return the IP
// address assigned to it.
func getInternalAddr() net.IP {
	for _, dev := range physDevices {
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
	config.HandleChange(`^@/clients/.*/ring$`, configClientChanged)
	config.HandleChange(`^@/nodes/`+nodeID+`/nics/.*$`, configNicChanged)
	config.HandleDelete(`^@/nodes/`+nodeID+`/nics/.*$`, configNicDeleted)
	config.HandleChange(`^@/rings/.*`, configRingChanged)
	config.HandleDelete(`^@/rings/.*/subnet$`, configRingSubnetDeleted)
	config.HandleChange(`^@/network/.*`, configNetworkChanged)
	config.HandleDelete(`^@/network/.*`, configNetworkDeleted)
	config.HandleChange(`^@/firewall/rules/`, configRuleChanged)
	config.HandleDelete(`^@/firewall/rules/`, configRuleDeleted)
	config.HandleChange(`^@/firewall/blocked/`, configBlocklistChanged)
	config.HandleExpire(`^@/firewall/blocked/`, configBlocklistExpired)
	config.HandleDelete(`^@/users/.*`, configUserDeleted)
	config.HandleExpire(`^@/users/.*`, configUserDeleted)

	rings = config.GetRings()
	clients = config.GetClients()

	props, err := config.GetProps("@/network")
	if err != nil {
		return fmt.Errorf("unable to fetch configuration: %v", err)
	}

	if err = globalWifiInit(props); err != nil {
		return err
	}

	getDevices()
	wanInit(config.GetWanInfo())

	// All wired devices that haven't yet been assigned to a ring will be
	// put into "standard" by default
	for _, dev := range physDevices {
		if dev.wifi == nil && dev.ring == "" {
			dev.ring = base_def.RING_STANDARD
		}
	}
	updateNicProperties()

	if err = loadFilterRules(); err != nil {
		return fmt.Errorf("unable to load filter rules: %v", err)
	}

	// We use the lowest byte of our internal IP address as a transient,
	// local node index.  For the gateway node, that will always be 1.  For
	// the satellite nodes, we need pull it from the address the gateway's
	// DHCP server gave us.
	networkNodeIdx = 1
	if satellite = aputil.IsSatelliteMode(); satellite {
		ip := getInternalAddr()
		if ip == nil {
			return fmt.Errorf("satellite node has no gateway " +
				"connection")
		}
		networkNodeIdx = ip[3]
	}

	ntpdSetup()

	return nil
}

// When we get a signal, signal any hostapd process we're monitoring.  We want
// to be sure the wireless interface has been released before we give mcp a
// chance to restart the whole stack.
func signalHandler() {
	var s os.Signal

	sig := make(chan os.Signal, 3)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	done := false
	for !done {
		if s = <-sig; s == syscall.SIGHUP {
			nextChanEval = time.Now()
		} else {
			done = true
		}
	}

	networkdStop(fmt.Sprintf("Received signal %v", s))
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
	go signalHandler()

	resetInterfaces()

	if !aputil.IsSatelliteMode() {
		wan.monitor()
		defer wan.stop()
	}

	go apMonitorLoop(&cleanup.wg, addDoneChan())
	go hostapdLoop(&cleanup.wg, addDoneChan())

	// for pprof
	go http.ListenAndServe(base_def.NETWORKD_DIAG_PORT, nil)

	cleanup.wg.Wait()
	slog.Infof("Cleaning up")

	networkCleanup()
	os.Exit(0)
}
