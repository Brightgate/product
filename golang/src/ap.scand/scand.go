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
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"ap_common/apcfg"
	"ap_common/broker"
	"ap_common/mcp"
	"ap_common/network"
	"base_def"
	"base_msg"

	"github.com/golang/protobuf/proto"
	nmap "github.com/ktscholl/go-nmap"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	activeHosts map[string]struct{}
	brokerd     broker.Broker
	config      *apcfg.APConfig
	scanQueue   chan ScanRequest
	quit        chan string

	nmapDir = flag.String("scandir", ".",
		"directory in which the nmap scan files should be stored")
	addr = flag.String("prom_address", base_def.SCAND_PROMETHEUS_PORT,
		"The address to listen on for Prometheus HTTP requests.")

	// prometheus metrics
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
	knownHostsGauge = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "known_hosts",
			Help: "Number of recognized hosts.",
		})
	scanDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "scan_duration",
			Help: "Scan duration in seconds, by IP and scan type.",
		},
		[]string{"ip", "type"})
	scannersFinished = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "scanners_finished",
			Help: "Number of scans finished, by IP and scan type.",
		},
		[]string{"ip", "type"})
	scannersStarted = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "scanners_started",
			Help: "Number of scans started, by IP and scan type.",
		},
		[]string{"ip", "type"})
)

const (
	bufferSize   = 100
	cleanFreq    = 10 * time.Minute
	defaultFreq  = 2 * time.Minute
	hostLifetime = 1 * time.Hour
	hostScanFreq = 5 * time.Minute
	maxFiles     = 10
	numScanners  = 10
	pname        = "ap.scand"
	udpFreq      = 30 * time.Minute
	// default scan takes 70sec on average
	// udp scan takes 1000sec on average
)

// ScanRequest is used to send tasks to scanners
type ScanRequest struct {
	IP   string
	Args string
	File string
}

// ByDateModified is for sorting files by date modified.
type ByDateModified []string

func (s ByDateModified) Len() int {
	return len(s)
}

func (s ByDateModified) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

// Nonexistent files are treated as if they were older.
func (s ByDateModified) Less(i, j int) bool {
	iFile, err := os.Stat(s[i])
	if err != nil {
		return false
	}
	jFile, err := os.Stat(s[j])
	if err != nil {
		return true
	}
	return iFile.ModTime().After(jFile.ModTime())
}

// schedule runs toRun with a frequency determined by freq.
func schedule(toRun func(), freq time.Duration, startNow bool) {
	if startNow {
		toRun()
	}
	ticker := time.NewTicker(freq)
	go func() {
		for {
			<-ticker.C
			toRun()
		}
	}()
}

// schedulePortScan sends the given request to scanners through the scanQueue
// at a frequency determined by freq. Requests are no longer sent if the
// request's IP is sent to quit.
func schedulePortScan(request ScanRequest, freq time.Duration) {
	scanQueue <- request
	ticker := time.NewTicker(freq)
	go func() {
		for {
			select {
			case <-ticker.C:
				scanQueue <- request
			case ip := <-quit:
				if ip == request.IP {
					ticker.Stop()
				}
			}
		}
	}()
}

// scanner performs ScanRequests as they come in through the scanQueue.
func scanner(id int) {
	for s := range scanQueue {
		t := time.Now()
		scannersStarted.WithLabelValues(s.IP, s.File).Inc()
		portScan(s.IP, s.Args, s.File)
		scanDuration.WithLabelValues(s.IP, s.File).Observe(
			time.Since(t).Seconds())
		scannersFinished.WithLabelValues(s.IP, s.File).Inc()
	}
}

// hostScan scans the network for new hosts and schedules regular port scans
// on the host if one is found.
func hostScan() {
	defer hostScanCount.Inc()
	files, err := ioutil.ReadDir(*nmapDir)
	if err != nil {
		log.Printf("Error checking known hosts: %v\n", err)
		return
	}
	// integrate hosts recognized from other daemons
	var knownHosts []string
	var numKnownHosts float64
	for _, file := range files {
		if file.IsDir() {
			knownHosts = append(knownHosts, file.Name())
			numKnownHosts++
		}
	} // note that this includes netscans
	knownHostsGauge.Set(numKnownHosts - 1)
	ipMap := config.GetSubnets()
	var numHostsUp float64
	for iface, subnetIP := range ipMap {
		// iface, subnetIP := "test", "192.168.2.1/24" <-- for testing on BUR01
		file := fmt.Sprintf("%snetscans/netscan-%d.xml", *nmapDir,
			int(time.Now().Unix()))
		args := "-sn -PS22,53,3389,80,443 -PA22,53,3389,80,443 -PU -PY"
		scanResults, err := scan(subnetIP, args, file, false)
		if err != nil {
			// error already printed in scan
			return
		}
		for _, host := range scanResults.Hosts {
			var ip string
			for _, addr := range host.Addresses {
				if addr.AddrType == "ipv4" {
					ip = addr.Addr
					break
				}
			}
			if host.Status.State == "up" && ip != "" &&
				ip != network.SubnetRouter(subnetIP) {
				numHostsUp++
				if _, ok := activeHosts[ip]; !ok {
					if !contains(knownHosts, ip) {
						// XXX message bus, net.entity or something similar
						log.Printf(
							"Unknown host discovered on %s: %s", iface, ip)
						if err := os.MkdirAll(*nmapDir+ip, 0755); err != nil {
							log.Printf(
								"Error adding directory %s: %v\n", ip, err)
							return
						}
					} else {
						log.Printf("%s is back online on %s, restarting scans",
							ip, iface)
					}
					// XXXX eventually, set scans and scan frequencies based on
					// type of device detected
					schedulePortScan(ScanRequest{ip, "-v -sV -O -T4", "default"},
						defaultFreq)
					schedulePortScan(ScanRequest{ip, "-sU -v -O -sV -T4", "udp"},
						udpFreq)
					activeHosts[ip] = struct{}{}
				}
				if _, err := os.Create(*nmapDir + ip + "/.keep"); err != nil {
					log.Printf("Error adding .keep file %s: %v\n", ip, err)
					return
				}
			}
		}
	}
	hostsUp.Set(numHostsUp)
}

// scan uses nmap to scan ip with the given arguments, outputting its results
// to the given file and parsing its contents into an NmapRun struct.
// If verbose is true, output of nmap is printed to log, otherwise it is ignored.
func scan(ip string, nmapArgs string, file string, verbose bool) (*nmap.NmapRun, error) {
	args := "nmap " + ip + " " + nmapArgs + " -oX " + file
	cmd := exec.Command("bash", "-c", args)
	if verbose {
		cmdReader, err := cmd.StdoutPipe()
		if err != nil {
			log.Printf("Error creating StdoutPipe for nmap: %v\n", err)
			return nil, err
		}
		scanner := bufio.NewScanner(cmdReader)
		go func() {
			for scanner.Scan() {
				log.Printf("%s\n", scanner.Text())
			}
		}()
		err = cmd.Start()
		if err != nil {
			log.Printf("Error starting nmap: %v\n", err)
			return nil, err
		}
		err = cmd.Wait()
		if err != nil {
			log.Printf("Error waiting for nmap: %v\n", err)
			return nil, err
		}
	} else {
		if err := cmd.Run(); err != nil {
			log.Printf("Error running nmap: %v\n", err)
			return nil, err
		}
	}
	fileContent, err := ioutil.ReadFile(file)
	if err != nil {
		log.Printf("Error reading file: %v\n", err)
		return nil, err
	}
	scanResults, err := nmap.Parse(fileContent)
	if err != nil {
		log.Printf("Error parsing file: %v\n", err)
		return nil, err
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
func portScan(ip string, nmapArgs string, filename string) {
	file := fmt.Sprintf("%s%s/%s-%d.xml", *nmapDir, ip, filename,
		int(time.Now().Unix()))
	scanResults, err := scan(ip, nmapArgs, file, false)
	if err != nil {
		return
	}
	if len(scanResults.Hosts) == 1 && scanResults.Hosts[0].Status.State != "up" {
		log.Printf("Host %s is down, stopping scans", ip)

		delete(activeHosts, ip)
		quit <- ip
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

// cleanAll deletes all but the most recent maxFiles files of any one scan type,
// and also deletes files older than hostLifetime. If a directory is empty, it
// is deleted.
func cleanAll() {
	defer cleanScanCount.Inc()
	files, err := ioutil.ReadDir(*nmapDir)
	if err != nil {
		log.Printf("Error checking known hosts: %v\n", err)
		return
	}
	var knownHosts []string
	for _, file := range files {
		if file.IsDir() {
			knownHosts = append(knownHosts, file.Name())
		}
	} // note that this includes netscans
	for _, host := range knownHosts {
		names := make(map[string][]string)

		out, err := ioutil.ReadDir(*nmapDir + host)
		for _, file := range out {
			path := *nmapDir + host + "/" + file.Name()
			if !file.IsDir() {
				if time.Now().Sub(file.ModTime()) > hostLifetime {
					log.Println("Deleting " + path)
					if err := os.Remove(path); err != nil {
						log.Printf("Error removing %s: %v\n",
							path, err)
						return
					}
				} else {
					fileName := file.Name()
					i := strings.Index(fileName, "-")
					if i > -1 {
						cutName := fileName[:i]
						names[cutName] = append(names[cutName], path)
					}
				}
			}
		}
		for name, paths := range names {
			if name != "" {
				sort.Sort(ByDateModified(paths))
				if len(paths) > maxFiles {
					toDelete := paths[maxFiles:]
					log.Println("Deleting ", toDelete)
					for _, path := range toDelete {
						if err := os.Remove(path); err != nil {
							log.Printf("Error removing %s: %v\n",
								path, err)
							return
						}
					}
				}
			}
		}

		dir, err := os.Open(*nmapDir + host)
		if err != nil {
			log.Printf("Error checking contents of %s: %v\n", host, err)
			return
		}
		_, err = dir.Readdirnames(1)
		dir.Close()
		if err == io.EOF && host != "netscans" {
			log.Printf("No recent scans for %s, forgetting host", host)

			delete(activeHosts, host)
			quit <- host

			if err := os.RemoveAll(*nmapDir + host); err != nil {
				log.Printf("Error removing directory %s%s: %v\n",
					*nmapDir, host, err)
			}
		}
	}
}

// echo echos back a recieved EventNetScan.
// Add brokerd.Handle(base_def.TOPIC_SCAN, echo) in main to run.
func echo(event []byte) {
	scan := &base_msg.EventNetScan{}
	proto.Unmarshal(event, scan)
	log.Println(scan)
}

func contains(s []string, e string) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
}

func main() {
	syscall.Setpgid(0, 0)
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	flag.Parse()

	mcp, err := mcp.New(pname)
	if err != nil {
		log.Printf("failed to connect to mcp\n")
	}

	if !strings.HasSuffix(*nmapDir, "/") {
		*nmapDir = *nmapDir + "/"
	}
	if err := os.MkdirAll(*nmapDir+"/netscans", 0755); err != nil {
		log.Fatalln("failed to make dir %s:", *nmapDir, err)
	}

	prometheus.MustRegister(cleanScanCount, scannersStarted, scannersFinished,
		hostScanCount, hostsUp, knownHostsGauge, scanDuration)

	http.Handle("/metrics", promhttp.Handler())
	go http.ListenAndServe(*addr, nil)

	config = apcfg.NewConfig(pname)
	brokerd.Init(pname)
	brokerd.Connect()
	defer brokerd.Disconnect()
	brokerd.Ping()
	if mcp != nil {
		if err = mcp.SetStatus("online"); err != nil {
			log.Printf("failed to set status\n")
		}
	}

	sig := make(chan os.Signal)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	scanQueue = make(chan ScanRequest, bufferSize)
	quit = make(chan string, bufferSize)
	activeHosts = make(map[string]struct{})

	for i := 0; i < numScanners; i++ {
		go scanner(i)
	}

	schedule(hostScan, hostScanFreq, true)
	schedule(cleanAll, cleanFreq, false)

	s := <-sig
	log.Printf("Signal (%v) received, stopping all scans\n", s)
	syscall.Kill(-syscall.Getpid(), syscall.SIGKILL) // kill potential orphans
}
