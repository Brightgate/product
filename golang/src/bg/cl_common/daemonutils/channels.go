/*
 * Copyright 2020 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package daemonutils

import (
	"sync"
)

// FanOut is a fan-out notification multiplexer for channels.  It receives a
// notification on an input channel designated at creation time, and copies that
// to all output channels added by AddReceiver.
type FanOut struct {
	input  chan struct{}
	output []chan struct{}
	sync.Mutex
}

// NewFanOut creates a new FanOut with a given input channel.
func NewFanOut(input chan struct{}) *FanOut {
	fo := &FanOut{input: input}

	go func() {
		for n := range input {
			fo.Lock()
			for _, out := range fo.output {
				out <- n
			}
			fo.Unlock()
		}
		fo.Lock()
		for _, out := range fo.output {
			close(out)
		}
		fo.Unlock()
	}()

	return fo
}

// AddReceiver creates a new output channel, adds it to the list, and returns
// it.
func (fo *FanOut) AddReceiver() chan struct{} {
	c := make(chan struct{})
	fo.Lock()
	fo.output = append(fo.output, c)
	fo.Unlock()
	return c
}

// Notify sends the notification to the input channel (and thus to all the
// receivers).
func (fo *FanOut) Notify() {
	fo.input <- struct{}{}
}

