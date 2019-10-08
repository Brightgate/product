/*
 * COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
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
