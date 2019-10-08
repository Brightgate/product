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
	"bg/common/wifi"
)

type wifiConfig struct {
	radiusSecret string
	domain       string
}

var (
	bands = []string{wifi.LoBand, wifi.HiBand}

	wifiEvaluate bool
	wconf        wifiConfig
)

type wifiInfo struct {
	state string

	configBand    string // user-configured band
	configChannel int    // user-configured channel
	configWidth   int    // user-configured channel width

	activeBand    string // band actually being used
	activeChannel int    // channel actually being used
	activeWidth   int    // witdh of channel actually being used

	cap *wificaps.WifiCapabilities // What features the device supports
}

// All legal wifi channels
var legalChannels map[int]bool

// For each band (i.e., these are the channels we are legally allowed to use
// in this region.
var bandChannels map[string]map[int]bool

// For each channel, this represents a set of the legal channel widths
var channelWidths map[int]map[int]bool

func setChannel(w *wifiInfo, band string, channel, width int) error {
	if width == 0 {
		// XXX - update for 160MHz wide channels
		if w.cap.WifiModes["ac"] && channelWidths[80][channel] {
			width = 80
		} else if channelWidths[40][channel] {
			width = 40
		} else {
			width = 20
		}
	}

	if !w.cap.Channels[channel] {
		return fmt.Errorf("channel %d not supported on this nic",
			channel)
	}
	if !bandChannels[band][channel] {
		return fmt.Errorf("channel %d not valid on %s", channel, band)
	}
	if !channelWidths[width][channel] {
		return fmt.Errorf("width %d not valid for channel %d",
			width, channel)
	}

	w.activeBand = band
	w.activeWidth = width
	w.activeChannel = channel
	return nil
}

// From a list of possible channels, select one at random that is supported by
// this wifi device
func randomChannel(w *wifiInfo, band, listName string, width int) {
	if w.configWidth != 0 && w.configWidth != width {
		return
	}

	list := wificaps.ChannelLists[listName]
	start := rand.Int() % len(list)
	idx := start
	for {
		if setChannel(w, band, list[idx], width) == nil {
			break
		}

		if idx++; idx == len(list) {
			idx = 0
		}
		if idx == start {
			break
		}
	}
}

// Choose a channel for this wifi device from within the specified band
func selectWifiChannel(d *physDevice, band string) error {
	var err error

	w := d.wifi
	if w.state != wifi.DevOK {
		return fmt.Errorf("device in bad state: %s", w.state)
	}

	if !w.cap.WifiBands[band] {
		return fmt.Errorf("doesn't support %s", band)
	}

	if w.configChannel != 0 {
		err = setChannel(w, band, w.configChannel, w.configWidth)
		if err != nil {
			w.state = wifi.DevBadChan
			err = fmt.Errorf("setChannel failed: %v", err)
		}
		return err
	}

	if band == wifi.LoBand {
		// We first try to choose one of the non-overlapping channels.
		// If that fails, we'll take any channel in this range.
		randomChannel(w, band, "loBandNoOverlap", 20)
		if w.activeChannel == 0 {
			randomChannel(w, band, "loBand20MHz", 20)
		}
	} else {
		if w.cap.WifiModes["ac"] {
			// XXX: update for 160MHz and 80+80
			randomChannel(w, band, "hiBand80MHz", 80)
		}
		if w.activeChannel == 0 && w.cap.HTCapabilities[wificaps.HTCAP_HT20_40] {
			randomChannel(w, band, "hiBand40MHz", 40)
		}
		if w.activeChannel == 0 {
			randomChannel(w, band, "hiBand20MHz", 20)
		}
	}

	if w.activeChannel == 0 {
		w.state = wifi.DevNoChan
		err = fmt.Errorf("no channels available")
	}
	return err
}

// How desirable is it to use this device in this band?
func score(d *physDevice, band string) int {
	var score int

	if d == nil || d.pseudo || d.wifi == nil {
		return 0
	}

	w := d.wifi
	if w.state != wifi.DevOK {
		return 0
	}

	if !w.cap.SupportVLANs || w.cap.Interfaces <= 1 || !w.cap.WifiBands[band] {
		return 0
	}

	if w.configBand != "" && w.configBand != band {
		return 0
	}

	if w.configChannel != 0 && !bandChannels[band][w.configChannel] {
		return 0
	}

	if band == wifi.LoBand {
		// We always want at least one NIC in the 2.4GHz range, so they
		// get an automatic bump
		score = 10
	}

	if w.cap.WifiModes["n"] {
		score = score + 1
	}

	if band == wifi.HiBand && w.cap.WifiModes["ac"] {
		score = score + 2
	}

	return score
}

// Examine the config settings for this device to determine whether they are
// legal and supported.
func setState(d *physDevice) {
	w := d.wifi
	if w == nil {
		return
	}
	w.state = wifi.DevOK

	if d.disabled {
		w.state = wifi.DevDisabled
		return
	}

	b := w.configBand
	if b != "" {
		if b != wifi.LoBand && b != wifi.HiBand {
			w.state = wifi.DevIllegalBand
		} else if !w.cap.WifiBands[b] {
			w.state = wifi.DevUnsupportedBand
		}
	}

	c := w.configChannel
	if c != 0 {
		if !legalChannels[c] {
			w.state = wifi.DevIllegalChan
		} else if b != "" && !bandChannels[b][c] {
			w.state = wifi.DevBadChan
		} else if !w.cap.Channels[c] {
			w.state = wifi.DevUnsupportedChan
		}
	}
}

func selectWifiDevices(oldList []*physDevice) []*physDevice {
	var selected map[string]*physDevice

	if !wifiEvaluate {
		return oldList
	}
	wifiEvaluate = false

	for _, d := range physDevices {
		setState(d)
	}

	best := 0
	for _, d := range oldList {
		if d.wifi.state != wifi.DevOK {
			// If one of the devices we were using now has a bad
			// config, we set 'best' to an invalid value to force a
			// reselection.
			best = -1
		}
		if d.wifi.activeBand != "" {
			best = best + score(d, d.wifi.activeBand)
		}
	}

	// Iterate over all possible combinations to find the pair of devices
	// that supports the most desirable combination of modes.
	oldScore := best
	for _, a := range physDevices {
		for _, b := range physDevices {
			if a == b {
				continue
			}
			scoreA := score(a, wifi.LoBand)
			scoreB := score(b, wifi.HiBand)
			if scoreA+scoreB > best {
				selected = make(map[string]*physDevice)
				if scoreA > 0 {
					selected[wifi.LoBand] = a
				}
				if scoreB > 0 {
					selected[wifi.HiBand] = b
				}
				best = scoreA + scoreB
			}
		}
	}

	if best == oldScore {
		return oldList
	}

	for _, d := range physDevices {
		if d.wifi != nil {
			d.wifi.activeChannel = 0
			d.wifi.activeWidth = 0
			d.wifi.activeBand = ""
		}
	}

	newList := make([]*physDevice, 0)
	for _, band := range bands {
		if d := selected[band]; d != nil {
			if err := selectWifiChannel(d, band); err != nil {
				slog.Warnf("%v", err)
			} else {
				newList = append(newList, d)
			}
		}
	}

	return newList
}

func makeValidChannelMaps() {
	widths := map[int][]int{
		20: append(
			wificaps.ChannelLists["loBand20MHz"],
			wificaps.ChannelLists["hiBand20MHz"]...),
		40: wificaps.ChannelLists["hiBand40MHz"],
		80: wificaps.ChannelLists["hiBand80MHz"],
	}

	// Convert the arrays of channels into channel- and width-indexed maps,
	// for easier lookup.
	legalChannels = make(map[int]bool)
	bandChannels = make(map[string]map[int]bool)
	for _, band := range bands {
		bandChannels[band] = make(map[int]bool)
		for _, channel := range wifi.Channels[band] {
			bandChannels[band][channel] = true
			legalChannels[channel] = true
		}
	}

	channelWidths = make(map[int]map[int]bool)
	for width, list := range widths {
		channelWidths[width] = make(map[int]bool)
		for _, channel := range list {
			channelWidths[width][channel] = true
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
