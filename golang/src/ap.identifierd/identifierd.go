/*
 * COPYRIGHT 2017 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

/*
Use observed data to make predictions about client identities.
To Do:
  1) Incorporate sampled data
  2) When an unknown manufacturer or unknown device is detected emit an event
     (maybe IdentifyException) which would trigger a more detailed scan and,
     subsequently, a telemetry report.
  3) Need for IdentifyRequest and Response?
  4) Make the proposed ap.namerd part of ap.identifierd.
*/
package main

import (
	"base_def"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"ap_common/apcfg"
	"ap_common/broker"
	"ap_common/mcp"
	"ap_common/model"
	"ap_common/network"
	"base_msg"

	"github.com/golang/protobuf/proto"
	"github.com/klauspost/oui"
)

var (
	dataDir  = flag.String("datadir", "./", "Directory containing data files")
	modelDir = flag.String("modeldir", "./", "Directory containing a saved model")
	logDir   = flag.String("logdir", "./", "Directory for device learning log data")

	brokerd *broker.Broker
	apcfgd  *apcfg.APConfig

	ouiDB   oui.DynamicDB
	mfgidDB = make(map[string]int)

	// DNS requests only contain the IP addr, so we maintin a map ipaddr -> hwaddr
	ipMtx sync.Mutex
	ipMap = make(map[uint32]uint64)

	testData = newObservations()
	newData  = newEntities()

	// See github.com/miekg/dns/types.go: func (q *Question) String() {}
	dnsQ = regexp.MustCompile(`;(.*?)\t`)
)

const (
	pname = "ap.identifierd"

	ouiFile   = "oui.txt"
	mfgidFile = "ap_mfgid.json"
	trainFile = "ap_identities.csv"
	saveFile  = "observations.csv"
)

func delHWaddr(hwaddr uint64) {
	ipMtx.Lock()
	defer ipMtx.Unlock()
	for ip, hw := range ipMap {
		if hw == hwaddr {
			delete(ipMap, ip)
			break
		}
	}
}

func getHWaddr(ip uint32) (uint64, bool) {
	ipMtx.Lock()
	defer ipMtx.Unlock()
	hwaddr, ok := ipMap[ip]
	return hwaddr, ok
}

func addIP(ip uint32, hwaddr uint64) {
	ipMtx.Lock()
	defer ipMtx.Unlock()
	ipMap[ip] = hwaddr
}

func removeIP(ip uint32) {
	ipMtx.Lock()
	defer ipMtx.Unlock()
	delete(ipMap, ip)
}

func handleEntity(event []byte) {
	entity := &base_msg.EventNetEntity{}
	proto.Unmarshal(event, entity)

	if entity.MacAddress == nil {
		return
	}

	hwaddr := network.Uint64ToHWAddr(*entity.MacAddress)
	entry, err := ouiDB.Query(hwaddr.String())
	if err != nil {
		log.Printf("MAC address %s not in OUI database\n", hwaddr)
		return
	}

	hostname := "Unknown"
	if entity.Hostname != nil {
		hostname = *entity.Hostname
	}
	newData.addIdentityHint(*entity.MacAddress, hostname)

	id := mfgidDB[entry.Manufacturer]
	testData.setByName(*entity.MacAddress, model.FormatMfgString(id))
}

func handleRequest(event []byte) {
	request := &base_msg.EventNetRequest{}
	proto.Unmarshal(event, request)

	if *request.Protocol != base_msg.Protocol_DNS {
		return
	}

	for _, q := range request.Request {
		qName := dnsQ.FindStringSubmatch(q)[1]

		// See record_client() in dns4d
		ip := net.ParseIP(*request.Requestor)
		if ip == nil {
			log.Printf("empty Requestor: %v\n", request)
			return
		}

		hwaddr, ok := getHWaddr(network.IPAddrToUint32(ip))
		if !ok {
			log.Println("unknown entity:", ip)
			return
		}

		newData.addAttr(hwaddr, qName)
		testData.setByName(hwaddr, qName)
	}
}

func configIPv4Changed(path []string, val string) {
	mac, err := net.ParseMAC(path[1])
	if err != nil {
		log.Printf("invalid MAC address %s", path[1])
		return
	}

	ipv4 := net.ParseIP(val)
	if ipv4 == nil {
		log.Printf("invalid IPv4 address %s", val)
		return
	}
	ipaddr := network.IPAddrToUint32(ipv4)
	addIP(ipaddr, network.HWAddrToUint64(mac))
}

func configIPv4Delexp(path []string) {
	mac, err := net.ParseMAC(path[1])
	if err != nil {
		log.Printf("invalid MAC address %s", path[1])
		return
	}

	delHWaddr(network.HWAddrToUint64(mac))
}

func handleScan(event []byte) {
	var mac string
	scan := &base_msg.EventNetScan{}
	proto.Unmarshal(event, scan)

	for _, h := range scan.Hosts {
		for _, a := range h.Addresses {
			if *a.Type == "mac" {
				mac = *a.Info
				break
			}
		}

		hwaddr, err := net.ParseMAC(mac)
		if err != nil {
			continue
		}

		for _, p := range h.Ports {
			if *p.State != "open" {
				continue
			}
			portString := model.FormatPortString(*p.Protocol, *p.PortId)
			newData.addAttr(network.HWAddrToUint64(hwaddr), portString)
			testData.setByName(network.HWAddrToUint64(hwaddr), portString)
		}
	}
}

func listenAttr(addr, kind string) {
	ip := net.ParseIP(addr)
	if ip == nil {
		return
	}

	hwaddr, ok := getHWaddr(network.IPAddrToUint32(ip))
	if !ok {
		return
	}

	newData.addAttr(hwaddr, kind)
	testData.setByName(hwaddr, kind)
}

func handleListen(event []byte) {
	listen := &base_msg.EventListen{}
	proto.Unmarshal(event, listen)

	switch *listen.Type {
	case base_msg.EventListen_SSDP:
		listenAttr(*listen.Ssdp.Address, "SSDP")
	case base_msg.EventListen_mDNS:
		listenAttr(*listen.Mdns.Address, "mDNS")
	}
}

func logger(stop chan bool, wg *sync.WaitGroup) {
	defer wg.Done()
	tick := time.NewTicker(5 * time.Minute)
	logFile := *logDir + saveFile

	w := func(path string) {
		if err := newData.writeCSV(logFile); err != nil {
			log.Println("Could not save observation data:", err)
		}
	}

	for {
		select {
		case <-tick.C:
			w(logFile)
		case <-stop:
			w(logFile)
			return
		}
	}
}

func updateClient(hwaddr uint64, devID string, confidence float32) {
	hw := network.Uint64ToHWAddr(hwaddr).String()
	identProp := "@/clients/" + hw + "/identity"
	confProp := "@/clients/" + hw + "/confidence"

	if err := apcfgd.CreateProp(identProp, devID, nil); err != nil {
		log.Printf("error creating prop %s: %s\n", identProp, err)
	}

	if err := apcfgd.CreateProp(confProp, fmt.Sprintf("%.2f", confidence), nil); err != nil {
		log.Printf("error creating prop %s: %s\n", confProp, err)
	}
}

func identify() {
	newIdentities := testData.predict()
	for {
		id := <-newIdentities
		devid, err := strconv.Atoi(id.devID)
		if err != nil || devid == 0 {
			log.Printf("returned a bogus identity for %v: %s",
				id.hwaddr, id.devID)
			continue
		}

		// XXX Set the bar low until the model gets more training data
		if id.probability > 0 {
			updateClient(id.hwaddr, id.devID, id.probability)
		}

		t := time.Now()
		identity := &base_msg.EventNetIdentity{
			Timestamp: &base_msg.Timestamp{
				Seconds: proto.Int64(t.Unix()),
				Nanos:   proto.Int32(int32(t.Nanosecond())),
			},
			Sender:     proto.String(brokerd.Name),
			Debug:      proto.String("-"),
			MacAddress: proto.Uint64(id.hwaddr),
			Devid:      proto.Int32(int32(devid)),
			Certainty:  proto.Float32(id.probability),
		}

		err = brokerd.Publish(identity, base_def.TOPIC_IDENTITY)
		if err != nil {
			log.Printf("couldn't publish %s: %v\n", base_def.TOPIC_IDENTITY, err)
		}
	}
}

func loadObservations() error {
	logFile := *logDir + saveFile
	f, err := os.Open(logFile)
	if err != nil {
		return err
	}
	defer f.Close()

	r := csv.NewReader(f)

	// Read attribute names
	header, err := r.Read()
	if err != nil {
		return fmt.Errorf("failed to read header from %s: %s", logFile, err)
	}

	header = header[1:]
	for {
		record, err := r.Read()
		if err == io.EOF {
			break
		} else if err != nil {
			return fmt.Errorf("failed to read saved observations from %s: %s", logFile, err)
		}

		mac, err := net.ParseMAC(record[0])
		if err != nil {
			return fmt.Errorf("invalid MAC addres %s: %s", record[0], err)
		}

		entry, err := ouiDB.Query(mac.String())
		if err != nil {
			return fmt.Errorf("MAC address %s not in OUI database: %s", mac, err)
		}

		id := mfgidDB[entry.Manufacturer]
		hwaddr := network.HWAddrToUint64(mac)
		testData.setByName(hwaddr, model.FormatMfgString(id))

		last := len(record) - 1
		newData.addIdentityHint(hwaddr, record[last])
		for i, feat := range record[1:last] {
			if feat == "0" {
				continue
			}
			testData.setByName(hwaddr, header[i])
			newData.addAttr(hwaddr, header[i])
		}

	}
	return nil
}

func recoverClients() {
	clients := apcfgd.GetClients()

	for macaddr, client := range clients {
		hwaddr, err := net.ParseMAC(macaddr)
		if err != nil {
			log.Printf("Invalid mac address in @/clients: %s\n", macaddr)
			continue
		}
		hw := network.HWAddrToUint64(hwaddr)

		if client.IPv4 != nil {
			addIP(network.IPAddrToUint32(client.IPv4), hw)
		}

		if client.DHCPName != "" {
			newData.addIdentityHint(hw, client.DHCPName)
		}
	}
}

func main() {
	var err error

	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	log.Println("start")

	flag.Parse()

	mcpd, err := mcp.New(pname)
	if err != nil {
		log.Printf("failed to connect to mcp\n")
	}

	if !strings.HasSuffix(*dataDir, "/") {
		*dataDir = *dataDir + "/"
	}

	if !strings.HasSuffix(*logDir, "/") {
		*logDir = *logDir + "/"
	}

	// OUI database
	ouiPath := *dataDir + ouiFile
	ouiDB, err = oui.OpenFile(ouiPath)
	if err != nil {
		log.Fatalf("failed to open OUI file %s: %s", ouiPath, err)
	}

	// Manufacturer database
	mfgidPath := *dataDir + mfgidFile
	file, err := ioutil.ReadFile(mfgidPath)
	if err != nil {
		log.Fatalf("failed to open manufacturer ID file %s: %v\n", mfgidPath, err)
	}

	err = json.Unmarshal(file, &mfgidDB)
	if err != nil {
		log.Fatalf("failed to import manufacturer IDs from %s: %v\n", mfgidPath, err)
	}

	if err = testData.loadModel(*dataDir+trainFile, *modelDir); err != nil {
		log.Fatalln("failed to load model", err)
	}

	if err := loadObservations(); err != nil {
		log.Println("failed to recover observations:", err)
	}

	// Use the broker to listen for appropriate messages to create and update
	// our observations.
	brokerd = broker.New(pname)
	defer brokerd.Fini()
	brokerd.Handle(base_def.TOPIC_ENTITY, handleEntity)
	brokerd.Handle(base_def.TOPIC_REQUEST, handleRequest)
	brokerd.Handle(base_def.TOPIC_SCAN, handleScan)
	brokerd.Handle(base_def.TOPIC_LISTEN, handleListen)

	apcfgd, err = apcfg.NewConfig(brokerd, pname)
	if err != nil {
		log.Fatalf("cannot connect to configd: %v\n", err)
	}

	recoverClients()

	apcfgd.HandleChange(`^@/clients/.*/ipv4$`, configIPv4Changed)
	apcfgd.HandleDelete(`^@/clients/.*/ipv4$`, configIPv4Delexp)
	apcfgd.HandleExpire(`^@/clients/.*/ipv4$`, configIPv4Delexp)

	if err := os.MkdirAll(*logDir, 0755); err != nil {
		log.Fatalln("failed to mkdir:", err)
	}

	if mcpd != nil {
		if err = mcpd.SetState(mcp.ONLINE); err != nil {
			log.Printf("failed to set status\n")
		}
	}

	stop := make(chan bool)
	wg := &sync.WaitGroup{}
	wg.Add(1)
	go logger(stop, wg)
	go identify()

	sig := make(chan os.Signal)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	// Tell the logger to stop, and wait for it to flush its output
	stop <- true
	wg.Wait()
}
