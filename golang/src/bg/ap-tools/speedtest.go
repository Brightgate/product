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
	"log"
	"time"

	"github.com/m-lab/ndt7-client-go"
	"github.com/m-lab/ndt7-client-go/spec"
)

func emitDownload(ev spec.Measurement) {
	log.Printf("Download Speed: %7.1f Mbit/s - RTT: %4.0f/%4.0f/%4.0f (min/smoothed/var ms)",
		float64(ev.BBRInfo.MaxBandwidth)/(1000.0*1000.0),
		ev.BBRInfo.MinRTT, ev.TCPInfo.SmoothedRTT, ev.TCPInfo.RTTVar)
}

func emitUpload(ev spec.Measurement) {
	log.Printf("  Upload speed: %7.1f Mbit/s",
		(8.0*float64(ev.AppInfo.NumBytes))/ev.Elapsed/(1000.0*1000.0))
}

func speedtest() {
	var all bool
	flag.BoolVar(&all, "all", false, "report all measurements")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	client := ndt7.NewClient("ap-speedtest", "0.1")
	ch, err := client.StartDownload(ctx)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Speed test against: %s", client.FQDN)

	var ev spec.Measurement
	for ev = range ch {
		if all {
			emitDownload(ev)
		}
	}
	if !all {
		emitDownload(ev)
	}

	ch, err = client.StartUpload(ctx)
	if err != nil {
		log.Fatal(err)
	}
	for ev = range ch {
		if all {
			emitUpload(ev)
		}
	}
	if !all {
		emitUpload(ev)
	}
}

func init() {
	addTool("ap-speedtest", speedtest)
}
