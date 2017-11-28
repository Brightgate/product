/*
 * COPYRIGHT 2017 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 *
 */

/*
 * hostapd instance monitor
 *
 * Responsibilities:
 * - to run one instance of hostapd
 * - to create a configuration file for that hostapd instance that reflects the
 *   desired configuration state of the appliance
 * - to restart or signal that hostapd instance when a relevant configuration
 *   event is received
 * - to emit availability events when the hostapd instance fails or is
 *   launched
 *
 * Questions:
 * - does a monitor offer statistics to Prometheus?
 * - can we update ourselves if the template file is updated (by a
 *   software update)?
 */

package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"text/template"
	"time"

	"bg/ap_common/apcfg"
	"bg/ap_common/aputil"
	"bg/ap_common/broker"
	"bg/ap_common/mcp"
	"bg/ap_common/network"
	"bg/base_def"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	addr = flag.String("listen-address", base_def.HOSTAPDM_PROMETHEUS_PORT,
		"address to listen on for HTTP requests")
	platform = flag.String("platform", "rpi3",
		"hardware platform name")
	templateDir = flag.String("template_dir", "golang/src/ap.networkd",
		"location of hostapd templates")
	rulesDir = flag.String("rules_dir", "./", "Location of the filter rules")

	aps         = make(apMap)
	physDevices = make(physDevMap)

	config  *apcfg.APConfig
	clients apcfg.ClientMap // macaddr -> ClientInfo
	rings   apcfg.RingMap   // ring -> config

	setupNic    string
	wanNic      string
	wifiNic     string
	meshNodeIdx byte

	hostapdLog   *log.Logger
	childProcess *os.Process // track the hostapd proc
	mcpd         *mcp.MCP

	running      bool
	setupNetwork bool
)

type apMap map[string]*apConfig
type physDevMap map[string]*physDevice

const (
	// Allow up to 4 failures in a 1 minute period before giving up
	failuresAllowed = 4
	period          = time.Duration(time.Minute)

	confdir        = "/tmp"
	hostapdPath    = "/usr/sbin/hostapd"
	hostapdOptions = "-dKt"
	brctlCmd       = "/sbin/brctl"
	sysctlCmd      = "/sbin/sysctl"
	ipCmd          = "/sbin/ip"
	vconfigCmd     = "/sbin/vconfig"
	pname          = "ap.networkd"
	setupPortal    = "_0"
)

type physDevice struct {
	name         string
	hwaddr       string
	ring         string
	wireless     bool
	supportVLANs bool
	multipleAPs  bool
}

type apConfig struct {
	// Fields used to populate the configuration template
	Interface     string // Linux device name
	HWaddr        string // Mac address to use
	SSID          string
	Passphrase    string
	HardwareModes string
	Channel       int
	SetupSSID     string // SSID to broadcast for setup network
	SetupComment  string // Used to disable setup net in .conf template
	ConfDir       string // Location of hostapd.conf, etc.

	confFile string // Name of this NIC's hostapd.conf

	status error // collect hostapd failures
}

//////////////////////////////////////////////////////////////////////////
//
// Interaction with the rest of the ap daemons
//

func apReset(conf *apConfig) {
	generateHostAPDConf(conf)
	if childProcess != nil {
		//
		// A SIGHUP will cause hostapd to reload its configuration.
		// However, it seems that we really need to kill and restart the
		// process for the changes to be propagated down to the wifi
		// hardware
		//
		childProcess.Signal(syscall.SIGINT)
	}
}

func configRingChanged(path []string, val string, expires *time.Time) {
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

	conf := aps[wifiNic]
	apReset(conf)
}

func configNetworkChanged(path []string, val string, expires *time.Time) {
	conf := aps[wifiNic]

	// Watch for changes to the network conf
	switch path[1] {
	case "ssid":
		conf.SSID = val

	case "passphrase":
		conf.Passphrase = val

	case "setupssid":
		conf.SetupSSID = val

	default:
		return
	}
	apReset(conf)
}

//
// Replace the final nybble of a mac address to match the transformations
// hostapd performs to support multple SSIDs
//
func macUpdateLastOctet(mac string, nybble uint64) string {
	octets := strings.Split(mac, ":")
	if len(octets) == 6 {
		b, _ := strconv.ParseUint(octets[5], 16, 32)
		newNybble := (b & 0xf0) | nybble
		if newNybble != b {
			octets[5] = fmt.Sprintf("%02x", newNybble)

			// Since we changed the mac address, we need to set the
			// 'locally administered' bit in the first octet
			b, _ = strconv.ParseUint(octets[0], 16, 32)
			b |= 0x02 // Set the "locally administered" bit
			octets[0] = fmt.Sprintf("%02x", b)
			mac = strings.Join(octets, ":")
		}
	} else {
		log.Printf("invalid mac address: %s", mac)
	}
	return mac
}

//
// Get network settings from configd and use them to initialize the AP
//
func getAPConfig(d *physDevice, props *apcfg.PropertyNode) error {
	var ssid, passphrase, setupSSID string
	var setupComment string
	var node *apcfg.PropertyNode

	meshNode := aputil.IsMeshMode()
	apSuffix := ""
	if meshNode {
		// XXX - for now we'll give mesh nodes their own SSID.  This
		// lets us sort out the layer 2/3 plumbing issues without having
		// to worry about AP handoff
		apSuffix += "-mesh"
	}

	if node = props.GetChild("ssid"); node == nil {
		return fmt.Errorf("no SSID configured")
	}
	ssid = node.GetValue() + apSuffix

	if node = props.GetChild("passphrase"); node == nil {
		return fmt.Errorf("no passphrase configured")
	}
	passphrase = node.GetValue()

	if node = props.GetChild("setupssid"); node != nil {
		setupSSID = node.GetValue() + apSuffix
	}

	if !meshNode && d.multipleAPs && len(setupSSID) > 0 {
		// If we create a second SSID for new clients to connect to,
		// its mac address will be derived from the nic's mac address by
		// adding 1 to the final octet.  To accomodate that, hostapd
		// wants the final nybble of the final octet to be 0.
		newMac := macUpdateLastOctet(d.hwaddr, 0)
		if newMac != d.hwaddr {
			log.Printf("Changed mac from %s to %s\n", d.hwaddr, newMac)
			d.hwaddr = newMac
		}
		setupComment = ""
		setupNetwork = true
	} else {
		setupComment = "#"
		setupNetwork = false
	}

	data := apConfig{
		Interface:     d.name,
		HWaddr:        d.hwaddr,
		SSID:          ssid,
		HardwareModes: "g",
		Channel:       6,
		Passphrase:    passphrase,
		SetupComment:  setupComment,
		SetupSSID:     setupSSID,
		ConfDir:       confdir,

		confFile: "hostapd.conf." + d.name,
	}
	aps[d.name] = &data
	return nil
}

//////////////////////////////////////////////////////////////////////////
//
// hostapd configuration and monitoring
//

//
// Generate the 3 configuration files needed for hostapd.
//
func generateHostAPDConf(conf *apConfig) string {
	var err error
	tfile := *templateDir + "/hostapd.conf.got"

	// Create hostapd.conf, using the apConfig contents to fill out the .got
	// template
	t, err := template.ParseFiles(tfile)
	if err != nil {
		log.Fatal(err)
		os.Exit(2)
	}

	fn := conf.ConfDir + "/" + conf.confFile
	cf, _ := os.Create(fn)
	defer cf.Close()

	err = t.Execute(cf, conf)
	if err != nil {
		log.Fatal(err)
		os.Exit(1)
	}

	// Create the 'accept_macs' file, which tells hostapd how to map clients
	// to VLANs.
	mfn := conf.ConfDir + "/" + "hostapd.macs"
	mf, err := os.Create(mfn)
	if err != nil {
		log.Fatalf("Unable to create %s: %v\n", mfn, err)
	}
	defer mf.Close()

	// One client per line, containing "<mac addr> <vlan_id>"
	for client, info := range clients {
		vlan := 0
		if ring, ok := rings[info.Ring]; ok {
			vlan = ring.Vlan
		}
		if vlan > 0 {
			fmt.Fprintf(mf, "%s %d\n", client, vlan)
		}
	}

	// Create the 'vlan' file, which tells hostapd which vlans to create
	vfn := conf.ConfDir + "/" + "hostapd.vlan"
	vf, err := os.Create(vfn)
	if err != nil {
		log.Fatalf("Unable to create %s: %v\n", vfn, err)
	}
	defer vf.Close()

	for _, ring := range rings {
		if ring.Vlan > 1 {
			fmt.Fprintf(vf, "%d\tvlan.%d\n", ring.Vlan, ring.Vlan)
		}
	}

	return fn
}

//
// When we get a signal, set the 'running' flag to false and signal any hostapd
// process we're monitoring.  We want to be sure the wireless interface has been
// released before we give mcp a chance to restart the whole stack.
//
func signalHandler() {
	attempts := 0
	sig := make(chan os.Signal)
	for {
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig

		running = false
		if childProcess != nil {
			if attempts < 5 {
				childProcess.Signal(syscall.SIGINT)
			} else {
				childProcess.Signal(syscall.SIGKILL)
			}
			attempts++
		}
	}
}

// Create a ring's network bridge.  If a nic is provided, attach it to the
// bridge.
func createBridge(ring *apcfg.RingConfig, nic string) {
	bridge := ring.Bridge

	err := exec.Command(brctlCmd, "addbr", bridge).Run()
	if err != nil {
		log.Printf("addbr %s failed: %v", bridge, err)
		return
	}

	err = exec.Command(ipCmd, "link", "set", "up", bridge).Run()
	if err != nil {
		log.Printf("bridge %s failed to come up: %v", bridge, err)
		return
	}

	if nic != "" {
		err = exec.Command(brctlCmd, "addif", bridge, nic).Run()
		if err != nil {
			log.Printf("addif %s %s failed: %v", bridge, nic, err)
		}
	}
}

func lanRing(ring string) bool {
	return (ring != base_def.RING_SETUP && ring != base_def.RING_INTERNAL)
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

func rebuildInternalNet() {
	meshNode := aputil.IsMeshMode()

	if !meshNode {
		prepareRingBridge(base_def.RING_INTERNAL)
	}

	br := rings[base_def.RING_INTERNAL].Bridge

	// For each internal network device, create a virtual device for each
	// LAN ring and attach it to the bridge for that ring
	for _, dev := range physDevices {
		if dev.ring != base_def.RING_INTERNAL {
			continue
		}

		if !meshNode {
			log.Printf("Adding %s to bridge %s\n", dev.name, br)
			err := exec.Command(brctlCmd, "addif", br, dev.name).Run()
			if err != nil {
				log.Printf("failed to add %s to %s: %v\n",
					dev.name, br, err)
				continue
			}
		}
		for name, ring := range rings {
			if lanRing(name) {
				addVif(dev.name, ring.Vlan)
			}
		}
	}
}

func rebuildLan() {
	for ring := range rings {
		if lanRing(ring) {
			prepareRingBridge(ring)
		}
	}

	// Connect all the wired LAN NICs to ring-appropriate bridges.
	for _, dev := range physDevices {
		name := dev.name
		ring := rings[dev.ring]

		if dev.wireless || ring == nil || !lanRing(dev.ring) {
			continue
		}

		br := ring.Bridge
		log.Printf("Connecting %s (%s) to the %s bridge: %s\n",
			name, dev.hwaddr, dev.ring, br)
		err := exec.Command(brctlCmd, "addif", br, name).Run()
		if err != nil {
			log.Printf("Failed to add %s: %v\n", name, err)
		}
	}
}

func resetInterfaces() {

	// We use the lowest byte of our internal IP address as a transient,
	// local node index.  For the gateway node, that will always be 1.  For
	// the mesh nodes, we need pull it from the address the gateway's DHCP
	// server gave us.
	meshNodeIdx = 1
	if aputil.IsMeshMode() {
		ip := getInternalAddr()
		if ip == nil {
			log.Printf("Mesh node has no gateway connection\n")
			return
		}
		meshNodeIdx = ip[3]
	}

	// hostapd creates most of the per-ring bridges.  We need to create
	// these two manually.
	createBridge(rings[base_def.RING_UNENROLLED], wifiNic)
	createBridge(rings[base_def.RING_INTERNAL], "")

	if setupNetwork {
		prepareRingBridge(base_def.RING_SETUP)
	}

	rebuildLan()
	rebuildInternalNet()
}

//
// Launch, monitor, and maintain the hostapd process for a single interface
//
func runOne(conf *apConfig, done chan *apConfig) {
	fn := generateHostAPDConf(conf)

	startTimes := make([]time.Time, failuresAllowed)
	for running {
		deleteBridges()

		child := aputil.NewChild(hostapdPath, fn)
		child.LogOutput("hostapd: ", log.Ldate|log.Ltime)

		startTime := time.Now()
		startTimes = append(startTimes[1:failuresAllowed], startTime)

		log.Printf("Starting hostapd for %s\n", conf.Interface)

		if err := child.Start(); err != nil {
			conf.status = fmt.Errorf("failed to launch: %v", err)
			break
		}

		childProcess = child.Process
		resetInterfaces()
		mcpd.SetState(mcp.ONLINE)

		child.Wait()

		log.Printf("hostapd for %s exited after %s\n",
			conf.Interface, time.Since(startTime))
		if time.Since(startTimes[0]) < period {
			conf.status = fmt.Errorf("dying too quickly")
			break
		}

		// Give everything a chance to settle before we attempt to
		// restart the daemon and reconfigure the wifi hardware
		time.Sleep(time.Second)
	}
	done <- conf
}

//
// Kick off the monitor routines for all of our NICs, and then wait until
// they've all exited.  (Since we only support a single AP right now, this is
// overkill, but harmless.)
//
func runAll() int {
	done := make(chan *apConfig)
	running := 0
	errors := 0

	for _, c := range aps {
		if c.Interface == wifiNic {
			running++
			go runOne(c, done)
		}
	}

	for running > 0 {
		c := <-done
		if c.status != nil {
			log.Printf("%s hostapd failed: %v\n", c.Interface,
				c.status)
			errors++
		} else {
			log.Printf("%s hostapd exited\n", c.Interface)
		}
		running--
	}
	deleteBridges()

	return errors
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
	err := exec.Command(vconfigCmd, "add", nic, vid).Run()
	if err != nil {
		log.Printf("Failed to create vif %s: %v\n", vif, err)
		return
	}

	err = exec.Command(brctlCmd, "addif", bridge, vif).Run()
	if err != nil {
		log.Printf("Failed to add %s to %s: %v\n", vif, bridge, err)
		return
	}

	err = exec.Command(ipCmd, "link", "set", "up", vif).Run()
	if err != nil {
		log.Printf("Failed to enable %s: %v\n", vif, err)
	}
}

func deleteVif(vif string) {
	exec.Command(ipCmd, "link", "del", vif).Run()
}

func deleteBridge(bridge string) {
	exec.Command(ipCmd, "link", "set", "down", bridge).Run()
	exec.Command(brctlCmd, "delbr", bridge).Run()
}

// Delete the bridges associated with each ring.  This gets us back to a known
// ground state, simplifying the task of rebuilding everything when hostapd
// starts back up.
func deleteBridges() {
	for _, conf := range rings {
		deleteBridge(conf.Bridge)
	}
}

//
// Determine the address to be used for the given ring's router on this node.
// If the AP has an address of 192.168.131.x on the internal subnet, then the
// router for each ring will be the corresponding .x address in that ring's
// subnet.
func localRouter(ring *apcfg.RingConfig) string {
	_, network, _ := net.ParseCIDR(ring.Subnet)
	raw := network.IP.To4()
	raw[3] = meshNodeIdx
	return (net.IP(raw)).String()
}

//
// Prepare a ring's bridge: clean up any old state, assign a new address, set up
// routes, etc.
//
func prepareRingBridge(ringName string) {
	ring := rings[ringName]
	bridge := ring.Bridge

	log.Printf("Preparing %s ring: %s %s\n", ringName, bridge, ring.Subnet)

	// The 'internal' bridge is a special case, since it will not get a
	// 'link up' until/unless the NIC is attached.  The other bridges
	// already have wireless NICs attached, and we expect them to be in the
	// 'link up' state already.
	if ringName != "internal" {
		err := network.WaitForDevice(bridge, 5*time.Second)
		if err != nil {
			log.Printf("%s failed to come online: %v\n", bridge, err)
			return
		}
	}

	// ip addr flush dev wlan0
	cmd := exec.Command(ipCmd, "addr", "flush", "dev", bridge)
	if err := cmd.Run(); err != nil {
		log.Fatalf("Failed to remove existing IP address: %v\n", err)
	}

	// ip route del 192.168.136.0/24
	cmd = exec.Command(ipCmd, "route", "del", ring.Subnet)
	cmd.Run()

	// ip addr add 192.168.136.1 dev wlan0
	router := localRouter(ring)
	cmd = exec.Command(ipCmd, "addr", "add", router, "dev", bridge)
	if err := cmd.Run(); err != nil {
		log.Fatalf("Failed to set the router address: %v\n", err)
	}

	// ip link set up wlan0
	cmd = exec.Command(ipCmd, "link", "set", "up", bridge)
	if err := cmd.Run(); err != nil {
		log.Fatalf("Failed to enable bridge: %v\n", err)
	}
	// ip route add 192.168.136.0/24 dev wlan0
	cmd = exec.Command(ipCmd, "route", "add", ring.Subnet, "dev", bridge)
	if err := cmd.Run(); err != nil {
		log.Fatalf("Failed to add %s as the new route: %v\n",
			ring.Subnet, err)
	}
}

func persistNicRing(mac, ring string) {
	node := aputil.GetNodeID().String()
	prop := "@/nodes/" + node + "/" + mac + "/ring"
	err := config.CreateProp(prop, ring, nil)
	if err != nil {
		log.Printf("Failed to persist %s as %s: %v\n", mac, ring, err)
	}
}

//
// Identify and prepare the WAN port.
//
func prepareWan() {
	var err error
	var available, wan *physDevice
	var outgoingRing string

	if aputil.IsMeshMode() {
		outgoingRing = base_def.RING_INTERNAL
	} else {
		outgoingRing = base_def.RING_WAN
	}

	// Enable packet forwarding
	cmd := exec.Command(sysctlCmd, "-w", "net.ipv4.ip_forward=1")
	if err = cmd.Run(); err != nil {
		log.Fatalf("Failed to enable packet forwarding: %v\n", err)
	}

	// Find the WAN device
	for name, dev := range physDevices {
		if dev.wireless {
			// XXX - at some point we should investigate using a
			// wireless link as a mesh backhaul
			continue
		}

		// On Raspberry Pi 3, use the OUI to identify the
		// on-board port.
		if *platform == "rpi3" {
			if !strings.HasPrefix(dev.hwaddr, "b8:27:eb:") {
				continue
			}
		} else if !strings.HasPrefix(name, "eth") &&
			!strings.HasPrefix(name, "enx") {
			continue
		}

		available = dev
		if dev.ring == outgoingRing {
			if wan == nil {
				wan = dev
			} else {
				log.Printf("Multiple outgoing nics configured.  "+
					"Using: %s\n", wan.hwaddr)
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
		persistNicRing(wan.hwaddr, wan.ring)
	}

	wanNic = wan.name
	return
}

//
// Choose a wifi NIC to host our wireless clients, and build a list of the
// wireless interfaces we'll be supporting
//
func prepareWireless(props *apcfg.PropertyNode) error {
	var available, wifi *physDevice

	for _, dev := range physDevices {
		if !dev.wireless {
			continue
		}

		if err := getAPConfig(dev, props); err != nil {
			return err
		}

		if available == nil || dev.supportVLANs {
			available = dev
		}

		if dev.ring == base_def.RING_UNENROLLED {
			if !dev.supportVLANs {
				log.Printf("%s doesn't support VLANs\n",
					dev.hwaddr)
			} else if wifi == nil {
				wifi = dev
			} else {
				log.Printf("Multiple wifi nics configured.  "+
					"Using: %s\n", wifi.hwaddr)
			}
		}
	}

	if available == nil {
		return fmt.Errorf("couldn't find a wifi device to use")
	}

	if !available.supportVLANs {
		return fmt.Errorf("no VLAN-enabled wifi device found")
	}

	if wifi == nil {
		wifi = available
		log.Printf("No wifi device configured.  Using %s\n", wifi.hwaddr)
		wifi.ring = base_def.RING_UNENROLLED
		persistNicRing(wifi.hwaddr, wifi.ring)
	}

	wifiNic = wifi.name
	log.Printf("Hosting wireless network on %s (%s)\n", wifiNic, wifi.hwaddr)
	if setupNetwork {
		setupNic = wifiNic + setupPortal
		mac := macUpdateLastOctet(wifi.hwaddr, 1)
		rings[base_def.RING_SETUP].Bridge = setupNic
		persistNicRing(mac, base_def.RING_SETUP)
		log.Printf("Hosting setup network on %s (%s)\n", setupNic, mac)
	}
	return nil
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
	if strings.HasSuffix(i.Name, setupPortal) {
		return nil
	}

	d := physDevice{
		name:     i.Name,
		hwaddr:   i.HardwareAddr.String(),
		wireless: true,
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
	out, err := exec.Command("/sbin/iw", "phy", phy, "info").Output()
	if err != nil {
		log.Printf("Failed to get %s capabilities: %v\n", i.Name, err)
		return nil
	}
	capabilities := string(out)

	//
	// Look for "AP/VLAN" as a supported "software interface mode"
	//
	vlanRE := regexp.MustCompile(`AP/VLAN`)
	vlanModes := vlanRE.FindAllStringSubmatch(capabilities, -1)
	d.supportVLANs = (len(vlanModes) > 0)

	//
	// Examine the "valid interface combinations" to see if any include more
	// than one AP.  This one does:
	//    #{ AP, mesh point } <= 8,
	// This one doesn't:
	//    #{ managed } <= 1, #{ AP } <= 1, #{ P2P-client } <= 1,
	//
	comboRE := regexp.MustCompile(`#{ [\w\-, ]+ } <= [0-9]+`)
	combos := comboRE.FindAllStringSubmatch(capabilities, -1)

	for _, line := range combos {
		for _, combo := range line {
			if strings.Contains(combo, "AP") {
				s := strings.Split(combo, " ")
				if len(s) > 0 {
					cnt, _ := strconv.Atoi(s[len(s)-1])
					if cnt > 1 {
						d.multipleAPs = true
					}
				}
			}
		}
	}

	return &d
}

//
// Inventory the physical network devices in the system
//
func getDevices() {
	// Get the known device->ring mappings from configd
	devToRing := make(map[string]string)
	nics, _ := config.GetProps("@/nodes/" + aputil.GetNodeID().String())
	if nics != nil {
		for _, nic := range nics.Children {
			if x := nic.GetChild("ring"); x != nil {
				devToRing[nic.Name] = x.Value
			}
		}
	}
	all, err := net.Interfaces()
	if err != nil {
		log.Fatalf("Unable to inventory network devices: %v\n", err)
	}

	for _, i := range all {
		var d *physDevice
		if strings.HasPrefix(i.Name, "eth") ||
			strings.HasPrefix(i.Name, "enx") {
			d = getEthernet(i)
		} else if strings.HasPrefix(i.Name, "wlan") ||
			strings.HasPrefix(i.Name, "wlx") {
			d = getWireless(i)
		}

		if d != nil {
			d.ring = devToRing[d.hwaddr]
			physDevices[i.Name] = d
		}
	}
}

// Connect to all of the other brighgate daemons and construct our initial model
// of the system
func daemonInit() error {
	var err error

	if mcpd, err = mcp.New(pname); err != nil {
		log.Printf("cannot connect to mcp: %v\n", err)
	} else {
		mcpd.SetState(mcp.INITING)
	}

	b := broker.New(pname)

	config, err = apcfg.NewConfig(b, pname)
	if err != nil {
		return fmt.Errorf("cannot connect to configd: %v", err)
	}
	config.HandleChange(`^@/clients/.*/ring$`, configRingChanged)
	config.HandleChange(`^@/network/`, configNetworkChanged)

	rings = config.GetRings()
	clients = config.GetClients()
	props, err := config.GetProps("@/network")
	if err != nil {
		return fmt.Errorf("unable to fetch configuration: %v", err)
	}

	getDevices()
	prepareWan()
	if err = prepareWireless(props); err != nil {
		return fmt.Errorf("unable to prep wifi devices: %v", err)
	}

	// All wired devices that haven't yet been assigned to a ring will be
	// put into "standard" by default

	for _, dev := range physDevices {
		if !dev.wireless && dev.ring == "" {
			dev.ring = base_def.RING_STANDARD
			persistNicRing(dev.hwaddr, dev.ring)
		}
	}

	if err = loadFilterRules(); err != nil {
		return fmt.Errorf("unable to load filter rules: %v", err)
	}

	return nil
}

func networkCleanup() {
	devs, _ := ioutil.ReadDir("/sys/devices/virtual/net")
	for _, dev := range devs {
		name := dev.Name()

		if strings.HasPrefix(name, "b") {
			deleteBridge(name)
		}
		if strings.HasPrefix(name, "e") && strings.Contains(name, ".") {
			deleteVif(name)
		}
	}
}

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	flag.Parse()
	*templateDir = aputil.ExpandDirPath(*templateDir)
	*rulesDir = aputil.ExpandDirPath(*rulesDir)

	http.Handle("/metrics", promhttp.Handler())
	go http.ListenAndServe(*addr, nil)

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

	os.Exit(runAll())
}
