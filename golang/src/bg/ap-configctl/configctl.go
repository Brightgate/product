/*
 * COPYRIGHT 2018 Brightgate Inc. All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or  alteration will be a violation of federal law.
 */

package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"bg/ap_common/apcfg"
	"bg/ap_common/device"
)

type setfunc func(string, string, *time.Time) error

const pname = "ap-configctl"

var apcfgd *apcfg.APConfig

func getRings(cmd string, args []string) error {
	if len(args) != 0 {
		usage(cmd)
	}

	rings := apcfgd.GetRings()
	clients := apcfgd.GetClients()

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

	fmt.Printf("%-10s %-8s %-4s %-9s %-18s %-7s\n",
		"ring", "auth", "vlan", "interface", "subnet", "clients")
	for _, name := range names {
		var vlan string

		ring := rings[name]
		if ring.Vlan >= 0 {
			vlan = strconv.Itoa(ring.Vlan)
		} else {
			vlan = "-"
		}

		fmt.Printf("%-10s %-8s %-4s %-9s %-18s %7d\n",
			name, ring.Auth, vlan, ring.Bridge, ring.Subnet,
			cnt[name])
	}
	return nil
}

func printClient(mac string, client *apcfg.ClientInfo) {
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
			exp = client.Expires.Format("2006-01-02T15:04")
		} else {
			exp = "static"
		}
	}

	confidence, err := strconv.ParseFloat(client.Confidence, 32)
	if err != nil {
		confidence = 0.0
	}

	// Don't confuse the user with a device ID unless the confidence
	// is better than even.
	identString := ""
	if confidence >= 0.5 {
		device, err := apcfgd.GetDevicePath("@/devices/" + client.Identity)
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
	if confidence < 0.87 {
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
	clients := apcfgd.GetClients()
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

func printDev(d *device.Device) {
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
	var root *apcfg.PropertyNode

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
		var d *device.Device
		if d, err = apcfgd.GetDevicePath(prop); err == nil {
			printDev(d)
		}
	} else {
		root, err = apcfgd.GetProps(prop)
		if err == nil {
			nodes := strings.Split(strings.Trim(prop, "/"), "/")
			label := nodes[len(nodes)-1]
			root.DumpTree(label)
		}
	}

	return err
}

func delProp(cmd string, args []string) error {
	if len(args) != 1 {
		usage(cmd)
	}
	return apcfgd.DeleteProp(args[0])
}

func setProp(cmd string, args []string) error {
	var expires *time.Time
	var err error

	if len(args) < 2 || len(args) > 3 {
		usage(cmd)
	}

	prop := args[0]
	val := args[1]
	if len(args) == 3 {
		seconds, _ := strconv.Atoi(args[2])
		dur := time.Duration(seconds) * time.Second
		tmp := time.Now().Add(dur)
		expires = &tmp
	}

	if cmd == "set" {
		err = apcfgd.SetProp(prop, val, expires)
	} else {
		err = apcfgd.CreateProp(prop, val, expires)
	}
	return err
}

var usages = map[string]string{
	"set": "<prop> <value [duration]>",
	"add": "<prop> <value [duration]>",
	"get": "<prop> | clients [-a] | rings",
	"del": "<prop>",
}

func usage(cmd string) {
	if u, ok := usages[cmd]; ok {
		fmt.Printf("usage: %s %s %s\n", pname, cmd, u)
	} else {
		fmt.Printf("Usage: %s\n", pname)
		for c, u := range usages {
			fmt.Printf("    %s %s\n", c, u)
		}
	}
	os.Exit(1)
}

func main() {
	var err error
	var cmd string
	var args []string

	if len(os.Args) < 1 {
		usage("")
	}

	cmd = os.Args[1]
	args = os.Args[2:]

	apcfgd, err = apcfg.NewConfig(nil, pname)
	if err != nil {
		fmt.Printf("cannot connect to configd: %v\n", err)
		os.Exit(1)
	}

	switch cmd {
	case "set":
		err = setProp(cmd, args)
	case "add":
		err = setProp(cmd, args)
	case "get":
		err = getProp(cmd, args)
	case "del":
		err = delProp(cmd, args)
	default:
		usage("")
	}

	if err != nil {
		fmt.Printf("%s failed: %v\n", cmd, err)
		os.Exit(1)
	} else if cmd != "get" {
		fmt.Println("ok")
	}
}
