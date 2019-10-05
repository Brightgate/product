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
	"math/rand"
	"os/exec"
	"regexp"
	"strings"

	"bg/ap_common/wificaps"
	"bg/common/cfgapi"
)

type wifiConfig struct {
	radiusSecret string
	domain       string
}

var (
	bands = []string{wificaps.LoBand, wificaps.HiBand}

	wifiEvaluate bool
	wconf        wifiConfig
)

type wifiInfo struct {
	cfgBand    string // user-configured band
	cfgChannel int    // user-configured channel
	cfgWidth   int    // user-configured channel width

	activeBand    string // band actually being used
	activeChannel int    // channel actually being used
	activeWidth   int    // witdh of channel actually being used

	cap *wificaps.WifiCapabilities // What features the device supports
}

// For each band (i.e., these are the channels we are legally allowed to use
// in this region.
var legalChannels map[string]map[int]bool

// For each channel, this represents a set of the legal channel widths
var legalWidths map[int]map[int]bool

func setChannel(w *wifiInfo, channel, width int) error {
	band := w.activeBand

	if width == 0 {
		// XXX - update for 160MHz wide channels
		if w.cap.WifiModes["ac"] && legalWidths[80][channel] {
			width = 80
		} else if legalWidths[40][channel] {
			width = 40
		} else {
			width = 20
		}
	}

	if !w.cap.Channels[channel] {
		return fmt.Errorf("channel %d not supported on this nic",
			channel)
	}
	if !legalChannels[band][channel] {
		return fmt.Errorf("channel %d not valid on %s", channel, band)
	}
	if !legalWidths[width][channel] {
		return fmt.Errorf("width %d not valid for channel %d",
			width, channel)
	}

	w.activeWidth = width
	w.activeChannel = channel
	return nil
}

// From a list of possible channels, select one at random that is supported by
// this wifi device
func randomChannel(w *wifiInfo, listName string, width int) error {
	if w.cfgWidth != 0 && w.cfgWidth != width {
		return fmt.Errorf("device configured for %d", w.cfgWidth)
	}

	list := wificaps.ChannelLists[listName]
	band := w.activeBand

	start := rand.Int() % len(list)
	idx := start
	for {
		if setChannel(w, list[idx], width) == nil {
			return nil
		}

		if idx++; idx == len(list) {
			idx = 0
		}
		if idx == start {
			return fmt.Errorf("no available channels for band %s", band)
		}
	}
}

// Choose a channel for this wifi device from within its configured band.
func selectWifiChannel(d *physDevice) error {
	var err error

	if d == nil || d.wifi == nil {
		return fmt.Errorf("not a wireless device")
	}

	w := d.wifi
	band := w.activeBand

	w.activeChannel = 0
	if !w.cap.WifiBands[band] {
		return fmt.Errorf("doesn't support %s", band)
	}

	// If the user has configured a channel for this nic, try that first.
	if w.cfgChannel != 0 {
		if err = setChannel(w, w.cfgChannel, w.cfgWidth); err == nil {
			return nil
		}
		slog.Debugf("setChannel failed: %v", err)
	}

	if band == wificaps.LoBand {
		// We first try to choose one of the non-overlapping channels.
		// If that fails, we'll take any channel in this range.
		if err = randomChannel(w, "loBandNoOverlap", 20); err != nil {
			err = randomChannel(w, "loBand20MHz", 20)
		}
	} else {
		if w.cap.WifiModes["ac"] {
			// XXX: update for 160MHz and 80+80
			randomChannel(w, "hiBand80MHz", 80)
		}
		if w.activeChannel == 0 && w.cap.HTCapabilities[wificaps.HTCAP_HT20_40] {
			randomChannel(w, "hiBand40MHz", 40)
		}
		if w.activeChannel == 0 {
			err = randomChannel(w, "hiBand20MHz", 20)
		}
	}

	return err
}

// How desirable is it to use this device in this band?
func score(d *physDevice, band string) int {
	var score int

	if d == nil || d.pseudo || d.wifi == nil || d.disabled {
		return 0
	}

	w := d.wifi
	if !w.cap.SupportVLANs || w.cap.Interfaces <= 1 || !w.cap.WifiBands[band] {
		return 0
	}

	if w.cfgBand != "" && w.cfgBand != band {
		return 0
	}

	if band == wificaps.LoBand {
		// We always want at least one NIC in the 2.4GHz range, so they
		// get an automatic bump
		score = 10
	}

	if w.cap.WifiModes["n"] {
		score = score + 1
	}

	if band == wificaps.HiBand && w.cap.WifiModes["ac"] {
		score = score + 2
	}

	return score
}

func selectWifiDevices(oldList []*physDevice) []*physDevice {
	var selected map[string]*physDevice

	if !wifiEvaluate {
		return oldList
	}
	wifiEvaluate = false

	best := 0
	for _, d := range oldList {
		if d.disabled {
			best = -1
			break
		}
		best = best + score(d, d.wifi.activeBand)
	}

	// Iterate over all possible combinations to find the pair of devices
	// that supports the most desirable combination of modes.
	oldScore := best
	for _, a := range physDevices {
		for _, b := range physDevices {
			if a == b {
				continue
			}
			scoreA := score(a, wificaps.LoBand)
			scoreB := score(b, wificaps.HiBand)
			if scoreA+scoreB > best {
				selected = make(map[string]*physDevice)
				if scoreA > 0 {
					selected[wificaps.LoBand] = a
				}
				if scoreB > 0 {
					selected[wificaps.HiBand] = b
				}
				best = scoreA + scoreB
			}
		}
	}

	if best == oldScore {
		return oldList
	}

	newList := make([]*physDevice, 0)
	for idx, band := range bands {
		if d := selected[band]; d != nil {
			d.wifi.activeBand = bands[idx]
			if err := selectWifiChannel(d); err != nil {
				slog.Warnf("%v", err)
			} else {
				newList = append(newList, d)
			}
		}
	}
	list := make([]string, 0)
	for _, d := range newList {
		list = append(list, d.name)
	}

	return newList
}

func makeValidChannelMaps() {
	// XXX: These (valid 20MHz) channel lists are legal for the US.  We will
	// need to ship per-country lists, presumably indexed by regulatory
	// domain.  In the US, with the exception of channel 165, all these
	// channels are valid primaries for 40MHz channels (see initChannelLists
	// in hostapd.go), though that is not true for all countries, and is not
	// true for 160MHz channels, once we start supporting those.
	channels := map[string][]int{
		wificaps.LoBand: {1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11},
		wificaps.HiBand: {36, 40, 44, 48, 52, 56, 60, 64, 100, 104, 108,
			112, 116, 120, 124, 128, 132, 136, 140, 144, 149, 153,
			157, 161, 165},
	}

	widths := map[int][]int{
		20: append(
			wificaps.ChannelLists["loBand20MHz"],
			wificaps.ChannelLists["hiBand20MHz"]...),
		40: wificaps.ChannelLists["hiBand40MHz"],
		80: wificaps.ChannelLists["hiBand80MHz"],
	}

	// Convert the arrays of channels into channel- and width-indexed maps,
	// for easier lookup.
	legalChannels = make(map[string]map[int]bool)
	for _, band := range bands {
		legalChannels[band] = make(map[int]bool)
		for _, channel := range channels[band] {
			legalChannels[band][channel] = true
		}
	}

	legalWidths = make(map[int]map[int]bool)
	for width, list := range widths {
		legalWidths[width] = make(map[int]bool)
		for _, channel := range list {
			legalWidths[width][channel] = true
		}
	}
}

func globalWifiInit(props *cfgapi.PropertyNode) error {
	locationRE := regexp.MustCompile(`^[A-Z][A-Z]$`)

	makeValidChannelMaps()

	wconf.domain = "US"
	if x, ok := props.Children["regdomain"]; ok {
		t := []byte(strings.ToUpper(x.Value))
		if !locationRE.Match(t) {
			slog.Warnf("Illegal @/network/regdomain: %s", x.Value)
		} else {
			wconf.domain = x.Value
		}
	}

	slog.Infof("Setting regulatory domain to %s", wconf.domain)
	cmd := exec.Command(plat.IwCmd, "reg", "set", wconf.domain)
	out, err := cmd.CombinedOutput()
	if err != nil {
		slog.Warnf("Failed to set domain: %v%s\n", err, out)
	}

	if node, ok := props.Children["radius_auth_secret"]; ok {
		wconf.radiusSecret = node.Value
	} else {
		slog.Warnf("no radius_auth_secret configured")
	}

	wifiEvaluate = true

	return nil
}
