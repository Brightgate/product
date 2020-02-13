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
