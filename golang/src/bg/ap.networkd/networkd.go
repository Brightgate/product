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
	"fmt"
	"io/ioutil"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"bg/ap_common/apcfg"
	"bg/ap_common/aputil"
	"bg/ap_common/broker"
	"bg/ap_common/mcp"
	"bg/ap_common/network"
	"bg/ap_common/platform"
	"bg/ap_common/wificaps"
	"bg/base_def"
	"bg/common/cfgapi"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
)

var (
	templateDir = flag.String("template_dir", "golang/src/ap.networkd",
		"location of hostapd templates")
	rulesDir       = flag.String("rules_dir", "./", "Location of the filter rules")
	hostapdLatency = flag.Int("hl", 5, "hostapd latency limit (seconds)")
	deadmanTimeout = flag.Duration("deadman", 5*time.Second,
		"time to wait for hostapd cleanup to complete")

	physDevices = make(map[string]*physDevice)

	mcpd     *mcp.MCP
	brokerd  *broker.Broker
	config   *cfgapi.Handle
	clients  cfgapi.ClientMap // macaddr -> ClientInfo
	rings    cfgapi.RingMap   // ring -> config
	nodeUUID string
	slog     *zap.SugaredLogger

	plat           *platform.Platform
	hostapd        *hostapdHdl
	running        bool
	satellite      bool
	networkNodeIdx byte
)

const (
	// Allow up to 4 failures in a 1 minute period before giving up
	failuresAllowed = 4
	period          = time.Duration(time.Minute)

	pname = "ap.networkd"
)

type physDevice struct {
	name   string // Linux device name
	hwaddr string // mac address
	ring   string // configured ring
	pseudo bool

	wifi *wifiInfo
}

//////////////////////////////////////////////////////////////////////////
//
// Interaction with the rest of the ap daemons
//

func configChannelChanged(path []string, val string, expires *time.Time) {
	nicID := path[3]
	newChannel, _ := strconv.Atoi(val)

	if p := physDevices[nicID]; p != nil && p.wifi != nil {
		if p.wifi.cfgChannel != newChannel {
			p.wifi.cfgChannel = newChannel
			wifiEvaluate = true
			hostapd.reload()
		}
	}
}

func configBandChanged(path []string, val string, expires *time.Time) {
	nicID := path[3]
	newBand := val

	if p := physDevices[nicID]; p != nil && p.wifi != nil {
		if p.wifi.cfgBand != newBand {
			p.wifi.cfgBand = newBand
			wifiEvaluate = true
			hostapd.reload()
		}
	}
}

func configRingChanged(path []string, val string, expires *time.Time) {
	nicID := path[3]
	newRing := val

	if p := physDevices[nicID]; p != nil {
		if p.ring != newRing {
			p.ring = newRing
			hostapd.reload()
		}
	}
}

func configClientChanged(path []string, val string, expires *time.Time) {
	hwaddr := path[1]
	newRing := val
	c, ok := clients[hwaddr]
	if !ok {
		c := cfgapi.ClientInfo{Ring: newRing}
		slog.Infof("New client %s in %s", hwaddr, newRing)
		clients[hwaddr] = &c
	} else if c.Ring != newRing {
		slog.Infof("Moving %s from %s to %s", hwaddr, c.Ring, newRing)
		c.Ring = newRing
	} else {
		// False alarm.
		return
	}

	hostapd.reload()
}

func configAuthChanged(path []string, val string, expires *time.Time) {
	ring := path[1]
	newAuth := val

	if newAuth != "wpa-psk" && newAuth != "wpa-eap" {
		slog.Warnf("Unknown auth set on %s: %s", ring, newAuth)
		return
	}

	r, ok := rings[ring]
	if !ok {
		slog.Warnf("Authentication set on unknown ring: %s", ring)
		return
	}

	if r.Auth != newAuth {
		slog.Infof("Changing auth for ring %s from %s to %s", ring,
			r.Auth, newAuth)
		r.Auth = newAuth
		hostapd.reload()
	}
}

func configSet(name, val string) bool {
	var prop *string

	switch name {
	case "ssid":
		prop = &wconf.ssid
	case "ssid-eap":
		prop = &wconf.ssidEAP
	case "ssid-5ghz":
		prop = &wconf.ssid5GHz
	case "ssid-eap-5ghz":
		prop = &wconf.ssid5GHzEAP
	case "passphrase":
		prop = &wconf.passphrase
	case "radiusAuthSecret":
		prop = &wconf.radiusSecret
	}

	if prop != nil && *prop != val {
		slog.Infof("%s changed to '%s'", name, val)
		*prop = val
		return true
	}
	return false
}

func configNetworkDeleted(path []string) {
	if configSet(path[1], "") {
		wifiEvaluate = true
		hostapd.reload()
	}
}

func configNetworkChanged(path []string, val string, expires *time.Time) {
	var reload bool

	if len(path) == 2 {
		reload = configSet(path[1], val)
	} else if len(path) == 3 && path[2] == "channel" {
		channel, _ := strconv.Atoi(val)
		band := path[1]

		if band == wificaps.LoBand || band == wificaps.HiBand {
			if legalChannels[band][channel] {
				wconf.channels[band] = channel
				reload = true
			} else {
				slog.Warnf("ignoring illegal channel '%d' "+
					"for %s", channel, band)
			}
		}
	}

	if reload {
		wifiEvaluate = true
		hostapd.reload()
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

func addDevToRingBridge(dev *physDevice, ring string) error {
	var err error

	err = exec.Command(plat.IPCmd, "link", "set", "up", dev.name).Run()
	if err != nil {
		slog.Warnf("Failed to enable %s: %v", dev.name, err)
	}

	if config := rings[ring]; config != nil {
		br := config.Bridge
		slog.Debugf("Connecting %s (%s) to the %s bridge: %s",
			dev.name, dev.hwaddr, ring, br)
		c := exec.Command(plat.BrctlCmd, "addif", br, dev.name)
		if out, rerr := c.CombinedOutput(); rerr != nil {
			err = fmt.Errorf(string(out))
		}
	} else {
		err = fmt.Errorf("non-existent ring %s", ring)
	}

	if err != nil {
		slog.Warnf("Failed to add %s: %v", dev.name, err)
	}
	return err
}

func rebuildInternalNet() {
	satNode := aputil.IsSatelliteMode()

	// For each internal network device, create a virtual device for each
	// LAN ring and attach it to the bridge for that ring
	for _, dev := range physDevices {
		if dev.ring != base_def.RING_INTERNAL {
			continue
		}

		if !satNode {
			err := addDevToRingBridge(dev, base_def.RING_INTERNAL)
			if err != nil {
				continue
			}
		}
		for name, ring := range rings {
			if name != base_def.RING_INTERNAL {
				addVif(dev.name, ring.Vlan)
			}
		}
	}
}

func rebuildLan() {
	// Connect all the wired LAN NICs to ring-appropriate bridges.
	for _, dev := range physDevices {
		if dev.wifi == nil && !plat.NicIsVirtual(dev.name) &&
			dev.ring != base_def.RING_INTERNAL &&
			dev.ring != base_def.RING_WAN {
			addDevToRingBridge(dev, dev.ring)
		}
	}
}

// If hostapd authorizes a client that isn't assigned to a VLAN, it gets
// connected to the physical wifi device rather than a virtual interface.
// Connect those physical devices to the UNENROLLED bridge once hostapd is
// running.  We don't have a good way to determine when hostapd has gotten far
// enough for this operation to succeed, so we just keep trying.
func rebuildUnenrolled(devs []*physDevice, interrupt chan bool) {
	t := time.NewTicker(time.Second)
	for len(devs) > 0 {
		select {
		case <-interrupt:
			return
		case <-t.C:
		}

		bad := make([]*physDevice, 0)
		for _, dev := range devs {
			err := addDevToRingBridge(dev, base_def.RING_UNENROLLED)
			if err != nil {
				bad = append(bad, dev)
			}
		}
		devs = bad
	}
}

func resetInterfaces() {
	deleteBridges()
	createBridges()
	rebuildLan()
	rebuildInternalNet()
}

//////////////////////////////////////////////////////////////////////////
//
// Low-level network manipulation.
//

// Create a virtual port for the given NIC / VLAN pair.  Attach the new virtual
// port to the bridge for the associated VLAN.
func addVif(nic string, vlan int) {
	vid := strconv.Itoa(vlan)
	vif := nic + "." + vid
	bridge := fmt.Sprintf("brvlan%d", vlan)

	deleteVif(vif)
	err := exec.Command(plat.VconfigCmd, "add", nic, vid).Run()
	if err != nil {
		slog.Warnf("Failed to create vif %s: %v", vif, err)
		return
	}

	err = exec.Command(plat.BrctlCmd, "addif", bridge, vif).Run()
	if err != nil {
		slog.Warnf("Failed to add %s to %s: %v", vif, bridge, err)
		return
	}

	err = exec.Command(plat.IPCmd, "link", "set", "up", vif).Run()
	if err != nil {
		slog.Warnf("Failed to enable %s: %v", vif, err)
	}
}

func deleteVif(vif string) {
	exec.Command(plat.IPCmd, "link", "del", vif).Run()
}

func deleteBridge(bridge string) {
	exec.Command(plat.IPCmd, "link", "set", "down", bridge).Run()
	exec.Command(plat.BrctlCmd, "delbr", bridge).Run()
}

// Delete the bridges associated with each ring.  This gets us back to a known
// ground state, simplifying the task of rebuilding everything when hostapd
// starts back up.
func deleteBridges() {
	for _, conf := range rings {
		deleteBridge(conf.Bridge)
	}
}

// Determine the address to be used for the given ring's router on this node.
// If the AP has an address of 192.168.131.x on the internal subnet, then the
// router for each ring will be the corresponding .x address in that ring's
// subnet.
func localRouter(ring *cfgapi.RingConfig) string {
	_, network, _ := net.ParseCIDR(ring.Subnet)
	raw := network.IP.To4()
	raw[3] = networkNodeIdx
	return (net.IP(raw)).String()
}

//
// Prepare a ring's bridge: clean up any old state, assign a new address, set up
// routes, etc.
//
func createBridge(ringName string) {
	ring := rings[ringName]
	bridge := ring.Bridge

	slog.Infof("Preparing %s ring: %s %s", ringName, bridge, ring.Subnet)

	err := exec.Command(plat.BrctlCmd, "addbr", bridge).Run()
	if err != nil {
		slog.Warnf("addbr %s failed: %v", bridge, err)
		return
	}

	err = exec.Command(plat.IPCmd, "link", "set", "up", bridge).Run()
	if err != nil {
		slog.Warnf("bridge %s failed to come up: %v", bridge, err)
		return
	}

	// ip addr flush dev brvlan0
	cmd := exec.Command(plat.IPCmd, "addr", "flush", "dev", bridge)
	if err := cmd.Run(); err != nil {
		slog.Fatalf("Failed to remove existing IP address: %v", err)
	}

	// ip route del 192.168.136.0/24
	cmd = exec.Command(plat.IPCmd, "route", "del", ring.Subnet)
	cmd.Run()

	// ip addr add 192.168.136.1 dev brvlan0
	router := localRouter(ring)
	cmd = exec.Command(plat.IPCmd, "addr", "add", router, "dev", bridge)
	slog.Debugf("Setting %s to %s", bridge, router)
	if err := cmd.Run(); err != nil {
		slog.Fatalf("Failed to set the router address: %v", err)
	}

	// ip link set up brvlan0
	cmd = exec.Command(plat.IPCmd, "link", "set", "up", bridge)
	if err := cmd.Run(); err != nil {
		slog.Fatalf("Failed to enable bridge: %v", err)
	}
	// ip route add 192.168.136.0/24 dev brvlan0
	cmd = exec.Command(plat.IPCmd, "route", "add", ring.Subnet, "dev", bridge)
	if err := cmd.Run(); err != nil {
		slog.Fatalf("Failed to add %s as the new route: %v",
			ring.Subnet, err)
	}
}

func createBridges() {
	satNode := aputil.IsSatelliteMode()

	for ring := range rings {
		if satNode && ring == base_def.RING_INTERNAL {
			// Satellite nodes don't build an internal ring - they connect
			// to the primary node's internal ring using DHCP.
			continue
		}

		createBridge(ring)
	}
}

func newNicOps(id string, nic *physDevice) []cfgapi.PropertyOp {
	base := "@/nodes/" + nodeUUID + "/nics/" + id

	ops := make([]cfgapi.PropertyOp, 0)
	if nic == nil {
		op := cfgapi.PropertyOp{
			Op:   cfgapi.PropDelete,
			Name: base,
		}
		ops = append(ops, op)
	} else if nic.ring != "" {
		var kind string
		if nic.wifi == nil {
			kind = "wired"
		} else {
			kind = "wireless"
		}

		op := cfgapi.PropertyOp{
			Op:    cfgapi.PropCreate,
			Name:  base + "/kind",
			Value: kind,
		}
		slog.Debugf("Setting %s to %s", op.Name, op.Value)
		ops = append(ops, op)

		op = cfgapi.PropertyOp{
			Op:    cfgapi.PropCreate,
			Name:  base + "/ring",
			Value: nic.ring,
		}
		slog.Debugf("Setting %s to %s", op.Name, op.Value)
		ops = append(ops, op)
	}

	return ops
}

// Update the config tree with the current NIC inventory
func updateNicProperties() {
	inventory := make(map[string]*physDevice)
	for id, d := range physDevices {
		inventory[id] = d
	}

	// Get the information currently recorded in the config tree
	nics := make(cfgapi.ChildMap)
	if r, _ := config.GetProps("@/nodes/" + nodeUUID + "/nics"); r != nil {
		nics = r.Children
	}

	// Examine each entry in the config tree to determine whether it matches
	// our current inventory.
	ops := make([]cfgapi.PropertyOp, 0)
	for id, nic := range nics {
		var ring string
		var newOps []cfgapi.PropertyOp

		if x := nic.Children["ring"]; x != nil {
			ring = x.Value
		}

		if dev := inventory[id]; dev != nil {
			// This nic is in the config tree and our discovered
			// inventory.  If the properties all match, then we can
			// leave this alone

			var ok bool
			if x := nic.Children["kind"]; x != nil {
				ok = (x.Value == "wired" && dev.wifi == nil) ||
					(x.Value == "wireless" && dev.wifi != nil)
			}
			if !ok || (ring != dev.ring) {
				newOps = newNicOps(id, dev)
			}
			delete(inventory, id)
		} else {
			// This nic is in the config tree, but not in our
			// current inventory.  Clean it up.
			newOps = newNicOps(id, nil)
		}
		ops = append(ops, newOps...)
	}

	// If we have any remaining NICs that weren't already in the
	// tree, add them now.
	for id, d := range inventory {
		newOps := newNicOps(id, d)
		ops = append(ops, newOps...)
	}

	if len(ops) != 0 {
		if _, err := config.Execute(nil, ops).Wait(nil); err != nil {
			slog.Warnf("Error updating NIC inventory: %v", err)
		}
	}
}

//
// Identify and prepare the WAN port.
//
func prepareWan() {
	var err error
	var available, wan *physDevice
	var outgoingRing string

	if aputil.IsSatelliteMode() {
		outgoingRing = base_def.RING_INTERNAL
	} else {
		outgoingRing = base_def.RING_WAN
	}

	// Enable packet forwarding
	cmd := exec.Command(plat.SysctlCmd, "-w", "net.ipv4.ip_forward=1")
	if err = cmd.Run(); err != nil {
		slog.Fatalf("Failed to enable packet forwarding: %v", err)
	}

	// Find the WAN device
	for _, dev := range physDevices {
		if dev.wifi != nil {
			// XXX - at some point we should investigate using a
			// wireless link as a mesh backhaul
			continue
		}

		if plat.NicIsWan(dev.name, dev.hwaddr) {
			available = dev
			if dev.ring == outgoingRing {
				if wan == nil {
					wan = dev
				} else {
					slog.Infof("Multiple wan nics found.  "+
						"Using: %s", wan.hwaddr)
				}
			}
		}
	}

	if available == nil {
		slog.Warnf("couldn't find a outgoing device to use")
		return
	}
	if wan == nil {
		wan = available
		slog.Infof("No outgoing device configured.  Using %s", wan.hwaddr)
		wan.ring = outgoingRing
	}

	wanNic = wan.name
}

func getEthernet(i net.Interface) *physDevice {
	d := physDevice{
		name:   i.Name,
		hwaddr: i.HardwareAddr.String(),
	}
	return &d
}

func getWireless(i net.Interface) *physDevice {
	var err error

	d := physDevice{
		name:   i.Name,
		hwaddr: i.HardwareAddr.String(),
	}

	if strings.HasPrefix(d.hwaddr, "02:00") {
		slog.Debugf("Skipping emulated device %s (%s)",
			d.name, d.hwaddr)
		return nil
	}

	d.wifi = new(wifiInfo)
	if d.wifi.cap, err = wificaps.GetCapabilities(d.name); err != nil {
		slog.Warnf("Couldn't determine wifi capabilities of %s: %v",
			d.name, err)
		return nil
	}

	slog.Infof("device: %s", d.name)
	// Emit one line at a time to the log, or only the first line will get
	// the log prefix.
	capstr := fmt.Sprintf("%s", d.wifi.cap)
	for _, line := range strings.Split(strings.TrimSuffix(capstr, "\n"), "\n") {
		slog.Debugf(line)
	}

	return &d
}

func getNicID(d *physDevice) string {
	return plat.NicID(d.name, d.hwaddr)
}

//
// Inventory the physical network devices in the system
//
func getDevices() {
	all, err := net.Interfaces()
	if err != nil {
		slog.Fatalf("Unable to inventory network devices: %v", err)
	}

	for _, i := range all {
		var d *physDevice

		if plat.NicIsVirtual(i.Name) {
			continue
		}
		if plat.NicIsWired(i.Name) {
			d = getEthernet(i)
		} else if plat.NicIsWireless(i.Name) {
			d = getWireless(i)
		}
		if d != nil {
			physDevices[getNicID(d)] = d
		}
	}

	nics, _ := config.GetProps("@/nodes/" + nodeUUID + "/nics")
	if nics != nil {
		for nicID, nic := range nics.Children {
			if d := physDevices[nicID]; d != nil {
				if x, ok := nic.Children["ring"]; ok {
					d.ring = x.Value
				}
				if x, ok := nic.Children["band"]; ok {
					d.wifi.cfgBand = x.Value
				}
				if x, ok := nic.Children["channel"]; ok {
					d.wifi.cfgChannel, _ = strconv.Atoi(x.Value)
				}
			}
		}
	}
}

func getGatewayIP() string {
	internal := rings[base_def.RING_INTERNAL]
	gateway := network.SubnetRouter(internal.Subnet)
	return gateway
}

func getNodeID() (string, error) {
	nodeUUID, err := plat.GetNodeID()
	if err != nil {
		return "", err
	}

	prop := "@/nodes/" + nodeUUID + "/platform"
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
	return nodeUUID, nil
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

	brokerd = broker.New(pname)

	config, err = apcfg.NewConfigd(brokerd, pname, cfgapi.AccessInternal)
	if err != nil {
		return fmt.Errorf("cannot connect to configd: %v", err)
	}

	if nodeUUID, err = getNodeID(); err != nil {
		return err
	}

	config.HandleChange(`^@/clients/.*/ring$`, configClientChanged)
	config.HandleChange(`^@/nodes/"+nodeUUID+"/nics/.*/band$`, configBandChanged)
	config.HandleChange(`^@/nodes/"+nodeUUID+"/nics/.*/channel$`, configChannelChanged)
	config.HandleChange(`^@/nodes/"+nodeUUID+"/nics/.*/ring$`, configRingChanged)
	config.HandleChange(`^@/rings/.*/auth$`, configAuthChanged)
	config.HandleChange(`^@/network/`, configNetworkChanged)
	config.HandleDelete(`^@/network/`, configNetworkDeleted)
	config.HandleChange(`^@/firewall/rules/`, configRuleChanged)
	config.HandleDelete(`^@/firewall/rules/`, configRuleDeleted)
	config.HandleChange(`^@/firewall/blocked/`, configBlocklistChanged)
	config.HandleExpire(`^@/firewall/blocked/`, configBlocklistExpired)

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
	prepareWan()

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

func networkCleanup() {
	devs, _ := ioutil.ReadDir("/sys/devices/virtual/net")
	for _, dev := range devs {
		name := dev.Name()

		if strings.HasPrefix(name, "b") {
			deleteBridge(name)
		}
		if plat.NicIsVirtual(name) {
			deleteVif(name)
		}
	}
}

//
// When we get a signal, set the 'running' flag to false and signal any hostapd
// process we're monitoring.  We want to be sure the wireless interface has been
// released before we give mcp a chance to restart the whole stack.
//
func signalHandler() {
	sig := make(chan os.Signal, 2)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	s := <-sig

	slog.Infof("Received signal %v", s)
	running = false
	hostapd.reset()
}

func prometheusInit() {
	http.Handle("/metrics", promhttp.Handler())
	go http.ListenAndServe(base_def.NETWORKD_PROMETHEUS_PORT, nil)
}

func main() {
	rand.Seed(time.Now().Unix())

	flag.Parse()
	slog = aputil.NewLogger(pname)
	defer slog.Sync()
	slog.Infof("starting")

	*templateDir = aputil.ExpandDirPath(*templateDir)
	*rulesDir = aputil.ExpandDirPath(*rulesDir)

	plat = platform.NewPlatform()
	prometheusInit()
	networkCleanup()
	if err := daemonInit(); err != nil {
		if mcpd != nil {
			mcpd.SetState(mcp.BROKEN)
		}
		slog.Fatalf("networkd failed to start: %v", err)
	}

	applyFilters()

	running = true
	go signalHandler()

	resetInterfaces()
	mcpd.SetState(mcp.ONLINE)
	hostapdLoop()

	slog.Infof("Cleaning up")
	networkCleanup()
}
