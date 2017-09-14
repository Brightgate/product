/*
 * COPYRIGHT 2017 Brightgate Inc. All rights reserved.
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
	"os"
	"strconv"
	"strings"
	"time"

	"ap_common/apcfg"
)

type setfunc func(string, string, *time.Time) error

const pname = "ap-configctl"

var apcfgd *apcfg.APConfig

func getRings() error {
	rings := apcfgd.GetRings()
	subnets := apcfgd.GetSubnets()
	nics, _ := apcfgd.GetLogicalNics()
	clients := apcfgd.GetClients()

	cnt := make(map[string]int)
	for _, c := range clients {
		cnt[c.Ring]++
	}

	fmt.Printf("%-10s %-9s %-18s %-7s\n",
		"ring", "interface", "subnet", "clients")
	for name, ring := range rings {

		nic := ring.Interface
		subnet := subnets[nic]
		if nic == "setup" {
			if nics[apcfg.N_SETUP] == nil {
				nic = "-"
			} else {
				nic = nics[apcfg.N_SETUP].Iface
			}
		} else if nic == "wifi" {
			if nics[apcfg.N_WIFI] == nil {
				nic = "-"
			} else {
				nic = nics[apcfg.N_WIFI].Iface
			}
		}
		fmt.Printf("%-10s %-9s %-18s %7d\n",
			name, nic, subnet, cnt[name])
	}
	return nil
}

func getClients() error {
	clients := apcfgd.GetClients()

	fmt.Printf("%-17s %-16s %-10s %-15s %-16s %-9s %-s\n",
		"macaddr", "name", "ring", "ip addr", "expiration",
		"identity", "confidence")

	for mac, client := range clients {
		name := "-"
		if client.DNSName != "" {
			name = client.DNSName
		} else if client.DHCPName != "" {
			name = client.DHCPName
		}

		ipv4 := "-"
		exp := "-"
		if client.IPv4 != nil {
			ipv4 = client.IPv4.String()
			if client.Expires != nil {
				exp = client.Expires.Format("2006-02-01T15:04")
			} else {
				exp = "static"
			}
		}

		fmt.Printf("%-17s %-16s %-10s %-15s %-16s %-9s %-s\n",
			mac, name, client.Ring, ipv4, exp,
			client.Identity, client.Confidence)
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

func printDev(d *apcfg.Device) {
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

	if !strings.HasPrefix(prop, "@") {
		err = getFormatted(prop)
	} else if strings.HasPrefix(prop, "@/devices") {
		var d *apcfg.Device
		if d, err = apcfgd.GetDevicePath(prop); err == nil {
			printDev(d)
		}
	} else {
		root, err := apcfgd.GetProps(prop)
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

	apcfgd = apcfg.NewConfig(pname)
	switch cmd {
	case "set":
		err = setProp(prop, newval, duration, apcfgd.SetProp)
		if err == nil {
			fmt.Printf("ok\n")
		}
	case "add":
		err = setProp(prop, newval, duration, apcfgd.CreateProp)
		if err == nil {
			fmt.Printf("ok\n")
		}
	case "get":
		err = getProp(prop)
	case "del":
		err = delProp(prop)
		if err == nil {
			fmt.Printf("ok\n")
		}
	}
	if err != nil {
		fmt.Printf("%s failed: %v\n", cmd, err)
		os.Exit(1)
	}
}
