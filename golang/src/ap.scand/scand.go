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
	"net"
	"os"
	"os/exec"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"ap_common"
	"base_def"
	"base_msg"

	"github.com/golang/protobuf/proto"
	nmap "github.com/ktscholl/go-nmap"
)

var (
	broker      ap_common.Broker
	activeHosts map[string]struct{}
	scanQueue   chan ScanRequest
	quit        chan string
	nmapDir     = flag.String("scandir", ".",
		"directory in which the nmap scan files should be stored")
	// default value?
)

const (
	numScanners  = 10
	hostScanFreq = 5 * time.Minute
	cleanFreq    = 10 * time.Minute
	maxFiles     = 10
	hostLifetime = 1 * time.Hour
	defaultFreq  = 2 * time.Minute
	udpFreq      = 10 * time.Minute
	bufferSize   = 100
)

// ScanRequest is used to send tasks to scanners
type ScanRequest struct {
	IP   string
	Args string
	File string
}

// Event contains information from scan to be sent via message bus.
type Event struct {
	// host info
	Addresses []nmap.Address
	Hostnames []nmap.Hostname
	Endtime   nmap.Timestamp
	Status    nmap.Status
	// port info
	PortID        int
	Protocol      string
	State         nmap.State
	ServiceName   string
	ServiceMethod string
	Confidence    int
	DeviceType    string
	Product       string
	ExtraInfo     string
	// extra port info (closed|filtered)
	EPState  string
	EPCount  int
	EPReason string
}

// ByDateModified is for sorting files by date modified
type ByDateModified []string

func (s ByDateModified) Len() int {
	return len(s)
}

func (s ByDateModified) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

func (s ByDateModified) Less(i, j int) bool {
	iFile, err := os.Stat(s[i])
	if err != nil {
		panic(err)
	}
	jFile, err := os.Stat(s[j])
	if err != nil {
		panic(err)
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
		log.Printf("Scanner %d starting %+v", id, s)
		portScan(s.IP, s.Args, s.File)
		log.Printf("Scanner %d finished %+v", id, s)
	}
}

// getIP gets the IP address of the created network. From arpspoof.go
func getIP() (ipaddr net.IP) {
	iface, err := net.InterfaceByName("wlan0")
	if err != nil {
		log.Fatalf("Unable to use interface wlan0")
	}
	ifaceAddrs, err := iface.Addrs()
	if err != nil {
		log.Fatalln("Unable to get interface unicast addresses:", err)
	}
	for _, addr := range ifaceAddrs {
		if ipnet, ok := addr.(*net.IPNet); ok {
			if ip4 := ipnet.IP.To4(); ip4 != nil {
				ipaddr = ip4
				break
			}
		}
	}
	if ipaddr == nil {
		log.Fatalln("Could not get interface address.")
	}
	return
}

// hostScan scans the network for new hosts and schedules regular port scans
// on the host if one is found.
func hostScan() {
	log.Println("Starting host scan")

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
	netIP := "192.168.2.35/24"
	// netIP := getIP().String() + "/24"
	// ^^ would scan our network, but right now that's not too interesting
	file := fmt.Sprintf("%snetscans/netscan-%d.xml", *nmapDir,
		int(time.Now().Unix()))
	args := "-sn -PS22,53,3389,80,443 -PA22,53,3389,80,443 -PU -PY"
	scanResults, err := scan(netIP, args, file, false)
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
		if host.Status.State == "up" && ip != "" {
			if _, ok := activeHosts[ip]; !ok {
				if !contains(knownHosts, ip) {
					// XXX message bus, net.entity or something similar
					log.Printf("Unknown host discovered: %s", ip)
					if err := os.MkdirAll(*nmapDir+ip, 0755); err != nil {
						log.Printf("Error adding directory %s: %v\n", ip, err)
						return
					}
				} else {
					log.Printf("%s is back online, restarting scans", ip)
				}
				// XXXX eventually, set scans and scan frequencies based on
				// type of device detected
				schedulePortScan(ScanRequest{ip, "-v -sV -T4", "default"}, defaultFreq)
				schedulePortScan(ScanRequest{ip, "-sU -v -sV -T4", "udp"}, udpFreq)
				activeHosts[ip] = struct{}{}
			}
			if _, err := os.Create(*nmapDir + ip + "/.keep"); err != nil {
				log.Printf("Error adding .keep file %s: %v\n", ip, err)
				return
			}
		}
	}
	log.Println("Finished host scan")
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

// start gives informations about the start conditions of a port scan from
// its NmapRun struct.
func start(s *nmap.NmapRun) string {
	return fmt.Sprintf("Nmap %s scan initiated %s as: %s", s.Version,
		s.StartStr, s.Args)
}

// toEvents separates a NmapRun struct into a slice of Events
func toEvents(s *nmap.NmapRun) (events []Event) {
	for _, host := range s.Hosts {
		if host.Status.State == "down" {
			e := *new(Event)
			e.Addresses = host.Addresses
			e.Hostnames = host.Hostnames
			e.Endtime = s.RunStats.Finished.Time
			e.Status = host.Status
			events = append(events, e)
		}
		for _, port := range host.Ports {
			e := *new(Event)
			e.Addresses = host.Addresses
			e.Hostnames = host.Hostnames
			e.Endtime = host.EndTime
			e.Status = host.Status
			e.PortID = port.PortId
			e.Protocol = port.Protocol
			e.State = port.State
			e.ServiceName = port.Service.Name
			e.Confidence = port.Service.Conf
			e.ServiceMethod = port.Service.Method
			e.DeviceType = port.Service.DeviceType
			e.Product = port.Service.Product
			e.ExtraInfo = port.Service.ExtraInfo
			events = append(events, e)
		}
		for _, extraports := range host.ExtraPorts {
			for _, reason := range extraports.Reasons {
				e := *new(Event)
				e.Addresses = host.Addresses
				e.Hostnames = host.Hostnames
				e.Endtime = host.EndTime
				e.Status = host.Status
				e.EPState = extraports.State
				e.EPReason = reason.Reason
				e.EPCount = reason.Count
				events = append(events, e)
			}
		}
	}
	return
}

// toScanPort takes an Event and returns a ScanPort. Used for message bus.
func (e Event) toScanPort() base_msg.ScanPort {
	addresses := make([]string, 0)
	addrTypes := make([]string, 0)
	hostnames := make([]string, 0)
	hostnameTypes := make([]string, 0)

	for _, addr := range e.Addresses {
		addresses = append(addresses, addr.Addr)
		addrTypes = append(addrTypes, addr.AddrType)
	}

	for _, hostname := range e.Hostnames {
		hostnames = append(hostnames, hostname.Name)
		hostnameTypes = append(hostnameTypes, hostname.Type)
	}
	t := time.Time(e.Endtime)

	scan := base_msg.ScanPort{
		// host information
		Address:      addresses,
		AddrType:     addrTypes,
		Hostname:     hostnames,
		HostnameType: hostnameTypes,
		ScanTime: &base_msg.Timestamp{
			Seconds: proto.Int64(t.Unix()),
			Nanos:   proto.Int32(int32(t.Nanosecond())),
		},
		// port information
		Status:        proto.String(e.Status.State),
		StatusReason:  proto.String(e.Status.Reason),
		PortId:        proto.Int(e.PortID),
		Protocol:      proto.String(e.Protocol),
		State:         proto.String(e.State.State),
		StateReason:   proto.String(e.State.Reason),
		ServiceName:   proto.String(e.ServiceName),
		ServiceMethod: proto.String(e.ServiceMethod),
		Confidence:    proto.Int(e.Confidence),
		DeviceType:    proto.String(e.DeviceType),
		Product:       proto.String(e.Product),
		ExtraInfo:     proto.String(e.ExtraInfo),
		// extra port info (closed|filtered)
		ExtraPortState:  proto.String(e.EPState),
		ExtraPortCount:  proto.Int(e.EPCount),
		ExtraPortReason: proto.String(e.EPReason),
	}
	return scan
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
	portScans := make([]*base_msg.ScanPort, 0)
	for _, event := range toEvents(scanResults) {
		sp := event.toScanPort()
		portScans = append(portScans, &sp)
	}
	t := time.Now()

	scan := &base_msg.EventNetScan{
		Timestamp: &base_msg.Timestamp{
			Seconds: proto.Int64(t.Unix()),
			Nanos:   proto.Int32(int32(t.Nanosecond())),
		},
		Sender:       proto.String(fmt.Sprintf("ap.dns4d(%d)", os.Getpid())),
		Debug:        proto.String("-"),
		ScanLocation: proto.String(file),
		StartInfo:    proto.String(start(scanResults)),
		PortScan:     portScans,
		Summary:      proto.String(scanResults.RunStats.Finished.Summary),
	}

	err = broker.Publish(scan, base_def.TOPIC_SCAN)
	if err != nil {
		log.Printf("Error sending scan: %v\n", err)
	}
}

// cleanAll deletes all but the most recent maxFiles files of any one scan type,
// and also deletes files older than hostLifetime. If a directory is empty, it
// is deleted.
func cleanAll() {
	log.Println("Beginning cleaning")

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
					// is a file and too old
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
						// are the inner slices initialized?
					}
				}
			}
		}
		for name, paths := range names {
			if name != "" {
				defer func() {
					if r := recover(); r != nil {
						log.Println("Error opening files", r)
						return
					}
				}()
				sort.Sort(ByDateModified(paths))
				// check for panic somehow?
				if len(paths) > maxFiles {
					toDelete := paths[maxFiles:]        // might be off by one
					log.Println("To delete:", toDelete) // check types
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

			// just in case
			delete(activeHosts, host)
			quit <- host

			if err := os.RemoveAll(*nmapDir + host); err != nil {
				log.Printf("Error removing directory %s%s: %v\n",
					*nmapDir, host, err)
			}
		}
	}
	log.Println("Done cleaning")
}

// echo prints a message upon recieving an EventNetScan. Can also echo back
// content sent by uncommenting line.
func echo(event []byte) {
	scan := &base_msg.EventNetScan{}
	proto.Unmarshal(event, scan)
	log.Println("New scan properly sent")
	//log.Println(scan)
}

// String returns the Event in string format. Primarily for debugging.
func String(e Event) (str string) {
	str += fmt.Sprint(time.Time(e.Endtime))
	str += " Address(es): "
	for _, addr := range e.Addresses {
		str += (addr.Addr + " (" + addr.AddrType)
		if addr.Vendor != "" {
			str += (", " + addr.Vendor)
		}
		str += ") "
	}
	if e.Hostnames != nil {
		str += "Hostname(s): "
		for _, hostname := range e.Hostnames {
			str += hostname.Name + " (" + hostname.Type + ") "
		}
	}
	str += fmt.Sprintf("Status: %s (%s)", e.Status.State, e.Status.Reason)
	if e.Status.State != "up" {
		return
	}
	if e.EPCount != 0 {
		str += fmt.Sprintf(" %d Port(s) State: %s (%s)", e.EPCount,
			e.EPState, e.EPReason)
		return
	}
	if e.PortID != 0 {
		str += fmt.Sprintf(" Port %d (%s) State: %s (%s) Service: %s "+
			"(%s, Confidence: %d)", e.PortID, e.Protocol, e.State.State,
			e.State.Reason, e.ServiceName, e.ServiceMethod, e.Confidence)
		if e.DeviceType != "" {
			str += fmt.Sprintf(" Device Type: %s", e.DeviceType)
		}
		if e.Product != "" {
			str += fmt.Sprintf(" Product: %s", e.Product)
		}
		if e.ExtraInfo != "" {
			str += fmt.Sprintf(" Extra Info: %s", e.ExtraInfo)
		}
	}
	return
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
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	flag.Parse()
	if !strings.HasSuffix(*nmapDir, "/") {
		*nmapDir = *nmapDir + "/"
	}
	if _, err := os.Stat(*nmapDir); os.IsNotExist(err) {
		log.Fatalf("Scan directory %s doesn't exist", *nmapDir)
	}

	broker.Init("ap.scand")
	broker.Handle(base_def.TOPIC_SCAN, echo)
	broker.Connect()
	defer broker.Disconnect()

	time.Sleep(time.Second)
	broker.Ping()

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
	log.Fatalf("Signal (%v) received, stopping all scans\n", s)
	// XXX handle incomplete xml files
}
