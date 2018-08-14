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
	"os"
	"sync"
	"time"

	"bg/ap_common/aputil"
	"bg/common/archive"
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

	currentStats    *archive.Snapshot
	historicalStats []*archive.Snapshot
	statsMtx        sync.RWMutex
)

func newDeviceRecord(mac string) *archive.DeviceRecord {
	var addr net.IP

	if ip, ok := macToIP[mac]; ok {
		addr = net.ParseIP(ip)
	}
	d := archive.DeviceRecord{
		Addr:       addr,
		Services:   make(map[int]string),
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
	file := dir + "/" + sn.Start.Format(time.RFC3339) + ".json"

	archive := []*archive.Snapshot{sn}
	s, err := json.MarshalIndent(archive, "", "  ")
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
		d := archive.DeviceRecord{
			Addr:       copyIP(cur.Addr),
			Services:   cur.Services,
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
		cur.LANStats = make(map[uint64]archive.XferStats)
		cur.WANStats = make(map[uint64]archive.XferStats)
		cur.Aggregate = archive.XferStats{}
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

	historicalStats = make([]*archive.Snapshot, 0)

	addWatcher("metrics", metricsInit, metricsFini)
}
