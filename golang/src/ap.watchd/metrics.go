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
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
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

func metricsInit() {
	prometheus.MustRegister(cleanScanCount, scansStarted, scansFinished,
		hostScanCount, hostsUp, scannedHostsGauge, scanDuration)

	http.Handle("/metrics", promhttp.Handler())
	go http.ListenAndServe(*addr, nil)
}
