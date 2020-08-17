/*
 * Copyright 2020 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package main

import (
	"bg/cl-obs/sentence"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

func TestSeries(t *testing.T) {
	assert := require.New(t)
	slog = zaptest.NewLogger(t).Sugar()

	hwaddr, _ := net.ParseMAC("00:11:22:33:44:55")

	ss := newSentenceSeries(hwaddr)
	ss.minRecords = 3
	ss.maxAge = time.Minute

	var redundant bool
	ts := time.Now()
	// These three are outside the time window; but will initially be included
	// because of minRecords
	redundant = ss.Add(ts.Add(-3*time.Hour), sentence.NewFromString("old3hr"))
	assert.False(redundant)
	// Should insert at 0
	redundant = ss.Add(ts.Add(-1*time.Hour), sentence.NewFromString("old1hr"))
	assert.False(redundant)
	// Should insert in the middle
	redundant = ss.Add(ts.Add(-2*time.Hour), sentence.NewFromString("old2hr"))
	assert.False(redundant)

	exp := sentence.NewFromString("old1hr old2hr old3hr")
	assert.Equal(exp.String(), ss.Sentence.String())
	assert.Len(ss.sRecs, 3)

	redundant = ss.Add(ts.Add(-30*time.Second), sentence.NewFromString("newOldest"))
	assert.False(redundant)
	assert.Len(ss.sRecs, 3)
	exp = sentence.NewFromString("old2hr old1hr newOldest")
	assert.Equal(exp.String(), ss.Sentence.String())

	// Insert newOldest again, but with a slightly altered ts; it should not
	// be "redundant" in this case because it should eliminate old1 from
	// the series, altering the aggregate sentence contents.
	redundant = ss.Add(ts.Add(-29*time.Second), sentence.NewFromString("newOldest"))
	assert.False(redundant)
	assert.Len(ss.sRecs, 3)
	exp = sentence.NewFromString("old1hr newOldest newOldest")
	assert.Equal(exp.String(), ss.Sentence.String())
	assert.Equal(exp.NaryString(), ss.Sentence.NaryString())

	redundant = ss.Add(ts.Add(-10*time.Second), sentence.NewFromString("new10s"))
	assert.False(redundant)
	assert.Len(ss.sRecs, 3)
	// remember: newOldest is in 2 records
	exp = sentence.NewFromString("newOldest newOldest new10s")
	assert.Equal(exp.String(), ss.Sentence.String())
	assert.Equal(exp.NaryString(), ss.Sentence.NaryString())

	redundant = ss.Add(ts.Add(-20*time.Second), sentence.NewFromString("new20s"))
	assert.False(redundant)
	assert.Len(ss.sRecs, 4)
	exp = sentence.NewFromString("newOldest newOldest new20s new10s")
	assert.Equal(exp.String(), ss.Sentence.String())
	assert.Equal(exp.NaryString(), ss.Sentence.NaryString())

	// Doesn't age anything out, everything is in the time window
	redundant = ss.Add(ts, sentence.NewFromString("new0"))
	assert.False(redundant)
	assert.Len(ss.sRecs, 5)
	exp = sentence.NewFromString("newOldest newOldest new20s new10s new0")
	assert.Equal(exp.String(), ss.Sentence.String())

	// Insert exact duplicate, should be redundant
	redundant = ss.Add(ts, sentence.NewFromString("new0"))
	assert.True(redundant)
	assert.Len(ss.sRecs, 5)
	exp = sentence.NewFromString("newOldest newOldest new20s new10s new0")
	assert.Equal(exp.String(), ss.Sentence.String())

	// Insert element which needs to land at 0th sRec.
	redundant = ss.Add(ts.Add(-45*time.Second), sentence.NewFromString("newExtraOld"))
	assert.Len(ss.sRecs, 6)
	exp = sentence.NewFromString("newExtraOld newOldest newOldest new20s new10s new0")
	assert.Equal(exp.String(), ss.Sentence.String())
	assert.False(redundant)

	// Insert element which is too old for series
	redundant = ss.Add(ts.Add(-45*time.Hour), sentence.NewFromString("tooOld"))
	assert.True(redundant)
	assert.Len(ss.sRecs, 6)
	exp = sentence.NewFromString("newExtraOld newOldest newOldest new20s new10s new0")
	assert.Equal(exp.String(), ss.Sentence.String())
}

