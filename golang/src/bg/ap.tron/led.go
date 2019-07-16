/*
 * COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 *
 */

package main

import (
	"os"
	"strconv"
	"sync"
	"time"
)

var (
	currentValues = make(map[string]int)
)

func writeLedFile(led, file, val string) {
	name := "/sys/class/leds/u7623-01:green:led" + led + "/" + file

	f, err := os.OpenFile(name, os.O_WRONLY, 0)
	if err != nil {
		logWarn("failed to open %s: %v", name, err)
	} else {
		_, err := f.Write([]byte(val))
		if err != nil {
			logWarn("failed to write to %s: %v", name, err)
		}
		f.Close()
	}
}

func setLed(led string, pct int) {
	on := 1000 * pct / 100
	off := 1000 - on

	writeLedFile(led, "brightness", "255")
	writeLedFile(led, "trigger", "timer")
	writeLedFile(led, "delay_on", strconv.Itoa(on))
	writeLedFile(led, "delay_off", strconv.Itoa(off))
}

func ledDriver(wg *sync.WaitGroup) {
	defer wg.Done()
	t := time.NewTicker(time.Second)

	// Once a second, we examine all of the test results to determine which
	// pattern to display on each LED.
	for running {
		for led, list := range perLedTests {
			new := 0
			for _, t := range list {
				if !t.pass {
					break
				}
				new = t.ledValue
			}

			current, ok := currentValues[led]
			if !ok || new != current {
				setLed(led, new)
				currentValues[led] = new
			}
		}

		<-t.C
	}
	for led := range perLedTests {
		setLed(led, 0)
	}
}
