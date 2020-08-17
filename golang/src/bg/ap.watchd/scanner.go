/*
 * Copyright 2020 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package main

import (
	"container/heap"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"bg/ap_common/apcfg"
	"bg/ap_common/aputil"
	"bg/ap_common/apvuln"
	"bg/ap_common/publiclog"
	"bg/base_def"
	"bg/base_msg"
	"bg/common/cfgapi"
	"bg/common/network"

	"github.com/golang/protobuf/proto"
	nmap "github.com/lair-framework/go-nmap"
	"go.uber.org/zap/zapcore"
)

type scanPool struct {
	pending scanQueue
	active  scanQueue

	scansRun  uint32
	scansLate uint32

	pauseUntil time.Time
	scanning   bool
	wg         sync.WaitGroup

	sync.Mutex
}

const (
	tcpPeriodDefault    = 15 * time.Minute
	udpPeriodDefault    = 60 * time.Minute
	passwdPeriodDefault = 24 * time.Hour
	vulnPeriodDefault   = 30 * time.Minute
	subnetPeriodDefault = 5 * time.Minute

	scanGateway = 0x01
	scanCloud   = 0x02
)

var (
	scanID uint32

	scanPools   map[string]*scanPool
	scanThreads = map[string]int{
		"tcp":    2,
		"udp":    2,
		"vuln":   1,
		"passwd": 1,
		"subnet": 1,
	}

	activeHosts    *hostmap // hosts we believe to be currently present
	activeServices map[string][]string

	vulnListFile string
	vulnList     map[string]vulnDescription
)

var (
	//   -O  Enable OS detection.
	//   -sV Probe open ports to determine service/version info.
	//   -T4 Timing template (controls RTT timeouts and retry limits).
	//   -v  Be verbose.
	tcpNmapArgs = []string{"-v", "-sV", "-O", "-T4"}
	udpNmapArgs = append(tcpNmapArgs, "-sU")

	//   - TCP SYN and ACK probe to the listed ports.
	//   - UDP ping to the default port (40125).
	//   - SCTP INIT ping to the default port (80).
	subnetNmapArgs = []string{"-sn", "-PS22,53,3389,80,443",
		"-PA22,53,3389,80,443", "-PU", "-PY"}

	vulnWarnFreq = apcfg.Duration("vuln_freq_warn", time.Hour, true, nil)
)

type vulnDescription struct {
	Nickname string   `json:"Nickname,omitempty"`
	Actions  []string `json:"Actions,omitempty"`
}

type hostmap struct {
	sync.Mutex
	active map[string]bool
}

func (h *hostmap) len() float64 {
	return float64(len(h.active))
}

func (h *hostmap) add(ip string) error {
	h.Lock()
	defer h.Unlock()
	if h.active[ip] {
		return fmt.Errorf("hostmap already contains %s", ip)
	}
	h.active[ip] = true
	metrics.knownHosts.Set(activeHosts.len())
	return nil
}

func (h *hostmap) del(ip string) error {
	h.Lock()
	defer h.Unlock()
	if !h.active[ip] {
		return fmt.Errorf("hostmap doesn't contain %s", ip)
	}
	delete(h.active, ip)
	metrics.knownHosts.Set(activeHosts.len())
	return nil
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
	Pool     *scanPool
	ID       uint32
	Ring     string
	IP       string
	Mac      string
	Args     []string
	ScanType string
	Scanner  func(*ScanRequest)
	Child    *aputil.Child
	Where    uint32

	Cancelled bool
	Period    time.Duration
	When      time.Time
	Last      time.Time

	index int // Used for heap maintenance
}

func (r *ScanRequest) String() string {
	if r.Mac != "" {
		return fmt.Sprintf("%s/%s/%s", r.ScanType, r.Mac, r.IP)
	}
	return fmt.Sprintf("%s/%s", r.ScanType, r.IP)
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

func nowString() string {
	return time.Now().Format(time.RFC3339)
}

type matchFunc func(*ScanRequest) bool

func reschedulePending(q *scanQueue, match matchFunc, period *time.Duration) int {
	resched := make([]*ScanRequest, 0)

	// Because each heap removal may reorder the queue, we need to restart
	// the search at the beginning each time we remove an entry
	for removed := true; removed; {
		removed = false
		for idx, req := range *q {
			if match(req) {
				heap.Remove(q, idx)
				removed = true
				resched = append(resched, req)
				break
			}
		}
	}

	for _, req := range resched {
		var next time.Time

		if period == nil {
			slog.Debugf("cancelling pending %v", req)
			req.Cancelled = true
			req.Period = 0
		} else {
			if *period == 0 {
				next = time.Time{}
			} else {
				req.Period = *period
				if !req.Last.IsZero() {
					next = req.Last.Add(*period)
				}
			}

			if !next.IsZero() {
				slog.Debugf("rescheduled pending %v to %s", req,
					next.Format(time.Stamp))
				req.When = next
			}
			heap.Push(q, req)
		}
	}

	return len(resched)
}

func rescheduleActive(q *scanQueue, match matchFunc, period *time.Duration) (int, int) {
	var processed, busy int

	for _, req := range *q {
		if req != nil && match(req) {
			if period == nil {
				slog.Debugf("cancelling active %v", req)
				processed++
				req.Period = 0
				req.Child.Stop()
			} else if *period == 0 {
				processed++
			} else {
				req.Period = *period
				busy++
			}
		}
	}

	return processed, busy
}

// Look through the pending and active queues for the provided pools, looking
// for scans that meet the caller's criteria.  If 'period' is nil, the scan will
// not be rescheduled and any active scans will be stopped.  If 'period' is 0,
// the scan will be rescheduled to run ASAP, but its period will not be changed.
// If 'period' is greater than 0, then its period will be changed and its next
// run time will be adjusted accordingly.
//
// The function returns the number of scans cancelled/rescheduled and the number
// that were already in progress.
func rescheduleScans(name string, match matchFunc, period *time.Duration) (int, int) {
	var processed, busy int

	if period != nil && *period < 0 {
		return 0, 0
	}

	for pname, pool := range scanPools {
		if name == "all" || name == pname {
			pool.Lock()
			pp := reschedulePending(&pool.pending, match, period)
			ap, ab := rescheduleActive(&pool.active, match, period)
			pool.Unlock()

			processed += (pp + ap)
			busy += ab
		}
	}

	return processed, busy
}

func rescheduleScan(scanID uint32, period *time.Duration) error {
	var err error

	match := func(r *ScanRequest) bool {
		return r.ID == scanID
	}

	p, b := rescheduleScans("all", match, period)
	if b != 0 {
		err = fmt.Errorf("scan already in progress")
	} else if p == 0 {
		err = fmt.Errorf("no matching scan found")
	}
	return err
}

func cancelAllScans(mac, ip string) {
	match := func(r *ScanRequest) bool {
		return (ip != "" && r.IP == ip) || (mac != "" && r.Mac == mac)
	}
	rescheduleScans("all", match, nil)

	if ip != "" {
		activeHosts.del(ip)
	}
}

func scheduleScan(request *ScanRequest, delay time.Duration, force bool) {
	if request == nil {
		slog.Fatalf("nil scan request")
	}

	if aputil.IsGatewayMode() && (request.Where&scanGateway) == 0 {
		return
	}
	if aputil.IsCloudAppMode() && (request.Where&scanCloud) == 0 {
		return
	}

	st := request.ScanType
	pool := request.Pool

	if pool == nil {
		var ok bool

		if pool, ok = scanPools[st]; ok {
			request.Pool = pool
		} else {
			slog.Errorf("bad scan type: %s", st)
			return
		}
	}

	if request.Mac == "" {
		request.Mac = getMacFromIP(request.IP)
	}

	if force || activeHosts.contains(request.IP) ||
		request.ScanType == "subnet" {
		request.When = time.Now().Add(delay)
		if request.ID == 0 {
			request.ID = atomic.AddUint32(&scanID, 1)
		}
		request.Cancelled = false
		slog.Debugf("scheduling %v at %s, args: %v", request,
			request.When.Format(time.RFC3339), request.Args)

		pool.Lock()
		heap.Push(&pool.pending, request)
		pool.Unlock()
	}
}

func configScanPolicyChanged(path []string, value string, expires *time.Time) {
	var match matchFunc

	// Look for:
	//    @/policy/ring/<ring>/scans/<scantype>/period
	//    @/policy/clients/<mac>/scans/<scantype>/period
	prop := "@/" + strings.Join(path, "/")
	slog.Infof("scan policy change: %s -> %s", prop, value)
	if len(path) != 6 || path[3] != "scans" || path[5] != "period" {
		return
	}

	if path[1] == "clients" {
		macAddr := path[2]
		match = func(r *ScanRequest) bool {
			return r.Mac == macAddr
		}
	} else if path[1] == "rings" {
		ring := path[2]
		if _, ok := rings[ring]; !ok {
			return
		}

		match = func(r *ScanRequest) bool {
			reqRing := r.Ring
			if reqRing == "" {
				reqRing = ipToRing(r.IP)
			}

			return reqRing == ring
		}
	}

	if match == nil {
		return
	}

	if d, err := time.ParseDuration(value); err == nil {
		if d == 0 {
			rescheduleScans(path[4], match, nil)
		} else {
			rescheduleScans(path[4], match, &d)
		}
	}
}

func poolGetNext(pool *scanPool) *ScanRequest {
	now := time.Now()

	if now.Before(pool.pauseUntil) || len(pool.pending) == 0 {
		return nil
	}

	req := pool.pending[0]

	delta := now.Sub(req.When).Seconds()
	if delta < 1.0 {
		return nil
	}

	req = heap.Remove(&pool.pending, 0).(*ScanRequest)
	pool.scansRun++
	if late := delta * -1.0; late > 2.0 {
		pool.scansLate++
		pct := 100.0 * float32(pool.scansLate) / float32(pool.scansRun)

		slog.Infof("starting %v %1.1f seconds late.  %1.0f%% of %s "+
			"scans are late.", req, late, pct, req.ScanType)
	}

	return req
}

// mkScanProp creates properties for @/clients/<mac>/scans/<scantype>/[start|finish]
// if @/clients/<mac> exists.
func mkScanProp(req *ScanRequest, phase string) {
	if req.Mac == "" || req.Cancelled {
		return
	}

	base := "@/clients/" + req.Mac
	scanProp := base + "/scans/" + req.ScanType + "/" + phase
	props := []cfgapi.PropertyOp{
		// avoid recreating clients which have been deleted.
		{Op: cfgapi.PropTest, Name: base},
		{Op: cfgapi.PropCreate, Name: scanProp, Value: nowString()},
	}
	_ = config.Execute(context.TODO(), props)
}

// scanner performs ScanRequests as they come in through the pending queue
func scanner(pool *scanPool, threadID int) {
	for pool.scanning {
		pool.Lock()
		if req := poolGetNext(pool); req != nil {
			pool.active[threadID] = req
			pool.Unlock()

			slog.Debugf("starting %v", req)
			mkScanProp(req, "start")

			req.Scanner(req)

			mkScanProp(req, "finish")
			slog.Debugf("finished %v", req)
			req.Last = time.Now()

			pool.Lock()
			pool.active[threadID] = nil
			pool.Unlock()

			if req.Period != 0 {
				scheduleScan(req, req.Period, false)
			}
		} else {
			pool.Unlock()
			time.Sleep(time.Second)
		}
	}
	pool.wg.Done()
}

func scanGetLists() ([]ScanRequest, []ScanRequest) {
	pending := make([]ScanRequest, 0)
	active := make([]ScanRequest, 0)

	for _, pool := range scanPools {
		pool.Lock()
		for _, req := range pool.pending {
			pending = append(pending, *req)
		}
		for _, req := range pool.active {
			if req != nil {
				active = append(active, *req)
			}
		}
		pool.Unlock()
	}
	return pending, active
}

func getScanPeriod(mac, ring, scantype string, def time.Duration) time.Duration {
	// First look for a per-client policy
	cprop := "@/policy/clients/" + mac + "/scans/" + scantype + "/period"
	if rval, err := config.GetPropDuration(cprop); err == nil {
		return rval
	}

	// Next, look for a per-ring policy
	rprop := "@/policy/rings/" + ring + "/scans/" + scantype + "/period"
	if rval, err := config.GetPropDuration(rprop); err == nil {
		return rval
	}

	// No policy set, so use the default value
	return def
}

func newSubnetScan(ring, subnet string) *ScanRequest {
	period := getScanPeriod("", ring, "subnet", subnetPeriodDefault)
	return &ScanRequest{
		Ring:     ring,
		IP:       subnet,
		Args:     subnetNmapArgs,
		ScanType: "subnet",
		Scanner:  subnetScan,
		Period:   period,
		Where:    scanGateway,
	}
}

func newTCPScan(mac, ip, ring string) *ScanRequest {
	period := getScanPeriod(mac, ring, "tcp", tcpPeriodDefault)
	if period == 0 {
		return nil
	}

	return &ScanRequest{
		IP:       ip,
		Mac:      mac,
		Args:     tcpNmapArgs,
		ScanType: "tcp",
		Scanner:  portScan,
		Period:   period,
		Where:    scanGateway,
	}
}

func newUDPScan(mac, ip, ring string) *ScanRequest {
	period := getScanPeriod(mac, ring, "udp", udpPeriodDefault)
	if period == 0 {
		return nil
	}

	return &ScanRequest{
		IP:       ip,
		Mac:      mac,
		Args:     udpNmapArgs,
		ScanType: "udp",
		Scanner:  portScan,
		Period:   period,
		Where:    scanGateway,
	}
}

func newVulnScan(mac, ip, ring string) *ScanRequest {
	period := getScanPeriod(mac, ring, "vuln", vulnPeriodDefault)
	if period == 0 {
		return nil
	}

	return &ScanRequest{
		IP:       ip,
		Mac:      mac,
		Args:     []string{"-t", "!defaultpassword"},
		ScanType: "vuln",
		Scanner:  vulnScan,
		Period:   period,
		Where:    scanGateway,
	}
}

func newPasswdScan(mac, ip, ring string) *ScanRequest {
	period := getScanPeriod(mac, ring, "passwd", passwdPeriodDefault)
	if period == 0 {
		return nil
	}

	return &ScanRequest{
		IP:       ip,
		Mac:      mac,
		Args:     []string{"-t", "defaultpassword"},
		ScanType: "passwd",
		Scanner:  vulnScan,
		Period:   period,
		Where:    scanGateway,
	}
}

func scannerRequest(mac, ip string, delay time.Duration) {
	if err := activeHosts.add(ip); err != nil {
		slog.Debugf("scannerRequest(%s, %s) ignored: %v", mac, ip, err)
		return
	}

	ring := ipToRing(ip)
	if ring == "" {
		slog.Debugf("not scanning %s - not in our network", ip)
		return
	}

	if scan := newTCPScan(mac, ip, ring); scan != nil {
		scheduleScan(scan, delay, false)
	}

	if scan := newUDPScan(mac, ip, ring); scan != nil {
		scheduleScan(scan, delay, false)
	}

	if scan := newVulnScan(mac, ip, ring); scan != nil {
		scheduleScan(scan, delay, false)
	}

	if scan := newPasswdScan(mac, ip, ring); scan != nil {
		// give the two port scans time to identify potential services
		// before testing those services for default passwords.
		scheduleScan(scan, delay+time.Hour, false)
	}
}

func getMacIP(host *nmap.Host) (mac, ip string) {
	for _, addr := range host.Addresses {
		if addr.AddrType == "ipv4" {
			ip = addr.Addr
		} else if addr.AddrType == "mac" {
			mac = strings.ToLower(addr.Addr)
		}
	}
	return
}

// subnetScan scans a ring's subnet for new hosts and schedules regular port
// scans on the host if one is found.
func subnetScan(req *ScanRequest) {
	start := time.Now()

	scanResults, err := nmapScan(req)
	if err != nil {
		slog.Warnf("Scan of %s ring failed: %v", req.Ring, err)
		return
	}
	for _, host := range scanResults.Hosts {
		slog.Debugf("nmap found %v: %s", host.Addresses,
			host.Status.State)
		if host.Status.State != "up" {
			continue
		}
		mac, ip := getMacIP(&host)

		macKey := network.MacToUint64(mac)
		if internalMacs[macKey] {
			// Don't probe any other APs
			continue
		}

		// Skip any incomplete records.  We also don't want to schedule
		// scans of the router (i.e., us)
		if ip == "" || ip == network.SubnetRouter(req.IP) {
			continue
		}

		// Already being regularly scanned, no need to schedule it
		if activeHosts.contains(ip) {
			continue
		}

		if getIPFromMac(mac) == "" {
			slog.Infof("Unknown host %s found on ring %s: %s",
				mac, req.Ring, ip)
			logUnknown(req.Ring, mac, ip)
		} else {
			slog.Infof("host %s now active on ring %s: %s",
				mac, req.Ring, ip)
		}
		scannerRequest(mac, ip, 0)
	}

	done := time.Now()
	metrics.subnetScans.Inc()
	metrics.subnetScanTime.Observe(done.Sub(start).Seconds())
}

func runCmd(req *ScanRequest, cmd string, args []string) error {
	child := aputil.NewChild(cmd, args...)
	req.Child = child

	if *nmapVerbose {
		child.UseZapLog("", slog, zapcore.DebugLevel)
	}

	err := child.Start()

	if err != nil {
		err = fmt.Errorf("error starting %s: %v", cmd, err)
	} else if err = child.Wait(); err != nil {
		err = fmt.Errorf("error running %s: %v", cmd, err)
	}
	req.Child = nil

	return err
}

// scan uses nmap to scan ip with the given arguments, parsing its results into
// an NmapRun struct.
func nmapScan(req *ScanRequest) (*nmap.NmapRun, error) {

	file, err := ioutil.TempFile("", pname+"."+req.ScanType+".")
	if err != nil {
		return nil, fmt.Errorf("unable to create temp file: %v", err)
	}
	name := file.Name()
	defer os.Remove(name)

	args := []string{req.IP, "-oX", name}
	args = append(args, req.Args...)

	if err = runCmd(req, "/usr/bin/nmap", args); err != nil {
		return nil, err
	}

	fileContent, err := ioutil.ReadFile(name)
	if err != nil {
		return nil, fmt.Errorf("error reading nmap results %s: %v",
			name, err)
	}
	scanResults, err := nmap.Parse(fileContent)
	if err != nil {
		return nil, fmt.Errorf("error parsing nmap results %s: %v",
			name, err)
	}
	return scanResults, nil
}

// Find and record all the ports/services we found open for this host.
func recordNmapResults(scanType string, host *nmap.Host) {
	mac, _ := getMacIP(host)
	dev := getDeviceRecord(mac)

	ports := make([]int, 0)
	services := make([]string, 0)
	for _, port := range host.Ports {
		ports = append(ports, port.PortId)
		services = append(services,
			fmt.Sprintf("%s:%d", port.Service.Name, port.PortId))
	}

	if scanType == "tcp" {
		activeServices[mac+":tcp"] = services
		dev.OpenTCP = ports
	} else if scanType == "udp" {
		activeServices[mac+":udp"] = services
		dev.OpenUDP = ports
	}

	releaseDeviceRecord(dev)
}

func timestampToProto(t nmap.Timestamp) *base_msg.Timestamp {
	tt := time.Time(t)
	return aputil.TimeToProtobuf(&tt)
}

// marshal an NmapRun struct into something that fits into a protobuf
func marshalNmapResults(host *nmap.Host) *base_msg.Host {
	var h base_msg.Host

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

	return &h
}

// Check to see whether a specific IP address is being scanned
func scanCheck(scantype, ip string) bool {
	busy := false
	if pool, ok := scanPools[scantype]; ok {
		pool.Lock()
		for _, req := range pool.active {
			if req != nil && req.IP == ip {
				busy = true
				break
			}
		}
		pool.Unlock()
	}

	return busy
}

func verifyLocalIP(ip string) error {
	ipv4 := net.ParseIP(ip)
	if ipv4 == nil {
		return fmt.Errorf("invalid IP address")
	}

	for _, ring := range rings {
		if ring.IPNet.Contains(ipv4) {
			return nil
		}
	}
	return fmt.Errorf("not on one of our subnets")
}

func iptablesRule(opt, rule string) {
	var action string

	if rule == "" {
		return
	}

	if opt == "-I" {
		action = "apply"
	} else if opt == "-D" {
		action = "delete"
	} else {
		slog.Warnf("invalid option '%s'", opt)
		return
	}

	opts := []string{"-w", opt}
	opts = append(opts, strings.Split(rule, " ")...)
	cmd := exec.Command(plat.IPTablesCmd, opts...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		slog.Warnf("failed to %s iptables rule '%s': %s",
			action, rule, out)
	}
}

// portScan scans the ports of the given IP address using nmap, putting
// results on the message bus.
func portScan(req *ScanRequest) {
	var rule string

	if err := verifyLocalIP(req.IP); err != nil {
		slog.Warnf("not scanning %s: %v", req.IP, err)
		req.Period = 0
		return
	}

	if req.ScanType == "tcp" {
		rule = "INPUT -s " + req.IP + " -p tcp -j ACCEPT"
	} else if req.ScanType == "udp" {
		rule = "INPUT -s " + req.IP + " -p udp -j ACCEPT"
	}

	iptablesRule("-I", rule)
	start := time.Now()
	res, err := nmapScan(req)
	done := time.Now()
	iptablesRule("-D", rule)

	if err != nil {
		slog.Warnf("portscan failed: %v", err)
		return
	}

	if req.ScanType == "tcp" {
		metrics.tcpScans.Inc()
		metrics.tcpScanTime.Observe(done.Sub(start).Seconds())
	} else if req.ScanType == "udp" {
		metrics.udpScans.Inc()
		metrics.udpScanTime.Observe(done.Sub(start).Seconds())
	}
	if len(res.Hosts) != 1 {
		slog.Infof("Scan of 1 host returned %d results: %v",
			len(res.Hosts), res)
		return
	}

	host := &res.Hosts[0]
	if host.Status.State != "up" {
		delete(activeServices, req.Mac+":"+req.ScanType)
		return
	}

	recordNmapResults(req.ScanType, host)
	marshalledHosts := make([]*base_msg.Host, 1)
	marshalledHosts[0] = marshalNmapResults(host)

	addr := net.ParseIP(req.IP)
	startInfo := fmt.Sprintf("Nmap %s scan initiated %s as: %s", res.Version,
		res.StartStr, res.Args)

	scan := &base_msg.EventNetScan{
		Timestamp:   aputil.NowToProtobuf(),
		Sender:      proto.String(brokerd.Name),
		Debug:       proto.String("-"),
		Ipv4Address: proto.Uint32(network.IPAddrToUint32(addr)),
		StartInfo:   proto.String(startInfo),
		StartTime:   aputil.TimeToProtobuf(&start),
		FinishTime:  aputil.TimeToProtobuf(&done),
		Hosts:       marshalledHosts,
		Summary:     proto.String(res.RunStats.Finished.Summary),
	}

	if req.ScanType == "tcp" {
		scan.ScanType = base_msg.ScanType_TCP_PORTS.Enum()
	} else if req.ScanType == "udp" {
		scan.ScanType = base_msg.ScanType_UDP_PORTS.Enum()
	}

	err = brokerd.Publish(scan, base_def.TOPIC_SCAN)
	if err != nil {
		slog.Warnf("Error sending scan: %v", err)
	}
}

func vulnException(mac, ip string, details []string) {
	reason := base_msg.EventNetException_VULNERABILITY_DETECTED
	entity := &base_msg.EventNetException{
		Timestamp:   aputil.NowToProtobuf(),
		Sender:      proto.String(brokerd.Name),
		Debug:       proto.String("-"),
		Reason:      &reason,
		MacAddress:  aputil.MacStrToProtobuf(mac),
		Ipv4Address: aputil.IPStrToProtobuf(ip),
		Details:     details,
	}

	err := brokerd.Publish(entity, base_def.TOPIC_EXCEPTION)
	if err != nil {
		slog.Warnf("couldn't publish %v to %s: %v",
			entity, base_def.TOPIC_EXCEPTION, err)
	}

	publiclog.SendLogVulnDetected(brokerd, mac, ip)
}

func vulnPropOp(mac, vuln, field, val string) cfgapi.PropertyOp {
	prop := fmt.Sprintf("@/clients/%s/vulnerabilities/%s/%s",
		mac, vuln, field)

	return cfgapi.PropertyOp{
		Op:    cfgapi.PropCreate,
		Name:  prop,
		Value: val,
	}
}

// Determine which actions need to be taken for this vulnerability, based on the
// information stored in the VulnInfo map.
func vulnActions(name string, vmap cfgapi.VulnMap) (first, warn, quarantine bool, text string) {
	ignore := false

	vi, ok := vmap[name]
	if ok {
		ignore = vi.Ignore
	} else {
		first = true
	}

	nickname := ""
	if v, ok := vulnList[name]; ok {
		nickname = v.Nickname
		for _, a := range v.Actions {
			switch a {
			case "Warn":
				if ignore {
					warn = false
				} else if vi == nil || vi.WarnedAt == nil {
					warn = true
				} else {
					warn = (time.Since(*vi.WarnedAt) > *vulnWarnFreq)
				}
			case "Quarantine":
				quarantine = !ignore
			}
		}
	} else {
		nickname = "unrecognized vulnerability"
	}
	text = name
	if nickname != "" {
		text += " (" + nickname + ")"
	}

	return
}

func vulnScanProcess(ip string, discovered map[string]apvuln.TestResult) {
	var found, event []string
	var quarantine bool

	mac := getMacFromIP(ip)
	vmap := config.GetVulnerabilities(mac)

	// Rather than updating each vulnerability timestamp independently,
	// batch the updates so we can apply them all at once
	now := nowString()
	ops := make([]cfgapi.PropertyOp, 0)

	// Don't update clients which might have been deleted while scanning
	ops = append(ops, cfgapi.PropertyOp{
		Op:   cfgapi.PropTest,
		Name: fmt.Sprintf("@/clients/%s", mac),
	})

	// Iterate over all of the vulnerabilities we discovered in this pass,
	// queue up the appropriate action for each, and note which properties
	// will need to be updated.
	for name, state := range discovered {
		props := make(map[string]string)

		if state.State == apvuln.Vulnerable {
			slog.Debugf("%s vulnerable to %s", mac, name)
			first, warn, q, text := vulnActions(name, vmap)
			props["active"] = "true"
			props["latest"] = now

			details := strings.TrimSpace(state.DetailsSummary())
			if len(details) > 0 {
				props["details"] = details
			}
			if first {
				props["first"] = now
			}
			if warn {
				props["warned"] = now
				event = append(event, name)
			}
			quarantine = quarantine || q
			found = append(found, text)

		} else if state.State == apvuln.Cleared {
			if _, ok := vmap[name]; ok {
				slog.Debugf("%s no longer vulnerable to %s",
					mac, name)
				props["cleared"] = now
				props["active"] = "false"
			}
		}

		for prop, val := range props {
			ops = append(ops, vulnPropOp(mac, name, prop, val))
		}
	}

	if quarantine {
		ringProp := "@/clients/" + mac + "/ring"
		homeProp := "@/clients/" + mac + "/home"
		qRing := base_def.RING_QUARANTINE
		qOps := []cfgapi.PropertyOp{
			{Op: cfgapi.PropCreate, Name: ringProp, Value: qRing},
			{Op: cfgapi.PropCreate, Name: homeProp, Value: qRing},
		}
		ops = append(ops, qOps...)

		slog.Infof("%s being quarantined", mac)

		publiclog.SendLogDeviceQuarantine(brokerd, mac)
	}

	if len(found) > 0 {
		slog.Infof("%s (seen as %s) vulnerable to: %s", mac, ip,
			strings.Join(found, ","))
	}
	if mac != "" && len(ops) > 0 {
		config.Execute(nil, ops)
	}
	if len(event) > 0 {
		vulnException(mac, ip, event)
	}
}

func vulnScan(req *ScanRequest) {
	if err := verifyLocalIP(req.IP); err != nil {
		slog.Warnf("not scanning %s: %v", req.IP, err)
		req.Period = 0
		return
	}

	prober := plat.ExpandDirPath("__APPACKAGE__", "bin/ap-vuln-aggregate")

	resFile, err := ioutil.TempFile("", "vuln.")
	if err != nil {
		slog.Warnf("failed to create result file: %v", err)
		return
	}
	name := resFile.Name()
	defer os.Remove(name)

	args := []string{"-d", vulnListFile, "-i", req.IP, "-o", name}

	services := make([]string, 0)
	services = append(services, activeServices[req.Mac+":tcp"]...)
	services = append(services, activeServices[req.Mac+":udp"]...)
	if len(services) > 0 {
		arg := strings.Join(services, ".")
		args = append(args, "-services", arg)
	}
	if len(req.Args) > 0 {
		args = append(args, req.Args...)
	}

	start := time.Now()
	slog.Debugf("vulnerability scan starting: %s %v", prober, args)
	if err = runCmd(req, prober, args); err != nil {
		slog.Warnf("vulnerability scan of %s failed: %v %v: %v",
			req.IP, prober, strings.Join(args, " "), err)

		return
	}

	found := make(map[string]apvuln.TestResult)
	file, err := ioutil.ReadFile(name)
	if err != nil {
		slog.Warnf("Failed to read scan resuts: %v", err)
		return
	}
	if err = json.Unmarshal(file, &found); err != nil {
		slog.Warnf("Failed to unmarshal resuts: %v", err)
		return
	}

	vulnScanProcess(req.IP, found)

	t := time.Now().Sub(start).Seconds()

	metrics.vulnScans.Inc()
	metrics.vulnScanTime.Observe(t)
	slog.Debugf("vulnerability scan of %s ended, %.1f seconds", req.IP, t)
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

func vulnInit() {
	vulnListFile = *watchDir + "/vuln-db.json"
	vulnList = make(map[string]vulnDescription, 0)
	activeServices = make(map[string][]string)

	file, err := ioutil.ReadFile(vulnListFile)
	if err != nil {
		slog.Errorf("Failed to read vulnerability list '%s': %v",
			vulnListFile, err)
		return
	}

	err = json.Unmarshal(file, &vulnList)
	if err != nil {
		slog.Errorf("Failed to load vulnerability list '%s': %v",
			vulnListFile, err)
		return
	}
	os.Setenv("NMAPDIR", plat.ExpandDirPath("__APPACKAGE__", "share/nmap"))

	// Make it possible for ap-vuln-aggregate to run ap-inspect without
	// hardcoding the path in the binary.
	os.Setenv("PATH", os.Getenv("PATH")+":"+plat.ExpandDirPath("__APPACKAGE__", "/bin"))
}

func poolStopActive(pool *scanPool) {
	for _, req := range pool.active {
		if req != nil {
			if !pool.scanning {
				// If we're shutting down the pool, don't
				// reschedule the scan.
				req.Period = 0
			}
			req.Child.Stop()
		}
	}
}

func scannerPauseUntil(when time.Time) {
	slog.Infof("pausing scanner threads until %s", when.Format(time.Stamp))
	for _, pool := range scanPools {
		pool.Lock()
		pool.pauseUntil = when
		poolStopActive(pool)
		pool.Unlock()
	}
}

func initScanPool(cnt int) *scanPool {
	pool := &scanPool{
		pending:    make(scanQueue, 0),
		active:     make([]*ScanRequest, cnt),
		pauseUntil: time.Now().Add(time.Minute),
		scanning:   true,
	}
	heap.Init(&pool.pending)

	for i := 0; i < cnt; i++ {
		pool.wg.Add(1)
		go scanner(pool, i)
	}

	return pool
}

func scannerFini(w *watcher) {
	slog.Infof("stopping scanner threads")
	for _, pool := range scanPools {
		pool.Lock()
		pool.scanning = false
		poolStopActive(pool)
		pool.Unlock()
	}
	for _, pool := range scanPools {
		pool.wg.Wait()
	}

	slog.Infof("Shutting down scanner")
	w.running = false
}

func scannerInit(w *watcher) {
	activeHosts = hostmapCreate()
	vulnInit()

	scanPools = make(map[string]*scanPool)
	for name, cnt := range scanThreads {
		scanPools[name] = initScanPool(cnt)
	}

	mapMtx.Lock()
	for mac, ip := range macToIP {
		scannerRequest(mac, ip, 0)
	}
	mapMtx.Unlock()

	for ring, config := range rings {
		if cfgapi.SystemRings[ring] {
			continue
		}

		subnetScan := newSubnetScan(ring, config.Subnet)
		scheduleScan(subnetScan, 0, true)
	}

	w.running = true
}

func init() {
	addWatcher("scanner", scannerInit, scannerFini)
}

