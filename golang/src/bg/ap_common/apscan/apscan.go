/*
 * COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package apscan

import (
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"time"

	"bg/ap_common/platform"
)

// ScannedAP carries information gathered when scanning for APs
type ScannedAP struct {
	Mac       string
	SSID      string
	Mode      string
	Channel   int
	Secondary int
	Width     int
	Strength  int
	LastSeen  time.Duration
}

var (
	octet   = `[[:xdigit:]][[:xdigit:]]`
	macAddr = octet + `:` + octet + `:` + octet + `:` +
		octet + `:` + octet + `:` + octet

	scanSplitRE = regexp.MustCompile(`(?m)^BSS`)

	// BSS 98:1e:19:20:79:df(on wlan0)
	bssMacRE = regexp.MustCompile(`^BSS (` + macAddr + `)`)

	// signal: -84.00 dBm
	bssSignalRE = regexp.MustCompile(`\ssignal: ([-|\.|\d]+)\sdBm`)

	// last seen: 360 ms ago
	bssSeenRE = regexp.MustCompile(`\slast seen: ([\d]+) ms ago`)

	// SSID: MySpectrumWiFid8-5G
	bssSSIDRE = regexp.MustCompile(`\sSSID: (.+)`)

	// * primary channel: 149
	bssChanRE = regexp.MustCompile(`\s\* primary channel: ([\d]+)`)

	// HT capabilities:
	bssHTRE = regexp.MustCompile(`\s+HT capabilities:`)

	// VHT capabilities:
	bssVHTRE = regexp.MustCompile(`\s+VHT capabilities:`)

	// * channel width: 1 (80 MHz)
	bssChanWidthRE = regexp.MustCompile(`\* channel width: \d+ \(([\d]+) MHz\)`)

	// * secondary channel offset: above
	// * secondary channel offset: no secondary
	bssSecondaryRE = regexp.MustCompile(`\* secondary channel offset: ([\S]+)`)

	plat *platform.Platform
)

func getFloatRE(data string, re *regexp.Regexp) float64 {
	var rval float64

	r := re.FindStringSubmatch(data)
	if len(r) > 1 {
		rval, _ = strconv.ParseFloat(r[1], 64)
	}
	return rval
}

func getIntRE(data string, re *regexp.Regexp) int {
	var rval int

	r := re.FindStringSubmatch(data)
	if len(r) > 1 {
		rval, _ = strconv.Atoi(r[1])
	}
	return rval
}

func getStringRE(data string, re *regexp.Regexp) string {
	var rval string

	r := re.FindStringSubmatch(data)
	if len(r) > 1 {
		rval = r[1]
	}
	return rval
}

func parseOneBSS(data string) *ScannedAP {
	ap := ScannedAP{
		Mac:      getStringRE(data, bssMacRE),
		SSID:     getStringRE(data, bssSSIDRE),
		Strength: int(getFloatRE(data, bssSignalRE)),
		Channel:  int(getIntRE(data, bssChanRE)),
	}

	ht := bssHTRE.FindString(data) != ""
	vht := bssVHTRE.FindString(data) != ""
	secondary := getStringRE(data, bssSecondaryRE)
	if ap.Channel < 32 {
		ap.Mode = "b/g"
	} else {
		ap.Mode = "a"
	}
	if vht {
		ap.Mode = "ac"
	} else if ht {
		ap.Mode += "/n"
	}

	if vht {
		ap.Width = getIntRE(data, bssChanWidthRE)
	} else if secondary == "above" {
		ap.Width = 40
		ap.Secondary = ap.Channel + 4
	} else if secondary == "below" {
		ap.Width = 40
		ap.Secondary = ap.Channel - 4
	} else {
		ap.Width = 20
	}

	d := getIntRE(data, bssSeenRE)
	ap.LastSeen = time.Duration(d) * time.Millisecond

	return &ap
}

func parseIwOutput(data string) []*ScannedAP {
	// Split the output from the 'iw dev <iface> scan' into per-BSS stanzas
	all := make([]string, 0)

	a := scanSplitRE.FindAllStringSubmatchIndex(data, -1)

	for i, s := range a {
		var end int
		if i < len(a)-1 {
			end = a[i+1][0]
		} else {
			end = len(data)
		}
		all = append(all, data[s[0]:end])
	}

	// parse each of the stanzas
	aps := make([]*ScannedAP, 0)
	for _, bss := range all {
		aps = append(aps, parseOneBSS(bss))
	}

	return aps
}

// ScanIface will use the provided interface to scan for nearby APs.  It returns
// a slice of ScannedAP structs containing per-AP information about each it
// found.
func ScanIface(iface string) []*ScannedAP {
	var aps []*ScannedAP

	if plat == nil {
		plat = platform.NewPlatform()
	}

	cmd := exec.Command(plat.IwCmd, "dev", iface, "scan")
	out, err := cmd.CombinedOutput()
	if err != nil {
		err = fmt.Errorf("failed to scan %s: %s", iface, string(out))
	} else {
		aps = parseIwOutput(string(out))
	}

	return aps
}
