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
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"

	"ap_common/apcfg"
	"ap_common/network"

	"base_def"
)

var (
	rules   ruleList
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
		"filter": {"INPUT", "FORWARD", "OUTPUT", "dropped"},
	}
)

const (
	iptablesRulesFile = "/tmp/iptables.rules"
	iptablesCmd       = "/sbin/iptables-restore"
)

// Implement the Sort interface for the list of rules
func (list ruleList) Len() int {
	return len(list)
}

func (list ruleList) Less(i, j int) bool {
	a := list[i]
	b := list[j]

	// First ordering criterion: ACCEPT takes precedence over BLOCK.
	// This works with our simple current ruleset, but may need to be
	// refined if/when we start blocking specific sites and/or services.
	if a.action != b.action {
		return a.action < b.action
	}

	// Second criterion: which rule has a more specific source
	afrom := E_MAX
	if a.from != nil {
		afrom = a.from.kind
	}
	bfrom := E_MAX
	if b.from != nil {
		bfrom = b.from.kind
	}
	if afrom != bfrom {
		return afrom < bfrom
	}

	// Third: which rule has a more specific destination
	ato := E_MAX
	if a.to != nil {
		ato = a.to.kind
	}
	bto := E_MAX
	if b.to != nil {
		bto = b.to.kind
	}
	if ato != bto {
		return ato < bto
	}

	// Fourth: which rules specifies more destination ports
	if len(a.dports) != len(b.dports) {
		return len(a.dports) > len(b.dports)
	}

	// Finally: which rules specifies more source ports
	if len(a.sports) != len(b.sports) {
		return len(a.sports) > len(b.sports)
	}

	return false
}

func (list ruleList) Swap(i, j int) {
	list[i], list[j] = list[j], list[i]
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
			// Set the default behavior for built-in chains to
			// ACCEPT.  Our added chain(s) must be set to "-"
			def := "ACCEPT"
			if c != strings.ToUpper(c) {
				def = "-"
			}
			fmt.Fprintf(f, ":%s %s\n", c, def)
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
	if wan == "" {
		return
	}

	// Traffic from the managed network has its IP addresses masqueraded
	masqRule := " -o " + wan
	masqRule += " -s " + iface.subnet
	masqRule += " -j MASQUERADE"
	iptablesAddRule("nat", "POSTROUTING", masqRule)

	// Route traffic from the managed network to the WAN
	connRule := " -i " + iface.name
	connRule += " -o " + wan
	connRule += " -s " + iface.subnet
	connRule += " -m conntrack --ctstate NEW"
	iptablesAddRule("filter", "FORWARD", connRule)
}

func genEndpointAddr(e *endpoint, src bool) (string, error) {
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

func genEndpointType(e *endpoint, src bool) (string, error) {
	// Types won't be supported until the identifier starts feeding results
	// into the config tree
	return "", nil
}

func genEndpointRing(ifaces ifaceMap, e *endpoint, src bool) (string, error) {
	var d string

	if src {
		d = "-s"
	} else {
		d = "-d"
	}

	for _, i := range ifaces {
		if i.ring == e.detail {
			r := fmt.Sprintf(" %s %s ", d, i.subnet)
			return r, nil
		}
	}

	return "", fmt.Errorf("no such ring: %s", e.detail)
}

func genEndpointIface(ifaces ifaceMap, e *endpoint, src bool) (string, error) {
	var d, name string

	if src {
		d = "-i"
	} else {
		d = "-o"
	}

	switch e.detail {
	case "wan":
		name = nics[apcfg.N_WAN]
	case "wifi":
		name = nics[apcfg.N_WIFI]
	case "wired":
		name = nics[apcfg.N_WIRED]
	case "setup":
		name = nics[apcfg.N_SETUP]
	}

	if name == "" {
		if i, ok := ifaces[e.detail]; ok {
			name = i.name
		}
	}

	if name == "" {
		return "", fmt.Errorf("no such interface: %s", e.detail)
	}
	return fmt.Sprintf(" %s %s ", d, name), nil
}

func genEndpoint(ifaces ifaceMap, r *rule, from bool) (ep string, err error) {
	var e *endpoint

	ep = ""
	err = nil

	if from {
		e = r.from
	} else {
		e = r.to
	}

	switch e.kind {
	case E_ADDR:
		ep, err = genEndpointAddr(e, from)
	case E_TYPE:
		ep, err = genEndpointType(e, from)
	case E_RING:
		ep, err = genEndpointRing(ifaces, e, from)
	case E_IFACE:
		ep, err = genEndpointIface(ifaces, e, from)
	}
	if err == nil && e.not {
		ep = " !" + ep
	}

	return
}

func genPorts(r *rule) (portList string, err error) {
	var d string
	var ports *[]int

	if len(r.sports) > 0 {
		d = " --sport"
		ports = &r.sports
	}
	if len(r.dports) > 0 {
		if ports != nil {
			err = fmt.Errorf("can't specify both SPORT and DPORT")
			return
		}

		d = " --dport"
		ports = &r.dports
	}
	if ports == nil {
		return
	}
	if len(*ports) > 1 {
		portList = fmt.Sprintf(" -m multiport %ss ", d)
	} else if r.proto == P_UDP {
		portList = fmt.Sprintf(" -m udp %s ", d)
	} else {
		portList = fmt.Sprintf(" -m tcp %s ", d)
	}

	for i, p := range *ports {
		if i > 0 {
			portList += ","
		}
		portList += strconv.Itoa(p)
	}

	return
}

//
// Build the iptables rules for a captive portal subnet.
// Currently this only supports capturing an IFACE endpoint.  There's no reason
// it couldn't be extended to support rings or individual clients in the
// future.
//
func addCaptureRules(ifaces ifaceMap, r *rule) error {
	if r.to != nil {
		return fmt.Errorf("CAPTURE rules only support source endpoints")
	}
	if r.from == nil {
		return fmt.Errorf("CAPTURE rules must provide source endpoint")
	}

	i, ok := ifaces[r.from.detail]
	if !ok {
		return fmt.Errorf("no such interface: %s", r.from.detail)
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
	otherDrop := ep + " -j dropped"

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

func addRule(ifaces ifaceMap, r *rule) error {
	var iptablesRule string

	if r.action == A_CAPTURE {
		// 'capture' isn't a single rule - it's a coordinated collection
		// of rules.
		return addCaptureRules(ifaces, r)
	}

	from := r.from
	to := r.to
	chain := "FORWARD"

	switch r.proto {
	case P_UDP:
		iptablesRule += " -p udp"
	case P_TCP:
		iptablesRule += " -p tcp"
	case P_ICMP:
		iptablesRule += " -p icmp"
	case P_IP:
		iptablesRule += " -p ip"
	}

	if from != nil {
		e, err := genEndpoint(ifaces, r, true)
		if err != nil {
			log.Printf("Bad 'from' endpoint: %v\n", err)
			return err
		}
		iptablesRule += e

		if from.kind == E_IFACE && from.detail == "wan" {
			chain = "INPUT"
		}
	}

	if to != nil {
		e, err := genEndpoint(ifaces, r, false)
		if err != nil {
			log.Printf("Bad 'to' endpoint: %v\n", err)
			return err
		}

		iptablesRule += e
	}

	e, err := genPorts(r)
	if err != nil {
		log.Printf("Bad port list: %v\n", err)
		return err
	}
	iptablesRule += e

	switch r.action {
	case A_ACCEPT:
		iptablesRule += " -j ACCEPT"
		iptablesAddRule("filter", chain, iptablesRule)
	case A_BLOCK:
		iptablesRule += " -j dropped"
		iptablesAddRule("filter", chain, iptablesRule)
	}

	return nil
	// XXX - handle start/end times
}

func iptablesRebuild(ifaces ifaceMap) {
	log.Printf("Rebuilding iptables rules\n")

	applied = make(map[string]map[string][]string)
	for _, t := range tables {
		applied[t] = make(map[string][]string)
	}

	// Allowed traffic on connected ports to flow from eth0 back to the
	// internal network
	iptablesAddRule("filter", "FORWARD",
		" -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT")

	iptablesAddRule("filter", "INPUT",
		" -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT")

	// Add the basic routing rules for each interface
	for _, i := range ifaces {
		if i.ring != base_def.RING_SETUP &&
			i.ring != base_def.RING_QUARANTINE {
			ifaceForwardRules(i)
		}
	}

	// Dropped packets should be logged
	// These messages can be collected in a dedicated log file by creating
	// the following .conf file:
	//
	// $ sudo cat /etc/rsyslog.d/bg.conf
	// :msg, contains, "DROPPED" -/var/log/bg-dropped.log
	// & ~

	// optionally skip logging of dropped packets on the WAN port.
	wan_filter := ""
	if _, err := config.GetProp("@/network/nologwan"); err == nil {
		wan_filter = "! -i " + nics[apcfg.N_WAN] + " "
	}
	iptablesAddRule("filter", "dropped", wan_filter+
		"-j LOG -m limit --limit 10/min  --log-prefix \"DROPPED \"")
	iptablesAddRule("filter", "dropped", "-j DROP")

	// Now add filter rules, from the most specific to the most general
	sort.Sort(rules)
	for _, r := range rules {
		addRule(ifaces, r)
	}
}

func loadFilterRules() error {
	var list ruleList

	dents, err := ioutil.ReadDir(*rulesDir)
	if err != nil {
		return fmt.Errorf("unable to process rules directory: %v", err)
	}

	for _, dent := range dents {
		name := dent.Name()
		if !strings.HasSuffix(name, ".rules") {
			continue
		}

		fullPath := *rulesDir + "/" + name
		ruleSet, err := parseRulesFile(fullPath)
		if err != nil {
			return fmt.Errorf("failed to import %s: %v",
				fullPath, err)
		}
		list = append(list, ruleSet...)
	}

	rules = list
	return nil
}

func applyFilters(ifaces ifaceMap) {
	iptablesRebuild(ifaces)
	iptablesReset()
}
