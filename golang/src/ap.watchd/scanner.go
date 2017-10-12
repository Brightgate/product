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
	"bufio"
	"container/heap"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"ap_common/aputil"
	"ap_common/network"
	"base_def"
	"base_msg"

	"github.com/golang/protobuf/proto"
	nmap "github.com/ktscholl/go-nmap"
)

var (
	scansPending scanQueue
	pendingLock  sync.Mutex

	scansRunning map[*os.Process]bool
	runLock      sync.Mutex

	activeHosts *hostmap // hosts we believe to be currently present
)

const (
	cleanFreq = 10 * time.Minute

	// default scan takes 70sec on average
	// udp scan takes 1000sec on average
	defaultFreq = 2 * time.Minute
	udpFreq     = 30 * time.Minute

	hostLifetime = 1 * time.Hour
	hostScanFreq = 5 * time.Minute

	maxFiles    = 10
	numScanners = 10
)

type hostmap struct {
	sync.Mutex
	active map[string]bool
}

func (h *hostmap) add(ip string) {
	h.Lock()
	defer h.Unlock()
	h.active[ip] = true
}

func (h *hostmap) del(ip string) {
	h.Lock()
	defer h.Unlock()
	delete(h.active, ip)
}

func (h *hostmap) contains(ip string) bool {
	h.Lock()
	defer h.Unlock()
	return h.active[ip]
}

func hostmapCreate() *hostmap {
	return &hostmap{
		active: make(map[string]bool),
	}
}

// ScanRequest is used to send tasks to scanners
type ScanRequest struct {
	IP   string
	Args []string
	File string

	Period time.Duration
	When   time.Time

	index int // Used for heap maintenance
}

/*******************************************************************
 *
 * Implement the functions required by the heap interface to maintain the
 * ScanRequest queue
 */
type scanQueue []*ScanRequest

func (q scanQueue) Len() int { return len(q) }

func (q scanQueue) Less(i, j int) bool {
	return (q[i].When).Before(q[j].When)
}

func (q scanQueue) Swap(i, j int) {
	q[i], q[j] = q[j], q[i]
	q[i].index = i
	q[j].index = j
}

func (q *scanQueue) Push(x interface{}) {
	n := len(*q)
	req := x.(*ScanRequest)
	req.index = n
	*q = append(*q, req)
}

func (q *scanQueue) Pop() interface{} {
	old := *q
	n := len(old)
	req := old[n-1]
	req.index = -1 // for safety
	*q = old[0 : n-1]
	return req
}

// Examine our collection of archived results to determine which hosts we've
// scanned in the past.
func getScannedHosts() (*hostmap, error) {
	h := hostmapCreate()

	files, err := ioutil.ReadDir(*nmapDir)
	if err != nil {
		return nil, fmt.Errorf("ReadDir failed: %v", err)
	}

	for _, file := range files {
		if file.IsDir() && file.Name() != "netscans" {
			h.add(file.Name())
		}
	}
	return h, nil
}

// schedule runs toRun with a frequency determined by freq.
func schedule(toRun func(), freq time.Duration, startNow bool) {
	ticker := time.NewTicker(freq)
	go func() {
		if startNow {
			toRun()
		}
		for {
			<-ticker.C
			toRun()
		}
	}()
}

// Look through the pending queue and remove any requests for the given IP
// address
func cancelPortScan(ip string) {
	pendingLock.Lock()
	removed := true

	// Because each heap removal may reorder the queue, we need to restart
	// the search at the beginning each time we remove an entry
	for removed {
		removed = false
		for i, req := range scansPending {
			if req.IP == ip {
				heap.Remove(&scansPending, i)
				removed = true
				break
			}
		}
	}
	pendingLock.Unlock()
}

func schedulePortScan(request *ScanRequest) {
	if activeHosts.contains(request.IP) {
		request.When = time.Now()
		pendingLock.Lock()
		heap.Push(&scansPending, request)
		pendingLock.Unlock()
	}
}

// scanner performs ScanRequests as they come in through the scansPending queue
func portScanner() {
	for {
		var req *ScanRequest

		pendingLock.Lock()
		now := time.Now()
		if len(scansPending) > 0 {
			req = scansPending[0]
			if req.When.After(now) {
				req = nil
			} else {
				heap.Pop(&scansPending)
			}
		}
		pendingLock.Unlock()
		if req == nil {
			time.Sleep(time.Second)
			continue
		}

		scansStarted.WithLabelValues(req.IP, req.File).Inc()
		portScan(req.IP, req.Args, req.File)
		dur := time.Since(now).Seconds()
		scanDuration.WithLabelValues(req.IP, req.File).Observe(dur)
		scansFinished.WithLabelValues(req.IP, req.File).Inc()

		schedulePortScan(req)
	}
}

func scannerRequest(ip string) {
	if activeHosts.contains(ip) {
		return
	}

	path := *nmapDir + "/" + ip
	if !aputil.FileExists(path) {
		if err := os.Mkdir(path, 0755); err != nil {
			log.Printf("Error adding directory %s: %v\n", path, err)
			return
		}
	}

	// Scan the host at ip:
	//   -O  Enable OS detection.
	//   -sU UDP Scan.
	//   -sV Probe open ports to determine service/version info.
	//   -T4 Timing template (controls RTT timeouts and retry limits).
	//   -v  Be verbose.
	//
	// XXXX eventually, set scans and scan frequencies based on
	// type of device detected
	baseArgs := []string{"-v", "-sV", "-O", "-T4"}
	udpArgs := append(baseArgs, "-sU")

	defaultScan := ScanRequest{
		IP:     ip,
		Args:   baseArgs,
		File:   "default",
		Period: defaultFreq,
	}

	UDPScan := ScanRequest{
		IP:     ip,
		Args:   udpArgs,
		File:   "default",
		Period: udpFreq,
	}

	activeHosts.add(ip)
	schedulePortScan(&defaultScan)
	schedulePortScan(&UDPScan)
}

func subnetHostScan(iface, subnet string, scannedHosts *hostmap) int {
	seen := 0

	if err := network.WaitForDevice(iface, 0); err != nil {
		if *verbose {
			log.Printf("%s offline.  Skipping scan.\n", iface)
		}
		return 0
	}

	file := fmt.Sprintf("%s/netscans/netscan-%d.xml", *nmapDir,
		int(time.Now().Unix()))

	// Attempt to discover hosts:
	//   - TCP SYN and ACK probe to the listed ports.
	//   - UDP ping to the default port (40125).
	//   - SCTP INIT ping to the default port (80).
	args := []string{"-sn", "-PS22,53,3389,80,443",
		"-PA22,53,3389,80,443", "-PU", "-PY"}

	scanResults, err := scan(subnet, args, file)
	if err != nil {
		log.Printf("Scan of %s failed: %v\n", iface, err)
		return 0
	}
	clients := config.GetClients()
	for _, host := range scanResults.Hosts {
		var ip, mac string
		if host.Status.State != "up" {
			continue
		}
		for _, addr := range host.Addresses {
			if addr.AddrType == "ipv4" {
				ip = addr.Addr
			} else if addr.AddrType == "mac" {
				mac = strings.ToLower(addr.Addr)
			}
		}

		// Skip any incomplete records.  We also don't want to schedule
		// scans of the router (i.e., us)
		if ip == "" || ip == network.SubnetRouter(subnet) {
			continue
		}

		seen++
		// Already being regularly scanned, no need to schedule it
		if activeHosts.contains(ip) {
			continue
		}

		if scannedHosts.contains(ip) {
			if _, ok := clients[mac]; !ok {
				log.Printf("Unknown host %s found on %s: %s",
					mac, iface, ip)
				logUnknown(iface, mac, ip)
			} else {
				log.Printf("host %s now active on %s: %s",
					mac, iface, ip)
			}
		} else {
			log.Printf("%s is back online on %s, restarting scans",
				ip, iface)
		}
		scannerRequest(ip)
	}
	return seen
}

// hostScan scans each of interfaces for new hosts and schedules regular port
// scans on the host if one is found.
func hostScan() {
	defer hostScanCount.Inc()

	scannedHosts, err := getScannedHosts()
	if err != nil {
		log.Println("error getting scannedHosts:", err)
		return
	}

	scannedHostsGauge.Set(float64(len(scannedHosts.active) - 1))
	ipMap := config.GetSubnets()

	seen := 0
	for iface, subnet := range ipMap {
		seen += subnetHostScan(iface, subnet, scannedHosts)
	}
	hostsUp.Set(float64(seen))
}

//
// Wait for stdout/stderr from a process, and print whatever it sends.  When the
// pipe is closed, notify our caller.
//
func handlePipe(r io.ReadCloser, done chan bool) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		log.Printf("%s\n", scanner.Text())
	}

	done <- true
}

// scan uses nmap to scan ip with the given arguments, outputting its results
// to the given file and parsing its contents into an NmapRun struct.
// If verbose is true, output of nmap is printed to log, otherwise it is ignored.
func scan(ip string, nmapArgs []string, file string) (*nmap.NmapRun, error) {
	var childProcess *os.Process

	args := []string{ip, "-oX", file}
	args = append(args, nmapArgs...)
	child := aputil.NewChild("/usr/bin/nmap", args...)

	if *verbose {
		child.LogOutput("", 0)
	}

	runLock.Lock()
	err := child.Start()
	if err == nil {
		childProcess = child.Process
		scansRunning[childProcess] = true
	}
	runLock.Unlock()

	if err != nil {
		return nil, fmt.Errorf("error starting nmap: %v", err)
	}

	err = child.Wait()

	runLock.Lock()
	delete(scansRunning, childProcess)
	runLock.Unlock()

	if err != nil {
		return nil, fmt.Errorf("error running nmap: %v", err)
	}
	fileContent, err := ioutil.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("error reading nmap results %s: %v",
			file, err)
	}
	scanResults, err := nmap.Parse(fileContent)
	if err != nil {
		return nil, fmt.Errorf("error parsig nmap results %s: %v",
			file, err)
	}
	return scanResults, nil
}

// toHosts changes an NmapRun struct into something that can be sent over
// the message bus
func toHosts(s *nmap.NmapRun) []*base_msg.Host {
	hosts := make([]*base_msg.Host, 0)
	for _, host := range s.Hosts {
		h := new(base_msg.Host)
		h.Starttime = timestampToProto(host.StartTime)
		h.Endtime = timestampToProto(host.EndTime)
		h.Status = proto.String(host.Status.State)
		h.StatusReason = proto.String(host.Status.Reason)
		for _, addr := range host.Addresses {
			a := &base_msg.InfoAndType{
				Info: proto.String(addr.Addr),
				Type: proto.String(addr.AddrType),
			}
			h.Addresses = append(h.Addresses, a)
		}
		for _, hostname := range host.Hostnames {
			hn := &base_msg.InfoAndType{
				Info: proto.String(hostname.Name),
				Type: proto.String(hostname.Type),
			}
			h.Hostnames = append(h.Hostnames, hn)
		}
		for _, extraports := range host.ExtraPorts {
			for _, reason := range extraports.Reasons {
				ep := &base_msg.ExtraPort{
					State:  proto.String(extraports.State),
					Count:  proto.Int(reason.Count),
					Reason: proto.String(reason.Reason),
				}
				h.ExtraPorts = append(h.ExtraPorts, ep)
			}
		}
		for _, port := range host.Ports {
			p := new(base_msg.Port)
			p.Protocol = proto.String(port.Protocol)
			p.PortId = proto.Int(port.PortId)
			p.State = proto.String(port.State.State)
			p.StateReason = proto.String(port.State.Reason)
			p.ServiceName = proto.String(port.Service.Name)
			p.ServiceMethod = proto.String(port.Service.Method)
			p.Confidence = proto.Int(port.Service.Conf)

			//optional
			p.DeviceType = proto.String(port.Service.DeviceType)
			p.Product = proto.String(port.Service.Product)
			p.ExtraInfo = proto.String(port.Service.ExtraInfo)
			p.ServiceFp = proto.String(port.Service.ServiceFp)
			p.Version = proto.String(port.Service.Version)
			for _, cpe := range port.Service.CPEs {
				p.Cpes = append(p.Cpes, string(cpe))
			}
			p.Ostype = proto.String(port.Service.OsType)

			h.Ports = append(h.Ports, p)
		}
		for _, usedPort := range host.Os.PortsUsed {
			up := &base_msg.UsedPort{
				State:    proto.String(usedPort.State),
				Protocol: proto.String(usedPort.Proto),
				PortId:   proto.Int(usedPort.PortId),
			}
			h.PortsUsed = append(h.PortsUsed, up)
		}
		for _, match := range host.Os.OsMatches {
			m := new(base_msg.OSMatch)
			m.Name = proto.String(match.Name)
			m.Accuracy = proto.String(match.Accuracy)
			m.Line = proto.String(match.Line)
			for _, class := range match.OsClasses {
				c := new(base_msg.OSClass)
				c.Type = proto.String(class.Type)
				c.Vendor = proto.String(class.Vendor)
				c.Osfamily = proto.String(class.OsFamily)
				c.Osgen = proto.String(class.OsGen)
				c.Accuracy = proto.String(class.Accuracy)
				for _, cpe := range class.CPEs {
					c.Cpes = append(c.Cpes, string(cpe))
				}
				m.OsClasses = append(m.OsClasses, c)
			}
			h.OsMatches = append(h.OsMatches, m)
		}
		for _, f := range host.Os.OsFingerprints {
			h.OsFingerprints = append(h.OsFingerprints, string(f.Fingerprint))
		}
		h.Uptime = proto.Int(host.Uptime.Seconds)
		h.Lastboot = proto.String(host.Uptime.Lastboot)
		hosts = append(hosts, h)
	}
	return hosts
}

func timestampToProto(t nmap.Timestamp) *base_msg.Timestamp {
	tt := time.Time(t)
	return &base_msg.Timestamp{
		Seconds: proto.Int64(tt.Unix()),
		Nanos:   proto.Int32(int32(tt.Nanosecond())),
	}
}

// portScan scans the ports of the given IP address using nmap, putting
// results on the message bus. Scans of IP are stopped if host is down.
func portScan(ip string, nmapArgs []string, filename string) {
	file := fmt.Sprintf("%s/%s/%s-%d.xml", *nmapDir, ip, filename,
		int(time.Now().Unix()))

	scanResults, err := scan(ip, nmapArgs, file)
	if err != nil {
		return
	}

	if len(scanResults.Hosts) == 1 && scanResults.Hosts[0].Status.State != "up" {
		log.Printf("Host %s is down, stopping scans", ip)
		activeHosts.del(ip)
		cancelPortScan(ip)
		return
	}

	hosts := toHosts(scanResults)
	t := time.Now()
	start := fmt.Sprintf("Nmap %s scan initiated %s as: %s", scanResults.Version,
		scanResults.StartStr, scanResults.Args)

	scan := &base_msg.EventNetScan{
		Timestamp: &base_msg.Timestamp{
			Seconds: proto.Int64(t.Unix()),
			Nanos:   proto.Int32(int32(t.Nanosecond())),
		},
		Sender:       proto.String(brokerd.Name),
		Debug:        proto.String("-"),
		ScanLocation: proto.String(file),
		StartInfo:    proto.String(start),
		Hosts:        hosts,
		Summary:      proto.String(scanResults.RunStats.Finished.Summary),
	}

	err = brokerd.Publish(scan, base_def.TOPIC_SCAN)
	if err != nil {
		log.Printf("Error sending scan: %v\n", err)
	}
}

// ByDateModified is for sorting files by date modified.
type ByDateModified []os.FileInfo

func (s ByDateModified) Len() int {
	return len(s)
}

func (s ByDateModified) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

func (s ByDateModified) Less(i, j int) bool {
	iTime := s[i].ModTime()
	jTime := s[j].ModTime()
	return (iTime.Before(jTime))
}

func cleanHostdir(path string) int {
	all, err := ioutil.ReadDir(path)
	if err != nil {
		log.Printf("Unable to read dir %s: %v\n", path, err)
		return -1
	}

	// ReadDir returns all the directory contents sorted by name.  We really
	// want a list of files sorted by age
	files := make([]os.FileInfo, 0)
	for _, obj := range all {
		if !obj.IsDir() {
			files = append(files, obj)
		}
	}
	sort.Sort(ByDateModified(files))

	remaining := len(files)
	oldest := time.Now().Add(-1 * hostLifetime)
	for _, x := range files {
		if remaining > maxFiles || x.ModTime().Before(oldest) {
			fullPath := path + "/" + x.Name()
			if err := os.Remove(fullPath); err != nil {
				log.Printf("Error removing %s: %v\n", fullPath, err)
			} else {
				remaining--
			}
		}
	}

	if remaining == 0 {
		if err := os.RemoveAll(path); err != nil {
			log.Printf("Error removing nmap dir %s: %v\n",
				path, err)
		}
	}

	return remaining
}

// cleanAll deletes all but the most recent maxFiles files of any one scan type,
// and also deletes files older than hostLifetime. If a directory is empty, it
// is deleted.
func cleanAll() {
	defer cleanScanCount.Inc()

	scannedHosts, err := getScannedHosts()
	if err != nil {
		log.Println("error getting scannedHosts:", err)
		return
	}

	for host := range scannedHosts.active {
		if cleanHostdir(*nmapDir+"/"+host) == 0 {
			log.Printf("No recent scans for %s, forgetting host", host)
			activeHosts.del(host)
			cancelPortScan(host)
		}
	}
}

func scannerFini() {
	log.Printf("Stopping active scans\n")
	runLock.Lock()
	for r := range scansRunning {
		r.Signal(syscall.SIGKILL)
	}
	runLock.Unlock()
	log.Printf("Shutting down scanner\n")
}

func scannerInit() {
	activeHosts = hostmapCreate()
	scansPending = make(scanQueue, 0)
	heap.Init(&scansPending)

	scansRunning = make(map[*os.Process]bool)

	os.MkdirAll(*nmapDir+"/netscans", 0755)
	for i := 0; i < numScanners; i++ {
		go portScanner()
	}

	schedule(hostScan, hostScanFreq, true)
	schedule(cleanAll, cleanFreq, false)
}
