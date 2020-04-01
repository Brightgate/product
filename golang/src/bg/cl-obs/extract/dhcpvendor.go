/*
 * COPYRIGHT 2020 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
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
