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
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"text/template"
	"time"

	"ap_common"
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
	aps           = make(map[string]*APConfig)
	clients       map[string]*ap_common.ClientInfo  // macaddr -> ClientInfo
	classes       map[string]*ap_common.ClassConfig // class -> config
	subnets       map[string]string                 // interface -> subnet
	child_process *os.Process                       // track the hostapd proc
	config        *ap_common.Config
)

const (
	// Allow up to 4 failures in a 1 minute period before giving up
	failures_allowed = 4
	period           = time.Duration(time.Minute)

	confdir         = "/tmp"
	conf_template   = "golang/src/ap.networkd/hostapd.conf.got"
	hostapd_path    = "/usr/sbin/hostapd"
	hostapd_options = "-dKt"
	iptables_cmd    = "/sbin/iptables"
	sysctl_cmd      = "/sbin/sysctl"
	ip_cmd          = "/sbin/ip"
	pname           = "ap.networkd"
)

type APConfig struct {
	Interface string // Linux device name
	Network   string // Network IP for non-VLAN configs
	UseVLANs  bool   // Use VLANs or not
	Status    error  // collect hostapd failures

	SSID          string
	HardwareModes string
	Channel       int
	Passphrase    string

	ConfDir     string // Location of hostapd.conf, etc.
	ConfFile    string // Name of this NIC's hostapd.conf
	VLANComment string // Used to disable vlan params in .conf template
}

func config_changed(event []byte) {
	reset := false
	config := &base_msg.EventConfig{}
	proto.Unmarshal(event, config)
	property := *config.Property
	path := strings.Split(property[2:], "/")

	// Watch for client identifications in @/client/<macaddr>/class.  If a
	// client changes class, we want it to go through the whole
	// authentication and connection process again.  Ideally we would just
	// disassociate that one client, but the big hammer will do for now
	//
	if len(path) == 3 && path[0] == "clients" && path[2] == "class" {
		log.Printf("%s changed class.  Force it to reauthenticate.\n",
			path[1])
		reset = true
	}

	// Watch for changes to the network conf
	if len(path) == 3 && path[0] == "network" {
		rewrite := false
		conf, ok := aps[path[1]]
		if !ok {
			log.Printf("Ignoring update for unknown NIC: %s\n", property)
			return
		}

		switch path[2] {
		case "ssid":
			conf.SSID = *config.NewValue
			rewrite = true

		case "passphrase":
			conf.Passphrase = *config.NewValue
			rewrite = true

		default:
			log.Printf("Ignoring update for unknown property: %s\n", property)
		}

		if rewrite {
			generateHostAPDConf(conf)
			reset = true
		}
	}

	if reset && child_process != nil {
		//
		// Ideally we would just send the child a SIGHUP and it would
		// reload the new configuration.  Unfortunately, hostapd seems
		// to need to go through a full reset before the changes are
		// correctly propagated to the wifi hw.
		//
		child_process.Signal(os.Interrupt)
	}
}

func getSubnetConfig(iface string) (string, bool) {
	var err error
	var props, node *ap_common.PropertyNode
	var use_vlans bool

	// In order to set up the correct routes, we need to know the network
	// we're expected to serve.
	if props, err = config.GetProps("@/dhcp/config"); err != nil {
		log.Fatalf("Failed to get DHCP configuration info: %v", err)
	}

	if node = props.GetChild("network"); node == nil {
		log.Fatalf("DHCP config has no network defined\n", iface)
	}
	network := node.Value
	if _, _, err := net.ParseCIDR(network); err != nil {
		log.Fatalf("DHCP config has illegal network '%s': %v\n",
			network, err)
	}

	propname := "@/network/" + iface + "/use_vlans"
	if prop, err := config.GetProp(propname); err == nil {
		if prop == "true" {
			use_vlans = true
		}
	}

	return network, use_vlans
}

//
// Get network settings from configd and use them to initialize the AP
//
func getAPConfig() {
	var name, ssid, passphrase, vlan_comment string

	props, err := config.GetProps("@/network")
	if err != nil {
		log.Fatalf("Failed to get network configuration info: %v", err)
	}

	if node := props.GetChild("default"); node != nil {
		name = node.Value
	} else {
		log.Fatalf("No default network defined\n")
	}

	iface := props.GetChild(name)
	if iface == nil {
		log.Fatalf("No configuration defined for %s\n", name)
	}

	if node := iface.GetChild("ssid"); node != nil {
		ssid = node.GetValue()
	} else {
		log.Fatalf("No SSID defined for %s\n", name)
	}

	if node := iface.GetChild("passphrase"); node != nil {
		passphrase = node.GetValue()
	} else {
		log.Fatalf("No passphrase defined for %s\n", name)
	}

	network, use_vlans := getSubnetConfig(name)
	if use_vlans {
		vlan_comment = ""
	} else {
		vlan_comment = "#"
	}

	data := APConfig{
		Interface:     name,
		Network:       network,
		UseVLANs:      use_vlans,
		SSID:          ssid,
		HardwareModes: "g",
		Channel:       6,
		Passphrase:    passphrase,
		ConfFile:      "hostapd.conf." + name,
		ConfDir:       confdir,
		VLANComment:   vlan_comment,
	}
	aps[name] = &data
}

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

	// Create hostapd.conf, using the APConfig contents to fill out the .got
	// template
	t, err := template.ParseFiles(conf_template)
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
		cc, ok := classes[info.Class]
		if !ok {
			log.Printf("%s in unrecognized class %s.\n",
				client, info.Class)
			cc, ok = classes["unclassified"]
		}
		if ok && cc.Interface != "default" {
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

	// Use the interface -> subnet map to identify all the VLAN IDs we want
	// to create
	for i, _ := range subnets {
		if i != "default" {
			fmt.Fprintf(vf, "%d\t%s\n", vlanID(i), i)
		}
	}

	return fn
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
			fmt.Printf("%s", buf)
		}
	}

	done <- name
}

//
// Launch, monitor, and maintain the hostapd process for a single interface
//
func runOne(conf *APConfig, done chan *APConfig) {
	fn := generateHostAPDConf(conf)

	start_times := make([]time.Time, failures_allowed)
	pipe_closed := make(chan string)
	for {
		cmd := exec.Command(hostapd_path, fn)

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
		child_process = cmd.Process

		prepareNet()

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
// they've all exited.  (Since we only support a single NIC right now, this is
// overkill, but harmless.)
//
func runAll() int {
	done := make(chan *APConfig)
	running := 0
	errors := 0

	for _, c := range aps {
		running++
		go runOne(c, done)
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

//
// Get a NIC ready to serve as the router for a NATted subnet
//
func prepareInterface(nic, subnet string) {
	fmt.Printf("Preparing %s %s\n", nic, subnet)
	bridge := vlanBridge(nic)
	if len(bridge) > 0 {
		nic = bridge
	}
	// ip addr flush dev wlan0
	cmd := exec.Command(ip_cmd, "addr", "flush", "dev", nic)
	if err := cmd.Run(); err != nil {
		log.Fatalf("Failed to remove existing IP address: %v\n", err)
	}

	// ip route del 192.168.136.0/24
	cmd = exec.Command(ip_cmd, "route", "del", subnet)
	cmd.Run()

	// Derive the router's IP address from the network.
	//    e.g., 192.168.136.0 -> 192.168.136.1
	_, network, _ := net.ParseCIDR(subnet)
	raw := network.IP.To4()
	raw[3] += 1
	router := (net.IP(raw)).String()

	// ip addr add 192.168.136.1 dev wlan0
	cmd = exec.Command(ip_cmd, "addr", "add", router, "dev", nic)
	if err := cmd.Run(); err != nil {
		log.Fatalf("Failed to set the router address: %v\n", err)
	}

	// ip link set up wlan0
	cmd = exec.Command(ip_cmd, "link", "set", "up", nic)
	if err := cmd.Run(); err != nil {
		log.Fatalf("Failed to enable nic: %v\n", err)
	}
	// ip route add 192.168.136.0/24 dev wlan0
	cmd = exec.Command(ip_cmd, "route", "add", subnet, "dev", nic)
	if err := cmd.Run(); err != nil {
		log.Fatalf("Failed to add %s as the new route: %v\n", subnet, err)
	}
}

//
// Add the iptables rules for a single managed subnet
//
func iptablesManaged(nic, subnet string) {
	// Route traffic from the managed network to eth0
	cmd := exec.Command(iptables_cmd,
		"-I", "FORWARD",
		"-i", nic,
		"-o", "eth0",
		"-s", subnet,
		"-m", "conntrack", "--ctstate", "NEW",
		"-j", "ACCEPT")
	if err := cmd.Run(); err != nil {
		fmt.Printf("%v\n", cmd)
		log.Fatalf("iptables route failed: %v\n", err)
	}

	// Traffic from the managed network has its IP addresses masqueraded
	cmd = exec.Command(iptables_cmd,
		"-t", "nat",
		"-I", "POSTROUTING",
		"-o", "eth0",
		"-s", subnet,
		"-j", "MASQUERADE")
	if err := cmd.Run(); err != nil {
		log.Fatalf("iptables NAT failed: %v\n", err)
	}
}

func iptablesReset() {
	// Flush out any existing rules
	cmd := exec.Command(iptables_cmd, "-F")
	if err := cmd.Run(); err != nil {
		log.Fatalf("failed to flush FILTER rules: %v\n", err)
	}
	cmd = exec.Command(iptables_cmd, "-t", "nat", "-F")
	if err := cmd.Run(); err != nil {
		log.Fatalf("failed to flush NAT rules: %v\n", err)
	}

	// Allowed traffic on connected ports to flow from eth0 back to the
	// internal network
	cmd = exec.Command(iptables_cmd,
		"-I", "FORWARD",
		"-m", "conntrack", "--ctstate", "RELATED,ESTABLISHED",
		"-j", "ACCEPT")
	if err := cmd.Run(); err != nil {
		log.Fatalf("iptables return data failed: %v\n", err)
	}
}

func prepareNet() {
	fmt.Printf("Preparing\n")
	// Enable packet forwarding
	cmd := exec.Command(sysctl_cmd, "-w", "net.ipv4.ip_forward=1")
	if err := cmd.Run(); err != nil {
		log.Fatalf("Failed to enable packet forwarding: %v\n", err)
	}

	// Build a list of all the subnets we're managing, and the interfaces
	// they live on
	managed := make(map[string]string)
	for _, a := range aps {
		if a.UseVLANs {
			for i, s := range subnets {
				if i == "default" {
					managed[a.Interface] = s
				} else {
					managed[i] = s
				}
			}
		} else {
			managed[a.Interface] = a.Network
		}
	}

	// If we're using VLANs, then we need to wait for hostapd to create the
	// devices
	start := time.Now()
	for i := range managed {
		err := network.WaitForDevice(i, 2*time.Second)
		if err != nil {
			log.Printf("%s failed to come online in %s.\n", i, time.Since(start))
			delete(managed, i)
		}
	}

	// Set up the IPtables rules and routing information for our managed
	// subnets
	iptablesReset()
	for i, s := range managed {
		iptablesManaged(i, s)
		prepareInterface(i, s)
	}
}

func main() {
	var b ap_common.Broker

	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	log.Println("start")

	flag.Parse()

	mcp, err := mcp.New(pname)
	if err != nil {
		log.Printf("Failed to connect to mcp\n")
	}

	http.Handle("/metrics", promhttp.Handler())
	go http.ListenAndServe(*addr, nil)

	log.Println("prometheus client thread launched")

	b.Init(pname)
	b.Handle(base_def.TOPIC_CONFIG, config_changed)
	b.Connect()
	defer b.Disconnect()

	config = ap_common.NewConfig(pname)
	getAPConfig()
	subnets = config.GetSubnets()
	classes = config.GetClasses()
	clients = config.GetClients()

	if mcp != nil {
		mcp.SetStatus("online")
	}

	os.Exit(runAll())
}
