/*
 * Copyright 2020 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package extract

import (
	"bg/cl-obs/defs"
	"fmt"
	"regexp"
)

// NormalizeDHCPVendor looks for a match in the known DHCP Vendor patterns.  If
// it finds one, it returns the normalized value for the DHCP Vendor.  For
// example, a DHCP Vendor of GoogleWifi would result in "google" as the result.
func NormalizeDHCPVendor(vendor string) (string, error) {
	for p, normalized := range defs.DHCPVendorPatterns {
		matched, _ := regexp.MatchString(p, vendor)
		if matched {
			return normalized, nil
		}
	}
	return defs.UnknownDHCPVendor, fmt.Errorf("No match for DHCP vendor %q", vendor)
}

