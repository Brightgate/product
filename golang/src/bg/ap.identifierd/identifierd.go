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
	"path/filepath"
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

	ouiFile     = "oui.txt"
	mfgidFile   = "ap_mfgid.json"
	trainFile   = "ap_identities.csv"
	testFile    = "test_data.csv"
	observeFile = "observations.pb"

	keepFor            = 2 * 24 * time.Hour
	logInterval        = 15 * time.Minute
	collectionDuration = 30 * time.Minute
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
	var id int
	msg := &base_msg.EventNetEntity{}
	proto.Unmarshal(event, msg)

	if msg.MacAddress == nil {
		return
	}
	hwaddr := *msg.MacAddress
	mac := network.Uint64ToHWAddr(hwaddr)
	entry, err := ouiDB.Query(mac.String())
	if err != nil {
		log.Printf("MAC address %s not in OUI database\n", mac)
		id = mfgidDB["Unknown"]
	} else {
		id = mfgidDB[entry.Manufacturer]
	}

	testData.setByName(hwaddr, model.FormatMfgString(id))
	newData.addMsgEntity(hwaddr, msg)
}

func handleRequest(event []byte) {
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

	for _, q := range request.Request {
		qName := model.DNSQ.FindStringSubmatch(q)[1]
		testData.setByName(hwaddr, qName)
	}

	newData.addMsgRequest(hwaddr, request)
}

func configDHCPChanged(path []string, val string) {
	mac, err := net.ParseMAC(path[1])
	if err != nil {
		log.Printf("invalid MAC address %s\n", path[1])
		return
	}

	hwaddr := network.HWAddrToUint64(mac)
	newData.addDHCPName(hwaddr, val)
}

func configIPv4Changed(path []string, val string) {
	mac, err := net.ParseMAC(path[1])
	if err != nil {
		log.Printf("invalid MAC address %s\n", path[1])
		return
	}

	ipv4 := net.ParseIP(val)
	if ipv4 == nil {
		log.Printf("invalid IPv4 address %s\n", val)
		return
	}
	ipaddr := network.IPAddrToUint32(ipv4)
	addIP(ipaddr, network.HWAddrToUint64(mac))
}

func configIPv4Delexp(path []string) {
	mac, err := net.ParseMAC(path[1])
	if err != nil {
		log.Printf("invalid MAC address %s\n", path[1])
		return
	}

	delHWaddr(network.HWAddrToUint64(mac))
}

func configPrivacyChanged(path []string, val string) {
	mac, err := net.ParseMAC(path[1])
	if err != nil {
		log.Printf("invalid MAC address %s: %s\n", path[1], err)
		return
	}

	private, err := strconv.ParseBool(val)
	if err != nil {
		log.Printf("invalid bool value %s: %s\n", val, err)
		return
	}

	newData.setPrivacy(mac, private)
}

func configPrivacyDelete(path []string) {
	mac, err := net.ParseMAC(path[1])
	if err != nil {
		log.Printf("invalid MAC address %s\n", path[1])
		return
	}

	newData.setPrivacy(mac, false)
}

func handleScan(event []byte) {
	scan := &base_msg.EventNetScan{}
	proto.Unmarshal(event, scan)

	hwaddr, ok := getHWaddr(*scan.Ipv4Address)
	if !ok {
		return
	}

	for _, h := range scan.Hosts {
		for _, p := range h.Ports {
			if *p.State != "open" {
				continue
			}
			portString := model.FormatPortString(*p.Protocol, *p.PortId)
			testData.setByName(hwaddr, portString)
		}
	}

	newData.addMsgScan(hwaddr, scan)
}

func handleListen(event []byte) {
	listen := &base_msg.EventListen{}
	proto.Unmarshal(event, listen)

	hwaddr, ok := getHWaddr(*listen.Ipv4Address)
	if !ok {
		return
	}

	switch *listen.Type {
	case base_msg.EventListen_SSDP:
		testData.setByName(hwaddr, "SSDP")
	case base_msg.EventListen_mDNS:
		testData.setByName(hwaddr, "mDNS")
	}

	newData.addMsgListen(hwaddr, listen)
}

func handleOptions(event []byte) {
	options := &base_msg.DHCPOptions{}
	proto.Unmarshal(event, options)
	newData.addMsgOptions(*options.MacAddress, options)
}

func save() {
	if err := newData.writeInventory(filepath.Join(*logDir, observeFile)); err != nil {
		log.Println("could not save observation data:", err)
	}

	if err := testData.saveTestData(filepath.Join(*dataDir, testFile)); err != nil {
		log.Println("could not save test data:", err)
	}
}

func clean() {
	walkFunc := func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return fmt.Errorf("error walking to %s: %v", path, err)
		}

		if info.IsDir() || !strings.HasPrefix(info.Name(), observeFile) {
			return nil
		}

		old := time.Now().Add(-keepFor)
		if info.ModTime().After(old) {
			return nil
		}

		if err := os.Remove(path); err != nil {
			return fmt.Errorf("error removing %s: %v", path, err)
		}

		return nil
	}

	if err := filepath.Walk(*logDir, walkFunc); err != nil {
		log.Printf("error walking %s: %v\n", *logDir, err)
	}
}

// logger periodically saves to disk both data for inference by the trained
// device ID model, and data observed from clients to be sent to the cloud.
// The observed data is kept for keepFor hours until it is removed by clean().
func logger(stop chan bool, wg *sync.WaitGroup) {
	defer wg.Done()
	tick := time.NewTicker(logInterval)

	for {
		select {
		case <-tick.C:
			save()
			clean()
		case <-stop:
			save()
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
			log.Printf("returned a bogus identity for %v: %s\n",
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

		newData.setPrivacy(hwaddr, client.DNSPrivate)
	}
}

func main() {
	var err error

	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	log.Println("start")

	flag.Parse()
	*dataDir = aputil.ExpandDirPath(*dataDir)
	*modelDir = aputil.ExpandDirPath(*modelDir)
	*logDir = aputil.ExpandDirPath(*logDir)

	mcpd, err := mcp.New(pname)
	if err != nil {
		log.Printf("failed to connect to mcp\n")
	}

	// OUI database
	ouiPath := filepath.Join(*dataDir, ouiFile)
	ouiDB, err = oui.OpenFile(ouiPath)
	if err != nil {
		log.Fatalf("failed to open OUI file %s: %s", ouiPath, err)
	}

	// Manufacturer database
	mfgidPath := filepath.Join(*dataDir, mfgidFile)
	file, err := ioutil.ReadFile(mfgidPath)
	if err != nil {
		log.Fatalf("failed to open manufacturer ID file %s: %v\n", mfgidPath, err)
	}

	err = json.Unmarshal(file, &mfgidDB)
	if err != nil {
		log.Fatalf("failed to import manufacturer IDs from %s: %v\n", mfgidPath, err)
	}

	if err = testData.loadModel(filepath.Join(*dataDir, trainFile), *modelDir); err != nil {
		log.Fatalln("failed to load model", err)
	}

	// Use the broker to listen for appropriate messages to create and update
	// our observations. To respect a client's privacy we won't register any
	// handlers until we have recovered each client's privacy configuration.
	brokerd = broker.New(pname)
	defer brokerd.Fini()

	apcfgd, err = apcfg.NewConfig(brokerd, pname)
	if err != nil {
		log.Fatalf("cannot connect to configd: %v\n", err)
	}

	recoverClients()

	if err := testData.loadTestData(filepath.Join(*dataDir, testFile)); err != nil {
		log.Println("failed to recover test data:", err)
	}

	brokerd.Handle(base_def.TOPIC_ENTITY, handleEntity)
	brokerd.Handle(base_def.TOPIC_REQUEST, handleRequest)
	brokerd.Handle(base_def.TOPIC_SCAN, handleScan)
	brokerd.Handle(base_def.TOPIC_LISTEN, handleListen)
	brokerd.Handle(base_def.TOPIC_OPTIONS, handleOptions)

	apcfgd.HandleChange(`^@/clients/.*/ipv4$`, configIPv4Changed)
	apcfgd.HandleChange(`^@/clients/.*/dhcp_name$`, configDHCPChanged)
	apcfgd.HandleDelete(`^@/clients/.*/ipv4$`, configIPv4Delexp)
	apcfgd.HandleExpire(`^@/clients/.*/ipv4$`, configIPv4Delexp)
	apcfgd.HandleChange(`^@/clients/.*/dns_private$`, configPrivacyChanged)
	apcfgd.HandleDelete(`^@/clients/.*/dns_private$`, configPrivacyDelete)

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
