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
	"bytes"
	"encoding/gob"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"strconv"
	"sync"
	"time"

	"bg/ap_common/apcfg"
	"bg/ap_common/aputil"
	"bg/common/archive"
	"bg/common/network"
)

type endpoint struct {
	ip     net.IP
	hwaddr net.HardwareAddr
	port   int
}

type timeStats struct {
	total    archive.XferStats
	previous archive.XferStats
	second   archive.XferStats
	minute   archive.XferStats
	hour     archive.XferStats
	day      archive.XferStats
}

var (
	sfreq   = apcfg.Duration("snapshot_freq", 5*time.Minute, false, nil)
	rfreq   = apcfg.Duration("rolling_freq", 5*time.Second, false, nil)
	dretain = apcfg.Duration("disk_retain", 24*time.Hour, true, nil)

	metricsDone      = make(chan bool, 1)
	metricsWaitGroup sync.WaitGroup

	rollingStats = make(map[string]*timeStats)
	currentStats *archive.Snapshot
	statsMtx     sync.RWMutex
)

func newDeviceRecord(mac string) *archive.DeviceRecord {
	var addr net.IP

	if ip, ok := macToIP[mac]; ok {
		addr = net.ParseIP(ip)
	}
	d := archive.DeviceRecord{
		Addr:       addr,
		OpenTCP:    make([]int, 0),
		OpenUDP:    make([]int, 0),
		BlockedOut: make(map[uint64]int),
		BlockedIn:  make(map[uint64]int),
		LANStats:   make(map[uint64]archive.XferStats),
		WANStats:   make(map[uint64]archive.XferStats),
	}

	return &d
}

func newSnapshot() *archive.Snapshot {
	s := archive.Snapshot{
		Data: make(map[string]*archive.DeviceRecord),
	}

	return &s
}

// releaseDeviceRecord drops the lock on a device record
func releaseDeviceRecord(d *archive.DeviceRecord) {
	d.Unlock()
}

// getDeviceRecord looks up a device record based on the provide mac address.
// If the corresponding device is found, the structure is locked and returned to
// the caller.  It must be released when the client is finished with it.
func getDeviceRecord(mac string) *archive.DeviceRecord {
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
func getDeviceRecordByIP(ip string) *archive.DeviceRecord {
	if mac := getMacFromIP(ip); mac != "" {
		return getDeviceRecord(mac)
	}
	return nil
}

func incSentStats(smap map[uint64]archive.XferStats, key uint64, len int) {
	x := smap[key]
	x.PktsSent++
	x.BytesSent += uint64(len)
	smap[key] = x
}

func incRcvdStats(smap map[uint64]archive.XferStats, key uint64, len int) {
	x := smap[key]
	x.PktsRcvd++
	x.BytesRcvd += uint64(len)
	smap[key] = x
}

func getKey(remoteIP net.IP, rport, lport int) uint64 {
	s := archive.Session{
		RAddr: remoteIP,
		RPort: rport,
		LPort: lport,
	}

	return (archive.SessionToKey(s))
}

func addMetrics(props map[string]string, base string, data *archive.XferStats) {
	props[base+"/"+"bytes_sent"] = strconv.FormatUint(data.BytesSent, 10)
	props[base+"/"+"pkts_sent"] = strconv.FormatUint(data.PktsSent, 10)
	props[base+"/"+"bytes_rcvd"] = strconv.FormatUint(data.BytesRcvd, 10)
	props[base+"/"+"pkts_rcvd"] = strconv.FormatUint(data.PktsRcvd, 10)
}

// Given an average value over periodA and a new value over periodB, calculate a
// rolling average of the two values.
func rollOne(avg, data, avgSec, dataSec uint64) uint64 {
	var rval uint64

	// Scale the units up to avoid losing precision during the integer
	// division.
	avg *= 100
	data *= 100
	if avgSec <= dataSec {
		// If the reporting period is less than the collecting period,
		// then a rolling average doesn't make sense.  We really want
		// the latest average, scaled down to fit the reporting period.
		// So to report a 1 second average from 5 seconds of data, we
		// return: new_data * (1 / 5).  To avoid rounding to zero while
		// doing the integer division, we do the multiplication first:
		rval = (data * avgSec) / dataSec
	} else {
		// To update a per-minute average with 10 seconds of new data,
		// we want: (avg - avg * (10/60)) + new_data.  As above, we
		// multiply first:
		rval = (avg - (avg*dataSec)/avgSec) + data
	}

	// De-scale the result
	return rval / 100
}

// Maintain a running average by periodically rolling in new data.  Returns True
// if any field was updated, False if not.  This allows us to reduce config
// traffic for clients that are largely idle.
func roll(avg, data *archive.XferStats, avgPeriod, dataPeriod time.Duration) bool {
	aSecs := uint64(avgPeriod.Seconds())
	dSecs := uint64(dataPeriod.Seconds())

	br := avg.BytesRcvd
	pr := avg.PktsRcvd
	bs := avg.BytesSent
	ps := avg.PktsSent

	avg.BytesRcvd = rollOne(avg.BytesRcvd, data.BytesRcvd, aSecs, dSecs)
	avg.PktsRcvd = rollOne(avg.PktsRcvd, data.PktsRcvd, aSecs, dSecs)
	avg.BytesSent = rollOne(avg.BytesSent, data.BytesSent, aSecs, dSecs)
	avg.PktsSent = rollOne(avg.PktsSent, data.PktsSent, aSecs, dSecs)

	return (br != avg.BytesRcvd || pr != avg.PktsRcvd ||
		bs != avg.BytesSent || ps != avg.PktsSent)
}

func updateRolling(period time.Duration) {
	// Prepopulate a map with zeroed entries for all clients we currently
	// know about.  We then replace the zero entries for all clients that
	// have some activity in the last period.  Pre-filling the map ensures
	// that idle clients have their rolling averages recalculated with the
	// idle activity in the final stage.
	delta := make(map[string]*archive.XferStats)
	for mac := range rollingStats {
		if mac != "" {
			delta[mac] = &archive.XferStats{}
		}
	}

	// Calculate the change in all the stats since the last time this
	// function was called.
	statsMtx.RLock()
	for mac, stats := range currentStats.Data {
		if mac == "" {
			continue
		}
		current := &stats.Aggregate
		running := rollingStats[mac]

		if running == nil {
			running = &timeStats{}
			rollingStats[mac] = running

		} else if current.BytesRcvd < running.previous.BytesRcvd ||
			current.PktsRcvd < running.previous.PktsRcvd ||
			current.BytesSent < running.previous.BytesSent ||
			current.PktsSent < running.previous.PktsSent {

			// If the current value(s) are lower than on the previous call,
			// then the numbers were reset to 0 on a snapshot event
			// immediately following our previous rollup.
			running.previous = archive.XferStats{}
		}

		delta[mac] = &archive.XferStats{
			BytesRcvd: current.BytesRcvd - running.previous.BytesRcvd,
			PktsRcvd:  current.PktsRcvd - running.previous.PktsRcvd,
			BytesSent: current.BytesSent - running.previous.BytesSent,
			PktsSent:  current.PktsSent - running.previous.PktsSent,
		}

		running.total.BytesRcvd += delta[mac].BytesRcvd
		running.total.PktsRcvd += delta[mac].PktsRcvd
		running.total.BytesSent += delta[mac].BytesSent
		running.total.PktsSent += delta[mac].PktsSent

		running.previous = *current
	}
	statsMtx.RUnlock()

	now := time.Now().Format(time.RFC3339)
	for mac, stats := range delta {
		macKey := network.MacToUint64(mac)
		if internalMacs[macKey] {
			continue
		}

		props := make(map[string]string)
		base := "@/metrics/clients/" + mac

		// If this client sent any data in the current period, update
		// its 'last_activity' property.
		if stats.BytesSent != 0 {
			props[base+"/last_activity"] = now
		}

		r := rollingStats[mac]
		if roll(&r.second, stats, time.Second, period) {
			addMetrics(props, base+"/total", &r.total)
			addMetrics(props, base+"/second", &r.second)
		}
		if roll(&r.minute, stats, time.Minute, period) {
			addMetrics(props, base+"/minute", &r.minute)
		}
		if roll(&r.hour, stats, time.Hour, period) {
			addMetrics(props, base+"/hour", &r.hour)
		}
		if roll(&r.day, stats, 24*time.Hour, period) {
			addMetrics(props, base+"/day", &r.day)
		}

		if err := config.CreateProps(props, nil); err != nil {
			slog.Warnf("updating %s failed: %v", base, err)
		}
	}
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
	s := archive.Session{RAddr: remote, RPort: rport, LPort: lport}
	key := archive.SessionToKey(s)

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

func writeStats(dir string, sn *archive.Snapshot) error {
	file := dir + "/" + sn.Start.Format(time.RFC3339) + ".gob"

	archive := []*archive.Snapshot{sn}
	buf := new(bytes.Buffer)
	enc := gob.NewEncoder(buf)
	err := enc.Encode(archive)
	if err != nil {
		err = fmt.Errorf("unable to construct snapshot GOB: %v", err)
	} else if err = ioutil.WriteFile(file, buf.Bytes(), 0644); err != nil {
		err = fmt.Errorf("failed to write snapshot file %s: %v",
			file, err)
	}

	return err
}

func snapshotStats(dir string) *archive.Snapshot {
	statsMtx.RLock()

	now := time.Now()
	sn := newSnapshot()

	sn.Start = currentStats.Start
	sn.End = now
	currentStats.Start = now
	currentStats.End = now.Add(*sfreq)
	lan, wan := 0, 0
	for mac, cur := range currentStats.Data {
		if mac == "" {
			continue
		}

		cur.Lock()

		// Make a copy of the current state.  For the elements we are
		// going to reset, we can just copy the pointers.  The elements
		// that are preserved across snapshots have to be copied by
		// value.
		d := archive.DeviceRecord{
			Addr:       copyIP(cur.Addr),
			BlockedOut: cur.BlockedOut,
			BlockedIn:  cur.BlockedIn,
			Aggregate:  cur.Aggregate,
			OpenTCP:    copyPortlist(cur.OpenTCP),
			OpenUDP:    copyPortlist(cur.OpenUDP),
			LANStats:   cur.LANStats,
			WANStats:   cur.WANStats,
		}
		lan += len(cur.LANStats)
		wan += len(cur.WANStats)

		cur.BlockedOut = make(map[uint64]int)
		cur.BlockedIn = make(map[uint64]int)
		cur.LANStats = make(map[uint64]archive.XferStats)
		cur.WANStats = make(map[uint64]archive.XferStats)
		cur.Aggregate = archive.XferStats{}
		cur.Unlock()

		sn.Data[mac] = &d
	}
	statsMtx.RUnlock()

	slog.Debugf("snapshot lan stats: %d  wan stats: %d", lan, wan)

	return sn
}

func snapshotClean(dir string) {
	diskLimit := time.Now().Add(-1 * *dretain)

	files, err := ioutil.ReadDir(dir)
	if err != nil {
		slog.Warnf("Unable to get contents of %s: %v", dir, err)
	}
	for _, f := range files {
		if f.ModTime().Before(diskLimit) {
			name := dir + "/" + f.Name()
			if err := os.Remove(name); err != nil {
				slog.Warnf("Unable to remove old %s: %v",
					name, err)
			} else {
				slog.Infof("removed stale stats: %s", name)
			}
		}
	}
}

// Every rfreq period, update the high-level rolling usage statistics.  Every
// sfreq period, persist detailed statistics to disk for upload to the cloud.
func snapshotter() {
	defer metricsWaitGroup.Done()

	ticker := time.NewTicker(*rfreq)
	defer ticker.Stop()

	statsDir := *watchDir + "/stats"
	if !aputil.FileExists(statsDir) {
		if err := os.MkdirAll(statsDir, 0755); err != nil {
			slog.Errorf("Unable to make stats directory %s: %v",
				statsDir, err)
			return
		}
	}

	nextSnapshot := time.Now().Add(*sfreq)
	nextClean := time.Now()
	for done := false; !done; {
		if time.Now().After(nextClean) {
			snapshotClean(statsDir)
			nextClean = time.Now().Add(24 * time.Hour)
		}

		select {
		case <-ticker.C:
		case done = <-metricsDone:
			nextSnapshot = time.Now()
		}

		updateRolling(*rfreq)
		if time.Now().After(nextSnapshot) {
			sn := snapshotStats(statsDir)
			if err := writeStats(statsDir, sn); err != nil {
				slog.Warnf("Persisting snapshot: %v", err)
			}
			nextSnapshot = time.Now().Add(*sfreq)
		}
	}
}

func metricsFini(w *watcher) {
	metricsDone <- true
	metricsWaitGroup.Wait()
	w.running = false
}

func metricsInit(w *watcher) {
	metricsWaitGroup.Add(1)
	go snapshotter()

	w.running = true
}

func init() {
	currentStats = newSnapshot()
	currentStats.Start = time.Now()
	currentStats.End = currentStats.Start.Add(*sfreq)

	addWatcher("metrics", metricsInit, metricsFini)
}
