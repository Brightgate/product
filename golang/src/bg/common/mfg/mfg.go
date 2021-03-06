/*
 * Copyright 2019 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


// Package mfg implements routines for processing manufacturing information.
// It is primarily concerned with Brightgate serial numbers.  The reference for
// this code is "External serial numbers, v1.1"
// https://docs.google.com/document/d/1kEolvqtqjHVzdWlTv_SZ7lXpXF0do5P1HUa8BnS5vAw/
package mfg

import (
	"errors"
	"fmt"
	"math/rand"
	"regexp"
	"strconv"
	"time"
)

var randomSite = [2]byte{'Z', 'Z'}
var extSerialRE = regexp.MustCompile(`^(\d{3})-(\d{4})(\d{2})([A-Z]{2})-(\d{6})$`)

// ErrInvalidSerial represents an invalid serial number
var ErrInvalidSerial = errors.New("invalid serial number")

// Some values (such as years less than 2018) are illegal; some are reserved
// (such as years 9990-9999).  This code does not presently make a distinction,
// and considers everything outside of the normal range to be illegal.
const minModel = 1
const maxModel = 989
const minYear = 2018
const maxYear = 9989
const minSerial = 1
const maxSerial = 999899

// ExtSerial represents an externally visible (to customers) serial number, as
// defined by "External serial numbers"
// https://docs.google.com/document/d/1kEolvqtqjHVzdWlTv_SZ7lXpXF0do5P1HUa8BnS5vAw
type ExtSerial struct {
	Model    int
	Year     int
	Week     int
	SiteCode [2]byte
	Serial   int
}

func (s ExtSerial) String() string {
	return fmt.Sprintf("%03d-%04d%02d%c%c-%06d",
		s.Model,
		s.Year, s.Week, s.SiteCode[0], s.SiteCode[1],
		s.Serial)
}

// NewExtSerial creates an external serial number according to the
// input parameters.
func NewExtSerial(model, year, week int, siteCode [2]byte, serial int) (*ExtSerial, error) {
	if model < minModel || model > maxModel {
		return nil, ErrInvalidSerial
	}
	if year < minYear || year > maxYear {
		return nil, ErrInvalidSerial
	}
	if week < 1 || week > 53 {
		return nil, ErrInvalidSerial
	}
	if siteCode[0] < 'A' || siteCode[0] > 'Z' {
		return nil, ErrInvalidSerial
	}
	if siteCode[1] < 'A' || siteCode[1] > 'Z' {
		return nil, ErrInvalidSerial
	}
	if serial < minSerial || serial > maxSerial {
		return nil, ErrInvalidSerial
	}
	return &ExtSerial{model, year, week, siteCode, serial}, nil
}

// ValidExtSerial determines whether the provided string represents a valid
// serial number or not.
func ValidExtSerial(serial string) bool {
	_, err := NewExtSerialFromString(serial)
	return err == nil
}

// NewExtSerialFromString parses a serial number from a string and returns
// a new ExtSerial
func NewExtSerialFromString(sn string) (*ExtSerial, error) {
	var err error
	match := extSerialRE.FindStringSubmatch(sn)
	if match == nil {
		return nil, ErrInvalidSerial
	}
	m, err := strconv.Atoi(match[1])
	if err != nil {
		return nil, ErrInvalidSerial
	}
	y, err := strconv.Atoi(match[2])
	if err != nil {
		return nil, ErrInvalidSerial
	}
	w, err := strconv.Atoi(match[3])
	if err != nil {
		return nil, ErrInvalidSerial
	}
	s, err := strconv.Atoi(match[5])
	if err != nil {
		return nil, ErrInvalidSerial
	}
	return NewExtSerial(m, y, w, [2]byte{match[4][0], match[4][1]}, s)
}

// IsExtSerialRandom returns true if the provided serial number appears to have
// been generated randomly.
func IsExtSerialRandom(sn *ExtSerial) bool {
	return sn.SiteCode == randomSite
}

// NewExtSerialRandom generates a random serial number for the provided model
// number.  The new serial number uses the reserved SiteCode 'ZZ' to indicate
// that it was generated locally on the appliance, rather than through the
// official build process.
func NewExtSerialRandom(model int) *ExtSerial {
	year, week := time.Now().ISOWeek()
	rand.Seed(int64(time.Now().Nanosecond()))
	serial := (rand.Int() % maxSerial) + 1

	return &ExtSerial{model, year, week, randomSite, serial}
}

