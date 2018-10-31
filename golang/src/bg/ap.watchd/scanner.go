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
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"

	"bg/ap_common/aputil"
	"bg/ap_common/apvuln"
	"bg/ap_common/network"
	"bg/base_def"
	"bg/base_msg"
	"bg/common/cfgapi"

	"github.com/golang/protobuf/proto"
	nmap "github.com/lair-framework/go-nmap"
	"go.uber.org/zap/zapcore"
)

var (
	scanID       int
	scansPending scanQueue
	scansRunning map[int]*ScanRequest

	// The follow locks protect scansPending and scansRunning, respectively.
	// If both locks are taken, pendingLock must be taken first.
	pendingLock sync.Mutex
	runningLock sync.Mutex

	tcpScans = make(map[string]bool)
	udpScans = make(map[string]bool)
	scanLock sync.RWMutex

	scanProcesses map[*os.Process]bool
	runLock       sync.Mutex

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
)

const (
	tcpFreq = 10 * time.Minute // How often to scan TCP ports
	udpFreq = 60 * time.Minute // How often to scan UDP ports

	vulnFreq     = 60 * time.Minute // How often to scan for vulnerabilities
	vulnWarnFreq = 3 * time.Hour    // How often to reissue vuln. warnings

	hostLifetime = 1 * time.Hour
	hostScanFreq = 5 * time.Minute

	numScanners = 5
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
	ID       int
	IP       string
	Mac      string
	Args     []string
	ScanType string
	Scanner  func(*ScanRequest)

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

func nowString() string {
	return time.Now().Format(time.RFC3339)
}

// schedule runs toRun with a frequency determined by freq.
func schedule(toRun func(), freq time.Duration, startNow bool) {
	ticker := time.NewTicker(freq)
	defer ticker.Stop()
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

func rescheduleScan(scanID int, when *time.Time) error {
	pendingLock.Lock()
	runningLock.Lock()
	defer runningLock.Unlock()
	defer pendingLock.Unlock()

	if req := scansRunning[scanID]; req != nil {
		return fmt.Errorf("scan is already running")
	}

	for i, req := range scansPending {
		if req.ID == scanID {
			heap.Remove(&scansPending, i)
			if when != nil {
				req.When = time.Now()
				heap.Push(&scansPending, req)
			}
			return nil
		}
	}

	return fmt.Errorf("no such scanID")
}

func cancelScan(scanID int) error {
	return rescheduleScan(scanID, nil)
}

// Look through the pending queue and remove any requests for the given IP
// address
func cancelAllScans(ip string) {
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

	// We let an in-process scan complete, but we prevent it from being
	// rescheduled
	runningLock.Lock()
	for _, req := range scansRunning {
		if req.IP == ip {
			req.Period = 0
		}
	}
	runningLock.Unlock()
}

func scheduleScan(request *ScanRequest, delay time.Duration, force bool) {
	if request.Mac == "" {
		request.Mac = getMacFromIP(request.IP)
	}

	if force || activeHosts.contains(request.IP) {
		request.When = time.Now().Add(delay)
		slog.Debugf("scheduling %s %s %s at %s, args: %v\n",
			request.IP, request.Mac, request.ScanType,
			request.When.Format(time.RFC3339), request.Args)
		pendingLock.Lock()
		if request.ID == 0 {
			scanID++
			request.ID = scanID
		}
		heap.Push(&scansPending, request)
		pendingLock.Unlock()
	}
}

// scanner performs ScanRequests as they come in through the scansPending queue
func scanner() {
	for {
		var req *ScanRequest

		pendingLock.Lock()
		now := time.Now()
		if len(scansPending) > 0 {
			r := scansPending[0]
			if r.When.Before(now) {
				req = heap.Remove(&scansPending, 0).(*ScanRequest)
			}
		}
		pendingLock.Unlock()

		if req == nil {
			time.Sleep(time.Second)
			continue
		}

		propBase := fmt.Sprintf("@/clients/%s/scans/%s/", req.Mac, req.ScanType)

		slog.Debugf("starting %s %s", req.IP, req.ScanType)
		runningLock.Lock()
		scansRunning[req.ID] = req
		runningLock.Unlock()
		config.CreateProp(propBase+"start", nowString(), nil)

		req.Scanner(req)

		config.CreateProp(propBase+"finish", nowString(), nil)
		runningLock.Lock()
		delete(scansRunning, req.ID)
		runningLock.Unlock()

		slog.Debugf("finished %s %s", req.IP, req.ScanType)

		if req.Period != 0 {
			scheduleScan(req, req.Period, false)
		}
	}
}

func scanGetLists() ([]ScanRequest, []ScanRequest) {
	pending := make([]ScanRequest, 0)
	running := make([]ScanRequest, 0)

	pendingLock.Lock()
	runningLock.Lock()
	for _, req := range scansPending {
		pending = append(pending, *req)
	}
	for _, req := range scansRunning {
		running = append(running, *req)
	}
	runningLock.Unlock()
	pendingLock.Unlock()
	return pending, running
}

func newTCPScan(mac, ip string) *ScanRequest {
	return &ScanRequest{
		IP:       ip,
		Mac:      mac,
		Args:     tcpNmapArgs,
		ScanType: "tcp_ports",
		Scanner:  portScan,
		Period:   tcpFreq,
	}
}

func newUDPScan(mac, ip string) *ScanRequest {
	return &ScanRequest{
		IP:       ip,
		Mac:      mac,
		Args:     udpNmapArgs,
		ScanType: "udp_ports",
		Scanner:  portScan,
		Period:   udpFreq,
	}
}

func newVulnScan(mac, ip string) *ScanRequest {
	return &ScanRequest{
		IP:       ip,
		Mac:      mac,
		ScanType: "vulnerability",
		Scanner:  vulnScan,
		Period:   vulnFreq,
	}
}

func scannerRequest(mac, ip string) {
	if err := activeHosts.add(ip); err != nil {
		slog.Debugf("scannerRequest(\"%s\", \"%s\") ignored: %s",
			mac, ip, "IP already in activeHosts")
		return
	}
	tcpScan := newTCPScan(mac, ip)
	udpScan := newUDPScan(mac, ip)
	vulnScan := newVulnScan(mac, ip)

	metrics.knownHosts.Set(activeHosts.len())
	scheduleScan(tcpScan, 10*time.Minute, false)
	scheduleScan(udpScan, 20*time.Minute, false)
	scheduleScan(vulnScan, 0, false) // 0 means now
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

func subnetHostScan(ring, subnet string) int {
	seen := 0

	// Attempt to discover hosts:
	//   - TCP SYN and ACK probe to the listed ports.
	//   - UDP ping to the default port (40125).
	//   - SCTP INIT ping to the default port (80).
	args := []string{"-sn", "-PS22,53,3389,80,443",
		"-PA22,53,3389,80,443", "-PU", "-PY"}

	scanResults, err := nmapScan("subnetscan", subnet, args)
	if err != nil {
		slog.Warnf("Scan of %s ring failed: %v", ring, err)
		return 0
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
		if ip == "" || ip == network.SubnetRouter(subnet) {
			continue
		}

		seen++
		// Already being regularly scanned, no need to schedule it
		if activeHosts.contains(ip) {
			continue
		}

		if _, ok := clients[mac]; !ok {
			slog.Infof("Unknown host %s found on ring %s: %s",
				mac, ring, ip)
			logUnknown(ring, mac, ip)
		} else {
			slog.Infof("host %s now active on ring %s: %s",
				mac, ring, ip)
		}
		scannerRequest(mac, ip)
	}
	return seen
}

// hostScan scans each of interfaces for new hosts and schedules regular port
// scans on the host if one is found.
func hostScan() {
	slog.Debugf("starting subnet scan")
	start := time.Now()
	seen := 0
	for ring, config := range rings {
		seen += subnetHostScan(ring, config.Subnet)
	}
	done := time.Now()
	slog.Debugf("completed subnet scan")
	metrics.hostScans.Inc()
	metrics.hostScanTime.Observe(done.Sub(start).Seconds())
}

func runCmd(cmd string, args []string) error {
	var childProcess *os.Process

	child := aputil.NewChild(cmd, args...)

	if *nmapVerbose {
		child.UseZapLog("", slog, zapcore.DebugLevel)
	}

	runLock.Lock()
	err := child.Start()
	if err == nil {
		childProcess = child.Process
		scanProcesses[childProcess] = true
	}
	runLock.Unlock()

	if err != nil {
		err = fmt.Errorf("error starting %s: %v", cmd, err)
	} else {
		if err = child.Wait(); err != nil {
			err = fmt.Errorf("error running %s: %v", cmd, err)
		}

		runLock.Lock()
		delete(scanProcesses, childProcess)
		runLock.Unlock()
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
	if scanType == "tcp_ports" {
		dev.OpenTCP = ports
	} else if scanType == "udp_ports" {
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

// Check to see if a specific port scan is in progresss
func scanCheck(proto, ip string) bool {
	var rval bool

	scanLock.RLock()
	if proto == "tcp" {
		rval = tcpScans[ip]
	} else if proto == "udp" {
		rval = udpScans[ip]
	}
	scanLock.RUnlock()
	return rval
}

// Update whether a specific port scan is in progresss
func scanUpdate(scantype, ip string, set bool) {
	var m map[string]bool

	if scantype == "tcp_ports" {
		m = tcpScans
	} else if scantype == "udp_ports" {
		m = udpScans
	}
	if m != nil {
		scanLock.Lock()
		if set {
			m[ip] = true
		} else {
			delete(m, ip)
		}
		scanLock.Unlock()
	}
}

// portScan scans the ports of the given IP address using nmap, putting
// results on the message bus. Scans of IP are stopped if host is down.
func portScan(req *ScanRequest) {
	start := time.Now()
	scanUpdate(req.ScanType, req.IP, true)
	res, err := nmapScan("portscan", req.IP, req.Args)
	scanUpdate(req.ScanType, req.IP, false)
	done := time.Now()

	if err != nil {
		return
	}

	if req.ScanType == "tcp_ports" {
		metrics.tcpScans.Inc()
		metrics.tcpScanTime.Observe(done.Sub(start).Seconds())
	} else if req.ScanType == "udp_ports" {
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
					warn = (time.Since(*vi.WarnedAt) > vulnWarnFreq)
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

	prober := aputil.ExpandDirPath("/bin/ap-vuln-aggregate")
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

	os.Setenv("NMAPDIR", aputil.ExpandDirPath("/share/nmap"))

	// Make it possible for ap-vuln-aggregate to run ap-inspect without
	// hardcoding the path in the binary.
	os.Setenv("PATH", os.Getenv("PATH")+":"+aputil.ExpandDirPath("/bin"))
}

func scannerFini(w *watcher) {
	slog.Infof("Stopping active scans")

	kill := func(sig syscall.Signal) error {
		runLock.Lock()
		for r := range scanProcesses {
			r.Signal(sig)
		}
		runLock.Unlock()
		// Don't bother trying to figure out partial errors.
		return nil
	}
	alive := func() bool { return len(scanProcesses) > 0 }

	aputil.RetryKill(kill, alive)

	slog.Infof("Shutting down scanner")
	w.running = false
}

func scannerInit(w *watcher) {
	activeHosts = hostmapCreate()
	scansRunning = make(map[int]*ScanRequest)
	scansPending = make(scanQueue, 0)
	heap.Init(&scansPending)
	vulnInit()

	scanProcesses = make(map[*os.Process]bool)

	for i := 0; i < numScanners; i++ {
		go scanner()
	}

	mapMtx.Lock()
	for mac, ip := range macToIP {
		scannerRequest(mac, ip)
	}
	mapMtx.Unlock()

	schedule(hostScan, hostScanFreq, true)
	w.running = true
}

func init() {
	addWatcher("scanner", scannerInit, scannerFini)
}
