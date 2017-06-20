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
  0) Need to read config dhcp leases to build initial membership
  1) Incorporate sampled data
  2) Incorporate scand data
  3) Improve training data
       - (Stephen) I think part of #3 is an event (maybe IdentifyException) when
         an unknown manufacturer or unknown device is detected. Receipt of #3
         would trigger a more detailed scan and, subsequently, a telemetry report.
  4) Create zmq REQ-REP for sending IdentifyRequest and IdentifyResponse
  5) Tie IdentifyRequest and Response into dhcp and config
  6) Make the proposed ap.namerd part of ap.identifierd.
*/
package main

import (
	"base_def"
	"encoding/json"
	"flag"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"ap_common"
	"ap_common/mcp"
	"ap_common/network"
	"base_msg"

	"ap.identifierd/model"

	"github.com/golang/protobuf/proto"
	"github.com/klauspost/oui"
	"github.com/sjwhitworth/golearn/base"
	"github.com/sjwhitworth/golearn/naive"
	"github.com/sjwhitworth/golearn/trees"
)

var (
	dataDir = flag.String("datadir", "./", "Directory containing data files")

	broker ap_common.Broker

	ouiDB   oui.DynamicDB
	mfgidDB = make(map[string]int)

	// Prospective models
	naiveBayes *naive.BernoulliNBClassifier
	id3Tree    *trees.ID3DecisionTree

	trainData *base.DenseInstances
	testData  = model.NewObservations()

	// See github.com/miekg/dns/types.go: func (q *Question) String() {}
	dnsQ = regexp.MustCompile(`;(.*?)\t`)
)

const pname = "ap.identifierd"

const (
	ouiFile   = "oui.txt"
	mfgidFile = "ap_mfgid.json"
	trainFile = "ap_identities.csv"
)

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

	id, ok := mfgidDB[entry.Manufacturer]
	if !ok {
		log.Printf("%s is a %s product: MID = SETME\n", hwaddr, entry.Manufacturer)
		return
	}

	testData.SetByName(*entity.MacAddress, "Manufacturer ID", strconv.Itoa(id))
}

func handleRequest(event []byte) {
	request := &base_msg.EventNetRequest{}
	proto.Unmarshal(event, request)

	if *request.Protocol != base_msg.Protocol_DNS {
		return
	}

	for _, q := range request.Request {
		qName := dnsQ.FindStringSubmatch(q)[1]
		ip := net.ParseIP(*request.Requestor)
		hwaddr, ok := testData.GetHWaddr(network.IPAddrToUint32(ip))
		if !ok {
			log.Println("unknown entity:", ip)
			return
		}

		log.Printf("%s %s: %s\n", network.Uint64ToHWAddr(hwaddr), *request.Requestor, qName)
		testData.SetByName(hwaddr, qName, "1")
	}
}

func handleConfig(event []byte) {
	eventConfig := &base_msg.EventConfig{}
	proto.Unmarshal(event, eventConfig)
	property := *eventConfig.Property
	path := strings.Split(property[2:], "/")

	// Ignore all properties other than "@/dhcp/leases/*"
	if len(path) != 3 || path[0] != "dhcp" || path[1] != "leases" {
		return
	}

	ipv4 := net.ParseIP(path[2])
	if ipv4 == nil {
		log.Printf("invalid IPv4 address %s", path[2])
		return
	}
	ipaddr := network.IPAddrToUint32(ipv4)

	mac, err := net.ParseMAC(*eventConfig.NewValue)
	if err != nil {
		log.Printf("invalid MAC address %s", *eventConfig.NewValue)
		return
	}
	hwaddr := network.HWAddrToUint64(mac)

	if *eventConfig.Type == base_msg.EventConfig_CHANGE {
		testData.AddIP(ipaddr, hwaddr)
	} else {
		testData.RemoveIP(ipaddr)
	}
}

func identify() {
	tick := time.NewTicker(time.Duration(time.Minute))

	for {
		<-tick.C
		testData.PredictBayes(naiveBayes)
		testData.PredictID3(id3Tree)
	}
}

func main() {
	var err error

	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	log.Println("start")

	flag.Parse()

	mcp, err := mcp.New(pname)
	if err != nil {
		log.Printf("failed to connect to mcp\n")
	}

	if !strings.HasSuffix(*dataDir, "/") {
		*dataDir = *dataDir + "/"
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

	// Load the training data and train the models. Eventually, read a trained
	// model from disk.
	trainData, err = model.LoadTrainingData(*dataDir+trainFile, testData)
	if err != nil {
		log.Fatalln("failed to load training data:", err)
	}

	id3Tree, err = model.NewTree(trainData)
	if err != nil {
		log.Fatalln("failed to make ID3 tree:", err)
	}

	naiveBayes, err = model.NewBayes(trainData)
	if err != nil {
		log.Fatalln("failed to make Naive Bayes:", err)
	}

	// Use the broker to listen for appropriate messages to create and update
	// our observations.
	broker.Init(pname)
	broker.Handle(base_def.TOPIC_ENTITY, handleEntity)
	broker.Handle(base_def.TOPIC_REQUEST, handleRequest)
	broker.Handle(base_def.TOPIC_CONFIG, handleConfig)
	broker.Connect()
	defer broker.Disconnect()
	broker.Ping()

	if mcp != nil {
		if err = mcp.SetStatus("online"); err != nil {
			log.Printf("failed to set status\n")
		}
	}

	go identify()

	sig := make(chan os.Signal)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
}
