//
// Copyright 2020 Brightgate Inc.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
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

