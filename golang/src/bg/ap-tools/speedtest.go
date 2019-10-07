/*
 * COPYRIGHT 2019 Brightgate Inc. All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or  alteration will be a violation of federal law.
 */

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/m-lab/ndt7-client-go"
	"github.com/m-lab/ndt7-client-go/spec"
)

func validMeasurement(ev spec.Measurement) bool {
	if ev.Origin != spec.OriginClient {
		return false
	}
	if ev.AppInfo == nil || ev.AppInfo.ElapsedTime <= 0 {
		return false
	}
	return true
}

func emitEvent(ev spec.Measurement) {
	elapsed := float64(ev.AppInfo.ElapsedTime) / 1e06
	v := (8.0 * float64(ev.AppInfo.NumBytes)) / elapsed / (1000.0 * 1000.0)

	var title string
	if ev.Test == "download" {
		title = "Download Speed:"
	} else if ev.Test == "upload" {
		title = "Upload Speed:"
	}

	var rttStr string
	// This may be available in later kernels; the NDT7 0.8 spec
	// specifically notes that >= 4.19 uses this, but it's been around for
	// much longer.  The NDT7 client interfaces expose this information, but
	// it's not set anywhere.
	if ev.TCPInfo != nil {
		minRTT := float64(ev.TCPInfo.MinRTT) / 1000.0
		smoothRTT := float64(ev.TCPInfo.RTT) / 1000.0
		varRTT := float64(ev.TCPInfo.RTTVar) / 1000.0
		rttStr = fmt.Sprintf(" - RTT: %4.0f/%4.0f/%4.0f (min/smoothed/var ms)",
			minRTT, smoothRTT, varRTT)
	}

	log.Printf("%-15s%7.1f Mbit/s%s", title, v, rttStr)
}

func speedtest() {
	var all bool
	flag.BoolVar(&all, "all", false, "report all measurements")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	client := ndt7.NewClient("ap-speedtest", "0.2")
	ch, err := client.StartDownload(ctx)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Speed test against: %s", client.FQDN)

	var lastEv, ev spec.Measurement
	for ev = range ch {
		if validMeasurement(ev) {
			if all {
				emitEvent(ev)
			}
			lastEv = ev
		}
	}
	if !all {
		emitEvent(lastEv)
	}

	ch, err = client.StartUpload(ctx)
	if err != nil {
		log.Fatal(err)
	}
	for ev = range ch {
		if validMeasurement(ev) {
			if all {
				emitEvent(ev)
			}
			lastEv = ev
		}
	}
	if !all {
		emitEvent(lastEv)
	}
}

func init() {
	addTool("ap-speedtest", speedtest)
}
