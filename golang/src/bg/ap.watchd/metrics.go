/*
 * COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
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
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"bg/ap_common/aputil"
	"bg/ap_common/watchd"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type endpoint struct {
	ip     net.IP
	hwaddr net.HardwareAddr
	port   int
}

var (
	sfreq = flag.Duration("sfreq", 5*time.Minute,
		"snapshot frequency (in minutes)")
	mretain = flag.Duration("mretain", 3*time.Hour,
		"stats history to retain in memory")
	dretain = flag.Duration("dretain", 24*time.Hour,
		"stats history to retain on disk")

	metricsDone      = make(chan bool, 1)
	metricsWaitGroup sync.WaitGroup

	currentStats    *watchd.Snapshot
	historicalStats []*watchd.Snapshot
	statsMtx        sync.RWMutex
)

var (
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

func newDeviceRecord(mac string) *watchd.DeviceRecord {
	var addr net.IP

	if ip, ok := macToIP[mac]; ok {
		addr = net.ParseIP(ip)
	}
	d := watchd.DeviceRecord{
		Addr:       addr,
		OpenTCP:    make([]int, 0),
		OpenUDP:    make([]int, 0),
		BlockedOut: make(map[uint64]int),
		BlockedIn:  make(map[uint64]int),
		LANStats:   make(map[uint64]watchd.XferStats),
		WANStats:   make(map[uint64]watchd.XferStats),
	}

	return &d
}

func newSnapshot() *watchd.Snapshot {
	s := watchd.Snapshot{
		Data: make(map[string]*watchd.DeviceRecord),
	}

	return &s
}

// releaseDeviceRecord drops the lock on a device record
func releaseDeviceRecord(d *watchd.DeviceRecord) {
	d.Unlock()
}

// getDeviceRecord looks up a device record based on the provide mac address.
// If the corresponding device is found, the structure is locked and returned to
// the caller.  It must be released when the client is finished with it.
func getDeviceRecord(mac string) *watchd.DeviceRecord {
	statsMtx.RLock()
	d, ok := currentStats.Data[mac]
	statsMtx.RUnlock()
	if !ok {
		statsMtx.Lock()
		if d, ok = currentStats.Data[mac]; !ok {
			d = newDeviceRecord(mac)
			currentStats.Data[mac] = d
		}
		statsMtx.Unlock()
	}
	d.Lock()
	return d
}

// getDeviceRecordByIP looks up a device record based on the provided IP address
func getDeviceRecordByIP(ip string) *watchd.DeviceRecord {
	if mac := getMacFromIP(ip); mac != "" {
		return getDeviceRecord(mac)
	}
	return nil
}

func incSentStats(smap map[uint64]watchd.XferStats, key uint64, len int) {
	x := smap[key]
	x.PktsSent++
	x.BytesSent += uint64(len)
	smap[key] = x
}

func incRcvdStats(smap map[uint64]watchd.XferStats, key uint64, len int) {
	x := smap[key]
	x.PktsRcvd++
	x.BytesRcvd += uint64(len)
	smap[key] = x
}

func getKey(remoteIP net.IP, rport, lport int) uint64 {
	s := watchd.Session{
		RAddr: remoteIP,
		RPort: rport,
		LPort: lport,
	}

	return (watchd.SessionToKey(s))
}

func updateStats(src, dst endpoint, proto string, len int) {
	srcLocal := localIPAddr(src.ip)
	dstLocal := localIPAddr(dst.ip) ||
		(proto == "udp" && broadcastUDPAddr(dst.ip))

	if srcLocal {
		key := getKey(dst.ip, dst.port, src.port)
		mac := src.hwaddr.String()
		dev := getDeviceRecord(mac)

		dev.Aggregate.PktsSent++
		dev.Aggregate.BytesSent += uint64(len)
		if dstLocal {
			incSentStats(dev.LANStats, key, len)
		} else {
			incSentStats(dev.WANStats, key, len)
		}
		releaseDeviceRecord(dev)
	}

	if dstLocal {
		key := getKey(src.ip, src.port, dst.port)
		dev := getDeviceRecordByIP(dst.ip.String())

		if dev != nil {
			dev.Aggregate.PktsRcvd++
			dev.Aggregate.BytesRcvd += uint64(len)
			if srcLocal {
				incRcvdStats(dev.LANStats, key, len)
			} else {
				incRcvdStats(dev.WANStats, key, len)
			}
			releaseDeviceRecord(dev)
		}
	}
}

// Update a device's 'packets blocked by the firewall' count
func incBlockCnt(proto, local string, remote net.IP, rport, lport int, out bool) {
	s := watchd.Session{RAddr: remote, RPort: rport, LPort: lport}
	key := watchd.SessionToKey(s)

	rec := getDeviceRecord(local)
	if out {
		rec.BlockedOut[key] = rec.BlockedOut[key] + 1
	} else {
		rec.BlockedIn[key] = rec.BlockedIn[key] + 1
	}
	releaseDeviceRecord(rec)
}

func copyIP(in net.IP) net.IP {
	out := make(net.IP, len(in))
	copy(out, in)
	return out
}

func copyPortlist(in []int) []int {
	out := make([]int, len(in))
	copy(out, in)
	return out
}

func writeStats(dir string, sn *watchd.Snapshot) error {
	file := dir + "/" + sn.Start.Format(time.RFC3339) + ".json"

	s, err := json.MarshalIndent(sn, "", "  ")
	if err != nil {
		err = fmt.Errorf("unable to construct snapshot JSON: %v", err)
	} else if err = ioutil.WriteFile(file, s, 0644); err != nil {
		err = fmt.Errorf("failed to write snapshot file %s: %v",
			file, err)
	}

	return err
}

func snapshotStats(dir string) {
	statsMtx.RLock()

	now := time.Now()
	sn := newSnapshot()

	sn.Start = currentStats.Start
	sn.End = now
	currentStats.Start = now
	currentStats.End = now.Add(*sfreq)
	for mac, cur := range currentStats.Data {
		cur.Lock()

		// Make a copy of the current state.  For the elements we are
		// going to reset, we can just copy the pointers.  The elements
		// that are preserved across snapshots have to be copied by
		// value.
		d := watchd.DeviceRecord{
			Addr:       copyIP(cur.Addr),
			BlockedOut: cur.BlockedOut,
			BlockedIn:  cur.BlockedIn,
			Aggregate:  cur.Aggregate,
			OpenTCP:    copyPortlist(cur.OpenTCP),
			OpenUDP:    copyPortlist(cur.OpenUDP),
			LANStats:   cur.LANStats,
			WANStats:   cur.WANStats,
		}

		cur.BlockedOut = make(map[uint64]int)
		cur.BlockedIn = make(map[uint64]int)
		cur.LANStats = make(map[uint64]watchd.XferStats)
		cur.WANStats = make(map[uint64]watchd.XferStats)
		cur.Aggregate = watchd.XferStats{}
		cur.Unlock()

		sn.Data[mac] = &d
	}
	statsMtx.RUnlock()

	historicalStats = append(historicalStats, sn)
}

func snapshotClean(dir string) {
	memLimit := time.Now().Add(-1 * *mretain)
	diskLimit := time.Now().Add(-1 * *dretain)

	oldest := 0
	for idx, sn := range historicalStats {
		if sn.End.After(memLimit) {
			break
		}
		oldest = idx
	}
	historicalStats = historicalStats[oldest:]

	// Once we start uploading these snapshots to the cloud, this cleanup
	// will be performed as a side effect of that process.
	files, err := ioutil.ReadDir(dir)
	if err != nil {
		log.Printf("Unable to get contents of %s: %v\n", dir, err)
	}
	for _, f := range files {
		if f.ModTime().Before(diskLimit) {
			name := dir + "/" + f.Name()
			if err := os.Remove(name); err != nil {
				log.Printf("Unable to remove old %s: %v\n",
					name, err)
			}
		}
	}
}

func snapshotter() {
	done := false
	ticker := time.NewTicker(*sfreq)
	statsDir := *watchDir + "/stats"

	if !aputil.FileExists(statsDir) {
		if err := os.MkdirAll(statsDir, 0755); err != nil {
			log.Printf("Unable to make stats directory %s: %v\n",
				statsDir, err)
			done = true
		}
	}

	for !done {
		snapshotClean(statsDir)

		select {
		case <-ticker.C:
		case done = <-metricsDone:
		}

		snapshotStats(statsDir)
		sn := historicalStats[len(historicalStats)-1]
		if err := writeStats(statsDir, sn); err != nil {
			log.Printf("Unable to persist snapshot: %v\n", err)
		}
	}
	metricsWaitGroup.Done()
}

func selectSnapshot(s *watchd.Snapshot, mac string,
	start, end *time.Time) *watchd.Snapshot {

	if s.Start.After(*end) || s.End.Before(*start) {
		return nil
	}

	// If the caller wants data for just a single device, we need to build a
	// new snapshot structure containing just that device.
	if mac != "ff:ff:ff:ff:ff:ff" {
		x := watchd.Snapshot{
			Start: s.Start,
			End:   s.End,
			Data:  make(map[string]*watchd.DeviceRecord),
		}
		if dev, ok := s.Data[mac]; ok {
			x.Data[mac] = dev
		}
		s = &x
	}

	return s
}

// Returns a JSON representation of all the data we have for the specified
// device within the specified time range.  If the MAC address is
// ff:ff:ff:ff:ff:ff, it means we should return the data for all devices.  If
// either the start or end time are 'nil', it means there is no limit in that
// direction.
func getMetrics(mac string, start, end *time.Time) (int, string) {
	if currentStats == nil {
		return OK, ""
	}

	if start == nil {
		t := time.Unix(0, 0)
		start = &t
	}

	if end == nil {
		t := time.Now()
		end = &t
	}

	// Find all of the snapshots that are within, or overlap, the desired
	// time range.
	stats := make([]*watchd.Snapshot, 0)
	statsMtx.RLock()
	for _, s := range historicalStats {
		if x := selectSnapshot(s, mac, start, end); x != nil {
			stats = append(stats, x)
		}
	}
	if x := selectSnapshot(currentStats, mac, start, end); x != nil {
		stats = append(stats, x)

		// Before trying build the json representation, we need to be
		// sure the dev structures aren't being modified.  We can do
		// this by grabbing and releasing each device lock, and relying
		// on the statsMtx to prevent any of them from being reacquired.
		for _, d := range x.Data {
			d.Lock()
			d.Unlock()
		}
	}

	var (
		status int
		rval   string
	)
	if m, err := json.MarshalIndent(stats, "", "  "); err != nil {
		status = ERR
		rval = fmt.Sprintf("unable to construct snapshot JSON: %v", err)
		log.Println(rval)
	} else {
		status = OK
		rval = string(m)
	}
	statsMtx.RUnlock()

	return status, rval
}

func metricsFini(w *watcher) {
	metricsDone <- true
	metricsWaitGroup.Wait()
	w.running = false
}

func metricsInit(w *watcher) {
	prometheus.MustRegister(scansStarted, scansFinished,
		hostScanCount, hostsUp, scanDuration)

	http.Handle("/metrics", promhttp.Handler())
	go http.ListenAndServe(*addr, nil)

	metricsWaitGroup.Add(1)
	go snapshotter()

	w.running = true
}

func init() {
	currentStats = newSnapshot()
	currentStats.Start = time.Now()
	currentStats.End = currentStats.Start.Add(*sfreq)

	historicalStats = make([]*watchd.Snapshot, 0)

	addWatcher("metrics", metricsInit, metricsFini)
}
