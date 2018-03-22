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
	"fmt"
	"log"
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

func getRings() error {
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

func getClients() error {
	clients := apcfgd.GetClients()

	// Build a list of client mac addresses, and sort them
	macs := make([]string, 0)
	for mac := range clients {
		macs = append(macs, mac)
	}
	sort.Strings(macs)

	fmt.Printf("%-17s %-16s %-10s %-15s %-16s %-9s\n",
		"macaddr", "name", "ring", "ip addr", "expiration",
		"device id")

	for _, mac := range macs {
		client := clients[mac]
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

	return nil
}

func getFormatted(prop string) error {
	switch prop {
	case "clients":
		return getClients()
	case "rings":
		return getRings()
	default:
		return fmt.Errorf("unrecognized property: %s", prop)
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

func getProp(prop string) error {
	var err error
	var root *apcfg.PropertyNode

	if !strings.HasPrefix(prop, "@") {
		err = getFormatted(prop)
	} else if strings.HasPrefix(prop, "@/devices") {
		var d *device.Device
		if d, err = apcfgd.GetDevicePath(prop); err == nil {
			printDev(d)
		}
	} else {
		root, err = apcfgd.GetProps(prop)
		if err == nil {
			root.DumpTree()
		}
	}

	return err
}

func delProp(prop string) error {
	return apcfgd.DeleteProp(prop)
}

func setProp(prop, val, dur string, f setfunc) error {
	var expires *time.Time

	if len(val) == 0 {
		return fmt.Errorf("no value specified for")
	}

	if len(dur) > 0 {
		seconds, _ := strconv.Atoi(dur)
		dur := time.Duration(seconds) * time.Second
		tmp := time.Now().Add(dur)
		expires = &tmp
	}

	return f(prop, val, expires)
}

type op struct {
	minargs int
	maxargs int
	usage   string
}

var ops = map[string]op{
	"set": {3, 4, "<prop> <value [duration]>"},
	"add": {3, 4, "<prop> <value [duration]>"},
	"get": {2, 2, "<prop>"},
	"del": {2, 2, "<prop>"},
}

func main() {
	var op *op
	var cmd, prop, newval, duration string
	var err error

	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	argc := len(os.Args) - 1
	if argc >= 1 {
		cmd = os.Args[1]
		if x, ok := ops[cmd]; ok {
			op = &x
		}
	}

	// Look for a valid command
	if op == nil {
		fmt.Printf("Usage: %s\n", pname)
		for c, o := range ops {
			fmt.Printf("    %s %s\n", c, o.usage)
		}
		os.Exit(1)
	}

	// Verify that the command has a valid number of arguments
	if argc < op.minargs || argc > op.maxargs {
		fmt.Printf("Usage: %s %s %s\n", pname, cmd, op.usage)
		os.Exit(1)
	}

	prop = os.Args[2]
	if argc >= 3 {
		newval = os.Args[3]
	}
	if argc >= 4 {
		duration = os.Args[4]
	}

	apcfgd, err = apcfg.NewConfig(nil, pname)
	if err != nil {
		log.Fatalf("cannot connect to configd: %v\n", err)
	}

	switch cmd {
	case "set":
		err = setProp(prop, newval, duration, apcfgd.SetProp)
	case "add":
		err = setProp(prop, newval, duration, apcfgd.CreateProp)
	case "get":
		err = getProp(prop)
	case "del":
		err = delProp(prop)
	}
	if err != nil {
		fmt.Printf("%s failed: %v\n", cmd, err)
		os.Exit(1)
	} else if cmd != "get" {
		fmt.Println("ok")
	}
}
