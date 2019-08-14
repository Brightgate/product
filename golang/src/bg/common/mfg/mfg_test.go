/*
 * COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package mfg

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewExtSerialFromString(t *testing.T) {
	assert := require.New(t)
	var err error
	var sn *ExtSerial

	goodStrings := []string{
		"001-201913BB-000001", // Lowest model #
		"989-201913BB-000001", // Highest model #
		"001-201801BB-000001", // Lowest year
		"001-998901BB-000001", // Highest year
		"001-201801BB-000001", // Lowest week
		"001-998953BB-000001", // Highest week
		"001-201801AA-000001", // Lowest Site
		"001-201801ZZ-000001", // Highest Site
		"001-201913BB-000001", // Lowest SN
		"001-201913BB-999899", // Highest SN
	}
	for _, s := range goodStrings {
		fmt.Printf("good: %v\n", s)
		sn, err = NewExtSerialFromString(s)
		assert.NoError(err, s)
		assert.Equal(s, sn.String())
		isValid := ValidExtSerial(s)
		assert.True(isValid)
	}
	badStrings := []string{
		"0",
		"001-",
		"001-2",
		"001-201801BB",
		"001-201801BB-",
		"XXX-201801BB-999900",  // Bad model
		"000-201801BB-999900",  // Bad model
		"0000-201801BB-999900", // Bad model
		"990-201801BB-999900",  // Bad model
		"001-200001BB-999900",  // Bad year
		"001-XXXX01BB-999900",  // Bad year
		"001-999001BB-4444444", // Bad year
		"001-201900BB-999900",  // Bad week
		"001-201954BB-999900",  // Bad week
		"001-2019XXBB-999900",  // Bad week
		"001-20190101-999900",  // Bad site
		"001-20190101-999900",  // Bad site
		"001-201901BB-000000",  // Bad serial
		"001-201901BB-999900",  // Bad serial
		"001-201901BB-4444444", // Bad serial
	}
	for _, s := range badStrings {
		fmt.Printf("bad: %v\n", s)
		sn, err = NewExtSerialFromString(s)
		assert.Error(err)
		assert.Nil(sn)
		isValid := ValidExtSerial(s)
		assert.False(isValid)
	}
}

func TestNewExtSerial(t *testing.T) {
	assert := require.New(t)
	var err error
	var sn *ExtSerial

	good := []ExtSerial{
		{1, 2019, 13, [2]byte{'B', 'B'}, 1},
		{989, 2018, 1, [2]byte{'A', 'A'}, 999899},
		{1, 2018, 53, [2]byte{'Z', 'Z'}, 111111},
		{1, 9989, 53, [2]byte{'Z', 'Z'}, 111111},
	}
	for _, s := range good {
		fmt.Printf("good: %v\n", s)
		sn, err = NewExtSerial(s.Model, s.Year, s.Week, s.SiteCode, s.Serial)
		assert.NoError(err)
		assert.Equal(s, *sn)
	}

	bad := []ExtSerial{
		{0, 0, 0, [2]byte{0, 0}, 0},
		{1000, 2018, 53, [2]byte{'B', 'B'}, 1},     // Bad model
		{0, 2018, 53, [2]byte{'B', 'B'}, 1},        // Bad model
		{990, 2018, 53, [2]byte{'B', 'B'}, 1},      // Bad model
		{1, 2000, 53, [2]byte{'B', 'B'}, 1},        // Bad year
		{1, 9990, 53, [2]byte{'B', 'B'}, 1},        // Bad year
		{1, 44444, 53, [2]byte{'B', 'B'}, 1},       // Bad year
		{1, 2019, 0, [2]byte{'B', 'B'}, 1},         // Bad week
		{1, 2019, 54, [2]byte{'B', 'B'}, 1},        // Bad week
		{1, 2019, 10, [2]byte{'1', 'B'}, 1},        // Bad mfg site
		{1, 2019, 10, [2]byte{0x01, 0x02}, 1},      // Bad mfg site
		{1, 2018, 53, [2]byte{'B', 'B'}, 0},        // Bad serial
		{1, 2018, 53, [2]byte{'B', 'B'}, 999900},   // Bad serial
		{1, 2018, 53, [2]byte{'B', 'B'}, 999999},   // Bad serial
		{1, 2018, 53, [2]byte{'B', 'B'}, 11111111}, // Bad serial
	}

	for _, s := range bad {
		fmt.Printf("bad: %v\n", s)
		sn, err = NewExtSerial(s.Model, s.Year, s.Week, s.SiteCode, s.Serial)
		assert.Error(err)
		assert.Nil(sn)
	}
}
