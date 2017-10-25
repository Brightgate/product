/*
 * COPYRIGHT 2017 Brightgate Inc.  All rights reserved.
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
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"sync"
	"time"

	"bg/ap_common/watchd"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type deviceRecord struct {
	Stats watchd.DeviceRecord
	sync.Mutex
}

type deviceMap map[string]*deviceRecord

var (
	aperiod = flag.Int("aperiod", 5, "aggregation period (in minutes)")

	protocols = []string{"tcp", "udp"}

	currentStats    = make(deviceMap)
	aggregatedStats = make(deviceMap)
	statsMtx        sync.Mutex
)

var (
	cleanScanCount = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "cleaning_scans",
			Help: "Number of cleaning scans completed.",
		})
	hostScanCount = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "host_scans",
			Help: "Number of host scans completed.",
		})
	hostsUp = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "hosts_up",
			Help: "Number of hosts currently up.",
		})
	scannedHostsGauge = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "scanned_hosts",
			Help: "Number of active hosts.",
		})
	scanDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "scan_duration",
			Help: "Scan duration in seconds, by IP and scan type.",
		},
		[]string{"ip", "type"})
	scansFinished = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "scans_finished",
			Help: "Number of scans finished, by IP and scan type.",
		},
		[]string{"ip", "type"})
	scansStarted = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "scans_started",
			Help: "Number of scans started, by IP and scan type.",
		},
		[]string{"ip", "type"})
)

func newProtoRecord() *watchd.ProtoRecord {
	p := watchd.ProtoRecord{
		OpenPorts:      make(map[int]bool),
		OutPorts:       make(map[int]bool),
		InPorts:        make(map[int]bool),
		OutgoingBlocks: make(map[string]int),
		IncomingBlocks: make(map[string]int),
	}
	return &p
}

func newDeviceRecord() *deviceRecord {
	d := deviceRecord{
		Stats: make(watchd.DeviceRecord),
	}
	for _, p := range protocols {
		d.Stats[p] = newProtoRecord()
	}
	return &d
}

func ReleaseDeviceRecord(d *deviceRecord) {
	d.Unlock()
}

func GetDeviceRecord(mac string) *deviceRecord {
	statsMtx.Lock()
	d, ok := currentStats[mac]
	if !ok {
		d = newDeviceRecord()
		currentStats[mac] = d
	}
	d.Lock()
	statsMtx.Unlock()

	return d
}

func GetProtoRecord(d *deviceRecord, proto string) *watchd.ProtoRecord {
	if d != nil {
		return d.Stats[proto]
	}

	return nil
}

func GetDeviceRecordByIP(ip string) *deviceRecord {
	if mac := getMacFromIP(ip); mac != "" {
		return GetDeviceRecord(mac)
	}
	return nil
}

func GetMetrics(mac string, start, end *time.Time) (int, string) {
	var inStats *deviceMap

	if start == nil && end == nil {
		// If the caller doesn't specify start or end times, then we
		// return the current statistics (i.e., those gathered since the
		// last snapshot)
		inStats = &currentStats
	} else if start != nil && end != nil && start.Equal(*end) {
		// If start and end times are identical, we return the
		// aggregated data
		inStats = &aggregatedStats
	} else {
		return API_ERR, "API doesn't yet support arbitrary time ranges"
	}

	outStats := make(map[string]*watchd.DeviceRecord)
	statsMtx.Lock()
	defer statsMtx.Unlock()
	if mac == "ff:ff:ff:ff:ff:ff" {
		// This mac address indicates that the caller wants the data for
		// all devices.
		for mac, dev := range *inStats {
			outStats[mac] = &dev.Stats
		}
	} else if dev, ok := (*inStats)[mac]; ok {
		outStats[mac] = &dev.Stats
	} else {
		return API_ERR, "no such device: " + mac
	}

	data, _ := json.MarshalIndent(outStats, "", "  ")

	return API_OK, string(data)
}

func aggregateBool(agg, cur map[int]bool) {
	for port := range cur {
		agg[port] = true
	}
}

func aggregateCount(agg, cur map[string]int) {
	for block, cnt := range cur {
		agg[block] += cnt
	}
}

func aggregate(agg, cur *watchd.ProtoRecord) {
	aggregateBool(agg.OpenPorts, cur.OpenPorts)
	aggregateBool(agg.OutPorts, cur.OutPorts)
	aggregateBool(agg.InPorts, cur.InPorts)
	aggregateCount(agg.OutgoingBlocks, cur.OutgoingBlocks)
	aggregateCount(agg.IncomingBlocks, cur.IncomingBlocks)
}

// Iterate over all of the device records, merging the current stats into the
// aggregate stats, and clear the current stats
// XXX: at some point, we could/should stash the snapshot values in a database,
// so we can track changes in behavior over time.
func aggregateStats() {
	statsMtx.Lock()
	for mac, cur := range currentStats {
		agg, ok := aggregatedStats[mac]
		if !ok {
			agg = newDeviceRecord()
			aggregatedStats[mac] = agg
		}

		for _, p := range protocols {
			aggregate(agg.Stats[p], cur.Stats[p])
			cur.Stats[p] = newProtoRecord()
		}
	}
	statsMtx.Unlock()
}

func metricsInit() error {
	prometheus.MustRegister(cleanScanCount, scansStarted, scansFinished,
		hostScanCount, hostsUp, scannedHostsGauge, scanDuration)

	http.Handle("/metrics", promhttp.Handler())
	go http.ListenAndServe(*addr, nil)

	if *aperiod < 1 {
		return fmt.Errorf("aperiod must be at least 1 minute")
	}

	ticker := time.NewTicker(time.Minute * time.Duration(*aperiod))
	go func() {
		for range ticker.C {
			aggregateStats()
		}
	}()

	return nil
}

func init() {
	addWatcher("metrics", metricsInit, nil)
}
