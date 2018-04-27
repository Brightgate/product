/*
 * COPYRIGHT 2018 Brightgate Inc. All rights reserved.
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
	"sort"
	"strconv"
	"time"

	"bg/ap_common/watchd"
)

const (
	pname = "ap-stats"
)

var (
	devFlag = flag.String("dev", "", "device mac address or 'all' ")
	durFlag = flag.Duration("dur", time.Duration(0), "duration")
)

func usage() {
	fmt.Printf("usage:\t%s -dev <[mac address | all]> [-dur <duration>]\n", pname)
	os.Exit(2)
}

func printPorts(indent, label string, ports []int) {
	if len(ports) == 0 {
		return
	}

	o := sort.IntSlice(ports)
	o.Sort()

	out := indent + label + ": "
	for i, p := range o {
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

func printLine(label string, x watchd.XferStats) {
	fmt.Printf("%30s%9d%12d%8s%9d%12d\n",
		label, x.PktsSent, x.BytesSent, " ", x.PktsRcvd, x.BytesRcvd)
}

func showRecord(mac string, rec *watchd.DeviceRecord) {
	var header string
	if mac != "" {
		header += "\nDevice: " + mac
		if rec.Addr != nil {
			header += fmt.Sprintf("  (IP: %v)", rec.Addr)
		}
	}
	fmt.Printf("%s\n", header)

	if len(rec.OpenTCP) > 0 || len(rec.OpenUDP) > 0 {
		fmt.Printf("  Open ports\n")
		printPorts("      ", "TCP", rec.OpenTCP)
		printPorts("      ", "UDP", rec.OpenUDP)
		fmt.Printf("\n")
	}

	fmt.Printf("%30s%9s%12s%8s%9s%12s\n", "",
		"Pkts Sent", "Bytes Sent", " ", "Pkts Rcvd", "Bytes Rcvd")

	if len(rec.LANStats) > 0 {
		fmt.Printf("  Local:\n")
		for k, x := range rec.LANStats {
			s := watchd.KeyToSession(k)
			label := fmt.Sprintf("%v:%-5d", s.RAddr, s.RPort)
			printLine(label, x)
		}
	}
	if len(rec.WANStats) > 0 {
		fmt.Printf("  Remote:\n")
		for k, x := range rec.WANStats {
			s := watchd.KeyToSession(k)
			label := fmt.Sprintf("%v:%-5d", s.RAddr, s.RPort)
			printLine(label, x)
		}
	}
	fmt.Printf("\n")
	label := fmt.Sprintf("%-30s", "  Total:")
	printLine(label, rec.Aggregate)
}

func main() {
	var (
		mac   string
		start *time.Time
	)

	flag.Parse()

	if *devFlag == "" {
		usage()
	} else if *devFlag == "all" {
		mac = "ff:ff:ff:ff:ff:ff"
	} else {
		mac = *devFlag
	}

	t := time.Now()
	if *durFlag != time.Duration(0) {
		t = t.Add(-1 * *durFlag)
	}
	start = &t

	w, err := watchd.New(pname)
	if err != nil {
		fmt.Printf("%s: unable to connect to watchd: %v\n", pname, err)
		os.Exit(1)
	}

	snapshots, err := w.GetStats(mac, start, nil)
	if err != nil {
		fmt.Printf("failed: %v\n", err)
		os.Exit(1)
	}

	for i, s := range snapshots {
		if len(s.Data) == 0 {
			continue
		}
		if i > 0 {
			fmt.Printf("\n--------------------\n")
		}
		fmt.Printf("%s - %s\n", s.Start.Format(time.Stamp),
			s.End.Format(time.Stamp))

		for mac, d := range s.Data {
			var label string
			if *devFlag == "all" {
				label = mac
			}
			showRecord(label, d)
		}
	}

	os.Exit(0)
}
