/*
 * Copyright 2020 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package main

import (
	"fmt"
	"math/rand"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"bg/ap_common/apcfg"
	"bg/ap_common/aputil"
	"bg/ap_common/broker"
	"bg/ap_common/mcp"
	"bg/ap_common/netctl"
	"bg/ap_common/platform"
	"bg/ap_common/wificaps"
	"bg/base_def"
	"bg/common/cfgapi"
	"bg/common/network"
	"bg/common/wifi"

	"go.uber.org/zap"
)

var (
	templateDir = apcfg.String("template_dir", "/etc/templates/ap.wifid",
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
	apScanFreq   = apcfg.Duration("ap_scan_freq", 7*time.Hour, true, nil)
	apStale      = apcfg.Duration("ap_stale", 10*time.Minute, true, nil)
	chanEvalFreq = apcfg.Duration("chan_eval_freq", 12*time.Hour, true, nil)
	_            = apcfg.String("log_level", "info", true,
		aputil.LogSetLevel)

	mcpd    *mcp.MCP
	brokerd *broker.Broker
	config  *cfgapi.Handle

	wirelessNics map[string]*physDevice

	clients    cfgapi.ClientMap // macaddr -> ClientInfo
	clientsMtx sync.Mutex
	rings      cfgapi.RingMap // ring -> config

	satellite bool
	nodeID    string
	slog      *zap.SugaredLogger

	plat           *platform.Platform
	hostapd        *hostapdHdl
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

	pname = "ap.wifid"
)

type physDevice struct {
	name     string // Linux device name
	hwaddr   string // mac address
	ring     string // configured ring
	pseudo   bool
	disabled bool

	wifi *wifiInfo
}

func list(slice []string) string {
	return strings.Join(slice, ",")
}

func slice(list string) []string {
	return strings.Split(list, ",")
}

func hostapdReset(name, val string) error {
	if hostapd != nil {
		hostapd.reset()
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
		if err = netctl.BridgeCreate(hotplugTrigger); err != nil {
			slog.Warnf("addbr %s failed: %v", hotplugTrigger, err)
		} else {
			time.Sleep(100 * time.Millisecond)
		}

		if err = netctl.BridgeDestroy(hotplugTrigger); err != nil {
			slog.Warnf("delbr %s failed: %v", hotplugTrigger, err)
		}
	}
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

// persist a device's dynamic properties to the config tree
func wifiDeviceToConfig(d *physDevice) {
	ops := make([]cfgapi.PropertyOp, 0)

	newVals := make(map[string]string)
	newVals["active_mode"] = d.wifi.activeMode
	newVals["active_band"] = d.wifi.activeBand
	newVals["active_channel"] = strconv.Itoa(d.wifi.activeChannel)
	newVals["active_width"] = strconv.Itoa(d.wifi.activeWidth)
	newVals["state"] = d.wifi.state

	base := "@/nodes/" + nodeID + "/nics/" + plat.NicID(d.name, d.hwaddr)
	for prop, val := range newVals {
		op := cfgapi.PropertyOp{
			Op:    cfgapi.PropCreate,
			Name:  base + "/" + prop,
			Value: val,
		}
		ops = append(ops, op)
		slog.Debugf("Setting %s to %s", op.Name, op.Value)
	}

	if _, err := config.Execute(nil, ops).Wait(nil); err != nil {
		slog.Warnf("Error updating %s config: %v", d.name, err)
	}
}

// construct a physDevice structure for a wireless nic from the data in the
// config tree
func wifiDeviceFromConfig(nic *cfgapi.PropertyNode) *physDevice {
	var d physDevice
	var w wifiInfo
	var err error

	if kind, _ := nic.GetChildString("kind"); kind != "wireless" {
		return nil
	}

	d.name, _ = nic.GetChildString("name")
	d.hwaddr, _ = nic.GetChildString("mac")
	d.ring, _ = nic.GetChildString("ring")
	if x, _ := nic.GetChildString("state"); x == wifi.DevDisabled {
		d.disabled = true
	}
	d.wifi = &w

	if d.disabled {
		w.state = wifi.DevDisabled
	} else {
		w.state = wifi.DevOK
	}

	w.configBand, _ = nic.GetChildString("cfg_band")
	w.configChannel, _ = nic.GetChildInt("cfg_channel")
	w.configWidth, _ = nic.GetChildInt("cfg_width")
	w.activeBand, _ = nic.GetChildString("active_band")
	w.activeChannel, _ = nic.GetChildInt("active_channel")
	w.activeWidth, _ = nic.GetChildInt("active_width")
	w.activeMode, _ = nic.GetChildString("active_mode")

	if w.cap, err = wificaps.GetCapabilities(d.name); err != nil {
		slog.Warnf("Couldn't determine wifi capabilities of %s: %v",
			d.name, err)
		return nil
	}

	return &d
}

func getDevices() {
	wirelessNics = make(map[string]*physDevice)

	nics, _ := config.GetProps("@/nodes/" + nodeID + "/nics")
	if nics != nil {
		for nicID, nic := range nics.Children {
			if d := wifiDeviceFromConfig(nic); d != nil {
				wirelessNics[nicID] = d
			}
		}
	}
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

	clients = make(cfgapi.ClientMap)
	rings = make(cfgapi.RingMap)

	config.HandleChange(`^@/siteid`, configSiteIDChanged)
	config.HandleChange(`^@/clients/.*$`, configClientChanged)
	config.HandleDelete(`^@/clients/.*$`, configClientDeleted)
	config.HandleChange(`^@/nodes/`+nodeID+`/nics/.*$`, configNicChanged)
	config.HandleDelete(`^@/nodes/`+nodeID+`/nics/.*$`, configNicDeleted)
	config.HandleChange(`^@/rings/.*`, configRingChanged)
	config.HandleChange(`^@/network/.*`, configNetworkChanged)
	config.HandleDelete(`^@/network/.*`, configNetworkDeleted)
	config.HandleChange(`^@/users/.*`, configUserChanged)
	config.HandleDelExp(`^@/users/.*`, configUserDeleted)
	config.HandleChange(`^@/network/radius_auth_secret`, configNetworkRadiusSecretChanged)
	config.HandleChange(`^@/certs/.*/state`, configCertStateChange)

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

	return nil
}

// When we get a signal, signal any hostapd process we're monitoring.  We want
// to be sure the wireless interface has been released before we give mcp a
// chance to restart the whole stack.
func signalHandler(wg *sync.WaitGroup, doneChan chan bool) {
	defer wg.Done()

	sig := make(chan os.Signal, 3)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	done := false
	for !done {
		select {
		case s := <-sig:
			if s == syscall.SIGHUP {
				nextChanEval = time.Now()
			} else {
				slog.Infof("Received signal %v", s)
				done = true
			}
		case done = <-doneChan:
		}
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

func wifidStop(msg string) {
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
	if err := daemonInit(); err != nil {
		if mcpd != nil {
			mcpd.SetState(mcp.BROKEN)
		}
		slog.Fatalf("networkd failed to start: %v", err)
	}

	if aputil.IsGatewayMode() {
		if err := radiusInit(); err != nil {
			slog.Warnf("failed to init RADIUS support: %v", err)
		}
	}

	mcpd.SetState(mcp.ONLINE)
	go signalHandler(&cleanup.wg, addDoneChan())

	wifiCleanup()

	go apMonitorLoop(&cleanup.wg, addDoneChan())
	go hostapdLoop(&cleanup.wg, addDoneChan())

	go http.ListenAndServe(base_def.WIFID_DIAG_PORT, nil)

	cleanup.wg.Wait()
	slog.Infof("Cleaning up")

	os.Exit(0)
}

