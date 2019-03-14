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

	iface  *net.Interface
	addr   net.IP
	subnet *net.IPNet
	route  net.IP

	dhcpAddr     string
	dhcpStart    time.Time
	dhcpDuration time.Duration
	dhcpRoute    string

	staticAddr     string
	staticRoute    net.IP
	staticAttempts int

	done chan bool
	wg   sync.WaitGroup
}

var wan *wanInfo

func wanSetStaticAddr(addr string) {
	ip, ipnet, err := net.ParseCIDR(addr)
	if err != nil || !ip.Equal(ip.To4()) {
		slog.Warnf("illegal static IPv4 address '%s': %v",
			addr, err)
	} else {
		wan.staticAddr = addr
		wan.staticAttempts = 0
		slog.Debugf("Setting static wan address to %s, subnet to %v",
			ip, ipnet)
	}
}

func wanSetStaticRoute(addr string) {
	ip := net.ParseIP(addr)
	if ip == nil || !ip.Equal(ip.To4()) {
		slog.Warnf("illegal static IPv4 route '%s'", addr)
	} else {
		slog.Debugf("Setting default route to %s", addr)
		wan.staticRoute = ip
		wan.staticAttempts = 0
	}
}

func wanStaticChanged(prop, val string) {
	if prop == "address" {
		wanSetStaticAddr(val)
	} else if prop == "route" {
		wanSetStaticRoute(val)
	}
}

func wanStaticDeleted(prop string) {
	if prop == "route" || prop == "address" {
		reset := (wan.staticRoute != nil)
		wan.staticRoute = nil
		wan.staticAttempts = 0
		if reset {
			wan.routeClear()
		}

		if prop == "address" {
			reset := (wan.staticAddr != "")
			wan.staticAddr = ""
			wan.staticAttempts = 0
			if reset {
				wan.ipClear()
			}
			err := dhcp.RenewLease(wan.nic)
			if err != nil {
				slog.Warnf("failed to renew lease: %v", err)
			}
		}
	}
}

func wanStaticInit(cfgWan *cfgapi.WanInfo) {
	if cfgWan == nil {
		return
	}
	if cfgWan.StaticAddress != "" {
		wanSetStaticAddr(cfgWan.StaticAddress)
	}
	if cfgWan.StaticRoute != nil {
		wanSetStaticRoute(cfgWan.StaticRoute.String())
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

func (w *wanInfo) ipClear() {
	cmd := exec.Command(plat.IPCmd, "addr", "flush", "dev", wan.nic)
	if err := cmd.Run(); err != nil {
		slog.Warnf("Failed to remove old IP address: %v", err)
	}
}

func (w *wanInfo) ipSet() {
	w.ipClear()

	bcast := network.SubnetBroadcast(w.staticAddr).String()
	cmd := exec.Command(plat.IPCmd, "addr", "add", w.staticAddr,
		"broadcast", bcast, "dev", wan.nic)
	if err := cmd.Run(); err != nil {
		slog.Errorf("Failed to set new IP address: %v", err)
	}
}

func (w *wanInfo) routeClear() {
	cmd := exec.Command(plat.IPCmd, "route", "del", "default")
	if err := cmd.Run(); err != nil {
		slog.Warnf("unable to flush default route: %v", err)
	}
}

func (w *wanInfo) routeSet() {
	w.routeClear()

	cmd := exec.Command(plat.IPCmd, "route", "add", "default", "via",
		w.staticRoute.String())
	if err := cmd.Run(); err != nil {
		slog.Errorf("Failed to set new default route: %v", err)
	}
}

// Check to see if the address has changed since we last looked at it.  If we
// have a static address configured which doesn't match the current value, try
// to set it.  This lets us handle changes in the static IP configuration as
// well as updates made by the DHCP daemon.
//
// Return 'true' if the address has changed since we last checked.
func (w *wanInfo) ipCheck() bool {
	var cidr string

	oldAddr := w.addr
	oldRoute := w.route

	cidr, w.addr, w.route = w.getAddrRoute()
	changed := (!oldAddr.Equal(w.addr) || !oldRoute.Equal(w.route))
	if changed {
		config.CreateProp("@/network/wan/current/address", cidr, nil)
	}

	staticIP, _, _ := net.ParseCIDR(w.staticAddr)
	if staticIP == nil {
		return changed
	}

	if !staticIP.Equal(w.addr) {
		wan.staticAttempts++
		if wan.staticAttempts == 1 {
			slog.Infof("setting wan address to %v", w.addr)
		}

		w.ipSet()
	}

	staticRoute := w.staticRoute
	if staticRoute == nil {
		r := network.SubnetRouter(w.staticAddr)
		w.staticRoute = net.ParseIP(r)
	}

	if !staticRoute.Equal(w.route) {
		w.routeSet()
	}
	return changed
}

func dhcpOp(prop, val string) cfgapi.PropertyOp {
	return cfgapi.PropertyOp{
		Op:    cfgapi.PropCreate,
		Name:  "@/network/wan/dhcp/" + prop,
		Value: val,
	}
}

func (w *wanInfo) dhcpRefresh() {
	d, err := dhcp.GetLease(w.nic)
	if err != nil {
		slog.Errorf("failed to get lease info: %v", err)
		return
	}

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

func (w *wanInfo) monitorLoop() {
	refresh := time.Now()
	t := time.NewTicker(time.Second)

	done := false
	for !done {
		select {
		case done = <-wan.done:
			return
		case <-t.C:
		}

		if w.ipCheck() {
			refresh = time.Now()
		}

		if time.Now().After(refresh) {
			w.dhcpRefresh()
			refresh = time.Now().Add(time.Minute)
		}
	}

	wan.wg.Done()
}

// Monitor the state of our wan connection.
// Every second, check to see if the IP address has changed.
//   If our current IP doesn't match our statically-configured IP, attempt to
//   set our IP.
// Every minute (or when our IP changes) see if our DHCP state has changed.
func (w *wanInfo) monitor() {
	wan.wg.Add(1)
	go w.monitorLoop()
}

func (w *wanInfo) stop() {
	wan.done <- true
	wan.wg.Wait()
}

func wanInit(cfgWan *cfgapi.WanInfo) {
	var err error
	var available, current *physDevice
	var outgoingRing string

	wan = &wanInfo{
		done: make(chan bool),
	}

	wanStaticInit(cfgWan)

	if aputil.IsSatelliteMode() {
		outgoingRing = base_def.RING_INTERNAL
	} else {
		outgoingRing = base_def.RING_WAN
	}

	// Enable packet forwarding
	cmd := exec.Command(plat.SysctlCmd, "-w", "net.ipv4.ip_forward=1")
	if err = cmd.Run(); err != nil {
		slog.Fatalf("Failed to enable packet forwarding: %v", err)
	}

	// Find the WAN device
	for _, dev := range physDevices {
		if dev.wifi != nil {
			// XXX - at some point we should investigate using a
			// wireless link as a mesh backhaul
			continue
		}

		if plat.NicIsWan(dev.name, dev.hwaddr) {
			available = dev
			if dev.ring == outgoingRing {
				if current == nil {
					current = dev
				} else {
					slog.Infof("Multiple wan nics found.  "+
						"Using: %s", current.hwaddr)
				}
			}
		}
	}

	if available == nil {
		slog.Warnf("couldn't find a outgoing device to use")
		return
	}
	if current == nil {
		current = available
		slog.Infof("No outgoing device configured.  Using %s",
			current.hwaddr)
		current.ring = outgoingRing
	}

	wan.setNic(current.name)
}
