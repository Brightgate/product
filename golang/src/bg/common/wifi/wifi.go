/*
 * Copyright 2019 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package wifi

// Names of the frequency bands.
const (
	LoBand = "2.4GHz"
	HiBand = "5GHz"
)

// Channels is a map of per-band arrays of valid 20MHz channel lists, which are
// legal for the US.  We will need to ship per-country lists, presumably indexed
// by regulatory domain.  In the US, with the exception of channel 165, all
// these channels are valid primaries for 40MHz channels.
var Channels = map[string][]int{
	LoBand: {1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11},
	HiBand: {36, 40, 44, 48, 52, 56, 60, 64, 100, 104, 108,
		112, 116, 120, 124, 128, 132, 136, 140, 144, 149, 153,
		157, 161, 165},
}

// The following are all the states a physical NIC may be in.  The first three
// apply to wired as well as wireless NICs, while the remaining states only
// apply to wireless NICs.
const (
	DevOK              = "ok"
	DevDisabled        = "disabled"
	DevBroken          = "broken"
	DevUnsupportedBand = "unsupported_band" // Doesn't support the configured band
	DevUnsupportedChan = "unsupported_chan" // Doesn't support the configured channel
	DevIllegalBand     = "illegal_band"     // Configured band doesn't exist
	DevIllegalChan     = "illegal_chan"     // Configured channel doesn't exist
	DevBadChan         = "bad_chan"         // Configured channel not in configured band
	DevNoChan          = "nochannel"        // No legal, supported channels available
)

// DeviceStates maps the configuration value to the corresponding const
var DeviceStates = map[string]bool{
	DevOK:              true,
	DevDisabled:        true,
	DevBroken:          true,
	DevUnsupportedBand: true,
	DevUnsupportedChan: true,
	DevIllegalBand:     true,
	DevIllegalChan:     true,
	DevBadChan:         true,
	DevNoChan:          true,
}

// ExpandChannels returns a slice of the 20MHz channels covered by the channel
// description provided.
func ExpandChannels(primary, secondary, width int) []int {
	c := make([]int, 0)
	c = append(c, primary)

	// XXX: this assumes the presence of a secondary channel means this is a
	// 40MHz 802.11n channel.  It will eventually need to handle 80+80MHz
	// 802.11ac channels as well.
	if secondary != 0 {
		c = append(c, secondary)
	} else {
		if width >= 40 {
			c = append(c, primary+4)
		}
		if width >= 80 {
			c = append(c, primary+8)
			c = append(c, primary+12)
		}
		if width == 160 {
			c = append(c, primary+16)
			c = append(c, primary+20)
			c = append(c, primary+24)
			c = append(c, primary+28)
		}
	}

	return c
}

