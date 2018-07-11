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

var (
	allow40MHz  = flag.Bool("40mhz", false, "allow 40MHz-wide channels")
	templateDir = flag.String("template_dir", "golang/src/ap.networkd",
		"location of hostapd templates")
	rulesDir       = flag.String("rules_dir", "./", "Location of the filter rules")
	hostapdLatency = flag.Int("hl", 5, "hostapd latency limit (seconds)")
	deadmanTimeout = flag.Duration("deadman", 5*time.Second,
		"time to wait for hostapd cleanup to complete")

	physDevices    = make(map[string]*physDevice)
	perModeDevices = make(map[string]map[*physDevice]bool)
	modes          = []string{"ac", "g"}

	mcpd     *mcp.MCP
	brokerd  *broker.Broker
	config   *apcfg.APConfig
	clients  apcfg.ClientMap // macaddr -> ClientInfo
	rings    apcfg.RingMap   // ring -> config
	nodeUUID string

	wifiCapable    bool
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

	cfgMode       string // configured mode
	activeMode    string // mode being used
	cfgChannel    int    // configured channel
	activeChannel int    // channel being used

	wireless     bool             // is this a wireless nic?
	supportVLANs bool             // does the nic support VLANs
	interfaces   int              // number of APs it can support
	channels     map[string][]int // Supported channels per-mode
}

// Per-mode, per-width valid channel lists
var validGChannels map[int]bool
var validACChannels map[int]map[int]bool

// Precompile some regular expressions
var (
	locationRE = regexp.MustCompile(`^[A-Z][A-Z]$`)

	vlanRE = regexp.MustCompile(`AP/VLAN`)

	// Match interface combination lines:
	//   #{ managed } <= 1, #{ AP } <= 1, #{ P2P-client } <= 1
	comboRE = regexp.MustCompile(`#{ [\w\-, ]+ } <= [0-9]+`)

	// Match channel/frequency lines:
	//   * 2462 MHz [11] (20.0 dBm)
	channelRE = regexp.MustCompile(`\* (\d+) MHz \[(\d+)\] \((.*)\)`)

	// Match capabilities line:
	// Capabilities: 0x2fe
	capabilitiesRE = regexp.MustCompile(`Capabilities: 0x([[:xdigit:]]+)`)
)

//////////////////////////////////////////////////////////////////////////
//
// Interaction with the rest of the ap daemons
//

func configChannelChanged(path []string, val string, expires *time.Time) {
	nicID := path[3]
	newChannel, _ := strconv.Atoi(val)

	if p := physDevices[nicID]; p != nil {
		if p.cfgChannel != newChannel {
			p.cfgChannel = newChannel
			hostapd.reload()
		}
	}
}

func configModeChanged(path []string, val string, expires *time.Time) {
	nicID := path[3]
	newMode := val

	if p := physDevices[nicID]; p != nil {
		if p.cfgMode != newMode {
			p.cfgMode = newMode
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
	// Watch for changes to the network conf
	switch path[1] {
	case "ssid":
		wifiSSID = val
		log.Printf("SSID changed to %s\n", val)

	case "passphrase":
		wifiPassphrase = val
		log.Printf("passphrase changed to %s\n", val)

	case "radiusAuthSecret":
		radiusSecret = val
		log.Printf("radiusAuthSecret changed to %s\n", val)

	default:
		return
	}
	hostapd.reload()
}

func selectWifiChannel(mode string, d *physDevice) {
	d.activeChannel = 0

	if d.cfgChannel > 0 {
		for _, x := range d.channels[mode] {
			if x == d.cfgChannel {
				d.activeChannel = d.cfgChannel
				return
			}
			log.Printf("%s is configured with channel %d, not "+
				"valid for mode %s\n",
				d.hwaddr, d.cfgChannel, mode)
		}
	}
	if mode == "g" {
		d.activeChannel = 6
	} else {
		n := rand.Int() % len(d.channels[mode])
		d.activeChannel = d.channels[mode][n]
	}
}

func selectWifiDevices() []*physDevice {
	active := make(map[string]*physDevice)
	config := make(map[string]*physDevice)

	// Identify which devices are being used for each mode now, and whether
	// the user has expressed a preference for one or more devices.
	for _, p := range physDevices {
		if p.activeMode != "" {
			active[p.activeMode] = p
		}
		if p.cfgMode != "" {
			config[p.cfgMode] = p
		}
	}

	// If the user has configured a particular NIC for any mode, make it the
	// active one.
	for _, mode := range modes {
		if nic := config[mode]; nic != nil {
			// If another NIC was being used, clear it
			if active[mode] != nil {
				active[mode].activeMode = ""
			}
			if perModeDevices[mode][nic] {
				active[mode] = nic
			}
		}
	}

	// If the user didn't configure an 802.11g NIC, choose one
	acList := perModeDevices["ac"]
	gList := perModeDevices["g"]
	if active["g"] == nil {
		var available *physDevice

		for p := range gList {
			if len(acList) == 1 && acList[p] {
				// This is the only 802.11ac-capable NIC.  Keep
				// looking for another option to use for mode g.
				available = p
				continue
			}
			active["g"] = p
			break
		}
		if active["g"] == nil && config["ac"] != available {
			// The only device available for a 'g' network is also
			// the only device available for 'ac'.  We will use it
			// for 'g' unless the user has explicitly indicated that
			// they want it to support 'ac'.
			active["g"] = available
		}
	}

	// If the user didn't configure an 802.11ac NIC, choose one
	if active["ac"] == nil {
		for p := range acList {
			if p != active["g"] {
				active["ac"] = p
				break
			}
		}
	}

	selected := make([]*physDevice, 0)
	for _, mode := range modes {
		if nic := active[mode]; nic != nil {
			nic.activeMode = mode
			selectWifiChannel(mode, nic)
			selected = append(selected, nic)
		}
	}

	return selected
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
		if !dev.wireless && !plat.NicIsVirtual(dev.name) &&
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

	for running {
		var selected []*physDevice

		if wifiCapable {
			selected = selectWifiDevices()
		}

		if len(selected) > 0 {
			startTimes = append(startTimes[1:failuresAllowed],
				time.Now())

			hostapd = startHostapd(selected)
			if err := hostapd.wait(); err != nil {
				log.Printf("%v\n", err)
			}
			hostapd = nil

			if time.Since(startTimes[0]) < period {
				log.Printf("hostapd is dying too quickly")
				wifiCapable = false
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
		if nic.wireless {
			kind = "wireless"
		} else {
			kind = "wired"
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
				ok = (x.Value == "wireless" && dev.wireless) ||
					(x.Value == "wired" && !dev.wireless)
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
		if dev.wireless {
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

//
// Identify all of the wifi devices that support the capabilities we need
//
func prepareWireless() {
	for _, mode := range modes {
		perModeDevices[mode] = make(map[*physDevice]bool)
	}

	for _, dev := range physDevices {
		if !dev.wireless {
			continue
		}

		if !dev.supportVLANs {
			log.Printf("%s doesn't support VLANs\n", dev.hwaddr)
			continue
		}

		if dev.interfaces <= 1 {
			log.Printf("%s doesn't support multiple SSIDs\n",
				dev.hwaddr)
			continue
		}

		if dev.ring != "" && dev.ring != base_def.RING_UNENROLLED {
			log.Printf("%s reserved for %s\n", dev.hwaddr, dev.ring)
		}

		for _, mode := range modes {
			if len(dev.channels[mode]) > 0 {
				perModeDevices[mode][dev] = true
			}
		}
	}

	for _, mode := range modes {
		if len(perModeDevices[mode]) > 0 {
			wifiCapable = true
		}
	}
}

func getEthernet(i net.Interface) *physDevice {
	d := physDevice{
		name:         i.Name,
		hwaddr:       i.HardwareAddr.String(),
		wireless:     false,
		supportVLANs: false,
	}
	return &d
}

//
// Given the name of a wireless NIC, construct a device structure for it
func getWireless(i net.Interface) *physDevice {
	d := physDevice{
		name:     i.Name,
		hwaddr:   i.HardwareAddr.String(),
		wireless: true,
	}

	if strings.HasPrefix(d.hwaddr, "02:00") {
		log.Printf("Skipping emulated device %s (%s)\n",
			d.name, d.hwaddr)
		return nil
	}

	d.channels = make(map[string][]int)
	for _, mode := range modes {
		d.channels[mode] = make([]int, 0)
	}

	data, err := ioutil.ReadFile("/sys/class/net/" + i.Name +
		"/phy80211/name")
	if err != nil {
		log.Printf("Couldn't get phy for %s: %v\n", i.Name, err)
		return nil
	}
	phy := strings.TrimSpace(string(data))

	//
	// The following is a hack.  This should (and will) be accomplished by
	// asking the nl80211 layer through the netlink interface.
	//
	out, err := exec.Command(plat.IwCmd, "phy", phy, "info").Output()
	if err != nil {
		log.Printf("Failed to get %s capabilities: %v\n", i.Name, err)
		return nil
	}
	capabilities := string(out)

	//
	// Look for "AP/VLAN" as a supported "software interface mode"
	//
	vlanModes := vlanRE.FindAllStringSubmatch(capabilities, -1)
	d.supportVLANs = (len(vlanModes) > 0)

	//
	// Examine the "valid interface combinations" to see if any include more
	// than one AP.  This one does:
	//    #{ AP, mesh point } <= 8,
	// This one doesn't:
	//    #{ managed } <= 1, #{ AP } <= 1, #{ P2P-client } <= 1,
	//
	combos := comboRE.FindAllStringSubmatch(capabilities, -1)
	for _, line := range combos {
		for _, combo := range line {
			s := strings.Split(combo, " ")
			if len(s) > 0 && strings.Contains(combo, "AP") {
				d.interfaces, _ = strconv.Atoi(s[len(s)-1])
			}
		}
	}

	// Figure out which frequency widths this device supports
	widths := make(map[int]bool)
	bandCapabilities := capabilitiesRE.FindAllStringSubmatch(capabilities, -1)
	for _, c := range bandCapabilities {
		// If bit 1 is set, then the device supports both 20MHz and
		// 40MHz.  If it isn't, then it only supports 20MHz.
		widths[20] = true
		if *allow40MHz {
			if len(c) == 2 {
				flags, _ := strconv.ParseUint(c[1], 16, 64)
				if (flags & (1 << 1)) != 0 {
					widths[40] = true
				}
			}
		}
	}

	// Find all the available channels and bin them into the appropriate
	// per-mode groups
	channels := channelRE.FindAllStringSubmatch(capabilities, -1)
	supported := 0
	for _, line := range channels {
		// Skip any channels that are unavailable for either technical
		// or regulatory reasons
		if strings.Contains(line[3], "disabled") ||
			strings.Contains(line[3], "no IR") ||
			strings.Contains(line[3], "radar detection") {
			continue
		}
		channel, _ := strconv.Atoi(line[2])

		mode := "unsupported"
		if validGChannels[channel] {
			mode = "g"
		} else {
			for w := range widths {
				for c := range validACChannels[w] {
					if channel == c {
						mode = "ac"
					}
				}
			}
		}

		if _, ok := d.channels[mode]; ok {
			d.channels[mode] = append(d.channels[mode], channel)
			supported++
		}
	}

	if supported == 0 {
		log.Printf("No supported channels found for %s\n", d.hwaddr)
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
				if x, ok := nic.Children["mode"]; ok {
					d.cfgMode = x.Value
				}
				if x, ok := nic.Children["channel"]; ok {
					d.cfgChannel, _ = strconv.Atoi(x.Value)
				}
			}
		}
	}
}

// Hardcode the valid channels for each mode
func makeValidChannelMaps() {
	validGChannels = make(map[int]bool)
	for i := 1; i <= 14; i++ {
		validGChannels[i] = true
	}

	HT20 := []int{36, 40, 44, 48, 52, 56, 60, 64, 100, 104, 108, 112, 116,
		120, 124, 128, 132, 136, 140, 144, 149, 153, 161, 165, 169}
	HT40 := []int{38, 46, 54, 62, 102, 110, 118,
		126, 134, 142, 151, 159}
	HT80 := []int{42, 58, 106, 122, 138, 155}

	// For AC mode, we need to build lists of channels for each channel
	// width.  Some wifi devices will claim to support some channels, even
	// if they can't support the associated channel width.  (presumably
	// because their radio covers that frequency.)
	validACChannels = make(map[int]map[int]bool)
	validACChannels[20] = make(map[int]bool)
	for _, c := range HT20 {
		validACChannels[20][c] = true
	}

	validACChannels[40] = make(map[int]bool)
	for _, c := range HT40 {
		validACChannels[40][c] = true
	}

	validACChannels[80] = make(map[int]bool)
	for _, c := range HT80 {
		validACChannels[80][c] = true
	}
}

func globalWifiInit(props *apcfg.PropertyNode) error {
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
	config.HandleChange(`^@/nodes/"+nodeUUID+"/nics/.*/mode$`, configModeChanged)
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
	prepareWireless()

	// All wired devices that haven't yet been assigned to a ring will be
	// put into "standard" by default
	for _, dev := range physDevices {
		if !dev.wireless && dev.ring == "" {
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
