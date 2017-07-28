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
 * ap.filterd is responsible for creating and maintaining the network rules that
 * implement both traditional firewalling and more specific internet
 * restrictions.
 *
 * The most basic rules implement general routing and NATting, and provide the
 * core plumbing needed for clients to interact with each other and the outside
 * world.  These rules are generally hardcoded into the daemon itself.
 *
 * Another set of rules are described in .rules files, found in
 * etc/filter.rules.d.  These rules generally describe firewalling behavior.
 * The rules describe the client(s) they apply to, and which network
 * interactions are explicitly forbidden or allowed.  These rules would allow us
 * to describe general restrictions such as "Nest Thermostats can access TCP
 * ports 80, 443, and 9543, but nothing else."
 *
 * These .rules files will be updated periodically from the Brightgate cloud.
 * We will use this update mechanism to roll out security updates in response to
 * emerging threats.  For example, when the WannaCry ransomware was discovered,
 * we could have immediately deployed an update that blocked the SMB1 ports
 * needed for the malware to spread.
 *
 * Another set of rules will be driven by customer configuration.  These rules
 * will allow for filtering tailored to a specific set of users and devices.
 * Examples would be "The kids can't acccess the internet after 10PM", or
 * "Access to the baby monitor is limited to Chris's iPhone and laptop."  These
 * rules will be captured in the configuration database.  It will presumably be
 * ap.filterd's responsibility to translate the configuration change into
 * specific rules.  The exact mechanism is TBD until we develop our user
 * configuration interface.
 *
 * A final set of rules will be generated dynamically in response to client
 * behavior.  For example, if we notice our Samsung fridge suddenly portscanning
 * our network, we can issue a rule that isolates that device completely.  If a
 * computer starts connecting to unrecognized hosts with non-standard ports at
 * 3:00AM, we can quarantine that computer and block interaction with those
 * hosts from all computers.  As with the configuration rules, this is all TBD
 * future work.
 */

package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

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
	rulesDir = flag.String("rdir", "./", "Location of the filter rules")

	config *apcfg.APConfig

	interfaces map[string]*iface
	nics       []*apcfg.Nic
	useVLANs   bool

	rules   []*Rule
	applied map[string]map[string][]string
)

//
// Linux has 5 pre-defined tables, but we are only using 'nat' and 'filter'.
// Each table has a set of predefined rule chains.
//
var (
	tables = []string{"nat", "filter"}
	chains = map[string][]string{
		"nat":    {"PREROUTING", "INPUT", "OUTPUT", "POSTROUTING"},
		"filter": {"INPUT", "FORWARD", "OUTPUT"},
	}
)

const (
	iptablesRulesFile = "/tmp/iptables.rules"
	iptablesCmd       = "/sbin/iptables-restore"
	pname             = "ap.filterd"
)

type iface struct {
	name   string
	subnet string
	class  string
}

func configChanged(event []byte) {
	config := &base_msg.EventConfig{}
	proto.Unmarshal(event, config)
	property := *config.Property
	path := strings.Split(property[2:], "/")

	// Watch for changes to the network conf
	if len(path) == 2 && path[0] == "network" {
		initNetwork()
		iptablesRebuild()
		iptablesReset()
	}
}

//
// Create a file containing all of the iptables rules to apply.  Use
// iptables-restore to apply the full set of rules in one go.
//
// The file contains a section for each table.  That section lists the default
// behavior for each chain, followed by a list of specific rules.  The section
// ends with a COMMIT.  For example:
//
//    *nat
//    :PREROUTING ACCEPT
//    :INPUT ACCEPT
//    :OUTPUT ACCEPT
//    :POSTROUTING ACCEPT
//    -A POSTROUTING -s 192.168.137.0/28 -o eth0 -j MASQUERADE
//    COMMIT
//
func iptablesReset() {
	log.Printf("Resetting iptables rules\n")

	f, err := os.Create(iptablesRulesFile)
	if err != nil {
		log.Printf("Unable to create %s: %v\n", iptablesRulesFile, err)
		return
	}
	defer f.Close()

	for _, t := range tables {
		// section marker for the table
		f.WriteString("*" + t + "\n")

		for _, c := range chains[t] {
			// default behavior for the chain
			f.WriteString(":" + c + " ACCEPT\n")
		}

		for _, c := range chains[t] {
			// per-table, per-chain rules:
			for _, r := range applied[t][c] {
				f.WriteString("-A " + c + " " + r + "\n")
			}
		}
		f.WriteString("COMMIT\n")
	}

	cmd := exec.Command(iptablesCmd, iptablesRulesFile)
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("failed to apply rules: %s\n", out)
	}
}

func iptablesAddRule(table, chain, rule string) {
	applied[table][chain] = append(applied[table][chain], rule)
}

//
// Build the core routing rules for a single managed subnet
//
func ifaceForwardRules(iface *iface) {
	wan := nics[apcfg.N_WAN]
	if wan == nil {
		return
	}

	// Traffic from the managed network has its IP addresses masqueraded
	masqRule := " -o " + wan.Iface
	masqRule += " -s " + iface.subnet
	masqRule += " -j MASQUERADE"
	iptablesAddRule("nat", "POSTROUTING", masqRule)

	// Route traffic from the managed network to the WAN
	connRule := " -i " + iface.name
	connRule += " -o " + wan.Iface
	connRule += " -s " + iface.subnet
	connRule += " -m conntrack --ctstate NEW"
	connRule += " -j ACCEPT"
	iptablesAddRule("filter", "FORWARD", connRule)
}

func genEndpointAddr(e *Endpoint, src bool) (string, error) {
	var d, r string

	if e.addr != nil {
		if src {
			d = "-s"
		} else {
			d = "-d"
		}
		r = fmt.Sprintf(" %s %v ", d, e.addr)
	}
	return r, nil
}

func genEndpointType(e *Endpoint, src bool) (string, error) {
	// Types won't be supported until the identifier starts feeding results
	// into the config tree
	return "", nil
}

func genEndpointClass(e *Endpoint, src bool) (string, error) {
	var d string

	if src {
		d = "-s"
	} else {
		d = "-d"
	}

	for _, i := range interfaces {
		if i.class == e.detail {
			r := fmt.Sprintf(" %s %s ", d, i.subnet)
			return r, nil
		}
	}

	return "", fmt.Errorf("no such class: %s", e.detail)
}

func genEndpointIface(e *Endpoint, src bool) (string, error) {
	var d, name string

	if src {
		d = "-i"
	} else {
		d = "-o"
	}

	var nic *apcfg.Nic
	switch e.detail {
	case "wan":
		nic = nics[apcfg.N_WAN]
	case "wifi":
		nic = nics[apcfg.N_WIFI]
	case "wired":
		nic = nics[apcfg.N_WIRED]
	case "connect":
		nic = nics[apcfg.N_CONNECT]
	}
	if nic != nil {
		name = nic.Iface
	}

	if name == "" {
		if i, ok := interfaces[e.detail]; ok {
			name = i.name
		}
	}

	if name == "" {
		return "", fmt.Errorf("no such interface: %s", e.detail)
	}
	return fmt.Sprintf(" %s %s ", d, name), nil
}

func genEndpoint(rule *Rule, from bool) (ep string, err error) {
	var e *Endpoint

	ep = ""
	err = nil

	if from {
		e = rule.from
	} else {
		e = rule.to
	}

	switch e.kind {
	case E_ADDR:
		ep, err = genEndpointAddr(e, from)
	case E_TYPE:
		ep, err = genEndpointType(e, from)
	case E_CLASS:
		ep, err = genEndpointClass(e, from)
	case E_IFACE:
		ep, err = genEndpointIface(e, from)
	}
	if err == nil && e.not {
		ep = " !" + ep
	}

	return
}

func genPorts(rule *Rule) (r string, err error) {
	var d string
	var ports *[]int

	if len(rule.sports) > 0 {
		d = " --sport"
		ports = &rule.sports
	}
	if len(rule.dports) > 0 {
		if ports != nil {
			err = fmt.Errorf("can't specify both SPORT and DPORT")
			return
		}

		d = " --dport"
		ports = &rule.dports
	}
	if ports == nil {
		return
	}
	if len(*ports) > 1 {
		r = fmt.Sprintf(" -m multiport %ss ", d)
	} else if rule.proto == P_UDP {
		r = fmt.Sprintf(" -m udp %s ", d)
	} else {
		r = fmt.Sprintf(" -m tcp %s ", d)
	}

	for i, p := range *ports {
		if i > 0 {
			r += ","
		}
		r += strconv.Itoa(p)
	}

	return r, nil
}

//
// Build the iptables rules for a captive portal subnet.
// Currently this only supports capturing an IFACE endpoint.  There's no reason
// it couldn't be extended to support classes or individual clients in the
// future.
//
func addCaptureRules(rule *Rule) error {
	if rule.to != nil {
		return fmt.Errorf("CAPTURE rules only support source endpoints")
	}
	if rule.from == nil {
		return fmt.Errorf("CAPTURE rules must provide source endpoint")
	}

	i, ok := interfaces[rule.from.detail]
	if !ok {
		return fmt.Errorf("no such interface: %s", rule.from.detail)
	}
	ep := " -i " + i.name
	webserver := network.SubnetRouter(i.subnet) + ":80"

	// All http packets get forwarded to our local web server
	captureRule := ep +
		" -p tcp --dport 80" +
		" -j DNAT --to-destination " + webserver

	// Allow local DNS packets through
	dnsAllow := ep + " -p udp --dport 53 -d " + i.subnet + " -j ACCEPT"

	// Allow DHCP packets through
	dhcpAllow := ep + " -p udp --dport 67 -j ACCEPT"

	// Allow http packets through to the FORWARD stage
	httpAllow := ep + " -p tcp --dport 80 -j ACCEPT"

	// http packets get forwarded.  Everything else gets dropped.
	otherDrop := ep + " -j DROP"

	iptablesAddRule("nat", "PREROUTING", captureRule)
	iptablesAddRule("filter", "INPUT", dnsAllow)
	iptablesAddRule("filter", "INPUT", dhcpAllow)
	iptablesAddRule("filter", "INPUT", httpAllow)
	iptablesAddRule("filter", "INPUT", otherDrop)

	iptablesAddRule("filter", "FORWARD", dnsAllow)
	iptablesAddRule("filter", "FORWARD", dhcpAllow)
	iptablesAddRule("filter", "FORWARD", httpAllow)
	iptablesAddRule("filter", "FORWARD", otherDrop)
	return nil
}

func addRule(rule *Rule) error {
	var r string

	if rule.action == A_CAPTURE {
		// 'capture' isn't a single rule - it's a coordinated collection
		// of rules.
		return addCaptureRules(rule)
	}

	from := rule.from
	to := rule.to
	chain := "FORWARD"

	switch rule.proto {
	case P_UDP:
		r += " -p udp"
	case P_TCP:
		r += " -p tcp"
	case P_ICMP:
		r += " -p icmp"
	case P_IP:
		r += " -p ip"
	}

	if from != nil {
		e, err := genEndpoint(rule, true)
		if err != nil {
			fmt.Printf("Bad 'from' endpoint: %v\n", err)
			return err
		}
		r += e

		if from.kind == E_IFACE && from.detail == "wan" {
			chain = "INPUT"
		}
	}

	if to != nil {
		e, err := genEndpoint(rule, false)
		if err != nil {
			fmt.Printf("Bad 'to' endpoint: %v\n", err)
			return err
		}

		r += e
	}

	e, err := genPorts(rule)
	if err != nil {
		fmt.Printf("Bad port list: %v\n", err)
		return err
	}
	r += e

	switch rule.action {
	case A_ACCEPT:
		r += " -j ACCEPT"
		iptablesAddRule("filter", chain, r)
	case A_BLOCK:
		r += " -j DROP"
		iptablesAddRule("filter", chain, r)
	}

	return nil
	// XXX - handle start/end times
}

func iptablesRebuild() {
	log.Printf("Rebuilding iptables rules\n")

	applied = make(map[string]map[string][]string)
	for _, t := range tables {
		applied[t] = make(map[string][]string)
	}

	// Allowed traffic on connected ports to flow from eth0 back to the
	// internal network
	iptablesAddRule("filter", "FORWARD",
		" -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT")

	// Add the basic routing rules for each interface
	for _, i := range interfaces {
		if i.class != "unauthorized" {
			ifaceForwardRules(i)
		}
	}

	// Now add filter rules, from the most specific to the most general
	for etype := E_ADDR; etype < E_MAX; etype++ {
		for _, r := range rules {
			// A given rule's specificity is determined by the most
			// specific endpoint
			rtype := E_MAX
			if r.from != nil {
				rtype = r.from.kind
			}
			if r.to != nil && r.to.kind < rtype {
				rtype = r.to.kind
			}

			if rtype == etype {
				addRule(r)
			}
		}
	}
}

func initNetwork() {
	useVLANs = false
	interfaces = make(map[string]*iface)

	wifiSubnet, _ := config.GetProp("@/dhcp/config/network")
	p, _ := config.GetProp("@/network/use_vlans")
	useVLANs = (p == "true")

	nics, _ = config.GetLogicalNics()
	subnets := config.GetSubnets()
	classes := config.GetClasses()

	//
	// Build the list of network interfaces we need to protect.  This
	// involves translating logical names (e.g., 'wifi' or 'connect') into
	// their physical instance names, and dropping interfaces not supported
	// by this hardware
	//
	for logical, subnet := range subnets {
		var name, class string

		// See if the interface belongs to a specific class
		for c, conf := range classes {
			if logical == conf.Interface {
				class = c
				break
			}
		}

		if logical == "wifi" {
			if nic := nics[apcfg.N_WIFI]; nic != nil {
				name = nic.Iface
			} else {
				log.Printf("No wifi network available\n")
				continue
			}
			if !useVLANs {
				subnet = wifiSubnet
			}
		} else if logical == "connect" {
			if nic := nics[apcfg.N_CONNECT]; nic != nil {
				name = nic.Iface
			} else {
				log.Printf("No connect network available\n")
				continue
			}
		} else {
			name = logical
		}

		if !useVLANs && strings.HasPrefix(name, "vlan") {
			continue
		}
		if len(name) == 0 || len(subnet) == 0 {
			continue
		}

		i := iface{
			name:   name,
			subnet: subnet,
			class:  class,
		}
		interfaces[logical] = &i
		fmt.Printf("iface %s -> %v\n", logical, i)
	}
}

func loadRules() error {
	dents, err := ioutil.ReadDir(*rulesDir)
	if err != nil {
		return fmt.Errorf("Unable to process rules directory: %v", err)
	}

	for _, dent := range dents {
		name := dent.Name()
		if !strings.HasSuffix(name, ".rule") {
			continue
		}

		fullPath := *rulesDir + "/" + name
		ruleSet, err := ParseRules(fullPath)
		if err != nil {
			return fmt.Errorf("failed to import %s: %v",
				fullPath, err)
		}
		rules = append(rules, ruleSet...)
	}
	return nil
}

func main() {
	var b broker.Broker

	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	flag.Parse()

	mcp, err := mcp.New(pname)
	if err != nil {
		log.Printf("Failed to connect to mcp\n")
	}

	http.Handle("/metrics", promhttp.Handler())
	go http.ListenAndServe(*addr, nil)

	log.Println("prometheus client thread launched")

	if err = loadRules(); err != nil {
		mcp.SetStatus("broken")
		log.Fatalf("Unable to load the rules files\n")
	}

	b.Init(pname)
	b.Handle(base_def.TOPIC_CONFIG, configChanged)
	b.Connect()
	defer b.Disconnect()

	config = apcfg.NewConfig(pname)

	if mcp != nil {
		mcp.SetStatus("online")
	}

	initNetwork()
	iptablesRebuild()
	iptablesReset()

	sig := make(chan os.Signal)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	s := <-sig
	log.Fatalf("Signal (%v) received, stopping\n", s)
}
