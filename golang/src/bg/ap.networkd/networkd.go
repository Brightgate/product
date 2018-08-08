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
	"log"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
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
	"bg/base_def"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	loBand = "2.4GHz"
	hiBand = "5GHz"
)

var (
	templateDir = flag.String("template_dir", "golang/src/ap.networkd",
		"location of hostapd templates")
	rulesDir       = flag.String("rules_dir", "./", "Location of the filter rules")
	hostapdLatency = flag.Int("hl", 5, "hostapd latency limit (seconds)")
	deadmanTimeout = flag.Duration("deadman", 5*time.Second,
		"time to wait for hostapd cleanup to complete")

	physDevices = make(map[string]*physDevice)
	activeWifi  []*physDevice

	bands = []string{loBand, hiBand}

	mcpd     *mcp.MCP
	brokerd  *broker.Broker
	config   *apcfg.APConfig
	clients  apcfg.ClientMap // macaddr -> ClientInfo
	rings    apcfg.RingMap   // ring -> config
	nodeUUID string

	wifiEvaluate   bool
	wifiChannels   map[string]int
	wifiSSID       string
	wifiPassphrase string
	radiusSecret   string

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

type wifiInfo struct {
	cfgBand    string // user-configured band
	cfgChannel int    // user-configured channel

	activeBand    string // band actually being used
	activeChannel int    // channel actually being used

	supportVLANs bool            // does the nic support VLANs?
	interfaces   int             // number of APs it can support
	channels     map[int]bool    // channels the device claims to support
	freqWidths   map[int]bool    // frequency widths it claims to support
	wifiBands    map[string]bool // frequency bands it supports
	wifiModes    map[string]bool // 802.11[a,b,g,n,ac] modes supported
}

// For each band (i.e., these are the channels we are legally allowed to use
// in this region.
var legalChannels map[string]map[int]bool

// Functional classification of channels used in the channel selection
// algorithm.  The intersection of these lists, the regulatory legalChannel
// list, and the per-device list of supported frequencies is used to choose a
// channel.
var channelLists = map[string][]int{
	"loBand20MHz":     {1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11},
	"loBandNoOverlap": {1, 6, 11},
	"hiBand20MHz": {36, 40, 44, 48, 52, 56, 60, 64, 100, 104, 108, 112, 116,
		120, 124, 128, 132, 136, 140, 144, 149, 153, 157, 161, 165},
	// These numbers are not the centers of the 40MHz channels, as shown in
	// many channel diagrams and the List of WLAN channels Wikipedia page.
	// Instead, they are the channel number of the primary 20MHz channel
	// component of the 40MHz channel (whether above or below the primary).
	// This is how hostapd expects you to tell it what channel to run on, as
	// well as how the Mac client interface numbers them.  See also
	// initChannelLists() in hostapd.go.
	"hiBand40MHz": {36, 40, 44, 48, 52, 56, 60, 64, 100, 104, 108, 112, 116,
		120, 124, 128, 132, 136, 140, 144, 149, 153, 157, 161},
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
		c := apcfg.ClientInfo{Ring: newRing}
		log.Printf("New client %s in %s\n", hwaddr, newRing)
		clients[hwaddr] = &c
	} else if c.Ring != newRing {
		log.Printf("Moving %s from %s to %s\n", hwaddr, c.Ring, newRing)
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
		log.Printf("Unknown auth set on %s: %s\n", ring, newAuth)
		return
	}

	r, ok := rings[ring]
	if !ok {
		log.Printf("Authentication set on unknown ring: %s\n", ring)
		return
	}

	if r.Auth != newAuth {
		log.Printf("Changing auth for ring %s from %s to %s\n", ring,
			r.Auth, newAuth)
		r.Auth = newAuth
		hostapd.reload()
	}
}

func configNetworkChanged(path []string, val string, expires *time.Time) {
	var reload bool

	if len(path) == 2 {
		switch path[1] {
		case "ssid":
			wifiSSID = val
			reload = true
			log.Printf("SSID changed to %s\n", val)

		case "passphrase":
			wifiPassphrase = val
			reload = true
			log.Printf("passphrase changed to %s\n", val)

		case "radiusAuthSecret":
			radiusSecret = val
			reload = true
			log.Printf("radiusAuthSecret changed to %s\n", val)
		}
	} else if len(path) == 3 && path[2] == "channel" {
		channel, _ := strconv.Atoi(val)
		band := path[1]

		if band == loBand || band == hiBand {
			if legalChannels[band][channel] {
				wifiChannels[band] = channel
				reload = true
				wifiEvaluate = true
			} else {
				log.Printf("ignoring illegal channel '%d' "+
					"for %s\n", channel, band)
			}
		}
	}

	if reload {
		hostapd.reload()
	}
}

func setChannel(w *wifiInfo, channel int) error {
	band := w.activeBand
	if w.channels[channel] && legalChannels[band][channel] {
		w.activeChannel = channel
		return nil
	}
	return fmt.Errorf("channel %d not valid on %s", channel, band)
}

// From a list of possible channels, select one at random that is supported by
// this wifi device
func randomChannel(w *wifiInfo, list []int) error {
	band := w.activeBand

	start := rand.Int() % len(list)
	idx := start
	for {
		if setChannel(w, list[idx]) == nil {
			return nil
		}

		if idx++; idx == len(list) {
			idx = 0
		}
		if idx == start {
			return fmt.Errorf("no available channels for band %s", band)
		}
	}
}

// Choose a channel for this wifi device from within its configured band.
func selectWifiChannel(d *physDevice) error {
	var err error

	if d == nil || d.wifi == nil {
		return fmt.Errorf("not a wireless device")
	}

	w := d.wifi
	band := w.activeBand

	w.activeChannel = 0
	if !w.wifiBands[band] {
		return fmt.Errorf("doesn't support %s", band)
	}

	// If the user has configured a channel for this nic, try that first.
	if w.cfgChannel != 0 {
		if err = setChannel(w, w.cfgChannel); err == nil {
			return nil
		}
		log.Printf("nic-specific %v\n", err)
	}

	// If the user has configured a channel for this band, try that next.
	if wifiChannels[band] != 0 {
		if err = setChannel(w, wifiChannels[band]); err == nil {
			return nil
		}
		log.Printf("band-specific %v\n", err)
	}

	if band == loBand {
		// We first try to choose one of the non-overlapping channels.
		// If that fails, we'll take any channel in this range.
		err = randomChannel(w, channelLists["loBandNoOverlap"])
		if err != nil {
			err = randomChannel(w, channelLists["loBand20MHz"])
		}
	} else {
		// Start by trying to get a wide channel.  If that fails, take
		// any narrow channel.
		// XXX: this gets more complicated with 802.11ac support.
		if w.freqWidths[40] {
			randomChannel(w, channelLists["hiBand40MHz"])
		}
		if w.activeChannel == 0 {
			err = randomChannel(w, channelLists["hiBand20MHz"])
		}
	}

	return err
}

// How desirable is it to use this device in this band?
func score(d *physDevice, band string) int {
	var score int

	if d == nil || d.pseudo || d.wifi == nil {
		return 0
	}

	w := d.wifi
	if !w.supportVLANs || w.interfaces <= 1 || !w.wifiBands[band] {
		return 0
	}

	if w.cfgBand != "" && w.cfgBand != band {
		return 0
	}

	if band == loBand {
		// We always want at least one NIC in the 2.4GHz range, so they
		// get an automatic bump
		score = 10
	}

	if w.wifiModes["n"] {
		score = score + 1
	}

	if w.wifiModes["ac"] {
		score = score + 2
	}

	return score
}

func selectWifiDevices() {
	var selected map[string]*physDevice

	best := 0
	for _, d := range activeWifi {
		best = best + score(d, d.wifi.activeBand)
	}

	// Iterate over all possible combinations to find the pair of devices
	// that supports the most desirable combination of modes.
	for _, a := range physDevices {
		for _, b := range physDevices {
			if a == b {
				continue
			}
			scoreA := score(a, loBand)
			scoreB := score(b, hiBand)
			if scoreA+scoreB > best {
				selected = make(map[string]*physDevice)
				if scoreA > 0 {
					selected[loBand] = a
				}
				if scoreB > 0 {
					selected[hiBand] = b
				}
				best = scoreA + scoreB
			}
		}
	}

	activeWifi = make([]*physDevice, 0)
	for idx, band := range bands {
		if d := selected[band]; d != nil {
			d.wifi.activeBand = bands[idx]
			if err := selectWifiChannel(d); err != nil {
				log.Printf("%v\n", err)
			} else {
				activeWifi = append(activeWifi, d)
			}
		}
	}
	if len(activeWifi) == 0 {
		log.Printf("no wireless devices available")
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
			log.Printf("Failed to get interface for %s: %v\n",
				dev.name, err)
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			log.Printf("Failed to get address for %s: %v\n",
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
		log.Printf("Failed to enable %s: %v\n", dev.name, err)
	}

	if config := rings[ring]; config != nil {
		br := config.Bridge
		log.Printf("Connecting %s (%s) to the %s bridge: %s\n",
			dev.name, dev.hwaddr, ring, br)
		c := exec.Command(plat.BrctlCmd, "addif", br, dev.name)
		if out, rerr := c.CombinedOutput(); rerr != nil {
			err = fmt.Errorf(string(out))
		}
	} else {
		err = fmt.Errorf("non-existent ring %s", ring)
	}

	if err != nil {
		log.Printf("Failed to add %s: %v\n", dev.name, err)
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

func runLoop() {
	startTimes := make([]time.Time, failuresAllowed)

	wifiEvaluate = true
	for running {
		if wifiEvaluate {
			selectWifiDevices()
		}

		if len(activeWifi) > 0 {
			startTimes = append(startTimes[1:failuresAllowed],
				time.Now())

			hostapd = startHostapd(activeWifi)
			if err := hostapd.wait(); err != nil {
				log.Printf("%v\n", err)
			}
			hostapd = nil

			if time.Since(startTimes[0]) < period {
				log.Printf("hostapd is dying too quickly")
				wifiEvaluate = false
			}
			resetInterfaces()
		}

		if running {
			time.Sleep(time.Second)
		}
	}
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
		log.Printf("Failed to create vif %s: %v\n", vif, err)
		return
	}

	err = exec.Command(plat.BrctlCmd, "addif", bridge, vif).Run()
	if err != nil {
		log.Printf("Failed to add %s to %s: %v\n", vif, bridge, err)
		return
	}

	err = exec.Command(plat.IPCmd, "link", "set", "up", vif).Run()
	if err != nil {
		log.Printf("Failed to enable %s: %v\n", vif, err)
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
func localRouter(ring *apcfg.RingConfig) string {
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

	log.Printf("Preparing %s ring: %s %s\n", ringName, bridge, ring.Subnet)

	err := exec.Command(plat.BrctlCmd, "addbr", bridge).Run()
	if err != nil {
		log.Printf("addbr %s failed: %v", bridge, err)
		return
	}

	err = exec.Command(plat.IPCmd, "link", "set", "up", bridge).Run()
	if err != nil {
		log.Printf("bridge %s failed to come up: %v", bridge, err)
		return
	}

	// ip addr flush dev brvlan0
	cmd := exec.Command(plat.IPCmd, "addr", "flush", "dev", bridge)
	if err := cmd.Run(); err != nil {
		log.Fatalf("Failed to remove existing IP address: %v\n", err)
	}

	// ip route del 192.168.136.0/24
	cmd = exec.Command(plat.IPCmd, "route", "del", ring.Subnet)
	cmd.Run()

	// ip addr add 192.168.136.1 dev brvlan0
	router := localRouter(ring)
	cmd = exec.Command(plat.IPCmd, "addr", "add", router, "dev", bridge)
	log.Printf("Setting %s to %s\n", bridge, router)
	if err := cmd.Run(); err != nil {
		log.Fatalf("Failed to set the router address: %v\n", err)
	}

	// ip link set up brvlan0
	cmd = exec.Command(plat.IPCmd, "link", "set", "up", bridge)
	if err := cmd.Run(); err != nil {
		log.Fatalf("Failed to enable bridge: %v\n", err)
	}
	// ip route add 192.168.136.0/24 dev brvlan0
	cmd = exec.Command(plat.IPCmd, "route", "add", ring.Subnet, "dev", bridge)
	if err := cmd.Run(); err != nil {
		log.Fatalf("Failed to add %s as the new route: %v\n",
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

func newNicOps(id string, nic *physDevice) []apcfg.PropertyOp {
	base := "@/nodes/" + nodeUUID + "/nics/" + id

	ops := make([]apcfg.PropertyOp, 0)
	if nic == nil {
		op := apcfg.PropertyOp{
			Op:   apcfg.PropDelete,
			Name: base,
		}
		ops = append(ops, op)
	} else {
		var kind string
		if nic.wifi == nil {
			kind = "wired"
		} else {
			kind = "wireless"
		}

		op := apcfg.PropertyOp{
			Op:    apcfg.PropCreate,
			Name:  base + "/kind",
			Value: kind,
		}
		log.Printf("Setting %s to %s\n", id, op.Value)
		ops = append(ops, op)

		op = apcfg.PropertyOp{
			Op:    apcfg.PropCreate,
			Name:  base + "/ring",
			Value: nic.ring,
		}
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
	nics := make(apcfg.ChildMap)
	if r, _ := config.GetProps("@/nodes/" + nodeUUID + "/nics"); r != nil {
		nics = r.Children
	}

	// Examine each entry in the config tree to determine whether it matches
	// our current inventory.
	ops := make([]apcfg.PropertyOp, 0)
	for id, nic := range nics {
		var ring string
		var newOps []apcfg.PropertyOp

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
		if _, err := config.Execute(ops); err != nil {
			log.Printf("Error updating NIC inventory: %v\n", err)
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
		log.Fatalf("Failed to enable packet forwarding: %v\n", err)
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
					log.Printf("Multiple wan nics found.  "+
						"Using: %s\n", wan.hwaddr)
				}
			}
		}
	}

	if available == nil {
		log.Printf("couldn't find a outgoing device to use")
		return
	}
	if wan == nil {
		wan = available
		log.Printf("No outgoing device configured.  Using %s\n", wan.hwaddr)
		wan.ring = outgoingRing
	}

	wanNic = wan.name
	return
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
		log.Printf("Skipping emulated device %s (%s)\n",
			d.name, d.hwaddr)
		return nil
	}

	if d.wifi, err = iwinfo(d.name); err != nil {
		log.Printf("Couldn't determine wifi capabilities of %s: %v\n",
			d.name, err)
		return nil
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
		log.Fatalf("Unable to inventory network devices: %v\n", err)
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

func makeValidChannelMaps() {
	// XXX: These (valid 20MHz) channel lists are legal for the US.  We will
	// need to ship per-country lists, presumably indexed by regulatory
	// domain.  In the US, with the exception of channel 165, all these
	// channels are valid primaries for 40MHz channels (see initChannelLists
	// in hostapd.go), though that is not true for all countries, and is not
	// true for 160MHz channels, once we start supporting those.
	channels := map[string][]int{
		loBand: {1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11},
		hiBand: {36, 40, 44, 48, 52, 56, 60, 64, 100, 104, 108, 112,
			116, 120, 124, 128, 132, 136, 140, 144, 149, 153, 157,
			161, 165},
	}

	// Convert the arrays of channels into channel-indexed maps, for easier
	// lookup.
	legalChannels = make(map[string]map[int]bool)
	for _, band := range bands {
		legalChannels[band] = make(map[int]bool)
		for _, channel := range channels[band] {
			legalChannels[band][channel] = true
		}
	}
}

func globalWifiInit(props *apcfg.PropertyNode) error {
	locationRE := regexp.MustCompile(`^[A-Z][A-Z]$`)

	makeValidChannelMaps()

	domain := "US"
	if x, ok := props.Children["regdomain"]; ok {
		t := []byte(strings.ToUpper(x.Value))
		if !locationRE.Match(t) {
			log.Printf("Illegal @/network/regdomain: %s\n", x.Value)
		} else {
			domain = x.Value
		}
	}

	log.Printf("Setting regulatory domain to %s\n", domain)
	out, err := exec.Command(plat.IwCmd, "reg", "set", domain).CombinedOutput()
	if err != nil {
		log.Printf("Failed to set domain: %v\n%s\n", err, out)
	}

	if node, ok := props.Children["radiusAuthSecret"]; ok {
		radiusSecret = node.Value
	} else {
		log.Printf("no radiusAuthSecret configured")
	}
	if node, ok := props.Children["ssid"]; ok {
		wifiSSID = node.Value
	} else {
		return fmt.Errorf("no SSID configured")
	}

	if node, ok := props.Children["passphrase"]; ok {
		wifiPassphrase = node.Value
	} else {
		return fmt.Errorf("no WPA-PSK passphrase configured")
	}

	wifiChannels = make(map[string]int)
	for _, band := range bands {
		if bprop, ok := props.Children[band]; ok {
			if c, ok := bprop.Children["channel"]; ok {
				wifiChannels[band], _ = strconv.Atoi(c.Value)
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
	nodeUUID, err := plat.GetNodeID()
	if err != nil {
		return "", err
	}

	prop := "@/nodes/" + nodeUUID + "/platform"
	oldName, _ := config.GetProp(prop)
	newName := plat.GetPlatform()

	if oldName != newName {
		if oldName == "" {
			log.Printf("Setting %s to %s\n", prop, newName)
		} else {
			log.Printf("Changing %s from %s to %s\n", prop,
				oldName, newName)
		}
		if err := config.CreateProp(prop, newName, nil); err != nil {
			log.Printf("failed to update %s: %v", prop, err)
		}
	}
	return nodeUUID, nil
}

// Connect to all of the other brightgate daemons and construct our initial model
// of the system
func daemonInit() error {
	var err error

	if mcpd, err = mcp.New(pname); err != nil {
		log.Printf("cannot connect to mcp: %v\n", err)
	} else {
		mcpd.SetState(mcp.INITING)
	}

	brokerd = broker.New(pname)

	config, err = apcfg.NewConfig(brokerd, pname, apcfg.AccessInternal)
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

	log.Printf("Received signal %v\n", s)
	running = false
	hostapd.reset()
}

func prometheusInit() {
	http.Handle("/metrics", promhttp.Handler())
	go http.ListenAndServe(base_def.NETWORKD_PROMETHEUS_PORT, nil)
}

func main() {
	rand.Seed(time.Now().Unix())
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	flag.Parse()
	*templateDir = aputil.ExpandDirPath(*templateDir)
	*rulesDir = aputil.ExpandDirPath(*rulesDir)

	plat = platform.NewPlatform()
	prometheusInit()
	networkCleanup()
	if err := daemonInit(); err != nil {
		if mcpd != nil {
			mcpd.SetState(mcp.BROKEN)
		}
		log.Fatalf("networkd failed to start: %v\n", err)
	}

	applyFilters()

	running = true
	go signalHandler()

	resetInterfaces()
	mcpd.SetState(mcp.ONLINE)
	runLoop()

	log.Printf("Cleaning up\n")
	networkCleanup()
}
