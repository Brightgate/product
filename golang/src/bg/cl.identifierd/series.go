/*
 * COPYRIGHT 2020 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package main

import (
	"net"
	"time"

	"bg/cl-obs/sentence"
)

const (
	// default tempory cutoff for observations
	defaultMaxAge = time.Hour * 24 * 90

	// default minimum number of records to retain, regardless of age
	defaultMinRecords = 50
)

type sentenceRecord struct {
	Sent      sentence.Sentence
	Timestamp time.Time
}

// SentenceSeries represents a time-ordered run of sentence.Sentence's, bounded
// at one end by a maximum age.  However, if fewer than minRecords are present,
// maxAge is ignored.  A summary Sentence is maintained for rapid access.
type SentenceSeries struct {
	HWAddr   net.HardwareAddr
	Sentence sentence.Sentence
	sRecs    []sentenceRecord
	// Records older than this are discarded
	maxAge time.Duration
	// Always keep at least this many records around, regardless of maxAge
	minRecords int
}

func newSentenceSeries(hwaddr net.HardwareAddr) *SentenceSeries {
	return &SentenceSeries{
		HWAddr:   hwaddr,
		Sentence: sentence.New(),
		sRecs:    make([]sentenceRecord, 0),
		// For now, default from const; in the future we might
		// allow this to be adjusted.
		maxAge:     defaultMaxAge,
		minRecords: defaultMinRecords,
	}
}

// Add handles the bookkeeping for a sentence which has arrived from an
// observation.  Sentences are kept in an ordered slice.  Two rules govern the
// slice contents.  If the slice has less than minRecords, records from an
// arbitrary time period are kept.  If the slice has at least minRecords, then
// records older than "now - maxAge" are discarded.
func (ss *SentenceSeries) Add(ts time.Time, sent sentence.Sentence) bool {
	redundant := true
	cutoffTime, minRecords := ss.Bounds()

	newRec := sentenceRecord{
		Sent:      sent,
		Timestamp: ts,
	}

	// Common cases, first insertion, or new element belongs at end
	nElems := len(ss.sRecs)
	if nElems == 0 || ts.After(ss.sRecs[nElems-1].Timestamp) {
		ss.sRecs = append(ss.sRecs, newRec)
		redundant = ss.Sentence.AddSentence(sent) && redundant
		goto cleanup
	}

	// See if record is too old to be relevant
	if nElems >= minRecords && ts.Before(cutoffTime) {
		slog.Debugf("%s: inserted sentence too old", ss.HWAddr)
		redundant = true
		goto cleanup
	}

	// Now we walk the elements backwards from the end, looking for the
	// insertion point.  It should be very unusual to receive a
	// significantly out of order element.
	for i := len(ss.sRecs) - 1; i >= 0; i-- {
		slog.Debugf("%s: i is %d", ss.HWAddr, i)
		if ts.Equal(ss.sRecs[i].Timestamp) {
			// This appears to be a dup; we don't add it and return.
			slog.Debugf("%s: duplicate sentence", ss.HWAddr)
			goto cleanup
		}
		// The element belongs after s.DeviceSentences[i]
		if ts.After(ss.sRecs[i].Timestamp) {
			ss.sRecs = append(ss.sRecs[:i+1], append([]sentenceRecord{newRec}, ss.sRecs[i+1:]...)...)
			redundant = ss.Sentence.AddSentence(sent) && redundant
			goto cleanup
		}
	}
	// The element belongs at the head of the list
	slog.Debugf("doing sRecs[0] insertion")
	ss.sRecs = append([]sentenceRecord{newRec}, ss.sRecs...)
	redundant = ss.Sentence.AddSentence(sent) && redundant

cleanup:
	// Now cleanup any other elements which are too old
	nElems = len(ss.sRecs)
	for nElems > minRecords && ss.sRecs[0].Timestamp.Before(cutoffTime) {
		redundant = ss.Sentence.SubtractSentence(ss.sRecs[0].Sent) && redundant
		slog.Debugf("%s: aging out %v", ss.HWAddr, ss.sRecs[0])
		// Drop sentence from sRecs
		ss.sRecs = ss.sRecs[1:]
		nElems--
	}
	return redundant
}

// Bounds returns the cutoffTime and the minimum number of records
// to accumulate (regardless of cutoffTime) for a client.  It's intended
// as a helper routine for Add() and for the Backfill operation.
func (ss *SentenceSeries) Bounds() (time.Time, int) {
	cutoffTime := time.Now().Add(-1 * ss.maxAge)
	return cutoffTime, ss.minRecords
}
