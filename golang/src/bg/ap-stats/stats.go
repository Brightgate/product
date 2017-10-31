/*
 * COPYRIGHT 2017 Brightgate Inc. All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"

	"bg/ap_common/watchd"
)

const (
	pname = "ap-stats"
)

var (
	dev = flag.String("dev", "", "device mac address or 'all' ")
	agg = flag.Bool("a", false, "aggregate data")

	protocols = []string{"tcp", "udp"}
)

func usage() {
	fmt.Printf("usage:\t%s [-a] -dev <[mac address | all]>\n", pname)
	os.Exit(2)
}

func mapToList(in map[int]bool) []int {
	if len(in) == 0 {
		return nil
	}

	list := make([]int, 0)
	for i := range in {
		list = append(list, i)
	}

	return list
}

func printPorts(indent, label string, ports []int) {
	if len(ports) == 0 {
		return
	}
	out := indent + label + ": "
	for i, p := range ports {
		if i > 0 {
			out += ", "
		}
		if len(out) >= 73 {
			fmt.Printf("%s\n", out)
			out = indent + "    "
		}
		out += strconv.Itoa(p)
	}

	fmt.Printf("%s\n", out)
}

func dumpOneBlock(indent, proto, direction string, blocks map[string]int) {
	if len(blocks) == 0 {
		return
	}

	fmt.Printf("%s%s %s:\n", indent, proto, direction)
	nextIndent := indent + "    "
	for addr, cnt := range blocks {
		fmt.Printf("%s%s (%d)\n", nextIndent, addr, cnt)
	}
}

// Dump all the recorded firewall blocks to/from a device
func dumpBlocked(indent string, dev watchd.DeviceRecord) {
	cnt := 0
	for _, proto := range protocols {
		for _, x := range dev[proto].OutgoingBlocks {
			cnt += x
		}
		for _, x := range dev[proto].IncomingBlocks {
			cnt += x
		}
	}

	if cnt == 0 {
		return
	}

	fmt.Printf("%sFirewall recorded %d blocked packets:\n", indent, cnt)
	nextIndent := indent + "    "
	for _, proto := range protocols {
		dumpOneBlock(nextIndent, proto, "from", dev[proto].IncomingBlocks)
		dumpOneBlock(nextIndent, proto, "to", dev[proto].OutgoingBlocks)
	}
}

func dumpOne(indent string, dev watchd.DeviceRecord) {
	for _, proto := range protocols {
		ports := mapToList(dev[proto].OpenPorts)
		printPorts(indent, "Open "+proto+" ports", ports)
	}

	nextIndent := indent + "    "
	fmt.Printf("%straffic (sampled):\n", indent)
	total := 0
	for _, proto := range protocols {
		ports := mapToList(dev[proto].InPorts)
		total += len(ports)
		printPorts(nextIndent, "Inbound "+proto+" ports", ports)
		ports = mapToList(dev[proto].OutPorts)
		total += len(ports)
		printPorts(nextIndent, "Outbound "+proto+" ports", ports)
	}
	if total == 0 {
		fmt.Printf("%sNone.\n", nextIndent)
	}

	dumpBlocked(indent, dev)
}

func dumpAll(all bool, devs watchd.DeviceMap) {
	for mac, dev := range devs {
		if all {
			fmt.Printf("\n%s\n", mac)
		}
		dumpOne("    ", dev)
	}
}

func main() {
	var devs watchd.DeviceMap

	flag.Parse()

	if *dev == "" {
		usage()
	}

	mac := *dev
	if *dev == "all" {
		mac = "ff:ff:ff:ff:ff:ff"
	}

	w, err := watchd.New(pname)
	if err != nil {
		fmt.Printf("%s: unable to connect to watchd: %v\n", pname, err)
		os.Exit(1)
	}

	if *agg {
		devs, err = w.GetStatsAggregate(mac)
	} else {
		devs, err = w.GetStatsCurrent(mac)
	}

	if err != nil {
		fmt.Printf("failed: %v\n", err)
		os.Exit(1)
	}

	dumpAll((*dev == "all"), devs)

	os.Exit(0)
}
