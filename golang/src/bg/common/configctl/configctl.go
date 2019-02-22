/*
 * COPYRIGHT 2019 Brightgate Inc. All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or  alteration will be a violation of federal law.
 */

package configctl

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"bg/common/cfgapi"
	"bg/common/deviceid"
)

var (
	configd *cfgapi.Handle
	pname   string
)

func timeString(t time.Time) string {
	return t.Format("2006-01-02T15:04:05")
}

func now() string {
	return timeString(time.Now())
}

func getRings(cmd string, args []string) error {
	if len(args) != 0 {
		usage(cmd)
	}

	rings := configd.GetRings()
	clients := configd.GetClients()

	// Build a list of ring names, and sort them by the vlan ID of the
	// corresponding ring config.
	names := make([]string, 0)
	for r := range rings {
		names = append(names, r)
	}
	sort.Slice(names,
		func(i, j int) bool {
			ringI := rings[names[i]]
			ringJ := rings[names[j]]
			return ringI.Vlan < ringJ.Vlan
		})

	cnt := make(map[string]int)
	for _, c := range clients {
		cnt[c.Ring]++
	}

	fmt.Printf("%-10s %-5s %-4s %-9s %-18s %-7s\n",
		"ring", "vap", "vlan", "interface", "subnet", "clients")
	for _, name := range names {
		var vlan string

		ring := rings[name]
		if ring.Vlan >= 0 {
			vlan = strconv.Itoa(int(ring.Vlan))
		} else {
			vlan = "-"
		}

		fmt.Printf("%-10s %-5s %-4s %-9s %-18s %7d\n",
			name, ring.VirtualAP, vlan, ring.Bridge, ring.Subnet,
			cnt[name])
	}
	return nil
}

func printClient(mac string, client *cfgapi.ClientInfo) {
	name := "-"
	if client.DNSName != "" {
		name = client.DNSName
	} else if client.DHCPName != "" {
		name = client.DHCPName
	}

	ring := "-"
	if client.Ring != "" {
		ring = client.Ring
	}

	ipv4 := "-"
	exp := "-"
	if client.IPv4 != nil {
		ipv4 = client.IPv4.String()
		if client.Expires != nil {
			exp = timeString(*client.Expires)
		} else {
			exp = "static"
		}
	}

	// Don't confuse the user with a device ID unless the confidence
	// is better than even.
	identString := ""
	if client.Confidence >= 0.5 {
		device, err := deviceid.GetDeviceByPath(configd,
			"@/devices/"+client.Identity)
		if err == nil {
			identString = fmt.Sprintf("%s %s", device.Vendor, device.ProductName)
		} else {
			identString = client.Identity
		}
	}

	// If the confidence is less than almost certain (as defined by
	// Words of Estimative Probability), prepend the device ID with
	// a question mark.
	confidenceMarker := ""
	if client.Confidence < 0.87 {
		confidenceMarker = "? "
	}

	fmt.Printf("%-17s %-16s %-10s %-15s %-16s %s%-9s\n",
		mac, name, ring, ipv4, exp, confidenceMarker, identString)
}

func getClients(cmd string, args []string) error {
	flags := flag.NewFlagSet("clients", flag.ContinueOnError)
	allClients := flags.Bool("a", false, "show all clients")

	if err := flags.Parse(args); err != nil {
		usage(cmd)
	}

	// Build a list of client mac addresses, and sort them
	clients := configd.GetClients()
	macs := make([]string, 0)
	for mac := range clients {
		macs = append(macs, mac)
	}
	sort.Strings(macs)

	fmt.Printf("%-17s %-16s %-10s %-15s %-16s %9s\n",
		"macaddr", "name", "ring", "ip addr", "expiration", "device id")

	for _, mac := range macs {
		client := clients[mac]
		if client.IsActive() || *allClients {
			printClient(mac, client)
		}
	}

	return nil
}

func getFormatted(cmd string, args []string) error {
	switch args[0] {
	case "clients":
		return getClients(cmd, args[1:])
	case "rings":
		return getRings(cmd, args[1:])
	default:
		return fmt.Errorf("unrecognized property: %s", args[0])
	}
}

func printDev(d *deviceid.Device) {
	fmt.Printf("  Type: %s\n", d.Devtype)
	fmt.Printf("  Vendor: %s\n", d.Vendor)
	fmt.Printf("  Product: %s\n", d.ProductName)
	if d.ProductVersion != "" {
		fmt.Printf("  Version: %s\n", d.ProductVersion)
	}
	if len(d.UDPPorts) > 0 {
		fmt.Printf("  UDP Ports: %v\n", d.UDPPorts)
	}
	if len(d.InboundPorts) > 0 {
		fmt.Printf("  TCP Inbound: %v\n", d.InboundPorts)
	}
	if len(d.OutboundPorts) > 0 {
		fmt.Printf("  TCP Outbound: %v\n", d.OutboundPorts)
	}
	if len(d.DNS) > 0 {
		fmt.Printf("  DNS Allowed: %v\n", d.OutboundPorts)
	}
}

func getProp(cmd string, args []string) error {
	var err error
	var root *cfgapi.PropertyNode

	if len(args) < 1 {
		usage(cmd)
	}

	prop := args[0]
	if !strings.HasPrefix(prop, "@") {
		return getFormatted(cmd, args)
	}

	if len(args) > 1 {
		usage(cmd)
	}

	if strings.HasPrefix(prop, "@/devices") {
		var d *deviceid.Device
		if d, err = deviceid.GetDeviceByPath(configd, prop); err == nil {
			printDev(d)
		}
	} else {
		if root, err = configd.GetProps(prop); err == nil {
			nodes := strings.Split(strings.Trim(prop, "/"), "/")
			label := nodes[len(nodes)-1]
			root.DumpTree(label)
		} else {
			err = fmt.Errorf("get failed: %v", err)
		}
	}

	return err
}

func hdlExpire(path []string) {
	fmt.Printf("%s Expired: %s\n", now(), strings.Join(path, "/"))
}

func hdlDelete(path []string) {
	fmt.Printf("%s Deleted: %s\n", now(), strings.Join(path, "/"))
}

func hdlUpdate(path []string, val string, exp *time.Time) {
	var at string

	if exp != nil {
		at = "  expires at: " + timeString(*exp)
	}
	fmt.Printf("%s Updated: %s -> %s%s\n", now(), strings.Join(path, "/"), val, at)

}

func monProp(cmd string, args []string) error {
	if len(args) != 1 {
		usage(cmd)
	}

	prop := args[0]
	if !strings.HasPrefix(prop, "@") {
		return fmt.Errorf("invalid property path: %s", prop)
	}

	fmt.Printf("monitoring %s\n", prop)
	if err := configd.HandleChange(prop, hdlUpdate); err != nil {
		return err
	}
	if err := configd.HandleDelete(prop, hdlDelete); err != nil {
		return err
	}
	if pname != "cl-configctl" {
		// Expiration events are only available on the client
		if err := configd.HandleExpire(prop, hdlExpire); err != nil {
			return err
		}
	}

	sig := make(chan os.Signal, 2)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	return nil
}

func makeDelProp(cmd string, args []string) *cfgapi.PropertyOp {
	if len(args) != 1 {
		usage(cmd)
	}

	op := cfgapi.PropertyOp{
		Op:   cfgapi.PropDelete,
		Name: args[0],
	}
	return &op
}

func makeSetProp(cmd string, args []string) *cfgapi.PropertyOp {
	if len(args) < 2 || len(args) > 3 {
		usage(cmd)
	}

	op := cfgapi.PropertyOp{
		Name:  args[0],
		Value: args[1],
	}

	if len(args) == 3 {
		seconds, _ := strconv.Atoi(args[2])
		dur := time.Duration(seconds) * time.Second
		tmp := time.Now().Add(dur)
		op.Expires = &tmp
	}

	if cmd == "set" {
		op.Op = cfgapi.PropSet
	} else {
		op.Op = cfgapi.PropCreate
	}
	return &op
}

func makeOp(cmd string, args []string) *cfgapi.PropertyOp {
	switch cmd {
	case "set":
		return makeSetProp(cmd, args)
	case "add":
		return makeSetProp(cmd, args)
	case "del":
		return makeDelProp(cmd, args)
	case "get":
		fmt.Printf("'get' must be a standalone command\n")
		os.Exit(1)
	default:
		usage("")
	}
	return nil
}

func makeOps(args []string) []cfgapi.PropertyOp {
	ops := make([]cfgapi.PropertyOp, 0)

	var cmd string
	var cmdArgs []string
	for _, f := range args {
		if cmd == "" {
			cmd = f
			cmdArgs = make([]string, 0)
		} else if f != "," {
			cmdArgs = append(cmdArgs, f)
		} else {
			ops = append(ops, *makeOp(cmd, cmdArgs))
			cmd = ""
		}
	}

	if cmd != "" {
		ops = append(ops, *makeOp(cmd, cmdArgs))
	}
	return ops
}

var usages = map[string]string{
	"ping": "",
	"set":  "<prop> <value [duration]>",
	"add":  "<prop> <value [duration]>",
	"get":  "<prop> | clients [-a] | rings",
	"del":  "<prop>",
	"mon":  "<prop>",
}

func usage(cmd string) {
	if u, ok := usages[cmd]; ok {
		fmt.Printf("usage: %s %s %s\n", pname, cmd, u)
	} else {
		fmt.Printf("usage: %s <cmd> <args> [, <cmd> <args> ]\n", pname)
		fmt.Printf("  commands:\n")
		for c, u := range usages {
			fmt.Printf("    %s %s\n", c, u)
		}

		fmt.Printf("\n  options:\n")
		flag.PrintDefaults()
	}

	os.Exit(1)
}

// Exec executes the bulk of the configctl work.
func Exec(p string, hdl *cfgapi.Handle, args []string) error {
	var err error

	configd = hdl
	pname = p

	if len(args) < 1 {
		usage("")
	}

	switch args[0] {
	case "get":
		err = getProp("get", args[1:])
	case "mon":
		err = monProp("mon", args[1:])
	case "ping":
		if err = hdl.Ping(nil); err == nil {
			fmt.Printf("ok\n")
		}
	default:
		ops := makeOps(args)
		_, err = configd.Execute(nil, ops).Wait(nil)
		if err == nil {
			fmt.Println("ok")
		}
	}

	return err
}
