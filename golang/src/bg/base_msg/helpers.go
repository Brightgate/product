//
// COPYRIGHT 2020 Brightgate Inc. All rights reserved.
//
// This copyright notice is Copyright Management Information under 17 USC 1202
// and is included to protect this work and deter copyright infringement.
// Removal or alteration of this Copyright Management Information without the
// express written permission of Brightgate Inc is prohibited, and any
// such unauthorized removal or  alteration will be a violation of federal law.
//
package base_msg

import (
	"time"
)

// MarshalText renders the Timestamp using time.RFC3339
func (t Timestamp) MarshalText() ([]byte, error) {
	if t.Seconds == nil {
		return []byte("<nil>"), nil
	}
	sec := t.GetSeconds()
	nano := int64(t.GetNanos())
	return []byte(time.Unix(sec, nano).Format(time.RFC3339)), nil
}
