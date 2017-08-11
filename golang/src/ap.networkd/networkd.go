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
	"io"
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

	"ap_common/apcfg"
	"ap_common/broker"
	"ap_common/mcp"
	"ap_common/network"

	"base_def"
	"base_msg"

	"github.com/golang/protobuf/proto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	addr = flag.String("listen-address", base_def.HOSTAPDM_PROMETHEUS_PORT,
		"The address to listen on for HTTP requests.")
	templateDir = flag.String("template_dir", "golang/src/ap.networkd",
		"location of hostapd templates")

	aps        = make(map[string]*APConfig)
	interfaces = make(map[string]*iface)
	devices    = make(map[string]*device) // physical network devices

	config  *apcfg.APConfig
	clients apcfg.ClientMap // macaddr -> ClientInfo
	classes apcfg.ClassMap  // class -> config
	subnets apcfg.SubnetMap // iface -> subnet

	activeWifi   *device     // live wireless iface
	connectWifi  string      // open "network connect" interface
	activeWan    *device     // WAN port
	activeWired  string      // wired bridge
	childProcess *os.Process // track the hostapd proc
	mcpPort      *mcp.MCP
	running      bool
)

const (
	// Allow up to 4 failures in a 1 minute period before giving up
	failures_allowed = 4
	period           = time.Duration(time.Minute)

	confdir        = "/tmp"
	hostapdPath    = "/usr/sbin/hostapd"
	hostapdOptions = "-dKt"
	brctlCmd       = "/sbin/brctl"
	sysctlCmd      = "/sbin/sysctl"
	ipCmd          = "/sbin/ip"
	pname          = "ap.networkd"
	bridge         = "br0"
	connectPortal  = "_0"
)

//
// Physical network devices
//
type device struct {
	name         string
	hwaddr       string
	wireless     bool
	supportVLANs bool
	multipleAPs  bool
}

//
// Network interfaces - may be physical or virtual
//
type iface struct {
	name     string
	subnet   string
	wireless bool
}

type APConfig struct {
	Interface string // Linux device name
	Network   string // Network IP for non-VLAN configs
	Hwaddr    string // Mac address to use
	UseVLANs  bool   // Use VLANs or not
	Status    error  // collect hostapd failures

	SSID          string
	HardwareModes string
	Channel       int
	Passphrase    string

	ConfDir        string // Location of hostapd.conf, etc.
	ConfFile       string // Name of this NIC's hostapd.conf
	VLANComment    string // Used to disable vlan params in .conf template
	ConnectSSID    string // SSID to broadcast for connect network
	ConnectComment string // Used to disable connect net in .conf template
}

//////////////////////////////////////////////////////////////////////////
//
// Interaction with the rest of the ap daemons
//

func config_changed(raw []byte) {
	event := &base_msg.EventConfig{}
	proto.Unmarshal(raw, event)
	property := *event.Property
	path := strings.Split(property[2:], "/")

	reset := false
	fullReset := false
	rewrite := false
	conf := aps[activeWifi.name]

	// Watch for client identifications in @/client/<macaddr>/class.  If a
	// client changes class, we need it to rewrite the mac_accept file and
	// then force the client to reassociate with its new VLAN.
	//
	if len(path) == 3 && path[0] == "clients" && path[2] == "class" {
		hwaddr := path[1]
		rewrite = true
		reset = true

		newClass := *event.NewValue
		if c, ok := clients[hwaddr]; ok {
			log.Printf("Moving %s from %s to %s\n", hwaddr, c.Class,
				newClass)
			c.Class = newClass
		} else {
			c := apcfg.ClientInfo{Class: newClass}
			log.Printf("New client %s in %s\n", hwaddr, newClass)
			clients[hwaddr] = &c
		}
	}

	// Watch for changes to the network conf
	if len(path) == 2 && path[0] == "network" {
		switch path[1] {
		case "ssid":
			conf.SSID = *event.NewValue
			rewrite = true
			fullReset = true

		case "passphrase":
			conf.Passphrase = *event.NewValue
			rewrite = true
			fullReset = true

		case "connectssid":
			conf.ConnectSSID = *event.NewValue
			rewrite = true
			fullReset = true

		default:
			log.Printf("Ignoring update for unknown property: %s\n",
				property)
		}

	}

	if rewrite {
		generateHostAPDConf(conf)
	}

	if childProcess != nil {
		//
		// A SIGHUP will cause hostapd to reload its configuration.
		// However, it seems that we really need to kill and restart the
		// process for ssid/passphrase changes to be propagated down to
		// the wifi hardware
		//
		if fullReset {
			childProcess.Signal(syscall.SIGINT)
		} else if reset {
			childProcess.Signal(syscall.SIGHUP)
		}
	}
}

func getSubnetConfig() string {
	var subnet string
	var err error

	// In order to set up the correct routes, we need to know the network
	// we're expected to serve.
	if subnet, err = config.GetProp("@/dhcp/config/network"); err != nil {
		log.Fatalf("Failed to get DHCP configuration info: %v", err)
	}

	if _, _, err = net.ParseCIDR(subnet); err != nil {
		log.Fatalf("DHCP config has illegal network '%s': %v\n",
			subnet, err)
	}

	return subnet
}

//
// Get network settings from configd and use them to initialize the AP
//
func getAPConfig(d *device, props *apcfg.PropertyNode) {
	var ssid, passphrase, connectSSID string
	var vlanComment, connectComment string
	var node *apcfg.PropertyNode

	if node = props.GetChild("ssid"); node == nil {
		log.Fatalf("No SSID configured\n")
	}
	ssid = node.GetValue()

	if node = props.GetChild("passphrase"); node == nil {
		log.Fatalf("No passphrase configured\n")
	}
	passphrase = node.GetValue()

	if node = props.GetChild("connectssid"); node != nil {
		connectSSID = node.GetValue()
	}
	if !d.supportVLANs {
		vlanComment = "#"
	}

	if d.multipleAPs && len(connectSSID) > 0 {
		// If we create a second SSID for new clients to connect to,
		// its mac address will be derived from the nic's mac address by
		// adding 1 to the final octet.  To accomodate that, hostapd
		// wants the final nybble of the final octet to be 0.
		octets := strings.Split(d.hwaddr, ":")
		if len(octets) != 6 {
			log.Fatalf("%s has an invalid mac address: %s\n",
				d.name, d.hwaddr)
		}
		b, _ := strconv.ParseUint(octets[5], 16, 32)
		if b&0xff != 0 {
			b &= 0xf0
			octets[5] = fmt.Sprintf("%02x", b)

			// Since we changed the mac address, we need to set the
			// 'locally administered' bit in the first octet
			b, _ = strconv.ParseUint(octets[0], 16, 32)
			b |= 0x02 // Set the "locally administered" bit
			octets[0] = fmt.Sprintf("%02x", b)
			o := d.hwaddr
			d.hwaddr = strings.Join(octets, ":")
			log.Printf("Changed mac from %s to %s\n", o, d.hwaddr)
		}
	} else {
		connectComment = "#"
	}
	network := getSubnetConfig()

	data := APConfig{
		Interface:      d.name,
		Network:        network,
		Hwaddr:         d.hwaddr,
		UseVLANs:       d.supportVLANs,
		SSID:           ssid,
		HardwareModes:  "g",
		Channel:        6,
		Passphrase:     passphrase,
		ConfFile:       "hostapd.conf." + d.name,
		ConfDir:        confdir,
		VLANComment:    vlanComment,
		ConnectComment: connectComment,
		ConnectSSID:    connectSSID,
	}
	aps[d.name] = &data
}

//////////////////////////////////////////////////////////////////////////
//
// hostapd configuration and monitoring
//

// Extract the VLAN ID from the VLAN name (i.e., vlan.5 returns 5)
func vlanID(vlan string) int {
	rval := -1
	s := strings.Split(vlan, ".")
	if len(s) == 2 {
		rval, _ = strconv.Atoi(s[1])
	}
	return rval
}

// Derive a VLAN's bridge name (i.e., vlan.5 returns brvlan5)
func vlanBridge(vlan string) string {
	rval := ""

	s := strings.Split(vlan, ".")
	if len(s) == 2 {
		rval = "brvlan" + s[1]
	}
	return rval
}

//
// Generate the 3 configuration files needed for hostapd.
//
func generateHostAPDConf(conf *APConfig) string {
	var err error
	tfile := *templateDir + "/hostapd.conf.got"

	// Create hostapd.conf, using the APConfig contents to fill out the .got
	// template
	t, err := template.ParseFiles(tfile)
	if err != nil {
		log.Fatal(err)
		os.Exit(2)
	}

	fn := conf.ConfDir + "/" + conf.ConfFile
	cf, _ := os.Create(fn)
	defer cf.Close()

	err = t.Execute(cf, conf)
	if err != nil {
		log.Fatal(err)
		os.Exit(1)
	}

	if !conf.UseVLANs {
		return fn
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
		if info.Class == "wired" {
			continue
		}

		cc, ok := classes[info.Class]
		if !ok {
			log.Printf("%s in unrecognized class %s.\n",
				client, info.Class)
			cc, ok = classes["unclassified"]
		}
		if ok && cc.Interface != "wifi" {
			fmt.Fprintf(mf, "%s %d\n", client, vlanID(cc.Interface))
		}
	}

	// Create the 'vlan' file, which tells hostapd which vlans to create
	vfn := conf.ConfDir + "/" + "hostapd.vlan"
	vf, err := os.Create(vfn)
	if err != nil {
		log.Fatalf("Unable to create %s: %v\n", vfn, err)
	}
	defer vf.Close()

	for _, class := range classes {
		iface := class.Interface

		if strings.HasPrefix(iface, "vlan") {
			fmt.Fprintf(vf, "%d\t%s\n", vlanID(iface), iface)
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

//
// Wait for stdout/stderr from a process, and print whatever it sends.  When the
// pipe is closed, notify our caller.
//
func handlePipe(name string, r io.ReadCloser, done chan string) {
	var err error

	buf := make([]byte, 1024)

	for err == nil {
		if _, err = r.Read(buf); err == nil {
			log.Printf("%s", buf)
		}
	}

	done <- name
}

func resetWifiInterfaces() {
	start := time.Now()
	for _, iface := range interfaces {
		if iface.wireless {
			name := iface.name
			err := network.WaitForDevice(name, 2*time.Second)
			if err == nil {
				prepareInterface(iface)
			} else {
				log.Printf("%s failed to come online in %s.\n",
					name, time.Since(start))
			}
		}
	}
}

//
// Launch, monitor, and maintain the hostapd process for a single interface
//
func runOne(conf *APConfig, done chan *APConfig) {
	fn := generateHostAPDConf(conf)

	start_times := make([]time.Time, failures_allowed)
	pipe_closed := make(chan string)
	for running {
		cmd := exec.Command(hostapdPath, fn)

		//
		// Set up pipes for the child's stderr and stdout, so we can get
		// the output while the child is still running
		//
		pipes := 0
		if stdout, err := cmd.StdoutPipe(); err == nil {
			pipes++
			go handlePipe("stdout", stdout, pipe_closed)
		}
		if stderr, err := cmd.StderrPipe(); err == nil {
			pipes++
			go handlePipe("stderr", stderr, pipe_closed)
		}

		start_time := time.Now()
		start_times = append(start_times[1:failures_allowed], start_time)

		log.Printf("Starting hostapd for %s\n", conf.Interface)

		if err := cmd.Start(); err != nil {
			conf.Status = fmt.Errorf("Failed to launch: %v", err)
			break
		}
		childProcess = cmd.Process

		resetWifiInterfaces()
		mcpPort.SetStatus("online")

		// Wait for the stdout/stderr pipes to close and for the child
		// process to exit
		for pipes > 0 {
			<-pipe_closed
			pipes--
		}
		cmd.Wait()

		log.Printf("hostapd for %s exited after %s\n",
			conf.Interface, time.Since(start_time))
		if time.Since(start_times[0]) < period {
			conf.Status = fmt.Errorf("Dying too quickly")
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
	done := make(chan *APConfig)
	running := 0
	errors := 0

	for _, c := range aps {
		if c.Interface == activeWifi.name {
			running++
			go runOne(c, done)
		}
	}

	for running > 0 {
		c := <-done
		if c.Status != nil {
			log.Printf("%s hostapd failed: %v\n", c.Interface,
				c.Status)
			errors++
		} else {
			log.Printf("%s hostapd exited\n", c.Interface)
		}
		running--
	}

	return errors
}

//////////////////////////////////////////////////////////////////////////
//
// Low-level network manipulation.
//

//
// Get a NIC ready to serve as the router for a NATted subnet
//
func prepareInterface(iface *iface) {
	nic := iface.name
	if iface.wireless {
		bridge := vlanBridge(iface.name)
		if len(bridge) > 0 {
			nic = bridge
		}
	}
	log.Printf("Preparing %s %s\n", nic, iface.subnet)

	// ip addr flush dev wlan0
	cmd := exec.Command(ipCmd, "addr", "flush", "dev", nic)
	if err := cmd.Run(); err != nil {
		log.Fatalf("Failed to remove existing IP address: %v\n", err)
	}

	// ip route del 192.168.136.0/24
	cmd = exec.Command(ipCmd, "route", "del", iface.subnet)
	cmd.Run()

	// ip addr add 192.168.136.1 dev wlan0
	router := network.SubnetRouter(iface.subnet)
	cmd = exec.Command(ipCmd, "addr", "add", router, "dev", nic)
	if err := cmd.Run(); err != nil {
		log.Fatalf("Failed to set the router address: %v\n", err)
	}

	// ip link set up wlan0
	cmd = exec.Command(ipCmd, "link", "set", "up", nic)
	if err := cmd.Run(); err != nil {
		log.Fatalf("Failed to enable nic: %v\n", err)
	}
	// ip route add 192.168.136.0/24 dev wlan0
	cmd = exec.Command(ipCmd, "route", "add", iface.subnet, "dev", nic)
	if err := cmd.Run(); err != nil {
		log.Fatalf("Failed to add %s as the new route: %v\n",
			iface.subnet, err)
	}
}

func addInterface(name, subnet string, wireless bool) {
	iface := iface{
		name:     name,
		subnet:   subnet,
		wireless: wireless,
	}

	interfaces[name] = &iface
}

//
// Set up the wired ethernet ports.
//
func prepareWired() {
	var err error
	var bridge, wired_subnet string

	// Enable packet forwarding
	cmd := exec.Command(sysctlCmd, "-w", "net.ipv4.ip_forward=1")
	if err = cmd.Run(); err != nil {
		log.Fatalf("Failed to enable packet forwarding: %v\n", err)
	}

	cc, wiredLan := classes["wired"]
	if wiredLan {
		var ok bool

		// Create a bridge for any wired ports, after tearing down any
		// left over from a previous instance
		bridge = cc.Interface
		exec.Command(ipCmd, "link", "set", "down", bridge).Run()
		exec.Command(brctlCmd, "delbr", bridge).Run()

		wired_subnet, ok = subnets[bridge]
		if !ok {
			err = fmt.Errorf("No subnet configured")
		} else {
			err = exec.Command(brctlCmd, "addbr", bridge).Run()
		}

		if err != nil {
			log.Printf("Failed to create wired bridge %s: %v\n",
				bridge, err)
			wiredLan = false
		}
	} else {
		log.Printf("No 'wired' client class defined\n")
	}

	//
	// Identify the on-board ethernet port, which will connect us to the
	// WAN.  All other wired ports will be connected to the client bridge.
	//
	for name, dev := range devices {
		if dev.wireless {
			continue
		}

		// Use the oui to identify the on-board port
		if strings.HasPrefix(dev.hwaddr, "b8:27:eb:") {
			log.Printf("Using %s for WAN\n", name)
			if activeWan != nil {
				log.Printf("Found multiple eth ports\n")
			} else {
				activeWan = dev
			}
		} else if wiredLan {
			log.Printf("Using %s for clients\n", name)

			cmd := exec.Command(brctlCmd, "addif", bridge, name)
			if err := cmd.Run(); err != nil {
				log.Printf("Failed to add %s to %s\n",
					name, bridge)
			}
		}
	}
	if wiredLan {
		activeWired = bridge
		addInterface(bridge, wired_subnet, false)
		prepareInterface(interfaces[bridge])
	} else {
		activeWired = ""
	}
}

//
// Choose a wifi NIC to host our wireless clients, and build a list of the
// wireless interfaces we'll be supporting
//
func prepareWireless(props *apcfg.PropertyNode) {
	for _, dev := range devices {
		if dev.wireless {
			getAPConfig(dev, props)
			if activeWifi == nil || dev.supportVLANs {
				activeWifi = dev
			}
		}
	}
	if activeWifi == nil {
		log.Fatalf("Couldn't find a wifi device to use\n")
	}
	log.Printf("Hosting wireless network on %s\n", activeWifi.name)

	if activeWifi.supportVLANs {
		for i, s := range subnets {
			if i == "wifi" {
				addInterface(activeWifi.name, s, true)
			} else if i == "connect" {
				if activeWifi.multipleAPs {
					connectWifi = activeWifi.name +
						connectPortal
					addInterface(connectWifi, s, true)
				}
			} else if strings.HasPrefix(i, "vlan.") {
				addInterface(i, s, true)
			}
		}
	} else {
		subnet := aps[activeWifi.name].Network
		addInterface(activeWifi.name, subnet, true)
	}

}

func getEthernet(i net.Interface) *device {
	d := device{
		name:         i.Name,
		hwaddr:       i.HardwareAddr.String(),
		wireless:     false,
		supportVLANs: false,
	}
	return &d
}

//
// Given the name of a wireless NIC, construct a device structure for it
func getWireless(i net.Interface) *device {
	if strings.HasSuffix(i.Name, connectPortal) {
		return nil
	}

	d := device{
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
// Inventory the network devices in the system
//
func getDevices() {
	all, err := net.Interfaces()
	if err != nil {
		log.Fatalf("Unable to inventory network devices: %v\n", err)
	}

	for _, i := range all {
		var d *device
		if strings.HasPrefix(i.Name, "eth") {
			d = getEthernet(i)
		} else if strings.HasPrefix(i.Name, "wlan") {
			d = getWireless(i)
		}

		if d != nil {
			devices[i.Name] = d
		}
	}
}

func updateNetworkProp(props *apcfg.PropertyNode, prop, new string) {
	old := ""
	if node := props.GetChild(prop); node != nil {
		old = node.GetValue()
	}
	if old != new {
		path := "@/network/" + prop
		err := config.CreateProp(path, new, nil)
		if err != nil {
			log.Printf("Failed to update %s: %v\n", err)
		}
	}
}

//
// If our device inventory caused us to change any of the old network choices,
// update the config now.
//
func updateNetworkConfig(props *apcfg.PropertyNode) {
	updateNetworkProp(props, "wifi_nic", activeWifi.name)
	updateNetworkProp(props, "connect_nic", connectWifi)
	updateNetworkProp(props, "wired_nic", activeWired)
	updateNetworkProp(props, "wan_nic", activeWan.name)
	if activeWifi.supportVLANs {
		updateNetworkProp(props, "use_vlans", "true")
	} else {
		updateNetworkProp(props, "use_vlans", "false")
	}
}

func main() {
	var b broker.Broker
	var err error

	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	flag.Parse()

	mcpPort, err = mcp.New(pname)
	if err != nil {
		log.Printf("Failed to connect to mcp\n")
	} else {
		mcpPort.SetStatus("initializing")
	}

	http.Handle("/metrics", promhttp.Handler())
	go http.ListenAndServe(*addr, nil)

	log.Println("prometheus client thread launched")

	b.Init(pname)
	b.Handle(base_def.TOPIC_CONFIG, config_changed)
	b.Connect()
	defer b.Disconnect()

	config = apcfg.NewConfig(pname)
	subnets = config.GetSubnets()
	classes = config.GetClasses()
	clients = config.GetClients()

	props, err := config.GetProps("@/network")
	if err != nil {
		log.Fatalf("Failed to get network configuration info: %v", err)
	}

	getDevices()
	prepareWired()
	prepareWireless(props)
	updateNetworkConfig(props)

	running = true
	go signalHandler()

	os.Exit(runAll())
}
