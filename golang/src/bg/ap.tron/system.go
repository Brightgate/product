/*
 * Copyright 2020 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package main

import (
	"io/ioutil"
	"math"
	"strconv"
	"strings"
	"time"
)

const (
	freqFile = "/sys/devices/system/cpu/cpu0/cpufreq/cpuinfo_cur_freq"
	tempFile = "/sys/class/thermal/thermal_zone0/temp"
	loadFile = "/proc/loadavg"
)

type valTrack struct {
	initted bool
	current int
	min     int
	max     int
	avg     int
}

var (
	sysTests = []*hTest{tempTest, freqTest, loadTest}

	// The following tests probe system-level values, inserting the results
	// into the config tree
	tempTest = &hTest{
		name:   "sys_temp",
		testFn: getSysTemp,
		period: 5 * time.Second,
	}
	freqTest = &hTest{
		name:   "cpu_freq",
		testFn: getCPUFreq,
		period: 5 * time.Second,
	}
	loadTest = &hTest{
		name:   "loadavg",
		testFn: getLoadAvg,
		period: 5 * time.Second,
	}

	freqTrack valTrack
	tempTrack valTrack
)

func getLineFromFile(name string) string {
	if d, err := ioutil.ReadFile(name); err == nil {
		return strings.TrimSpace(string(d))
	}
	return ""
}

func getIntFromFile(name string) int {
	rval := -1

	if line := getLineFromFile(name); line != "" {
		rval, _ = strconv.Atoi(line)
	}
	return rval
}

func updateTrack(t *hTest, track *valTrack, current int) {
	if current > 0 {
		if !track.initted {
			track.min = int(math.MaxInt32)
			track.avg = current
			track.initted = true
		}

		track.current = current
		track.avg = ((track.avg * 11) + current) / 12
		t.setValue("avg", strconv.Itoa(track.avg))

		s := strconv.Itoa(current)
		t.setValue("current", s)
		if current > track.max {
			track.max = current
			t.setValue("max", s)
		}

		if current < track.min {
			track.min = current
			t.setValue("min", s)
		}
	}
}

func getCPUFreq(t *hTest) bool {
	freq := getIntFromFile(freqFile)
	updateTrack(t, &freqTrack, freq)
	return true
}

func getSysTemp(t *hTest) bool {
	temp := getIntFromFile(tempFile)
	updateTrack(t, &tempTrack, temp)
	return true
}

func getLoadAvg(t *hTest) bool {
	if line := getLineFromFile(loadFile); line != "" {
		t.setValue("current", line)
	}
	return true
}

