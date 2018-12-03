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
	"strconv"
	"strings"

	"bg/ap_common/wificaps"
	"bg/common/cfgapi"
)

type wifiConfig struct {
	ssid         string
	ssidEAP      string
	ssid5GHz     string
	ssid5GHzEAP  string
	channels     map[string]int
	passphrase   string
	radiusSecret string
}

var (
	bands = []string{wificaps.LoBand, wificaps.HiBand}

	wifiEvaluate bool
	wconf        wifiConfig
)

type wifiInfo struct {
	cfgBand    string // user-configured band
	cfgChannel int    // user-configured channel

	activeBand    string // band actually being used
	activeChannel int    // channel actually being used

	cap *wificaps.WifiCapabilities // What features the device supports
}

// For each band (i.e., these are the channels we are legally allowed to use
// in this region.
var legalChannels map[string]map[int]bool

func setChannel(w *wifiInfo, channel int) error {
	band := w.activeBand
	if w.cap.Channels[channel] && legalChannels[band][channel] {
		w.activeChannel = channel
		return nil
	}
	return fmt.Errorf("channel %d not valid on %s", channel, band)
}

// From a list of possible channels, select one at random that is supported by
// this wifi device
func randomChannel(w *wifiInfo, list []int) error {
	band := w.activeBand

	start := rand.Int() % len(list)
	idx := start
	for {
		if setChannel(w, list[idx]) == nil {
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
		if err = setChannel(w, w.cfgChannel); err == nil {
			return nil
		}
		slog.Debugf("nic-specific %v", err)
	}

	// If the user has configured a channel for this band, try that next.
	if wconf.channels[band] != 0 {
		if err = setChannel(w, wconf.channels[band]); err == nil {
			return nil
		}
		slog.Debugf("band-specific %v", err)
	}

	if band == wificaps.LoBand {
		// We first try to choose one of the non-overlapping channels.
		// If that fails, we'll take any channel in this range.
		err = randomChannel(w, wificaps.ChannelLists["loBandNoOverlap"])
		if err != nil {
			err = randomChannel(w, wificaps.ChannelLists["loBand20MHz"])
		}
	} else {
		// Start by trying to get a wide channel.  If that fails, take
		// any narrow channel.
		// XXX: this gets more complicated with 802.11ac support.
		if w.cap.FreqWidths[40] {
			randomChannel(w, wificaps.ChannelLists["hiBand40MHz"])
		}
		if w.activeChannel == 0 {
			err = randomChannel(w, wificaps.ChannelLists["hiBand20MHz"])
		}
	}

	return err
}

// How desirable is it to use this device in this band?
func score(d *physDevice, band string) int {
	var score int

	psk, eap := genSSIDs(band)
	if psk == "" && eap == "" {
		return 0
	}

	if d == nil || d.pseudo || d.wifi == nil {
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

	if w.cap.WifiModes["ac"] {
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

	// Convert the arrays of channels into channel-indexed maps, for easier
	// lookup.
	legalChannels = make(map[string]map[int]bool)
	for _, band := range bands {
		legalChannels[band] = make(map[int]bool)
		for _, channel := range channels[band] {
			legalChannels[band][channel] = true
		}
	}
}

func globalWifiInit(props *cfgapi.PropertyNode) error {
	locationRE := regexp.MustCompile(`^[A-Z][A-Z]$`)

	makeValidChannelMaps()

	domain := "US"
	if x, ok := props.Children["regdomain"]; ok {
		t := []byte(strings.ToUpper(x.Value))
		if !locationRE.Match(t) {
			slog.Warnf("Illegal @/network/regdomain: %s", x.Value)
		} else {
			domain = x.Value
		}
	}

	slog.Infof("Setting regulatory domain to %s", domain)
	out, err := exec.Command(plat.IwCmd, "reg", "set", domain).CombinedOutput()
	if err != nil {
		slog.Warnf("Failed to set domain: %v%s\n", err, out)
	}

	if node, ok := props.Children["radiusAuthSecret"]; ok {
		wconf.radiusSecret = node.Value
	} else {
		slog.Warnf("no radiusAuthSecret configured")
	}
	if node, ok := props.Children["ssid"]; ok {
		wconf.ssid = node.Value
	} else {
		slog.Warnf("no SSID configured")
	}
	if node, ok := props.Children["ssid-eap"]; ok {
		wconf.ssidEAP = node.Value
	}
	if node, ok := props.Children["ssid-5ghz"]; ok {
		wconf.ssid5GHz = node.Value
	}
	if node, ok := props.Children["ssid-eap-5ghz"]; ok {
		wconf.ssid5GHzEAP = node.Value
	}

	if node, ok := props.Children["passphrase"]; ok {
		wconf.passphrase = node.Value
	} else {
		slog.Warnf("no WPA-PSK passphrase configured")
	}

	wconf.channels = make(map[string]int)
	for _, band := range bands {
		if bprop, ok := props.Children[band]; ok {
			if c, ok := bprop.Children["channel"]; ok {
				wconf.channels[band], _ = strconv.Atoi(c.Value)
			}
		}
	}
	wifiEvaluate = true

	return nil
}
