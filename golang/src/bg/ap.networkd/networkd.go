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
	addr = flag.String("listen-address", base_def.NETWORKD_PROMETHEUS_PORT,
		"address to listen on for HTTP requests")
	platform = flag.String("platform", "rpi3",
		"hardware platform name")
	templateDir = flag.String("template_dir", "golang/src/ap.networkd",
		"location of hostapd templates")
	rulesDir = flag.String("rules_dir", "./", "Location of the filter rules")

	aps         = make(map[string]*apConfig)
	physDevices = make(map[string]*physDevice)

	config  *apcfg.APConfig
	clients apcfg.ClientMap // macaddr -> ClientInfo
	rings   apcfg.RingMap   // ring -> config

	setupNic       string
	wanNic         string
	wifiNic        string
	networkNodeIdx byte

	hostapdLog     *log.Logger
	hostapdProcess *aputil.Child // track the hostapd proc
	mcpd           *mcp.MCP

	running      bool
	setupNetwork bool
)

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
	iwCmd          = "/sbin/iw"
	vconfigCmd     = "/sbin/vconfig"
	pname          = "ap.networkd"
	setupPortal    = "_0"
)

type physDevice struct {
	name         string // Linux device name
	hwaddr       string // mac address
	ring         string // configured ring
	wireless     bool   // is this a wireless nic?
	supportVLANs bool   // does the nic support VLANs
	multipleAPs  bool   // does the nic support multiple APs
	channelsG    []int  // Supported 802.11g channels
	channelsAC   []int  // Supported 802.11ac channels
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
	PskComment    string // Used to disable wpa-psk in .conf template
	EapComment    string // Used to disable wpa-eap in .conf template
	ConfDir       string // Location of hostapd.conf, etc.

	confFile string // Name of this NIC's hostapd.conf
	status   error  // collect hostapd failures

	RadiusAuthServer     string
	RadiusAuthServerPort string
	RadiusAuthSecret     string // RADIUS shared secret
}

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
)

//////////////////////////////////////////////////////////////////////////
//
// Interaction with the rest of the ap daemons
//

func apReset(conf *apConfig) {
	log.Printf("Resetting hostapd\n")
	generateHostAPDConf(conf)
	//
	// A SIGHUP will cause hostapd to reload its configuration.
	// However, it seems that we really need to kill and restart the
	// process for the changes to be propagated down to the wifi
	// hardware
	//
	hostapdProcess.Signal(syscall.SIGINT)
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
		conf := aps[wifiNic]
		apReset(conf)
	}
}

func configNetworkChanged(path []string, val string, expires *time.Time) {
	conf := aps[wifiNic]

	// Watch for changes to the network conf
	switch path[1] {
	case "ssid":
		conf.SSID = val
		log.Printf("SSID changed to %s\n", val)

	case "passphrase":
		conf.Passphrase = val
		log.Printf("SSID changed to %s\n", val)

	case "setupssid":
		conf.SetupSSID = val
		log.Printf("setupSSID changed to %s\n", val)

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

// Select a channel and mode for this AP.  If we already have one or the other
// configured, make sure they are mutually compatible and supported by this
// hardware.  If the configuration is missing or broken, fall back to some
// sensible defaults.
func getModeChannel(d *physDevice) (mode string, channel int) {
	prop := "@/nodes/" + aputil.GetNodeID().String() + "/" + d.hwaddr
	if nic, _ := config.GetProps(prop); nic != nil {
		if x := nic.GetChild("mode"); x != nil {
			mode = x.Value
		}
		if x := nic.GetChild("channel"); x != nil {
			if channel, _ = strconv.Atoi(x.Value); channel == 0 {
				log.Printf("%s is not a valid channel number\n",
					x.Value)
			}
		}
	}

	if mode != "" {
		if mode == "ac" {
			// hostapd supports AC, but expects it to be called "a"
			mode = "a"
		}

		if mode == "a" {
			if len(d.channelsAC) == 0 {
				log.Printf("%s doesn't support mode ac\n",
					d.hwaddr)
				mode = ""
			}
		} else if mode == "g" {
			if len(d.channelsG) == 0 {
				log.Printf("%s doesn't support mode g\n",
					d.hwaddr)
				mode = ""
			}
		} else {
			log.Printf("Configured mode '%s' is invalid\n", mode)
			mode = ""
		}
	}

	if mode == "" {
		if len(d.channelsAC) > 0 {
			mode = "a"
		} else {
			mode = "g"
		}
	}

	if channel != 0 {
		var list []int
		if mode == "a" {
			list = d.channelsAC
		} else {
			list = d.channelsG
		}

		valid := false
		for _, x := range list {
			if channel == x {
				valid = true
				break
			}
		}
		if !valid {
			log.Printf("%d is not a valid channel for mode %s\n",
				channel, mode)
			channel = 0
		}
	}

	if channel == 0 {
		if mode == "g" {
			channel = 6
		} else {
			n := rand.Int() % len(d.channelsAC)
			channel = d.channelsAC[n]
		}
	}

	return mode, channel
}

//
// Get network settings from configd and use them to initialize the AP
//
func getAPConfig(d *physDevice, props *apcfg.PropertyNode) error {
	var ssid, passphrase, setupSSID string
	var setupComment string
	var radiusSecret string
	var node *apcfg.PropertyNode

	satNode := aputil.IsSatelliteMode()
	if node = props.GetChild("ssid"); node == nil {
		return fmt.Errorf("no SSID configured")
	}
	ssid = node.GetValue()

	if node = props.GetChild("passphrase"); node == nil {
		return fmt.Errorf("no passphrase configured")
	}
	passphrase = node.GetValue()

	// If @/network/radiusAuthSecret is already set, retrieve its value.
	sp, err := config.GetProp("@/network/radiusAuthSecret")
	if err == nil {
		radiusSecret = sp
	} else {
		radiusSecret = ""
	}

	ssidCnt := 0
	if node = props.GetChild("setupssid"); node != nil {
		setupSSID = node.GetValue()
	}

	pskComment := "#"
	eapComment := "#"
	for _, r := range rings {
		if r.Auth == "wpa-psk" {
			pskComment = ""
			ssidCnt++
		} else if r.Auth == "wpa-eap" {
			eapComment = ""
			ssidCnt++
		}
	}

	if !satNode && d.multipleAPs && len(setupSSID) > 0 {
		setupComment = ""
		setupNetwork = true
		ssidCnt++
	} else {
		setupComment = "#"
		setupNetwork = false
	}

	if ssidCnt > 1 {
		// If we create multiple SSIDs, hostapd will generate
		// additional bssids by incrementing the final octet of the
		// nic's mac address.  To accommodate that, hostapd wants the
		// final nybble of the final octet to be 0.
		newMac := macUpdateLastOctet(d.hwaddr, 0)
		if newMac != d.hwaddr {
			log.Printf("Changed mac from %s to %s\n", d.hwaddr, newMac)
			d.hwaddr = newMac
		}
	}

	mode, channel := getModeChannel(d)

	data := apConfig{
		Interface:     d.name,
		HWaddr:        d.hwaddr,
		SSID:          ssid,
		HardwareModes: mode,
		Channel:       channel,
		Passphrase:    passphrase,
		SetupComment:  setupComment,
		PskComment:    pskComment,
		EapComment:    eapComment,
		SetupSSID:     setupSSID,
		ConfDir:       confdir,

		confFile:             "hostapd.conf." + d.name,
		RadiusAuthServer:     "127.0.0.1",
		RadiusAuthServerPort: "1812",
		RadiusAuthSecret:     radiusSecret,
	}

	if satNode {
		data.RadiusAuthServer = "gateway"
	}

	aps[d.name] = &data
	return nil
}

//////////////////////////////////////////////////////////////////////////
//
// hostapd configuration and monitoring
//

//
// Generate the configuration files needed for hostapd.
//
func generateVlanConf(conf *apConfig, auth string) {
	// Create the 'accept_macs' file, which tells hostapd how to map clients
	// to VLANs.
	mfn := conf.ConfDir + "/" + "hostapd." + auth + ".macs"
	mf, err := os.Create(mfn)
	if err != nil {
		log.Fatalf("Unable to create %s: %v\n", mfn, err)
	}
	defer mf.Close()

	// Create the 'vlan' file, which tells hostapd which vlans to create
	vfn := conf.ConfDir + "/" + "hostapd." + auth + ".vlan"
	vf, err := os.Create(vfn)
	if err != nil {
		log.Fatalf("Unable to create %s: %v\n", vfn, err)
	}
	defer vf.Close()

	for ring, config := range rings {
		if config.Auth != auth || config.Vlan <= 0 {
			continue
		}

		fmt.Fprintf(vf, "%d\tvlan.%d\n", config.Vlan, config.Vlan)

		// One client per line, containing "<mac addr> <vlan_id>"
		for client, info := range clients {
			if info.Ring == ring {
				fmt.Fprintf(mf, "%s %d\n", client, config.Vlan)
			}
		}
	}
}

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

	generateVlanConf(conf, "wpa-psk")
	generateVlanConf(conf, "wpa-eap")

	return fn
}

//
// When we get a signal, set the 'running' flag to false and signal any hostapd
// process we're monitoring.  We want to be sure the wireless interface has been
// released before we give mcp a chance to restart the whole stack.
//
func signalHandler() {
	sig := make(chan os.Signal)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	s := <-sig

	log.Printf("Received signal %v\n", s)
	running = false
	hostapdProcess.Stop()
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

func rebuildInternalNet(ra *aputil.RunAbort) {
	satNode := aputil.IsSatelliteMode()

	if !satNode {
		prepareRingBridge(base_def.RING_INTERNAL, ra)
	}
	if ra.IsAbort() {
		return
	}

	br := rings[base_def.RING_INTERNAL].Bridge

	// For each internal network device, create a virtual device for each
	// LAN ring and attach it to the bridge for that ring
	for _, dev := range physDevices {
		if dev.ring != base_def.RING_INTERNAL {
			continue
		}

		if !satNode {
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

func rebuildLan(ra *aputil.RunAbort) {
	for ring := range rings {
		if ra.IsAbort() {
			return
		}
		if lanRing(ring) {
			prepareRingBridge(ring, ra)
		}
	}

	// Connect all the wired LAN NICs to ring-appropriate bridges.
	for _, dev := range physDevices {
		if ra.IsAbort() {
			return
		}

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

func resetInterfaces(ra *aputil.RunAbort) {

	// We use the lowest byte of our internal IP address as a transient,
	// local node index.  For the gateway node, that will always be 1.  For
	// the satellite nodes, we need pull it from the address the gateway's
	// DHCP server gave us.
	networkNodeIdx = 1
	if aputil.IsSatelliteMode() {
		ip := getInternalAddr()
		if ip == nil {
			log.Printf("Satellite node has no gateway connection\n")
			return
		}
		networkNodeIdx = ip[3]
	}

	// hostapd creates most of the per-ring bridges.  We need to create
	// these two manually.
	createBridge(rings[base_def.RING_UNENROLLED], wifiNic)
	createBridge(rings[base_def.RING_INTERNAL], "")

	if setupNetwork {
		prepareRingBridge(base_def.RING_SETUP, ra)
	}

	rebuildLan(ra)
	rebuildInternalNet(ra)

	if !ra.IsAbort() {
		mcpd.SetState(mcp.ONLINE)
	}
	ra.ClearRunning()
}

//
// Launch, monitor, and maintain the hostapd process for a single interface
//
func runOne(conf *apConfig, done chan *apConfig) {
	fn := generateHostAPDConf(conf)

	startTimes := make([]time.Time, failuresAllowed)
	for running {
		var ra aputil.RunAbort

		deleteBridges()

		hostapdProcess = aputil.NewChild(hostapdPath, fn)
		hostapdProcess.LogOutputTo("hostapd: ",
			log.Ldate|log.Ltime, os.Stderr)

		startTime := time.Now()
		startTimes = append(startTimes[1:failuresAllowed], startTime)

		log.Printf("Starting hostapd for %s\n", conf.Interface)

		if err := hostapdProcess.Start(); err != nil {
			conf.status = fmt.Errorf("failed to launch: %v", err)
			break
		}

		ra.SetRunning()
		ra.ClearAbort()
		go resetInterfaces(&ra)

		hostapdProcess.Wait()

		log.Printf("hostapd for %s exited after %s\n",
			conf.Interface, time.Since(startTime))

		// If the child died before all the interfaces were reset, tell
		// them to give up.
		ra.SetAbort()
		for ra.IsRunning() {
			time.Sleep(10 * time.Millisecond)
		}
		if !running {
			break
		}

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
	numRunning := 0
	errors := 0

	for _, c := range aps {
		if c.Interface == wifiNic {
			numRunning++
			go runOne(c, done)
		}
	}

	for numRunning > 0 {
		c := <-done
		if c.status != nil {
			log.Printf("%s hostapd failed: %v\n", c.Interface,
				c.status)
			errors++
		} else {
			log.Printf("%s hostapd exited\n", c.Interface)
		}
		numRunning--
	}
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
	raw[3] = networkNodeIdx
	return (net.IP(raw)).String()
}

//
// Prepare a ring's bridge: clean up any old state, assign a new address, set up
// routes, etc.
//
func prepareRingBridge(ringName string, ra *aputil.RunAbort) {
	ring := rings[ringName]
	bridge := ring.Bridge

	log.Printf("Preparing %s ring: %s %s\n", ringName, bridge, ring.Subnet)

	// The 'internal' bridge is a special case, since it will not get a
	// 'link up' until/unless the NIC is attached.  The other bridges
	// already have wireless NICs attached, and we expect them to be in the
	// 'link up' state already.
	if ringName != base_def.RING_INTERNAL {
		err := network.WaitForDevice(bridge, 5*time.Second, ra)
		if err != nil {
			log.Printf("%s failed to come online: %v\n", bridge, err)
			return
		}
		if ra.IsAbort() {
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

	if aputil.IsSatelliteMode() {
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
		name:       i.Name,
		hwaddr:     i.HardwareAddr.String(),
		wireless:   true,
		channelsG:  make([]int, 0),
		channelsAC: make([]int, 0),
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
	out, err := exec.Command(iwCmd, "phy", phy, "info").Output()
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

	// Find all the available channels and bin them into the appropriate
	// per-mode groups
	channels := channelRE.FindAllStringSubmatch(capabilities, -1)
	for _, line := range channels {
		// Skip any channels that are unavailable for either technical
		// or regulatory reasons
		if strings.Contains(line[3], "disabled") ||
			strings.Contains(line[3], "no IR") ||
			strings.Contains(line[3], "radar detection") {
			continue
		}
		channel, _ := strconv.Atoi(line[2])
		if channel >= 1 && channel <= 14 {
			d.channelsG = append(d.channelsG, channel)
		} else if channel >= 36 && channel <= 165 {
			d.channelsAC = append(d.channelsAC, channel)
		}
	}

	if len(d.channelsG) == 0 && len(d.channelsAC) == 0 {
		log.Printf("No supported channels found for %s\n",
			d.hwaddr)
		return nil
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

func setRegulatoryDomain(prop *apcfg.PropertyNode) {
	domain := "US"

	if x := prop.GetChild("regdomain"); x != nil {
		t := []byte(strings.ToUpper(x.Value))
		if !locationRE.Match(t) {
			log.Printf("Illegal @/network/regdomain: %s\n", x.Value)
		} else {
			domain = x.Value
		}
	}

	log.Printf("Setting regulatory domain to %s\n", domain)
	out, err := exec.Command(iwCmd, "reg", "set", domain).CombinedOutput()
	if err != nil {
		log.Printf("Failed to set domain: %v\n%s\n", err, out)
	}
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

	b := broker.New(pname)

	config, err = apcfg.NewConfig(b, pname)
	if err != nil {
		return fmt.Errorf("cannot connect to configd: %v", err)
	}
	config.HandleChange(`^@/clients/.*/ring$`, configRingChanged)
	config.HandleChange(`^@/rings/.*/auth$`, configAuthChanged)
	config.HandleChange(`^@/network/`, configNetworkChanged)
	config.HandleChange(`^@/firewall/active/`, configBlocklistChanged)
	config.HandleExpire(`^@/firewall/active/`, configBlocklistExpired)

	rings = config.GetRings()
	clients = config.GetClients()
	props, err := config.GetProps("@/network")
	if err != nil {
		return fmt.Errorf("unable to fetch configuration: %v", err)
	}
	setRegulatoryDomain(props)

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
	rand.Seed(time.Now().Unix())
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
	errcnt := runAll()

	log.Printf("Cleaning up\n")
	networkCleanup()

	os.Exit(errcnt)
}
