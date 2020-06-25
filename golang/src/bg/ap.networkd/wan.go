/*
 * COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
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
	"bufio"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"bg/ap_common/aputil"
	"bg/ap_common/dhcp"
	"bg/base_def"
	"bg/common/cfgapi"
	"bg/common/network"
)

type wanInfo struct {
	ring string
	nic  string

	staticCapable bool

	iface  *net.Interface
	addr   net.IP
	subnet *net.IPNet
	route  net.IP

	dhcpAddr     string
	dhcpStart    time.Time
	dhcpDuration time.Duration
	dhcpRoute    string
	dhcpDomain   string

	staticAddr      string
	staticRoute     net.IP
	staticDNSServer string

	updateNeeded chan bool
}

var wan *wanInfo

// Validate and update/delete the static address in the wanInfo structure.
func wanSetStaticAddr(addr string) {
	ip, ipnet, err := net.ParseCIDR(addr)
	if err != nil || !ip.Equal(ip.To4()) {
		slog.Warnf("illegal static IPv4 address '%s': %v",
			addr, err)
	} else {
		wan.staticAddr = addr
		slog.Debugf("Setting static wan address to %s, subnet to %v",
			ip, ipnet)
	}
}

// Validate and update/delete the static route in the wanInfo structure.
func wanSetStaticRoute(addr string) {
	ip := net.ParseIP(addr)
	if ip == nil || !ip.Equal(ip.To4()) {
		slog.Warnf("illegal static IPv4 route '%s'", addr)
	} else {
		slog.Debugf("Setting default route to %s", addr)
		wan.staticRoute = ip
	}
}

// One of the properties related to the static address config changed.  Update
// both our internal structure and the platform's system configuration.
func wanStaticChanged(prop, val string) {
	if !wan.staticCapable {
		return
	}

	changed := true

	switch prop {
	case "address":
		wanSetStaticAddr(val)
	case "route":
		wanSetStaticRoute(val)
	case "dnsserver":
		wan.staticDNSServer = val
	default:
		changed = false
	}

	if changed {
		wan.updateNeeded <- true
	}
}

// One of the properties related to the static address config was deleted.
// Update both our internal structure and the platform's system configuration.
func wanStaticDeleted(prop string) {
	if !wan.staticCapable {
		return
	}

	oldDNS := wan.staticDNSServer
	oldAddr := wan.staticAddr
	oldRoute := wan.staticRoute

	switch prop {
	case "all":
		wan.staticAddr = ""
		wan.staticRoute = nil

	case "address":
		wan.staticAddr = ""

	case "route":
		wan.staticRoute = nil

	case "dnsserver":
		wan.staticDNSServer = ""
	}

	if (oldDNS != wan.staticDNSServer) || (oldAddr != wan.staticAddr) ||
		!oldRoute.Equal(wan.staticRoute) {

		wan.updateNeeded <- true
	}
}

func (w *wanInfo) updateConfig() {
	var err error

	if w == nil || w.nic == "" || !wan.staticCapable {
		return
	}

	if w.staticAddr == "" {
		err = plat.NetConfig(w.nic, "dhcp", "", "", "")
	} else {
		// Strip any :port off of the server
		tmp := strings.Split(w.staticDNSServer, ":")
		dnsServer := tmp[0]

		gw := w.staticRoute
		if gw == nil {
			guess := network.SubnetRouter(w.staticAddr)
			gw = net.ParseIP(guess)
		}

		err = plat.NetConfig(w.nic, "static", w.staticAddr, gw.String(),
			dnsServer)
	}

	if err != nil {
		slog.Warnf("platform config failed: %v", err)
	}
}

func (w *wanInfo) getNic() string {
	return w.nic
}

func (w *wanInfo) setNic(nic string) {
	slog.Debugf("set upstream nic to %s", nic)
	w.nic = nic
	if nic == "" {
		w.iface = nil
	} else {
		var err error

		w.iface, err = net.InterfaceByName(nic)
		if err != nil {
			slog.Errorf("getting interface '%s': %v", nic, err)
		}
	}
	if !aputil.IsSatelliteMode() {
		w.ipCheck()
	}
}

// translate (for example) 0134A8C0 to 192.168.52.1
func xlateIP(hex string) net.IP {
	var a, b, c, d uint64
	var err error

	if len(hex) != 8 {
		return nil
	}

	if a, err = strconv.ParseUint(hex[6:], 16, 8); err != nil {
		return nil
	}
	if b, err = strconv.ParseUint(hex[4:6], 16, 8); err != nil {
		return nil
	}
	if c, err = strconv.ParseUint(hex[2:4], 16, 8); err != nil {
		return nil
	}
	if d, err = strconv.ParseUint(hex[0:2], 16, 8); err != nil {
		return nil
	}

	return net.IPv4(byte(a), byte(b), byte(c), byte(d))
}

// Get the currently assigned IP address and default route for the wan NIC
func (w *wanInfo) getAddrRoute() (string, net.IP, net.IP) {
	var ip, route net.IP
	var cidr string

	addrs, _ := w.iface.Addrs()
	for _, a := range addrs {
		if a.Network() == "ip+net" {
			cidr = a.String()
			ip, _, _ = net.ParseCIDR(a.String())
			break
		}
	}

	if file, err := os.Open("/proc/net/route"); err == nil {
		defer file.Close()
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			// Each line contains:
			//   Iface  Destination     Gateway ...
			//   eth0   0034A8C0        00000000
			line := scanner.Text()
			fields := strings.Fields(line)
			if len(fields) >= 3 {
				nic := fields[0]
				dest := fields[1]
				gw := fields[2]

				if nic == w.nic && dest == "00000000" {
					if r := xlateIP(gw); r != nil {
						route = r
						break
					}
				}
			}
		}
	}

	return cidr, ip, route
}

// Check to see if the address has changed since we last looked at it.
//
// Return 'true' if the address has changed since we last checked.
func (w *wanInfo) ipCheck() bool {
	var cidr string

	oldAddr := w.addr
	oldRoute := w.route

	cidr, w.addr, w.route = w.getAddrRoute()
	if !oldAddr.Equal(w.addr) || !oldRoute.Equal(w.route) {
		config.CreateProp("@/network/wan/current/address", cidr, nil)
		return true
	}

	return false
}

func dhcpOp(prop, val string) cfgapi.PropertyOp {
	return cfgapi.PropertyOp{
		Op:    cfgapi.PropCreate,
		Name:  "@/network/wan/dhcp/" + prop,
		Value: val,
	}
}

func (w *wanInfo) dhcpRenew() {
	if w != nil && w.staticAddr == "" {
		if err := dhcp.RenewLease(wan.nic); err != nil {
			slog.Warnf("failed to renew lease: %v", err)
		}
	}
}

func (w *wanInfo) dhcpRefresh() {
	tlog := aputil.GetThrottledLogger(slog, time.Second, 10*time.Minute)

	d, err := dhcp.GetLease(w.nic)
	if d == nil {
		if w.staticAddr == "" {
			if err != nil {
				tlog.Warnf("failed to get lease info: %v", err)
			} else {
				tlog.Warnf("no DHCP lease found for %s", w.nic)
			}
		}
		return
	}
	tlog.Clear()

	ops := make([]cfgapi.PropertyOp, 0)
	if d.Addr != "" {
		ops = append(ops, dhcpOp("address", d.Addr))
	}
	if d.Route != "" {
		ops = append(ops, dhcpOp("route", d.Route))
	}
	if !d.LeaseStart.IsZero() {
		start := d.LeaseStart.Format(time.RFC3339)
		ops = append(ops, dhcpOp("start", start))
	}
	if d.LeaseDuration != 0 {
		duration := strconv.Itoa(int(d.LeaseDuration.Seconds()))
		ops = append(ops, dhcpOp("duration", duration))
	}

	update := false
	if w.dhcpDomain != d.DomainName {
		update = true
		w.dhcpDomain = d.DomainName
	}
	if w.dhcpAddr != d.Addr {
		update = true
		w.dhcpAddr = d.Addr
	}
	if w.dhcpRoute != d.Route {
		update = true
		w.dhcpRoute = d.Route
	}
	if w.dhcpStart != d.LeaseStart {
		update = true
		w.dhcpStart = d.LeaseStart
	}
	if w.dhcpDuration != d.LeaseDuration {
		update = true
		w.dhcpDuration = d.LeaseDuration
	}
	if update {
		config.DeleteProp("@/network/wan/dhcp/")
		if _, err := config.Execute(nil, ops).Wait(nil); err != nil {
			slog.Warnf("DHCP update failed: %v", err)
		}
	}
}

func (w *wanInfo) monitorLoop(wg *sync.WaitGroup, doneChan chan bool) {
	defer wg.Done()

	refresh := time.Now()
	t := time.NewTicker(time.Second)

	done := false
	for !done {
		if w.ipCheck() {
			// address changed since last check, force dhcp to
			// refresh
			refresh = time.Now()
		}

		if time.Now().After(refresh) {
			w.dhcpRefresh()
			refresh = time.Now().Add(time.Minute)
		}

		select {
		case done = <-doneChan:
		case <-w.updateNeeded:
			w.updateConfig()
		case <-t.C:
		}
	}
}

// Determine the external-facing NIC for this node
func findWanDevice() *physDevice {
	var available, selected *physDevice

	ring := base_def.RING_WAN
	if aputil.IsSatelliteMode() {
		ring = base_def.RING_INTERNAL
	}

	for _, dev := range wiredNics {
		if !plat.NicIsWan(dev.name, dev.hwaddr) {
			continue
		}

		available = dev
		if dev.ring == ring {
			// If this nic has been explicitly configured as the WAN
			// device, it takes precedence.
			if selected == nil {
				selected = dev
			} else {
				slog.Infof("Multiple wan nics found.  "+
					"Using: %s", selected.hwaddr)
			}
		}
	}

	if available == nil {
		slog.Warnf("No WAN network device available")
	} else if selected == nil {
		selected = available
		selected.ring = ring
		slog.Infof("No WAN network device configured.  Using %s",
			selected.hwaddr)
		configUpdateRing(selected)
	}

	return selected
}

func enablePacketForwarding() {
	cmd := exec.Command(plat.SysctlCmd, "-w", "net.ipv4.ip_forward=1")
	if err := cmd.Run(); err != nil {
		slog.Fatalf("Failed to enable packet forwarding: %v", err)
	}
}

func wanInit(cfgWan *cfgapi.WanInfo) {
	enablePacketForwarding()

	wan = &wanInfo{
		updateNeeded:  make(chan bool, 2),
		staticCapable: plat.NetworkManaged && !aputil.IsSatelliteMode(),
	}

	if cfgWan != nil && wan.staticCapable {
		if static := cfgWan.StaticAddress; static != "" {
			var route string

			if cfgWan.StaticRoute != nil {
				route = cfgWan.StaticRoute.String()
			} else {
				route = network.SubnetRouter(static)
			}
			wanSetStaticAddr(static)
			wanSetStaticRoute(route)

		}
		wan.staticDNSServer = cfgWan.DNSServer
	}

	if dev := findWanDevice(); dev != nil {
		wan.setNic(dev.name)
		wan.updateNeeded <- true
	}
}
