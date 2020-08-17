/*
 * Copyright 2019 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package aputil

import (
	"fmt"
	"time"
)

// PaceTracker tracks how frequently an event occurs.  If the frequency exceeds
// the desired threshold, subsequent calls to the Tick() will fail.
type PaceTracker struct {
	limit  int
	period time.Duration
	starts []time.Time
}

// NewPaceTracker defines a PaceTracker with the provided rate limits.
func NewPaceTracker(limit int, period time.Duration) *PaceTracker {
	return &PaceTracker{
		limit:  limit,
		period: period,
		starts: make([]time.Time, limit),
	}
}

// Tick is used to indicate that an event has occured.  If the event frequency
// has exceeded the desired threshold, this call will return an error.
func (p *PaceTracker) Tick() error {
	var err error

	now := time.Now()
	p.starts = append(p.starts[1:p.limit], now)
	if delta := now.Sub(p.starts[0]); delta < p.period {
		err = fmt.Errorf("%d ticks in %v", p.limit, delta)
	}

	return err
}

