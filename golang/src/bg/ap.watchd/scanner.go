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
	"syscall"
	"time"

	"bg/ap_common/apcfg"
	"bg/ap_common/aputil"
	"bg/ap_common/apvuln"
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

	sync.Mutex
}

var (
	scanID uint32

	scanPools   map[string]*scanPool
	scanThreads = map[string]int{
		"tcp":    2,
		"udp":    2,
		"vuln":   3,
		"subnet": 1,
	}

	childProcesses map[*os.Process]bool
	childLock      sync.Mutex

	activeHosts *hostmap // hosts we believe to be currently present

	vulnListFile  string
	vulnList      map[string]vulnDescription
	vulnScannable bool
)

var (
	//   -O  Enable OS detection.
	//   -sV Probe open ports to determine service/version info.
	//   -T4 Timing template (controls RTT timeouts and retry limits).
	//   -v  Be verbose.
	tcpNmapArgs = []string{"-v", "-sV", "-O", "-T4"}
	udpNmapArgs = append(tcpNmapArgs, "-sU")

	tcpFreq      = apcfg.Duration("tcp_freq", 2*time.Minute, true, nil)
	udpFreq      = apcfg.Duration("udp_freq", 30*time.Minute, true, nil)
	vulnFreq     = apcfg.Duration("vuln_freq", 30*time.Minute, true, nil)
	vulnWarnFreq = apcfg.Duration("vuln_freq_warn", time.Hour, true, nil)

	hostLifetime = apcfg.Duration("host_lifetime", time.Hour, true, nil)
	hostScanFreq = apcfg.Duration("hostscan_freq", 5*time.Minute, true, nil)
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
	return nil
}

func (h *hostmap) del(ip string) error {
	h.Lock()
	defer h.Unlock()
	if !h.active[ip] {
		return fmt.Errorf("hostmap doesn't contain %s", ip)
	}
	delete(h.active, ip)
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

	Period *time.Duration
	When   time.Time

	index int // Used for heap maintenance
}

/*******************************************************************
 *
 * Implement the functions required by the heap interface to maintain the
 * ScanRequest queue
 */
type scanQueue []*ScanRequest

func (q scanQueue) Remove(idx int) *ScanRequest {
	return heap.Remove(&q, 0).(*ScanRequest)
}

func (q scanQueue) SearchID(scanID uint32) int {
	for i, req := range q {
		if req != nil && req.ID == scanID {
			return i
		}
	}
	return -1
}

func (q scanQueue) SearchIP(ip string) int {
	for i, req := range q {
		if req != nil && req.IP == ip {
			return i
		}
	}
	return -1
}

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

func rescheduleScan(scanID uint32, when *time.Time) error {
	err := fmt.Errorf("no such scanID")
	for _, pool := range scanPools {
		var idx int

		pool.Lock()
		if idx = pool.active.SearchID(scanID); idx >= 0 {
			err = fmt.Errorf("scan is already running")

		} else if idx = pool.pending.SearchID(scanID); idx >= 0 {
			err = nil
			req := pool.pending.Remove(idx)
			if when != nil {
				req.When = *when
				heap.Push(&pool.pending, req)
			}
		}
		pool.Unlock()
		if idx >= 0 {
			break
		}
	}

	return err
}

func cancelScan(scanID uint32) error {
	return rescheduleScan(scanID, nil)
}

// Look through the pending queue and remove any requests for the given IP
// address
func cancelAllScans(ip string) {
	for _, pool := range scanPools {
		pool.Lock()
		// Because each heap removal may reorder the queue, we need to
		// restart the search at the beginning each time we remove an
		// entry
		removed := true
		for removed {
			removed = false
			if idx := pool.pending.SearchIP(ip); idx >= 0 {
				heap.Remove(&pool.pending, idx)
				removed = true
			}
		}

		// We let an in-process scan complete, but we prevent it from
		// being rescheduled
		if idx := pool.active.SearchIP(ip); idx >= 0 {
			pool.active[idx].Period = nil
		}
		pool.Unlock()
	}
}

func scheduleScan(request *ScanRequest, delay time.Duration, force bool) {
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
		request.When = time.Now().Add(delay)
		slog.Debugf("scheduling %s %s %s at %s, args: %v\n",
			request.IP, request.Mac, st,
			request.When.Format(time.RFC3339), request.Args)

		pool.Lock()
		heap.Push(&pool.pending, request)
		pool.Unlock()
	}
}

func poolGetNext(pool *scanPool) *ScanRequest {
	pool.Lock()
	defer pool.Unlock()

	if len(pool.pending) == 0 {
		return nil
	}

	now := time.Now()
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

		slog.Infof("starting %s:%s %1.1f seconds late.  "+
			" %1.0f%% of %s scans are late.",
			req.ScanType, req.IP, late, pct, req.ScanType)
	}

	return req
}

// mkScanProp creates properties for @/clients/<mac>/scans/<scantype>/[start|finish]
// if @/clients/<mac> exists.
func mkScanProp(mac, scanType, startfinish, value string) {
	propTest := fmt.Sprintf("@/clients/%s", mac)
	propCreate := fmt.Sprintf("@/clients/%s/scans/%s/%s", mac, scanType, startfinish)
	props := []cfgapi.PropertyOp{
		// avoid recreating clients which have been deleted.
		{Op: cfgapi.PropTest, Name: propTest},
		{Op: cfgapi.PropCreate, Name: propCreate, Value: nowString()},
	}
	slog.Debugf("props: %v", props)
	_ = config.Execute(context.TODO(), props)
}

// scanner performs ScanRequests as they come in through the pending queue
func scanner(pool *scanPool, threadID int) {
	for {
		req := poolGetNext(pool)

		if req == nil {
			time.Sleep(time.Second)
			continue
		}

		slog.Debugf("starting %s %s", req.IP, req.ScanType)

		pool.Lock()
		pool.active[threadID] = req
		pool.Unlock()

		if req.Mac != "" {
			mkScanProp(req.Mac, req.ScanType, "start", nowString())
		}

		req.Scanner(req)

		if req.Mac != "" {
			mkScanProp(req.Mac, req.ScanType, "finish", nowString())
		}

		pool.Lock()
		pool.active[threadID] = nil
		pool.Unlock()

		slog.Debugf("finished %s %s", req.IP, req.ScanType)

		if req.Period != nil {
			scheduleScan(req, *req.Period, false)
		}
	}
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

func newSubnetScan(ring, subnet string) *ScanRequest {
	return &ScanRequest{
		Ring:     ring,
		IP:       subnet,
		ScanType: "subnet",
		Scanner:  subnetScan,
		Period:   hostScanFreq,
	}
}

func newTCPScan(mac, ip string) *ScanRequest {
	return &ScanRequest{
		IP:       ip,
		Mac:      mac,
		Args:     tcpNmapArgs,
		ScanType: "tcp",
		Scanner:  portScan,
		Period:   tcpFreq,
	}
}

func newUDPScan(mac, ip string) *ScanRequest {
	return &ScanRequest{
		IP:       ip,
		Mac:      mac,
		Args:     udpNmapArgs,
		ScanType: "udp",
		Scanner:  portScan,
		Period:   udpFreq,
	}
}

func newVulnScan(mac, ip string) *ScanRequest {
	return &ScanRequest{
		IP:       ip,
		Mac:      mac,
		ScanType: "vuln",
		Scanner:  vulnScan,
		Period:   vulnFreq,
	}
}

func scannerRequest(mac, ip string, delay time.Duration) {
	if err := activeHosts.add(ip); err != nil {
		slog.Debugf("scannerRequest(\"%s\", \"%s\") ignored: %s",
			mac, ip, "IP already in activeHosts")
		return
	}
	tcpScan := newTCPScan(mac, ip)
	udpScan := newUDPScan(mac, ip)
	vulnScan := newVulnScan(mac, ip)

	metrics.knownHosts.Set(activeHosts.len())
	scheduleScan(tcpScan, delay, false)
	scheduleScan(udpScan, delay, false)
	scheduleScan(vulnScan, delay, false)
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

	// Attempt to discover hosts:
	//   - TCP SYN and ACK probe to the listed ports.
	//   - UDP ping to the default port (40125).
	//   - SCTP INIT ping to the default port (80).
	args := []string{"-sn", "-PS22,53,3389,80,443",
		"-PA22,53,3389,80,443", "-PU", "-PY"}

	scanResults, err := nmapScan("subnetscan", req.IP, args)
	if err != nil {
		slog.Warnf("Scan of %s ring failed: %v", req.Ring, err)
		return
	}
	clients := config.GetClients()
	for _, host := range scanResults.Hosts {
		slog.Debugf("nmap found %v: %s", host.Addresses,
			host.Status.State)
		if host.Status.State != "up" {
			continue
		}
		mac, ip := getMacIP(&host)

		if internalMacs[network.MacToUint64(mac)] {
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

		if _, ok := clients[mac]; !ok {
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

func runCmd(cmd string, args []string) error {
	var childProcess *os.Process

	child := aputil.NewChild(cmd, args...)

	if *nmapVerbose {
		child.UseZapLog("", slog, zapcore.DebugLevel)
	}

	err := child.Start()
	if err == nil {
		childLock.Lock()
		childProcess = child.Process
		childProcesses[childProcess] = true
		childLock.Unlock()
	}

	if err != nil {
		err = fmt.Errorf("error starting %s: %v", cmd, err)
	} else {
		if err = child.Wait(); err != nil {
			err = fmt.Errorf("error running %s: %v", cmd, err)
		}

		childLock.Lock()
		delete(childProcesses, childProcess)
		childLock.Unlock()
	}
	return err
}

// scan uses nmap to scan ip with the given arguments, parsing its results into
// an NmapRun struct.
func nmapScan(prefix, ip string, nmapArgs []string) (*nmap.NmapRun, error) {

	file, err := ioutil.TempFile("", pname+"."+prefix+".")
	if err != nil {
		return nil, fmt.Errorf("unable to create temp file: %v", err)
	}
	name := file.Name()
	defer os.Remove(name)

	args := []string{ip, "-oX", name}
	args = append(args, nmapArgs...)

	if err = runCmd("/usr/bin/nmap", args); err != nil {
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
	for _, port := range host.Ports {
		ports = append(ports, port.PortId)
		dev.Services[port.PortId] = port.Service.Name
	}
	if scanType == "tcp" {
		dev.OpenTCP = ports
	} else if scanType == "udp" {
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

func scanCheck(scantype, ip string) bool {
	busy := false

	if pool, ok := scanPools[scantype]; ok {
		pool.Lock()
		if idx := pool.active.SearchIP(ip); idx >= 0 {
			busy = true
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

	opts := []string{opt}
	opts = append(opts, strings.Split(rule, " ")...)
	cmd := exec.Command(plat.IPTablesCmd, opts...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		slog.Warnf("failed to %s iptables rule '%s': %s",
			action, rule, out)
	}
}

// portScan scans the ports of the given IP address using nmap, putting
// results on the message bus. Scans of IP are stopped if host is down.
func portScan(req *ScanRequest) {
	var rule string

	if err := verifyLocalIP(req.IP); err != nil {
		slog.Warnf("not scanning %s: %v", req.IP, err)
		req.Period = nil
		return
	}

	if req.ScanType == "tcp" {
		rule = "INPUT -s " + req.IP + " -p tcp -j ACCEPT"
	} else if req.ScanType == "udp" {
		rule = "INPUT -s " + req.IP + " -p udp -j ACCEPT"
	}

	iptablesRule("-I", rule)
	start := time.Now()
	res, err := nmapScan("portscan", req.IP, req.Args)
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
		slog.Infof("Host %s is down, stopping scans", req.IP)
		activeHosts.del(req.IP)
		metrics.knownHosts.Set(activeHosts.len())
		cancelAllScans(req.IP)
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
	for name := range discovered {
		current := discovered[name]
		if !current.Vuln { // Don't report info if not vulnerable
			continue
		}
		first, warn, q, text := vulnActions(name, vmap)
		ops = append(ops, vulnPropOp(mac, name, "active", "true"))
		ops = append(ops, vulnPropOp(mac, name, "latest", now))
		if ds := current.DetailsSummary(); len(ds) > 0 {
			// Config tree doesn't like extra space
			ds = strings.TrimSpace(ds)
			ops = append(ops, vulnPropOp(mac, name, "details", ds))
		}
		if first {
			ops = append(ops, vulnPropOp(mac, name, "first", now))
		}
		if warn {
			ops = append(ops, vulnPropOp(mac, name, "warned", now))
			event = append(event, name)
		}
		quarantine = quarantine || q

		found = append(found, text)
	}

	if quarantine {
		op := cfgapi.PropertyOp{
			Op:    cfgapi.PropSet,
			Name:  "@/clients/" + mac + "/ring",
			Value: base_def.RING_QUARANTINE,
		}
		ops = append(ops, op)
		slog.Infof("%s being quarantined", mac)
	}

	// Iterate over all of the vulnerabilities discovered in the past.  If
	// they do not appear in the current list, mark them as 'active = false'
	// in the config tree.
	for name, vi := range vmap {
		current, ok := discovered[name]
		if vi.Active && (!ok || !current.Vuln) {
			ops = append(ops, vulnPropOp(mac, name, "active",
				"false"))
			ops = append(ops, vulnPropOp(mac, name, "cleared", now))
		}
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
	if !vulnScannable {
		return
	}

	if err := verifyLocalIP(req.IP); err != nil {
		slog.Warnf("not scanning %s: %v", req.IP, err)
		req.Period = nil
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

	if dev := getDeviceRecord(req.Mac); dev != nil {
		// build <service:port> list to scan for vulnerabilities
		services := make([]string, 0)
		for port, service := range dev.Services {
			services = append(services,
				fmt.Sprintf("%s:%d", service, port))
		}
		releaseDeviceRecord(dev)

		if len(services) > 0 {
			arg := strings.Join(services, ".")
			args = append(args, "-services", arg)
		}
	}

	start := time.Now()
	slog.Debugf("vulnerability scan starting: %s %v", prober, args)
	if err = runCmd(prober, args); err != nil {
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

	done := time.Now()
	metrics.vulnScans.Inc()
	metrics.vulnScanTime.Observe(done.Sub(start).Seconds())
	slog.Debugf("vulnerability scan of %s ended, %.1f seconds",
		req.IP, done.Sub(start).Seconds())
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
	vulnScannable = true

	os.Setenv("NMAPDIR", plat.ExpandDirPath("__APPACKAGE__", "share/nmap"))

	// Make it possible for ap-vuln-aggregate to run ap-inspect without
	// hardcoding the path in the binary.
	os.Setenv("PATH", os.Getenv("PATH")+":"+plat.ExpandDirPath("__APPACKAGE__", "/bin"))
}

func initScanPool(cnt int) *scanPool {
	pool := &scanPool{
		pending: make(scanQueue, 0),
		active:  make([]*ScanRequest, cnt),
	}
	heap.Init(&pool.pending)

	for i := 0; i < cnt; i++ {
		go scanner(pool, i)
	}

	return pool
}

func scannerFini(w *watcher) {
	slog.Infof("Stopping active scans")

	kill := func(sig syscall.Signal) error {
		childLock.Lock()
		for r := range childProcesses {
			r.Signal(sig)
		}
		childLock.Unlock()
		// Don't bother trying to figure out partial errors.
		return nil
	}
	alive := func() bool { return len(childProcesses) > 0 }

	aputil.RetryKill(kill, alive)

	slog.Infof("Shutting down scanner")
	w.running = false
}

func scannerInit(w *watcher) {
	activeHosts = hostmapCreate()
	vulnInit()

	childProcesses = make(map[*os.Process]bool)

	scanPools = make(map[string]*scanPool)
	for name, cnt := range scanThreads {
		scanPools[name] = initScanPool(cnt)
	}

	mapMtx.Lock()
	for mac, ip := range macToIP {
		scannerRequest(mac, ip, 30*time.Second)
	}
	mapMtx.Unlock()

	for ring, config := range rings {
		subnetScan := newSubnetScan(ring, config.Subnet)
		scheduleScan(subnetScan, 0, true)
	}

	w.running = true
}

func init() {
	addWatcher("scanner", scannerInit, scannerFini)
}
