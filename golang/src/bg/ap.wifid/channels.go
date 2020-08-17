/*
 * Copyright 2019 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package main

import (
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"bg/ap_common/apscan"
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
	nextChanEval time.Time
	wconf        wifiConfig
)

type wifiInfo struct {
	state string

	configBand    string // user-configured band
	configChannel int    // user-configured channel
	configWidth   int    // user-configured channel width

	activeMode    string // mode being used
	activeBand    string // band actually being used
	activeChannel int    // channel actually being used
	activeWidth   int    // witdh of channel actually being used

	cap *wificaps.WifiCapabilities // What features the device supports
}

var (
	// All legal wifi channels
	legalChannels map[int]bool

	// For each band (i.e., these are the channels we are legally allowed to use
	// in this region.
	bandChannels map[string]map[int]bool

	// For each channel, this represents a set of the legal channel widths
	channelWidths map[int]map[int]bool

	// In 802.11n, a 40MHz channel is constructed from 2 20MHz channels.
	// Whether the primary channel is above or below the secondary will
	// determine one of the ht_capab settings.
	nModePrimaryAbove map[int]bool
	nModePrimaryBelow map[int]bool
)

// used to track the potential interference from other APs
type apTrack struct {
	ssid     string
	channels []int
	strength int
	lastSeen time.Time
}

var (
	// For each width, there is a per-channel map tracking the estimated
	// congestion.
	congestionMap map[int]map[int]int

	// all of the nearby APs we've seen recently
	apMap  map[string]*apTrack
	apLock sync.Mutex
)

func wifiInfoEqual(a, b *wifiInfo) bool {
	return a.configChannel == b.configChannel &&
		a.activeChannel == b.activeChannel &&
		a.configWidth == b.configWidth &&
		a.activeWidth == b.activeWidth &&
		a.configBand == b.configBand &&
		a.activeBand == b.activeBand &&
		a.activeMode == b.activeMode &&
		a.state == b.state
}

// trigger a scan for nearby APs on the given device, and use the results to
// update the tracking map.
func updateAPScan(dev string) {
	aps := apscan.ScanIface(dev)
	now := time.Now()

	ourRadios := make(map[string]bool)
	for _, d := range wirelessNics {
		ourRadios[strings.ToLower(d.hwaddr)] = true
	}
	apLock.Lock()
	for _, ap := range aps {
		// Ignore old sightings
		if ap.LastSeen > 10*time.Second {
			continue
		}
		// Ignore nonsense results.  If they persist, the AP will simply
		// age out.
		if ap.Strength == 0 {
			continue
		}

		// Ignore our own radios
		if ourRadios[strings.ToLower(ap.Mac)] {
			continue
		}
		t := apMap[ap.Mac]
		if t == nil {
			t = &apTrack{}
			apMap[ap.Mac] = t
		}
		t.ssid = ap.SSID
		t.channels = wifi.ExpandChannels(ap.Channel, ap.Secondary,
			ap.Width)
		t.lastSeen = now.Add(-1 * ap.LastSeen)

		// The strength reading is somewhat noisy, so we keep a rolling
		// average to reduce the noise.
		t.strength = (t.strength*7 + ap.Strength) / 8
	}

	// Get rid of any APs we haven't seen in a while
	for mac, ap := range apMap {
		if time.Since(ap.lastSeen) > *apStale {
			delete(apMap, mac)
		}
	}
	apLock.Unlock()

	buildCongestionMap()
}

// Use the accumulated AP observations to build a table tracking the relative
// desirability of each channel.  Ideally we would measure the actual traffic in
// each channel to identify the least congested, but we don't have that
// information.  Instead, we use the number and strength of APs as a proxy for
// that measurement.
func buildCongestionMap() {
	cmap := make(map[int]map[int]int)
	cmap[20] = make(map[int]int)
	cmap[40] = make(map[int]int)
	cmap[80] = make(map[int]int)

	cnt := make(map[int]int)
	apLock.Lock()

	// First build the congestion map for the basic 20MHz ranges.  The raw
	// strength numbers are reported in dBm between -30 and -100, where -30
	// is stronger than -100.  Because our congestion estimates of narrow
	// channels are made by adding together per-AP values and our wide
	// estimates are made by adding the narrow results together, it's easier
	// to work with positive numbers.  So, we move each AP's strength into
	// the positive range by simply adding 100 to it.
	for _, ap := range apMap {
		for _, c := range ap.channels {
			// This simple math means we consider 2 APs at 30
			// to contribute as much congestion as 1 at 60.
			// (XXX: Because dBm is a logarithmic scale, this
			// simplification is mathematically bogus, but it may be
			// good enough for our purposes.)
			cmap[20][c] += (100 + ap.strength)
			cnt[c]++
		}
	}

	// Use the 20MHz data to construct the maps for the wider channels
	for _, c := range wificaps.ChannelLists["hiBand40MHz"] {
		if nModePrimaryAbove[c] {
			cmap[40][c] = cmap[20][c] + cmap[20][c-4]
		} else {
			cmap[40][c] = cmap[20][c] + cmap[20][c+4]
		}
	}

	for _, c := range wificaps.ChannelLists["hiBand80MHz"] {
		cmap[80][c] = cmap[20][c] + cmap[20][c+4] +
			cmap[20][c+8] + cmap[20][c+12]
	}
	congestionMap = cmap
	apLock.Unlock()
}

// Given a slice of channel numbers for a given width, sort the slice according
// to the calculated congestion on each channel.
func congestionSortList(list []int, width int) {
	sort.Slice(list, func(i, j int) bool {
		chanI := list[i]
		chanJ := list[j]
		return congestionMap[width][chanI] < congestionMap[width][chanJ]
	})
}

func copyChannelList(name string) []int {
	return append([]int(nil), wificaps.ChannelLists[name]...)
}

// Monitor the other APs we can see, so we can try to avoid using the same
// channel(s) they are.
func apMonitorLoop(wg *sync.WaitGroup, doneChan chan bool) {
	defer func() {
		slog.Infof("AP monitor loop exiting")
		wg.Done()
	}()

	freq := *apScanFreq
	t := time.NewTicker(freq)
	slog.Infof("AP monitor loop starting")
	for {
		select {
		case <-doneChan:
			return

		case <-t.C:
		}

		for _, d := range wirelessNics {
			if !d.pseudo {
				updateAPScan(d.name)
			}
		}

		if time.Now().After(nextChanEval) {
			wifiEvaluate = true
			hostapd.reset()
		}

		// If the frequency setting has been changed, reset our timer to
		// the new value.
		if freq != *apScanFreq {
			freq = *apScanFreq
			t.Stop()
			t = time.NewTicker(freq)
		}
	}
}

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

	if band == wifi.HiBand && w.cap.WifiModes["ac"] {
		w.activeMode = "ac"
	} else {
		if band == wifi.LoBand {
			w.activeMode = "b/g"
		} else {
			w.activeMode = "a"
		}
		if w.cap.WifiModes["n"] {
			w.activeMode += "/n"
		}
	}

	w.activeBand = band
	w.activeWidth = width
	w.activeChannel = channel
	return nil
}

// From a list of possible channels, try to select the least congested one
func findChannel(w *wifiInfo, band, listName string, width int) {
	if w.configWidth != 0 && w.configWidth != width {
		return
	}

	list := copyChannelList(listName)
	congestionSortList(list, width)
	slog.Debugf("congestion map for %dMHz %s: %v", width, band,
		congestionMap[width])

	for _, channel := range list {
		if err := setChannel(w, band, channel, width); err == nil {
			slog.Debugf("chose %d (width=%d) from %v", channel,
				width, list)
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

	oldInfo := *w

	w.activeChannel = 0
	if w.configChannel != 0 {
		err = setChannel(w, band, w.configChannel, w.configWidth)
		if err != nil {
			w.state = wifi.DevBadChan
			err = fmt.Errorf("setChannel failed: %v", err)
		}
		if !wifiInfoEqual(&oldInfo, w) {
			wifiDeviceToConfig(d)
			return err
		}
	}

	if band == wifi.LoBand {
		// We first try to choose one of the non-overlapping
		// channels.  If that fails, we'll take any channel in
		// this range.
		findChannel(w, band, "loBandNoOverlap", 20)
		if w.activeChannel == 0 {
			findChannel(w, band, "loBand20MHz", 20)
		}
	} else {
		if w.cap.WifiModes["ac"] {
			// XXX: update for 160MHz and 80+80
			findChannel(w, band, "hiBand80MHz", 80)
		}
		if w.activeChannel == 0 && w.cap.HTCapabilities[wificaps.HTCAP_HT20_40] {
			findChannel(w, band, "hiBand40MHz", 40)
		}
		if w.activeChannel == 0 {
			findChannel(w, band, "hiBand20MHz", 20)
		}
	}

	if w.activeChannel == 0 {
		w.state = wifi.DevNoChan
		err = fmt.Errorf("no channels available")
	}

	if !wifiInfoEqual(&oldInfo, w) {
		wifiDeviceToConfig(d)
	}
	return err
}

// How desirable is it to use this device in this band?
func score(d *physDevice, band string) int {
	var score int

	if d == nil || d.pseudo {
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

	if d.disabled {
		w.state = wifi.DevDisabled
		return
	}

	w.state = wifi.DevOK
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

func selectWifiChannels(devices []*physDevice) []*physDevice {
	nextChanEval = time.Now().Add(*chanEvalFreq)
	good := make([]*physDevice, 0)
	for _, d := range devices {
		err := selectWifiChannel(d, d.wifi.activeBand)
		if err != nil {
			d.wifi.activeBand = ""
			slog.Warnf("%v", err)
		} else {
			good = append(good, d)
		}
	}
	return good
}

func selectWifiDevices(oldList []*physDevice) []*physDevice {
	var selected map[string]*physDevice

	if !wifiEvaluate {
		return oldList
	}
	wifiEvaluate = false

	for _, d := range wirelessNics {
		if !d.pseudo {
			setState(d)
		}
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
	for _, a := range wirelessNics {
		for _, b := range wirelessNics {
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

	for _, d := range wirelessNics {
		d.wifi.activeChannel = 0
		d.wifi.activeWidth = 0
		d.wifi.activeBand = ""
	}

	newList := make([]*physDevice, 0)
	for _, band := range bands {
		if d := selected[band]; d != nil {
			if d.pseudo {
				slog.Panicf("selected pseudo nic: %v / %d",
					d, d.wifi)
			}
			d.wifi.activeBand = band
			newList = append(newList, d)
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

	// The 2.4GHz band is crowded, so the use of 40MHz bonded channels is
	// discouraged.  Thus, the following lists only include channels in the
	// 5GHz band.
	above := []int{36, 44, 52, 60, 100, 108, 116, 124, 132, 140, 149, 157}
	below := []int{40, 48, 56, 64, 104, 112, 120, 128, 136, 144, 153, 161}

	nModePrimaryAbove = make(map[int]bool)
	for _, c := range above {
		nModePrimaryAbove[c] = true
	}

	nModePrimaryBelow = make(map[int]bool)
	for _, c := range below {
		nModePrimaryBelow[c] = true
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

	wconf.radiusSecret, _ = props.GetChildString("radius_auth_secret")
	if wconf.radiusSecret == "" {
		slog.Warnf("no radius_auth_secret configured")
	}

	wifiEvaluate = true

	congestionMap = make(map[int]map[int]int)
	apMap = make(map[string]*apTrack)

	return nil
}

