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
	"net"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"bg/ap_common/network"
	"bg/base_def"
	"bg/common/cfgapi"
)

var (
	rules      ruleList
	applied    map[string]map[string][]string
	blockedIPs map[uint32]struct{}
	wanNic     string
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

const iptablesRulesFile = "/tmp/iptables.rules"

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
	afrom := endpointMAX
	if a.from != nil {
		afrom = a.from.kind
	}
	bfrom := endpointMAX
	if b.from != nil {
		bfrom = b.from.kind
	}
	if afrom != bfrom {
		return afrom < bfrom
	}

	// Third: which rule has a more specific destination
	ato := endpointMAX
	if a.to != nil {
		ato = a.to.kind
	}
	bto := endpointMAX
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
	slog.Infof("Resetting iptables rules")

	f, err := os.Create(iptablesRulesFile)
	if err != nil {
		slog.Warnf("Unable to create %s: %v", iptablesRulesFile, err)
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

	cmd := exec.Command(plat.RestoreCmd, iptablesRulesFile)
	out, err := cmd.CombinedOutput()
	if err != nil {
		slog.Warnf("failed to apply rules: %s", out)
	}
}

func iptablesAddRule(table, chain, rule string) {
	applied[table][chain] = append(applied[table][chain], rule)
}

//
// Build the core routing rules for a single managed subnet
//
func ifaceForwardRules(ring string) {
	if ring == base_def.RING_QUARANTINE {
		// The quarantine ring doesn't get the normal NAT behavior
		return
	}
	config := rings[ring]

	// Traffic from the managed network has its IP addresses masqueraded
	masqRule := " -o " + wanNic
	masqRule += " -s " + config.Subnet
	masqRule += " -j MASQUERADE"
	iptablesAddRule("nat", "POSTROUTING", masqRule)

	// Route traffic from the managed network to the WAN
	connRule := " -i " + config.Bridge
	connRule += " -o " + wanNic
	connRule += " -s " + config.Subnet
	connRule += " -m conntrack --ctstate NEW"
	connRule += " -j ACCEPT"
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

func genEndpointRing(e *endpoint, src bool) (string, error) {
	var d string

	if src {
		d = "-i" // incoming interface
	} else {
		d = "-o" // outgoing interface
	}

	if ring := rings[e.detail]; ring != nil {
		b := ring.Bridge
		if e.detail == base_def.RING_INTERNAL && satellite {
			// The gateway node has an internal bridge that all
			// satellite links connect to.  Satellite nodes use
			// their 'wan' nic for internal traffic.
			b = wanNic
		}

		return fmt.Sprintf(" %s %s ", d, b), nil
	}

	return "", fmt.Errorf("no such ring: %s", e.detail)
}

func genEndpointIface(e *endpoint, src bool) (string, error) {
	var d, name string

	if src {
		d = "-i"
	} else {
		d = "-o"
	}

	if e.detail == "wan" && wanNic != "" {
		name = wanNic
	} else {
		return "", fmt.Errorf("no such interface: %s", e.detail)
	}
	return fmt.Sprintf(" %s %s ", d, name), nil
}

func genEndpoint(r *rule, from bool) (ep string, err error) {
	var e *endpoint

	ep = ""
	err = nil

	if from {
		e = r.from
	} else {
		e = r.to
	}

	switch e.kind {
	case endpointAddr:
		ep, err = genEndpointAddr(e, from)
	case endpointType:
		ep, err = genEndpointType(e, from)
	case endpointRing:
		ep, err = genEndpointRing(e, from)
	case endpointIface:
		ep, err = genEndpointIface(e, from)
	}
	if err == nil && e.not {
		ep = " !" + ep
	}

	return
}

func genPorts(r *rule) (portList string, err error) {
	const (
		lowMask  = (uint64(1) << 32) - 1
		highMask = lowMask << 32
	)
	var d string
	var ports *[]uint64

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
	} else if r.proto == protoUDP {
		portList = fmt.Sprintf(" -m udp %s ", d)
	} else {
		portList = fmt.Sprintf(" -m tcp %s ", d)
	}

	for i, p := range *ports {
		if i > 0 {
			portList += ","
		}

		low := p & lowMask
		high := (p & highMask) >> 32

		portList += strconv.FormatUint(low, 10)
		if high != 0 {
			portList += ":" + strconv.FormatUint(high, 10)
		}
	}

	return
}

//
// Build the iptables rules for a captive portal subnet.
// Currently this only supports capturing a RING endpoint.  There's no reason it
// couldn't be extended to support individual clients in the future.
//
func addCaptureRules(r *rule) error {
	if r.to != nil {
		return fmt.Errorf("CAPTURE rules only support source endpoints")
	}
	if r.from == nil {
		return fmt.Errorf("CAPTURE rules must provide source endpoint")
	}

	ring := rings[r.from.detail]
	if ring == nil {
		return fmt.Errorf("CAPTURE rules must specify a source ring")
	}
	if ring.Bridge == "" {
		slog.Warnf("No bridge defined for %s.  Skipping.",
			r.from.detail)
		return nil
	}

	ep := " -i " + ring.Bridge
	webserver := network.SubnetRouter(ring.Subnet) + ":80"

	// All http packets get forwarded to our local web server
	captureRule := ep +
		" -p tcp --dport 80" +
		" -j DNAT --to-destination " + webserver

	// Allow local DNS packets through
	dnsAllow := ep + " -p udp --dport 53 -d " + ring.Subnet + " -j ACCEPT"

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

func addRule(r *rule) error {
	var iptablesRule string

	if r.action == actionCapture {
		// 'capture' isn't a single rule - it's a coordinated collection
		// of rules.
		return addCaptureRules(r)
	}

	from := r.from
	to := r.to
	chain := "FORWARD"

	switch r.proto {
	case protoUDP:
		iptablesRule += " -p udp"
	case protoTCP:
		iptablesRule += " -p tcp"
	case protoICMP:
		iptablesRule += " -p icmp"
	case protoIP:
		iptablesRule += " -p ip"
	}

	if from != nil {
		e, err := genEndpoint(r, true)
		if err != nil {
			slog.Warnf("Bad 'from' endpoint: %v", err)
			return err
		}
		iptablesRule += e
	}

	if to != nil {
		e, err := genEndpoint(r, false)
		if err != nil {
			slog.Warnf("Bad 'to' endpoint: %v", err)
			return err
		}

		iptablesRule += e

		if to.kind == endpointAP {
			chain = "INPUT"
		}
	}

	e, err := genPorts(r)
	if err != nil {
		slog.Warnf("Bad port list: %v", err)
		return err
	}
	iptablesRule += e

	switch r.action {
	case actionAccept:
		iptablesRule += " -j ACCEPT"
		iptablesAddRule("filter", chain, iptablesRule)
	case actionBlock:
		iptablesRule += " -j dropped"
		iptablesAddRule("filter", chain, iptablesRule)
	}

	return nil
	// XXX - handle start/end times
}

func iptablesRuleApply(rule string) {
	args := strings.Split(rule, " ")
	args = append([]string{plat.IPTablesCmd}, args...)
	cmd := exec.Command(plat.IPTablesCmd)
	cmd.Args = args

	if out, err := cmd.CombinedOutput(); err != nil {
		slog.Warnf("iptables rule '%s' failed: %s", rule, out)
	}
}

// Update the live iptables rules needed to add or remove blocks for
// incoming and outgoing traffic from a blocked IP address
func updateBlockRules(addr string, add bool) {
	var action string
	if add {
		slog.Infof("Adding active block on %s", addr)
		action = "-I"
	} else {
		slog.Infof("Removing active block on %s", addr)
		action = "-D"
	}

	inputRule := "-t filter " + action + " INPUT -s " + addr + " -j dropped"
	fwdRule := "-t filter " + action + " FORWARD -d " + addr + " -j dropped"
	iptablesRuleApply(inputRule)
	iptablesRuleApply(fwdRule)
}

// Extract and validate an IP address from a firewall property like
// @/firewall/blocked/2.3.4.5
func getAddrFromPath(path []string) (addr string, flat uint32) {
	if len(path) == 3 {
		addr = path[2]
		if ip := net.ParseIP(addr); ip != nil {
			flat = network.IPAddrToUint32(ip)
		}
	}
	return
}

// An active block on an IP address has expired.  Remove that from the list of
// blocked IPs and delete the iptables rules currently implementing the block.
func configBlocklistExpired(path []string) {
	addr, flat := getAddrFromPath(path)
	if flat != 0 {
		if _, blocked := blockedIPs[flat]; blocked {
			delete(blockedIPs, flat)
			updateBlockRules(addr, false)
		}
	}
}

// An active block on an IP address has been added.  Add the address to the list
// of blocked IPs and insert new iptables rules to prevent traffic to/from that
// IP.
func configBlocklistChanged(path []string, val string, expires *time.Time) {
	addr, flat := getAddrFromPath(path)
	if flat != 0 {
		if _, blocked := blockedIPs[flat]; !blocked {
			blockedIPs[flat] = struct{}{}
			updateBlockRules(addr, true)
		}
	}
}

func configRuleChanged(path []string, val string, expires *time.Time) {
	if len(path) >= 3 {
		slog.Infof("Responding to change in firewall rule '%s'", path[2])
	} else {
		slog.Infof("Responding to change in firewall rules")
	}
	iptablesRebuild()
	iptablesReset()
}

func configRuleDeleted(path []string) {
	configRuleChanged(path, "", nil)
}

func firewallRule(p *cfgapi.PropertyNode) (*rule, error) {
	active, ok := p.Children["active"]
	if !ok || active.Value != "true" {
		return nil, nil
	}
	if rule, ok := p.Children["rule"]; ok {
		return parseRule(rule.Value)
	}
	return nil, fmt.Errorf("missing rule text")
}

func firewallRules() {
	const root = "@/firewall/rules"

	rules, err := config.GetProps(root)
	if rules == nil {
		if err != nil {
			slog.Warnf("Failed to get %s: %v", root, err)
		}
		return
	}

	for name, rule := range rules.Children {
		r, err := firewallRule(rule)
		if err != nil {
			slog.Warnf("bad firewall rule '%s': %v", name, err)
		} else if r != nil {
			addRule(r)
		}
	}
}

func iptablesRebuild() {
	slog.Infof("Rebuilding iptables rules")

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

	iptablesAddRule("filter", "INPUT", " -s 127.0.0.1 -j ACCEPT")

	// Add the basic routing rules for each interface
	if wanNic == "" {
		slog.Warnf("No WAN interface defined - cannot set up NAT")
	} else {
		for ring := range rings {
			ifaceForwardRules(ring)
		}
	}

	// Repopulate the list of blocked  IPs
	active := config.GetActiveBlocks()
	blockedIPs = make(map[uint32]struct{})
	for _, addr := range active {
		if ip := net.ParseIP(addr); ip != nil {
			flat := network.IPAddrToUint32(ip)
			blockedIPs[flat] = struct{}{}
			dropRule := addr + " -j dropped"
			iptablesAddRule("filter", "INPUT", " -s "+dropRule)
			iptablesAddRule("filter", "FORWARD", " -d "+dropRule)
		}
	}

	// Dropped packets should be logged.  We use different rules for LAN and
	// WAN drops so they can be rate-limited independently.  We can
	// optionally skip logging of dropped packets on the WAN port
	// altogether.
	wanFilter := ""
	if wanNic != "" {
		wanFilter = "-i " + wanNic + " "
	}
	lanFilter := "! " + wanFilter
	if _, err := config.GetProp("@/network/nologwan"); err != nil {
		// Limit logged WAN drops to 1/second
		iptablesAddRule("filter", "dropped", wanFilter+
			"-j LOG -m limit --limit 60/min  --log-prefix \"DROPPED \"")
	}

	// Limit logged LAN drops to 10/second
	iptablesAddRule("filter", "dropped", lanFilter+
		"-j LOG -m limit --limit 600/min  --log-prefix \"DROPPED \"")
	iptablesAddRule("filter", "dropped", "-j DROP")

	firewallRules()

	// Now add filter rules, from the most specific to the most general
	sort.Sort(rules)
	for _, r := range rules {
		addRule(r)
	}

	// That which is not expressly allowed is forbidden
	iptablesAddRule("filter", "INPUT", "-j dropped")
	iptablesAddRule("filter", "FORWARD", "-j dropped")
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

func applyFilters() {
	iptablesRebuild()
	iptablesReset()
}
