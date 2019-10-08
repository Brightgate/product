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

// Package wificaps provides information about the WiFi capabilities of a
// system's devices
package wificaps

import (
	"fmt"
	"io/ioutil"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"bg/ap_common/platform"
	"bg/common/wifi"
)

type capability struct {
	mask  uint64
	match uint64
	name  string
}

// ChannelLists is the classification by band and width of 802.11 channels used
// in the channel selection algorithm.  The intersection of these lists, the
// regulatory legalChannel list (in ap.networkd), and the per-device list of
// supported frequencies is used to choose a channel.
var ChannelLists = map[string][]int{
	"loBand20MHz":     {1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11},
	"loBandNoOverlap": {1, 6, 11},
	"hiBand20MHz": {36, 40, 44, 48, 52, 56, 60, 64, 100, 104, 108, 112, 116,
		120, 124, 128, 132, 136, 140, 144, 149, 153, 157, 161, 165},
	// These numbers are not the centers of the 40MHz channels, as shown in
	// many channel diagrams and the List of WLAN channels Wikipedia page.
	// Instead, they are the channel number of the primary 20MHz channel
	// component of the 40MHz channel (whether above or below the primary).
	// This is how hostapd expects you to tell it what channel to run on, as
	// well as how the Mac client interface numbers them.  See also
	// initChannelLists() in hostapd.go.
	"hiBand40MHz": {36, 40, 44, 48, 52, 56, 60, 64, 100, 104, 108, 112, 116,
		120, 124, 128, 132, 136, 140, 144, 149, 153, 157, 161},
	"hiBand80MHz": {36, 52, 100, 116, 132, 149},
}

// WifiCapabilities represents the attributes of a wireless device which are
// useful to the Brightgate stack.
type WifiCapabilities struct {
	SupportVLANs    bool            // does the nic support VLANs?
	Interfaces      int             // number of APs it can support
	Channels        map[int]bool    // channels the device claims to support
	WifiBands       map[string]bool // frequency bands it supports
	WifiModes       map[string]bool // 802.11[a,b,g,n,ac] modes supported
	HTCapabilities  map[int]bool    // 802.11n capabilities supported
	VHTCapabilities map[int]bool    // 802.11ac capabilities supported
}

// Does this device support VLANs?
func getVlanSupport(w *WifiCapabilities, info string) {
	// Look for "AP/VLAN" as a supported "software interface mode"
	// Ignore "AP/VLAN:", which appears under "TX frame types"
	vlanRE := regexp.MustCompile(`AP/VLAN[^:]`)

	vlanModes := vlanRE.FindAllStringSubmatch(info, -1)
	w.SupportVLANs = (len(vlanModes) > 0)
}

// How many APs can this device support?
func getInterfaces(w *WifiCapabilities, info string) {
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
				w.Interfaces, _ = strconv.Atoi(s[len(s)-1])
			}
		}
	}
}

// Which channel/frequencies does this device support?
func getChannels(w *WifiCapabilities, info string) {
	w.WifiBands = make(map[string]bool)
	w.Channels = make(map[int]bool)

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
		w.Channels[channel] = true

		frequency, _ := strconv.Atoi(line[1])
		if frequency <= 2484 {
			w.WifiBands[wifi.LoBand] = true
		} else if frequency >= 5035 {
			w.WifiBands[wifi.HiBand] = true
		}
	}
}

// Figure out which frequency widths this device supports
func getCapabilities(w *WifiCapabilities, info string) {
	htRE := regexp.MustCompile(`\sCapabilities: 0x([[:xdigit:]]+)`)
	vhtRE := regexp.MustCompile(`VHT Capabilities \(0x([[:xdigit:]]+)\)`)

	w.HTCapabilities = make(map[int]bool)
	w.VHTCapabilities = make(map[int]bool)

	if caps := htRE.FindStringSubmatch(info); len(caps) == 2 {
		flags, _ := strconv.ParseUint(caps[1], 16, 64)
		for i, c := range htCaps {
			if (flags & c.mask) == c.match {
				w.HTCapabilities[i] = true
			}
		}
	}

	if caps := vhtRE.FindStringSubmatch(info); len(caps) == 2 {
		flags, _ := strconv.ParseUint(caps[1], 16, 64)
		for i, c := range vhtCaps {
			if (flags & c.mask) == c.match {
				w.VHTCapabilities[i] = true
			}

		}
	}
}

// Using some very crude heuristics, try to determine which wifi modes this
// device supports
func getWifiModes(w *WifiCapabilities, info string) {
	w.WifiModes = make(map[string]bool)

	// 2.4GHz frequencies imply mode 802.11g support
	if w.WifiBands[wifi.LoBand] {
		w.WifiModes["g"] = true
	}

	// 5GHz frequencies imply mode 802.11a support
	if w.WifiBands[wifi.HiBand] {
		w.WifiModes["a"] = true
	}

	// High Throughput implies 802.11n
	modeN := regexp.MustCompile(`(HT20|HT40)`)
	if modeN.MatchString(info) {
		w.WifiModes["n"] = true
	}

	// Very High Throughput implies 802.11ac
	modeAC := regexp.MustCompile(`VHT Capabilities`)
	if modeAC.MatchString(info) {
		w.WifiModes["ac"] = true
	}
}

func buildCapabilitiesString(all map[int]capability, found map[int]bool) string {
	list := make([]int, 0)

	for candidate := range found {
		list = append(list, candidate)
	}
	sort.Ints(list)

	rval := ""
	for _, c := range list {
		next := "?"
		if cap, ok := all[c]; ok {
			next = cap.name
		}

		rval += "[" + next + "]"
	}

	return rval
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

// String implements the Stringer interface for WifiCapabilities objects.
func (w *WifiCapabilities) String() string {
	allModes := []string{"a", "g", "n", "ac"}
	modes := make([]string, 0)
	for _, mode := range allModes {
		if w.WifiModes[mode] {
			modes = append(modes, mode)
		}
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("   Supported modes: %s\n", strings.Join(modes, "/")))
	b.WriteString(fmt.Sprintf("   Supported interfaces: %d\n", w.Interfaces))
	b.WriteString(fmt.Sprintf("   VLAN support: %v\n", w.SupportVLANs))

	b.WriteString(fmt.Sprintf("   2.4GHz Band:\n"))
	b.WriteString(fmt.Sprintf("      20MHz: %s\n",
		buildChannelString(ChannelLists["loBand20MHz"], w.Channels)))

	b.WriteString(fmt.Sprintf("   5GHz Band:\n"))
	b.WriteString(fmt.Sprintf("      20MHz: %s\n",
		buildChannelString(ChannelLists["hiBand20MHz"], w.Channels)))
	if w.HTCapabilities[HTCAP_HT20_40] {
		b.WriteString(fmt.Sprintf("      40MHz: %s\n",
			buildChannelString(ChannelLists["hiBand40MHz"], w.Channels)))
	}
	if len(w.VHTCapabilities) > 0 {
		b.WriteString(fmt.Sprintf("      80MHz: %s\n",
			buildChannelString(ChannelLists["hiBand80MHz"], w.Channels)))
	}
	b.WriteString(fmt.Sprintf("   HT Capabilities: %s\n",
		buildCapabilitiesString(htCaps, w.HTCapabilities)))
	b.WriteString(fmt.Sprintf("   VHT Capabilities: %s\n",
		buildCapabilitiesString(vhtCaps, w.VHTCapabilities)))

	return b.String()
}

// GetCapabilities takes the name of a wireless device (typically "wlanX") and
// returns a pointer to the WifiCapabilities object which represents it.
func GetCapabilities(name string) (*WifiCapabilities, error) {
	var w WifiCapabilities

	data, err := ioutil.ReadFile("/sys/class/net/" + name + "/phy80211/name")
	if err != nil {
		return nil, fmt.Errorf("couldn't get phy: %v", err)
	}
	phy := strings.TrimSpace(string(data))

	plat := platform.NewPlatform()
	out, err := exec.Command(plat.IwCmd, "phy", phy, "info").Output()
	if err != nil {
		return nil, fmt.Errorf("iw info failed: %v", err)
	}
	info := string(out)

	getVlanSupport(&w, info)
	getInterfaces(&w, info)
	getChannels(&w, info)
	getWifiModes(&w, info)
	getCapabilities(&w, info)

	return &w, nil
}
