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
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"bg/ap_common/aputil"
	"bg/ap_common/network"
	"bg/base_def"
	"bg/common/archive"
)

var (
	pipeName = flag.String("pipeName", "/var/tmp/bgpipe",
		"rsyslog named pipe to monitor")

	wanIfaces map[string]bool
	logpipe   *os.File
	dropDir   string

	droplogRunning bool
	dropThreads    sync.WaitGroup

	lanDrops []*archive.DropRecord
	wanDrops []*archive.DropRecord
)

// Default logfile format:
//
// Sep 19 17:20:59 bgrouter kernel: [271855.655121] DROPPED
//             IN=brvlan5 OUT=brvlan4 MAC=9c:ef:d5:fe:e8:36:b8:27:eb:19:0f:23:08:00
//             SRC=192.168.137.13 DST=192.168.136.4 LEN=60 TOS=0x00 PREC=0x00 TTL=63
//             ID=35144 DF PROTO=TCP SPT=55276 DPT=22 WINDOW=29200 RES=0x00 SYN URGP=0
//
// Log entries sent to a named pipe should have a compatible format, if rsyslog
// is configured as follows:
//
//    $template BGFormat,"%timegenerated% %msg:::drop-last-lf%\n"
//    :msg, contains, "DROPPED IN" |/var/tmp/bgpipe ; BGFormat
//    & ~
//

// Use a regular expression to extract the date and details of a dropped packet
// message.  We use the square brackets to divide the line.  Note also the use
// of \b (word boundary) to force the datestamp not to have any trailing
// whitespace (time.Parse gets mad).
var dropRE = regexp.MustCompile(`(.+)\b\s+\[.+\]\s+DROPPED\s+(.*)`)

func getDrop(line string) *archive.DropRecord {
	d := &archive.DropRecord{}

	l := dropRE.FindStringSubmatch(line)
	if l == nil {
		// Ignore any log messages that don't look like drops
		log.Printf("ignored message <%s>\n", line)
		return nil
	}

	// The first matched expression is the date
	when, err := time.Parse("Jan 2 15:04:05", l[1])
	if err == nil {
		year := time.Now().Year()
		d.Time = when.AddDate(year, 0, 0)
	} else {
		log.Printf("Failed to read time from substring <%s> of "+
			"full line <%s>: %v\n", l[1], line, err)
	}

	// The second match contains the contents of the DROP message.
	for _, field := range strings.Split(l[2], " ") {
		var key, val string

		f := strings.SplitN(field, "=", 2)
		key = strings.ToLower(f[0])
		if len(f) > 1 {
			val = strings.ToLower(f[1])
		}
		switch key {
		case "in":
			d.Indev = val
		case "src":
			d.SrcIP = net.ParseIP(val)
		case "dst":
			d.DstIP = net.ParseIP(val)
		case "mac":
			// The MAC field contains both the source and
			// destination MAC addresses.  Because we only drop
			// packets that are crossing (v)LAN boundaries, the
			// destination MAC address is generally meaningless.
			if len(f) > 1 {
				all := strings.Split(val, ":")
				if len(all) >= 12 {
					d.Smac = strings.Join(all[6:12], ":")
				}
			}
		case "spt":
			d.SrcPort, _ = strconv.Atoi(val)
		case "dpt":
			d.DstPort, _ = strconv.Atoi(val)
		case "proto":
			d.Proto = val
		}
	}
	if d.Indev == "" {
		log.Printf("bad line: <%s>\n", line)
		return nil
	}
	d.Dst = d.DstIP.String() + ":" + strconv.Itoa(d.DstPort)
	d.Src = d.SrcIP.String() + ":" + strconv.Itoa(d.SrcPort)

	// If we are currently scanning this client, ignore any dropped packets
	// to the gateway.  We assume that the packets are responses to our
	// probing rather than traffic initiated by the client.
	if gateways[network.IPAddrToUint32(d.DstIP)] &&
		scanCheck(d.Proto, d.SrcIP.String()) {
		d = nil
	}

	return d
}

// Persist a single set of drop records to the watchd spool area
func archiveOne(start, end time.Time, lan, wan []*archive.DropRecord) {
	rec := archive.DropArchive{
		Start: start,
		End:   end,
	}
	if len(lan) > 0 {
		rec.LanDrops = lan
	}
	if len(wan) > 0 {
		rec.WanDrops = wan
	}
	file := dropDir + "/" + start.Format(time.RFC3339) + ".json"

	archive := []archive.DropArchive{rec}
	s, err := json.MarshalIndent(&archive, "", "  ")
	if err != nil {
		log.Printf("unable to construct droplog JSON: %v", err)
	} else if err = ioutil.WriteFile(file, s, 0644); err != nil {
		log.Printf("failed to write droplog file %s: %v", file, err)
	}
}

// Periodically archive and reset the lists of firewall drop records
func archiver(lock *sync.Mutex, done chan bool) {
	defer dropThreads.Done()

	ticker := time.NewTicker(*sfreq)
	start := time.Now()
	for droplogRunning {
		snapshotClean(dropDir)
		select {
		case <-ticker.C:
			lock.Lock()
			now := time.Now()
			saveLan := lanDrops
			saveWan := wanDrops
			lanDrops = make([]*archive.DropRecord, 0)
			wanDrops = make([]*archive.DropRecord, 0)
			lock.Unlock()

			archiveOne(start, now, saveLan, saveWan)
			start = now
		case <-done:
		}
	}
	archiveOne(start, time.Now(), lanDrops, wanDrops)
	log.Printf("Archiver done\n")
}

func countDrop(d *archive.DropRecord) {
	if mac, ok := ipToMac[d.SrcIP.String()]; ok {
		incBlockCnt(d.Proto, mac, d.DstIP, d.DstPort, d.SrcPort, true)
	}

	if mac, ok := ipToMac[d.DstIP.String()]; ok {
		incBlockCnt(d.Proto, mac, d.SrcIP, d.SrcPort, d.DstPort, false)
	}
}

// Monitor the named pipe to which rsyslog sends firewall drop messages.  Each
// valid message is turned into a 'DropRecord' struct, which eventually gets
// archived.
func logMonitor(name string) {
	var lock sync.Mutex
	defer dropThreads.Done()

	lanDrops = make([]*archive.DropRecord, 0)
	wanDrops = make([]*archive.DropRecord, 0)

	openPipe()
	doneChan := make(chan bool)
	dropThreads.Add(1)
	go archiver(&lock, doneChan)
	scanner := bufio.NewScanner(logpipe)

	for droplogRunning && scanner.Scan() {
		if d := getDrop(scanner.Text()); d != nil {
			lock.Lock()
			if wanIfaces[d.Indev] {
				wanDrops = append(wanDrops, d)
			} else {
				lanDrops = append(lanDrops, d)
				countDrop(d)
			}
			lock.Unlock()
		}
	}

	if err := scanner.Err(); err != nil {
		log.Fatalf("error processing log pipe: %v\n", err)
	}

	doneChan <- true
}

// Open the named pipe into which rsyslog deposits DROP messages.
func openPipe() {
	var err error
	var warned bool

	log.Printf("Opening droplog pipe: %s\n", *pipeName)
	for droplogRunning && logpipe == nil {
		logpipe, err = os.OpenFile(*pipeName, os.O_RDONLY,
			os.ModeNamedPipe)
		if err != nil {
			if !warned {
				log.Printf("Failed to open droplog pipe %s: %v",
					*pipeName, err)
				warned = true
			}
			time.Sleep(time.Second)
		}
	}
	log.Printf("Opened droplog pipe: %s\n", *pipeName)
}

// When properly configured rsyslog will copy DROP messages to a named pipe, but
// it is our responsibility to create the pipe.
func createPipe() error {
	if !aputil.FileExists(*pipeName) {
		log.Printf("Creating named pipe %s for log input\n", *pipeName)
		if err := syscall.Mkfifo(*pipeName, 0600); err != nil {
			return fmt.Errorf("failed to create %s: %v", *pipeName, err)
		}

		log.Printf("Restarting rsyslogd\n")
		c := exec.Command("/bin/systemctl", "restart", "rsyslog")
		if err := c.Run(); err != nil {
			return fmt.Errorf("failed to restart rsyslogd: %v", err)
		}
	}

	return nil
}

// Identify all NICs the connect us to the outside world
func findWanNics() {
	wanIfaces = make(map[string]bool)

	nics, err := config.GetNics(base_def.RING_WAN, false)
	if err != nil {
		log.Printf("failed to get list of WAN NICs: %v\n", err)
		return
	}

	all, err := net.Interfaces()
	if err != nil {
		log.Printf("failed to get local interface list: %v\n", err)
		return
	}

	for _, iface := range all {
		name := strings.ToLower(iface.Name)
		mac := iface.HardwareAddr.String()
		for _, nic := range nics {
			if nic == mac {
				wanIfaces[name] = true
				break
			}
		}
	}
}

func droplogFini(w *watcher) {
	w.running = false
	droplogRunning = false
	if logpipe != nil {
		logpipe.Close()
	}
	dropThreads.Wait()
}

func droplogInit(w *watcher) {
	findWanNics()

	dropDir = *watchDir + "/droplog"
	if !aputil.FileExists(dropDir) {
		if err := os.MkdirAll(dropDir, 0755); err != nil {
			log.Printf("Unable to make droplog directory %s: %v\n",
				dropDir, err)
			return
		}
	}

	if err := createPipe(); err != nil {
		log.Printf("error creating syslog pipe %s: %v", *pipeName, err)
	} else {
		dropThreads.Add(1)
		go logMonitor(*pipeName)
		droplogRunning = true
		w.running = true
	}
}

func init() {
	addWatcher("droplog", droplogInit, droplogFini)
}
