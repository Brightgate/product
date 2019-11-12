/*
 * COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
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
	"io/ioutil"
	"math/rand"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/exec"
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
	"bg/ap_common/platform"
	"bg/ap_common/wificaps"
	"bg/base_def"
	"bg/base_msg"
	"bg/common/cfgapi"
	"bg/common/network"
	"bg/common/wifi"

	"github.com/golang/protobuf/proto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
)

var (
	templateDir = apcfg.String("template_dir", "/etc/templates/ap.networkd",
		true, nil)
	rulesDir = apcfg.String("rules_dir", "/etc/filter.rules.d",
		true, nil)
	hostapdLatency = apcfg.Int("hostapd_latency", 5, true, nil)
	hostapdDebug   = apcfg.Bool("hostapd_debug", false, true,
		hostapdReset)
	hostapdVerbose = apcfg.Bool("hostapd_verbose", false, true,
		hostapdReset)
	deadmanTimeout      = apcfg.Duration("deadman", 5*time.Second, true, nil)
	retransmitSoftLimit = apcfg.Int("retransmit_soft", 3, true, nil)
	retransmitHardLimit = apcfg.Int("retransmit_hard", 6, true, nil)
	retransmitTimeout   = apcfg.Duration("retransmit_timeout",
		5*time.Minute, true, nil)
	apScanFreq   = apcfg.Duration("ap_scan_freq", time.Minute, true, nil)
	apStale      = apcfg.Duration("ap_stale", 5*time.Minute, true, nil)
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
	failuresAllowed = 4
	maxSSIDs        = 4
	period          = time.Duration(time.Minute)

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

//////////////////////////////////////////////////////////////////////////
//
// Interaction with the rest of the ap daemons
//

func hostapdReset(name, val string) error {
	if hostapd != nil {
		hostapd.reset()
	}
	return nil
}

func configNicChanged(path []string, val string, expires *time.Time) {
	var eval bool

	if len(path) != 5 {
		return
	}
	p := physDevices[path[3]]
	if p == nil {
		return
	}

	switch path[4] {
	case "cfg_channel":
		x, _ := strconv.Atoi(val)
		if eval = (p.wifi != nil && p.wifi.configChannel != x); eval {
			p.wifi.configChannel = x
		}
	case "cfg_width":
		x, _ := strconv.Atoi(val)
		if eval = (p.wifi != nil && p.wifi.configWidth != x); eval {
			p.wifi.configWidth = x
		}
	case "cfg_band":
		if eval = (p.wifi != nil && p.wifi.configBand != val); eval {
			p.wifi.configBand = val
		}
	case "ring":
		if p.ring != val {
			p.ring = val
			networkdStop("exiting to rebuild network")
		}
	case "state":
		oldVal := p.disabled
		p.disabled = (strings.ToLower(val) == "disabled")
		eval = (oldVal != p.disabled)
	}

	if eval {
		wifiEvaluate = true
		hostapd.reset()
	}
}

func configNicDeleted(path []string) {
	if len(path) == 5 {
		switch path[4] {
		case "cfg_channel", "cfg_width", "cfg_band", "ring", "state":
			configNicChanged(path, "", nil)
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
		hostapd.disassociate(hwaddr)
	} else if c.Ring != newRing {
		slog.Infof("Moving %s from %s to %s", hwaddr, c.Ring, newRing)
		c.Ring = newRing
		hostapd.reload()
		hostapd.disassociate(hwaddr)
	} else {
		// False alarm.
		return
	}

	hostapd.reload()
}

func configUserDeleted(path []string) {
	if len(path) == 2 {
		hostapd.deauthUser(path[1])
	}
}

func configRingSubnetDeleted(path []string) {
	ring := path[1]

	if _, ok := rings[ring]; !ok {
		slog.Warnf("Unknown ring: %s", ring)
	} else {
		slog.Infof("Deleted subnet for ring %s", ring)
		networkdStop("exiting to rebuild network")
	}
}

func configRingChanged(path []string, val string, expires *time.Time) {

	if len(path) != 3 {
		return
	}

	ring := path[1]
	r, ok := rings[ring]
	if !ok {
		slog.Warnf("Unknown ring: %s", ring)
		return
	}

	switch path[2] {
	case "vap":
		if r.VirtualAP != val {
			slog.Infof("Changing VAP for ring %s from %s to %s",
				ring, r.VirtualAP, val)
			r.VirtualAP = val
			hostapd.reset()
		}
	case "subnet":
		if r.Subnet != val {
			slog.Infof("Changing subnet for ring %s from %s to %s",
				ring, r.Subnet, val)
			networkdStop("exiting to rebuild network")
		}
	}
}

func configSet(name, val string) bool {
	var reload bool

	switch name {
	case "base_address":
		networkdStop("base_address changed - exiting to rebuild network")
		return false

	case "radius_auth_secret":
		prop := &wconf.radiusSecret
		if prop != nil && *prop != val {
			slog.Infof("%s changed to '%s'", name, val)
			*prop = val
			reload = true
		}

	case "dnsserver":
		wanStaticChanged(name, val)
	}

	return reload
}

func configNetworkDeleted(path []string) {
	if configSet(path[1], "") {
		wifiEvaluate = true
		hostapd.reload()
	} else if len(path) >= 3 && path[1] == "wan" && path[2] == "static" {
		field := "all"
		if len(path) > 3 {
			field = path[3]
		}
		wanStaticDeleted(field)
	}
}

func configSiteIndexChanged(path []string, val string, expires *time.Time) {
	networkdStop("site_index changed - exiting to rebuild network")
}

func configNetworkChanged(path []string, val string, expires *time.Time) {
	var reload bool

	switch len(path) {
	case 2:
		reload = configSet(path[1], val)
	case 4:
		if path[1] == "vap" {
			hostapd.reload()
		} else if path[1] == "wan" && path[2] == "static" {
			wanStaticChanged(path[3], val)
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
		if dev.disabled {
			continue
		}

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
				addVif(dev.name, ring.Vlan, ring.Bridge)
			}
		}
	}
}

func rebuildLan() {
	// Connect all the wired LAN NICs to ring-appropriate bridges.
	for _, dev := range physDevices {
		if !dev.disabled && dev.wifi == nil &&
			!plat.NicIsVirtual(dev.name) &&
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
	defer t.Stop()
	for len(devs) > 0 {
		select {
		case <-interrupt:
			return
		case <-t.C:
		}

		bad := make([]*physDevice, 0)
		for _, dev := range devs {
			if dev.disabled {
				continue
			}

			_, err := net.InterfaceByName(dev.name)
			if err == nil {
				err = addDevToRingBridge(dev,
					base_def.RING_UNENROLLED)
			}
			if err != nil {
				bad = append(bad, dev)
			}
		}
		devs = bad
	}
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

func resetInterfaces() {
	if err := sanityCheckSubnets(); err != nil {
		slog.Errorf("%v", err)
		mcpd.SetState(mcp.BROKEN)
		networkdStop("subnet sanity check failed")
		return
	}
	deleteBridges()
	createBridges()
	rebuildLan()
	rebuildInternalNet()

	resource := &base_msg.EventNetUpdate{
		Timestamp: aputil.NowToProtobuf(),
		Sender:    proto.String(brokerd.Name),
		Debug:     proto.String("-"),
	}

	if err := brokerd.Publish(resource, base_def.TOPIC_UPDATE); err != nil {
		slog.Warnf("couldn't publish %s: %v", base_def.TOPIC_UPDATE, err)
	}
}

//////////////////////////////////////////////////////////////////////////
//
// Low-level network manipulation.
//

// Create a virtual port for the given NIC / VLAN pair.  Attach the new virtual
// port to the bridge for the associated VLAN.
func addVif(nic string, vlan int, bridge string) {
	vid := strconv.Itoa(vlan)
	vif := nic + "." + vid

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
	slog.Debugf("deleting nic %s", vif)
	exec.Command(plat.IPCmd, "link", "del", vif).Run()
}

func deleteBridge(bridge string) {
	slog.Debugf("deleting bridge %s", bridge)
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
	raw := ring.IPNet.IP.To4()
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

func newNicOps(id string, nic *physDevice,
	cur *cfgapi.PropertyNode) []cfgapi.PropertyOp {

	ops := make([]cfgapi.PropertyOp, 0)
	newVals := make(map[string]string)

	if nic != nil {
		newVals["name"] = nic.name
		newVals["mac"] = nic.hwaddr
		if nic.ring != "" {
			newVals["ring"] = nic.ring
		}
		if w := nic.wifi; w != nil {
			newVals["kind"] = "wireless"
			if cap := w.cap; cap != nil {
				b := aputil.SortStringKeys(cap.WifiBands)
				m := aputil.SortStringKeys(cap.WifiModes)
				x := aputil.SortIntKeys(cap.Channels)
				c := make([]string, 0)
				for _, channel := range x {
					c = append(c, strconv.Itoa(channel))
				}
				if len(b) > 0 {
					newVals["bands"] = strings.Join(b, ",")
				}
				if len(m) > 0 {
					newVals["modes"] = strings.Join(m, ",")
				}
				if len(c) > 0 {
					newVals["channels"] = strings.Join(c, ",")
				}
			}
			if x := w.activeMode; x != "" {
				newVals["active_mode"] = x
			}
			if x := w.configBand; x != "" {
				newVals["cfg_band"] = x
			}
			if x := w.activeBand; x != "" {
				newVals["active_band"] = x
			}
			if x := w.configChannel; x != 0 {
				newVals["cfg_channel"] = strconv.Itoa(x)
			}
			if x := w.activeChannel; x != 0 {
				newVals["active_channel"] = strconv.Itoa(x)
			}
			if x := w.configWidth; x != 0 {
				newVals["cfg_width"] = strconv.Itoa(x)
			}
			if x := w.activeWidth; x != 0 {
				newVals["active_width"] = strconv.Itoa(x)
			}
			if w.state == "" {
				newVals["state"] = wifi.DevOK
			} else {
				newVals["state"] = w.state
			}
		} else {
			newVals["kind"] = "wired"
			if nic.disabled {
				newVals["state"] = wifi.DevDisabled
			} else {
				newVals["state"] = wifi.DevOK
			}
		}
		if nic.pseudo {
			newVals["pseudo"] = "true"
		} else {
			newVals["pseudo"] = "false"
		}

		// Check to see whether anything has changed before we send any
		// updates to configd
		if cur != nil {
			matches := 0
			for prop, val := range newVals {
				if old, ok := cur.Children[prop]; ok {
					if old.Value == val {
						matches++
					}
				}
			}
			if matches == len(newVals) {
				// everything matches - send back an empty slice
				return ops
			}
		}
	}

	base := "@/nodes/" + nodeID + "/nics/" + id
	if cur != nil {
		op := cfgapi.PropertyOp{
			Op:   cfgapi.PropDelete,
			Name: base,
		}
		ops = append(ops, op)
	}
	for prop, val := range newVals {
		op := cfgapi.PropertyOp{
			Op:    cfgapi.PropCreate,
			Name:  base + "/" + prop,
			Value: val,
		}
		ops = append(ops, op)
		slog.Debugf("Setting %s to %s", op.Name, op.Value)
	}

	return ops
}

// Update the config tree with the current NIC inventory
func updateNicProperties() {
	needName := !aputil.IsSatelliteMode()

	inventory := make(map[string]*physDevice)
	for id, d := range physDevices {
		inventory[id] = d
	}

	// Get the information currently recorded in the config tree
	root := "@/nodes/" + nodeID
	nics := make(cfgapi.ChildMap)
	if r, _ := config.GetProps(root); r != nil {
		if r.Children != nil {
			if r.Children["name"] != nil {
				needName = false
			}
			if n := r.Children["nics"]; n != nil {
				nics = n.Children
			}
		}
	}

	// Examine each entry in the config tree to determine whether it matches
	// our current inventory.
	ops := make([]cfgapi.PropertyOp, 0)
	for id, nic := range nics {
		var newOps []cfgapi.PropertyOp

		if dev := inventory[id]; dev != nil {
			newOps = newNicOps(id, dev, nic)
			delete(inventory, id)
		} else {
			// This nic is in the config tree, but not in our
			// current inventory.  Clean it up.
			newOps = newNicOps(id, nil, nic)
		}
		ops = append(ops, newOps...)
	}

	// If we have any remaining NICs that weren't already in the
	// tree, add them now.
	for id, d := range inventory {
		newOps := newNicOps(id, d, nil)
		ops = append(ops, newOps...)
	}

	// If this is the gateway node and it doesn't already have a name,
	// give it the default value of "gateway"
	if needName {
		op := cfgapi.PropertyOp{
			Op:    cfgapi.PropCreate,
			Name:  root + "/name",
			Value: "gateway",
		}
		ops = append(ops, op)
	}

	if len(ops) != 0 {
		if _, err := config.Execute(nil, ops).Wait(nil); err != nil {
			slog.Warnf("Error updating NIC inventory: %v", err)
		}
	}
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

	// When we create multiple SSIDs, hostapd will generate additional
	// bssids by incrementing the final octet of the nic's mac address.
	// hostapd requires that the base and generated mac addresses share the
	// upper 47 bits, so we need to ensure that the base address has the
	// lowest bits set to 0.
	oldMac := d.hwaddr
	d.hwaddr = macUpdateLastOctet(d.hwaddr, 0)
	if d.hwaddr != oldMac {
		slog.Debugf("Changed mac from %s to %s", oldMac, d.hwaddr)
	}

	// If we generate new macs for multiple SSIDs, those generated macs will
	// have the locally administered bit set.  Because we need the upper
	// bits of all macs to match, we have to set the bit for the base mac
	// even if we haven't modified it.
	d.hwaddr = macSetLocal(d.hwaddr)

	return &d
}

func getNicID(d *physDevice) string {
	return plat.NicID(d.name, d.hwaddr)
}

// Find the other nodes on which a device with this mac is present.  Returns
// strings listing the remote nodes and the offline nodes, with each instance
// named "<node>/<device name>".
func getRemoteWifi(mac string, nodes []cfgapi.NodeInfo) (string, string) {
	remote := make([]string, 0)
	offline := make([]string, 0)

	for _, node := range nodes {
		if node.ID == nodeID {
			continue
		}
		for _, nic := range node.Nics {
			if nic.MacAddr == mac && nic.WifiInfo != nil {
				n := node.ID + "/" + nic.Name
				if node.Alive == nil {
					offline = append(offline, n)
				} else {
					remote = append(remote, n)
				}
			}
		}
	}

	return strings.Join(remote, ","), strings.Join(offline, ",")
}

//
// Inventory the physical network devices in the system
//
func getDevices() {
	all, err := net.Interfaces()
	if err != nil {
		slog.Fatalf("Unable to inventory network devices: %v", err)
	}

	nodes, err := config.GetNodes()
	if err != nil {
		slog.Warnf("getting @/nodes: %v", err)
	}

	macs := make(map[string]*physDevice)
	for _, i := range all {
		var d *physDevice

		if i.HardwareAddr.String() == "00:00:00:00:00:00" {
			slog.Warnf("bogus mac address for %s: %s", i.Name,
				i.HardwareAddr.String())
			continue
		}
		if plat.NicIsVirtual(i.Name) {
			continue
		}
		if plat.NicIsWired(i.Name) {
			d = getEthernet(i)
		} else if plat.NicIsWireless(i.Name) {
			d = getWireless(i)
		}

		// If this is a wireless device and we already have another
		// wireless nic with the same mac address, we want to leave this
		// one offline.
		if d != nil && d.wifi != nil {
			var conflicts, faults string

			name := d.name
			mac := d.hwaddr
			if local := macs[mac]; local != nil {
				faults = " local: " + local.name
				d = nil
			}

			remote, offline := getRemoteWifi(mac, nodes)
			if len(remote) > 0 {
				faults += " remote nodes: " + remote
				d = nil
			}
			if len(offline) > 0 {
				// If the other node is offline, it's safe to
				// use this device despite the conflict.  It's
				// still worth noting in the log.
				conflicts = " offline nodes: " + offline
			}

			if len(faults+conflicts) > 0 {
				msg := fmt.Sprintf("multiple instances of %s:%s",
					mac, faults+conflicts)
				slog.Warn(msg)

				if len(faults) > 0 {
					aputil.ReportHardware(name, msg)
				}
			}
		}

		if d != nil {
			physDevices[getNicID(d)] = d
			macs[d.hwaddr] = d
		}
	}

	nics, _ := config.GetProps("@/nodes/" + nodeID + "/nics")
	if nics != nil {
		for nicID, nic := range nics.Children {
			if d := physDevices[nicID]; d != nil {
				if x, ok := nic.Children["ring"]; ok {
					d.ring = x.Value
				}
				if x, ok := nic.Children["cfg_band"]; ok {
					d.wifi.configBand = x.Value
				}
				if x, ok := nic.Children["cfg_channel"]; ok {
					d.wifi.configChannel, _ = strconv.Atoi(x.Value)
				}
				if x, ok := nic.Children["cfg_width"]; ok {
					d.wifi.configWidth, _ = strconv.Atoi(x.Value)
				}
				if x, ok := nic.Children["state"]; ok {
					if strings.ToLower(x.Value) == "disabled" {
						d.disabled = true
					}
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

func networkCleanup() {
	devs, _ := ioutil.ReadDir("/sys/devices/virtual/net")
	slog.Debugf("deleting virtual NICs")
	for _, dev := range devs {
		name := dev.Name()

		if plat.NicIsVirtual(name) {
			deleteVif(name)
		}
	}

	slog.Debugf("deleting bridges")
	for _, dev := range devs {
		name := dev.Name()

		if strings.HasPrefix(name, "b") {
			deleteBridge(name)
		}
	}

	wan.dhcpRenew()
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

func prometheusInit() {
	http.Handle("/metrics", promhttp.Handler())
	go http.ListenAndServe(base_def.NETWORKD_DIAG_PORT, nil)
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
	prometheusInit()
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

	cleanup.wg.Wait()
	slog.Infof("Cleaning up")

	networkCleanup()
	os.Exit(0)
}
