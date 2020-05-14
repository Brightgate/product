/*
 * COPYRIGHT 2020 Brightgate Inc.  All rights reserved.
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
	"strings"
	"time"
)

var (
	forwardTargets map[string]string
)

// If any of the forwarding properties were changed, reevaluate our iptables
// rules
func forwardUpdated(path []string, val string, expires *time.Time) {
	applyFilters()
}

// If any of the forwarding properties were removed, reevaluate our iptables
// rules
func forwardDeleted(path []string) {
	applyFilters()
}

// If any client that was a forwarding target has had its IP address added,
// changed, or removed, reevaluate our iptables rules
func forwardUpdateTarget(mac, ip string) {
	if oldip, ok := forwardTargets[mac]; ok && oldip != ip {
		applyFilters()
	}
}

// Given a forwarding target, return the mac, ip, and port numbers
func forwardTarget(t string) (string, string, string, error) {
	var mac, ip, port string
	var err error

	f := strings.Split(t, "/")
	if c, ok := clients[f[0]]; ok {
		mac = f[0]
		if c.IPv4 != nil {
			ip = c.IPv4.String()
		}
	} else {
		slog.Infof("unknown client: %s", f[0])
	}

	if len(f) == 2 {
		port = f[1]
	} else if len(f) > 2 {
		err = fmt.Errorf("improperly formatted target: %s", t)
	}

	return mac, ip, port, err

}

// Build the iptables rules needed to forward packets from the wan interface to
// the correct client and port.  Also open the appropriate holes in the firewall
// to allow those packets to enter.
func forwardingRules() {
	// As long as we're making a full pass over all of the forwarding rules,
	// refresh our list of the active forwarding targets.  By populating a
	// new map like this, we can make read-only access to the active map
	// lock-free.
	newTargets := make(map[string]string)
	defer func() { forwardTargets = newTargets }()

	wanNic := wan.getNic()
	if wanNic == "" {
		return
	}

	fw, _ := config.GetProps("@/policy/site/network/forward")
	if fw == nil {
		return
	}

	clientsMtx.Lock()
	defer clientsMtx.Unlock()

	// .../<tcp|udp>/<port> -> <mac[/port]>
	for proto, node := range fw.Children {

		// .../<port> -> <mac[/port]>
		for port, target := range node.Children {
			mac, tgtIP, tgtPort, err := forwardTarget(target.Value)
			if err != nil {
				slog.Warnf("%v", err)
				continue
			}
			newTargets[mac] = tgtIP

			if tgtIP == "" {
				// likely means the client doesn't have a
				// current DHCP lease.  Since there is nothing
				// on the receiving end of this forward
				// property, don't create the iptables rules for
				// it.
				continue
			}
			if tgtPort == "" {
				// by default, we forward to the same port on
				// the target machine
				tgtPort = port
			}

			// Forward packets from the wan port to the intended
			// client.  This doesn't fit into our standard firewall
			// syntax, so the iptables rule has to be hand-crafted.
			rule := "-i " + wanNic + " -p " + proto +
				" --dport " + port + " -j DNAT " +
				"--to-destination " + tgtIP + ":" + tgtPort
			iptablesAddRule("nat", "PREROUTING", rule)

			// Open a hole in the firewall for the forwarded packets
			rule = "ACCEPT " + proto + " FROM IFACE wan TO ADDR " +
				tgtIP + "/32 DPORTS " + tgtPort
			r, err := parseRule(rule)
			if err != nil {
				slog.Warnf("bad rule %s: %v", rule, err)
			} else {
				addRule(r)
			}
		}
	}
}
