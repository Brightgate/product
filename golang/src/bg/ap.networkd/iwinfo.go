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
	"fmt"
	"io/ioutil"
	"log"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

func getVlanSupport(w *wifiInfo, info string) {
	// Look for "AP/VLAN" as a supported "software interface mode"
	vlanRE := regexp.MustCompile(`AP/VLAN`)

	vlanModes := vlanRE.FindAllStringSubmatch(info, -1)
	w.supportVLANs = (len(vlanModes) > 0)
}

func getInterfaces(w *wifiInfo, info string) {
	// Match interface combination lines:
	//   #{ managed } <= 1, #{ AP } <= 1, #{ P2P-client } <= 1
	comboRE := regexp.MustCompile(`#{ [\w\-, ]+ } <= [0-9]+`)

	//
	// Examine the "valid interface combinations" to see if any include more
	// than one AP.  This one does:
	//    #{ AP, mesh point } <= 8,
	// This one doesn't:
	//    #{ managed } <= 1, #{ AP } <= 1, #{ P2P-client } <= 1,
	//
	combos := comboRE.FindAllStringSubmatch(info, -1)
	for _, line := range combos {
		for _, combo := range line {
			s := strings.Split(combo, " ")
			if len(s) > 0 && strings.Contains(combo, "AP") {
				w.interfaces, _ = strconv.Atoi(s[len(s)-1])
			}
		}
	}
}

// Which channel/frequencies does this device support?
func getChannels(w *wifiInfo, info string) {
	w.wifiBands = make(map[string]bool)
	w.channels = make(map[int]bool)

	// Match channel/frequency lines:
	//   * 2462 MHz [11] (20.0 dBm)
	chanRE := regexp.MustCompile(`\* (\d+) MHz \[(\d+)\] \((.*)\)`)
	channels := chanRE.FindAllStringSubmatch(info, -1)
	for _, line := range channels {
		// Skip any channels that are unavailable for either technical
		// or regulatory reasons
		if strings.Contains(line[3], "disabled") ||
			strings.Contains(line[3], "no IR") ||
			strings.Contains(line[3], "radar detection") {
			continue
		}
		channel, _ := strconv.Atoi(line[2])
		w.channels[channel] = true

		frequency, _ := strconv.Atoi(line[1])
		if frequency <= 2484 {
			w.wifiBands[loBand] = true
		} else if frequency >= 5035 {
			w.wifiBands[hiBand] = true
		}
	}
}

// Figure out which frequency widths this device supports
func getFrequencyWidths(w *wifiInfo, info string) {
	// Match capabilities line:
	// Capabilities: 0x2fe
	capRE := regexp.MustCompile(`Capabilities: 0x([[:xdigit:]]+)`)

	w.freqWidths = make(map[int]bool)
	bcaps := capRE.FindAllStringSubmatch(info, -1)
	for _, c := range bcaps {
		// If bit 1 is set, then the device supports both 20MHz and
		// 40MHz.  If it isn't, then it only supports 20MHz.
		w.freqWidths[20] = true
		if len(c) == 2 {
			flags, _ := strconv.ParseUint(c[1], 16, 64)
			if (flags & (1 << 1)) != 0 {
				w.freqWidths[40] = true
			}
		}
	}
	// XXX - add 80 and 160
}

// Using some very crude heuristics, try to determine which wifi modes this
// device supports
func getWifiModes(w *wifiInfo, info string) {
	w.wifiModes = make(map[string]bool)

	// 2.4GHz frequencies imply mode 802.11g support
	if w.wifiBands[loBand] {
		w.wifiModes["g"] = true
	}

	// 5GHz frequencies imply mode 802.11a support
	if w.wifiBands[hiBand] {
		w.wifiModes["a"] = true
	}

	// High Throughput implies 802.11n
	modeN := regexp.MustCompile(`(HT20|HT40)`)
	if modeN.MatchString(info) {
		w.wifiModes["n"] = true
	}

	// Very High Throughput implies 802.11ac
	modeAC := regexp.MustCompile(`VHT Capabilities`)
	if modeAC.MatchString(info) {
		w.wifiModes["ac"] = true
	}
}

func buildChannelString(all []int, found map[int]bool) string {
	list := make([]string, 0)

	for _, candidate := range all {
		if found[candidate] {
			list = append(list, strconv.Itoa(candidate))
		}
	}

	return strings.Join(list, ",")
}

func dumpInfo(name string, w *wifiInfo) {
	log.Printf("device: %s\n", name)

	allModes := []string{"a", "g", "n", "ac"}
	modes := make([]string, 0)
	for _, mode := range allModes {
		if w.wifiModes[mode] {
			modes = append(modes, mode)
		}
	}
	log.Printf("   Supported modes: %s\n", strings.Join(modes, "/"))
	log.Printf("   Supported interfaces: %d\n", w.interfaces)
	log.Printf("   VLAN support: %v\n", w.supportVLANs)

	log.Printf("   2.4GHz Band:\n")
	log.Printf("      20MHz: %s\n",
		buildChannelString(channelLists["loBand20MHz"], w.channels))

	log.Printf("   5GHz Band:\n")
	log.Printf("      20MHz: %s\n",
		buildChannelString(channelLists["hiBand20MHz"], w.channels))
	log.Printf("      40MHz: %s\n",
		buildChannelString(channelLists["hiBand40MHz"], w.channels))
}

func iwinfo(name string) (*wifiInfo, error) {
	var w wifiInfo

	data, err := ioutil.ReadFile("/sys/class/net/" + name + "/phy80211/name")
	if err != nil {
		return nil, fmt.Errorf("couldn't get phy: %v", err)
	}
	phy := strings.TrimSpace(string(data))

	out, err := exec.Command(plat.IwCmd, "phy", phy, "info").Output()
	if err != nil {
		return nil, fmt.Errorf("iwinfo failed: %v", err)
	}
	info := string(out)

	getVlanSupport(&w, info)
	getInterfaces(&w, info)
	getChannels(&w, info)
	getFrequencyWidths(&w, info)
	getWifiModes(&w, info)

	dumpInfo(name, &w)

	return &w, nil
}
