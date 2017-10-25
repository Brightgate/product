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
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"bg/ap_common/apcfg"
	"bg/ap_common/aputil"
	"bg/ap_common/broker"
	"bg/ap_common/mcp"
	"bg/ap_common/model"
	"bg/ap_common/network"
	"bg/base_def"
	"bg/base_msg"

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
)

const (
	pname = "ap.identifierd"

	ouiFile   = "oui.txt"
	mfgidFile = "ap_mfgid.json"
	trainFile = "ap_identities.csv"
	saveFile  = "observations.pb"
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

func handleEntity(hwaddr uint64, msg *base_msg.EventNetEntity) {
	var id int
	mac := network.Uint64ToHWAddr(hwaddr)
	entry, err := ouiDB.Query(mac.String())
	if err != nil {
		log.Printf("MAC address %s not in OUI database\n", mac)
		id = mfgidDB["Unknown"]
	} else {
		id = mfgidDB[entry.Manufacturer]
	}

	testData.setByName(hwaddr, model.FormatMfgString(id))
	newData.addTimeout(hwaddr)
	newData.addMsgEntity(hwaddr, msg)
}

func handleEntityRaw(event []byte) {
	msg := &base_msg.EventNetEntity{}
	proto.Unmarshal(event, msg)
	if msg.MacAddress == nil {
		return
	}
	handleEntity(*msg.MacAddress, msg)
}

func handleRequest(hwaddr uint64, msg *base_msg.EventNetRequest) {
	var force bool
	for _, q := range msg.Request {
		qName := model.DNSQ.FindStringSubmatch(q)[1]
		force = force || testData.setByName(hwaddr, qName)
	}
	newData.addMsgRequest(hwaddr, msg, force)
}

func handleRequestRaw(event []byte) {
	request := &base_msg.EventNetRequest{}
	proto.Unmarshal(event, request)

	if *request.Protocol != base_msg.Protocol_DNS {
		return
	}

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
	handleRequest(hwaddr, request)
}

func configDHCPChanged(path []string, val string) {
	mac, err := net.ParseMAC(path[1])
	if err != nil {
		log.Printf("invalid MAC address %s", path[1])
		return
	}

	hwaddr := network.HWAddrToUint64(mac)
	newData.addDHCPName(hwaddr, val)
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

func handleScan(hwaddr uint64, scan *base_msg.EventNetScan) {
	var force bool
	for _, h := range scan.Hosts {
		for _, p := range h.Ports {
			if *p.State != "open" {
				continue
			}
			portString := model.FormatPortString(*p.Protocol, *p.PortId)
			force = force || testData.setByName(hwaddr, portString)
		}
	}
	newData.addMsgScan(hwaddr, scan, force)
}

func handleScanRaw(event []byte) {
	scan := &base_msg.EventNetScan{}
	proto.Unmarshal(event, scan)

	hwaddr, ok := getHWaddr(*scan.Ipv4Address)
	if !ok {
		return
	}
	handleScan(hwaddr, scan)
}

func handleListen(hwaddr uint64, listen *base_msg.EventListen) {
	var force bool
	switch *listen.Type {
	case base_msg.EventListen_SSDP:
		force = testData.setByName(hwaddr, "SSDP")
	case base_msg.EventListen_mDNS:
		force = testData.setByName(hwaddr, "mDNS")
	}

	newData.addMsgListen(hwaddr, listen, force)
}

func handleListenRaw(event []byte) {
	listen := &base_msg.EventListen{}
	proto.Unmarshal(event, listen)

	hwaddr, ok := getHWaddr(*listen.Ipv4Address)
	if !ok {
		return
	}
	handleListen(hwaddr, listen)
}

func handleOptions(hwaddr uint64, options *base_msg.DHCPOptions) {
	newData.addMsgOptions(hwaddr, options)
}

func handleOptionsRaw(event []byte) {
	options := &base_msg.DHCPOptions{}
	proto.Unmarshal(event, options)
	handleOptions(*options.MacAddress, options)
}

func logger(stop chan bool, wg *sync.WaitGroup) {
	defer wg.Done()
	tick := time.NewTicker(5 * time.Minute)
	logFile := *logDir + saveFile

	w := func(path string) {
		if err := newData.writeInventory(logFile); err != nil {
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

		identity := &base_msg.EventNetIdentity{
			Timestamp:  aputil.NowToProtobuf(),
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
	in, err := ioutil.ReadFile(logFile)
	if err != nil {
		return err
	}
	inventory := &base_msg.DeviceInventory{}
	proto.Unmarshal(in, inventory)

	for _, devInfo := range inventory.Devices {
		hwaddr := *devInfo.MacAddress
		if devInfo.Entity != nil {
			handleEntity(hwaddr, devInfo.Entity)
		}

		if devInfo.Scan != nil {
			for _, msg := range devInfo.Scan {
				handleScan(hwaddr, msg)
			}
		}

		if devInfo.Request != nil {
			for _, msg := range devInfo.Request {
				handleRequest(hwaddr, msg)
			}
		}

		if devInfo.Listen != nil {
			for _, msg := range devInfo.Listen {
				handleListen(hwaddr, msg)
			}
		}

		if devInfo.Options != nil {
			for _, msg := range devInfo.Options {
				handleOptions(hwaddr, msg)
			}
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
			newData.addDHCPName(hw, client.DHCPName)
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
	brokerd.Handle(base_def.TOPIC_ENTITY, handleEntityRaw)
	brokerd.Handle(base_def.TOPIC_REQUEST, handleRequestRaw)
	brokerd.Handle(base_def.TOPIC_SCAN, handleScanRaw)
	brokerd.Handle(base_def.TOPIC_LISTEN, handleListenRaw)
	brokerd.Handle(base_def.TOPIC_OPTIONS, handleOptionsRaw)

	apcfgd, err = apcfg.NewConfig(brokerd, pname)
	if err != nil {
		log.Fatalf("cannot connect to configd: %v\n", err)
	}

	recoverClients()

	apcfgd.HandleChange(`^@/clients/.*/ipv4$`, configIPv4Changed)
	apcfgd.HandleChange(`^@/clients/.*/dhcp_name$`, configDHCPChanged)
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
