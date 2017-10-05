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

// Use observations collected from identifierd to create new training data.
// Currently a human is required to select features, but eventually we can try
// automatic feature selection through some statistical measure of significance.
package main

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"strconv"
	"strings"

	"ap_common/device"
	"ap_common/model"
	"ap_common/network"
	"base_msg"

	"github.com/golang/protobuf/proto"
	"github.com/klauspost/oui"
)

const (
	identOut = "new_ap_identities.csv"
	mfgOut   = "new_ap_mfgid.json"
)

var (
	observedFiles = flag.String("observed", "",
		"A comma separated list of .pb files, each containing a single DeviceInventory.")
	identFile = flag.String("identities", "$ETC/ap_identities.csv",
		"The CSV file of identities to update.")
	mfgFile = flag.String("mfgIDs", "$ETC/ap_mfgid.json",
		"The JSON file of MFG IDs to update.")
	ouiFile = flag.String("oui", "$ETC/oui.txt",
		"The path to oui.txt.")
	devFile = flag.String("dev", "$ETC/devices.json",
		"The path to devices.json.")
	etcDir = flag.String("etc", "proto.armv7l/opt/com.brightgate/etc",
		"The path to the brighgate etc/ directory")

	ouiDB   oui.DynamicDB
	mfgidDB = make(map[string]int)
	maxMfg  int
	devices device.DeviceMap

	attrUnion = make(map[string]bool)
	output    = make([]*sample, 0)
)

type sample struct {
	name     string
	identity int
	attrs    map[string]string
}

func addFeat(user *bufio.Reader, s *sample, feat string) bool {
	fmt.Printf("\tAdd feature %q to %s? (Y/n) ", feat, s.name)
	response, _ := user.ReadString('\n')
	if strings.Contains(response, "n") {
		return false
	}
	s.attrs[feat] = "1"
	attrUnion[feat] = true
	return true
}

func addDNS(devInfo *base_msg.DeviceInfo, user *bufio.Reader, s *sample) {
	if devInfo.Request == nil {
		return
	}

	added := make(map[string]bool)
	for _, msg := range devInfo.Request {
		for _, q := range msg.Request {
			feat := model.DNSQ.FindStringSubmatch(q)[1]
			if added[feat] {
				continue
			}
			added[feat] = addFeat(user, s, feat)
		}
	}
}

func addScan(devInfo *base_msg.DeviceInfo, user *bufio.Reader, s *sample) {
	if devInfo.Scan == nil {
		return
	}

	added := make(map[string]bool)
	for _, msg := range devInfo.Scan {
		for _, h := range msg.Hosts {
			for _, p := range h.Ports {
				if *p.State != "open" {
					continue
				}
				feat := model.FormatPortString(*p.Protocol, *p.PortId)
				if added[feat] {
					continue
				}
				added[feat] = addFeat(user, s, feat)
			}
		}
	}
}

func addListen(devInfo *base_msg.DeviceInfo, user *bufio.Reader, s *sample) {
	if devInfo.Listen == nil {
		return
	}

	added := make(map[string]bool)
	for _, msg := range devInfo.Listen {
		var feat string
		switch *msg.Type {
		case base_msg.EventListen_SSDP:
			feat = "SSDP"
		case base_msg.EventListen_mDNS:
			feat = "mDNS"
		}
		if added[feat] {
			continue
		}
		added[feat] = addFeat(user, s, feat)
	}
}

func readObservations(path string) {
	in, err := ioutil.ReadFile(path)
	if err != nil {
		log.Fatalf("failed to open data file %s\n", path)
	}

	inventory := &base_msg.DeviceInventory{}
	proto.Unmarshal(in, inventory)
	userInput := bufio.NewReader(os.Stdin)

	for _, devInfo := range inventory.Devices {
		hwaddr := *devInfo.MacAddress
		mac := network.Uint64ToHWAddr(hwaddr)
		entry, err := ouiDB.Query(mac.String())
		if err != nil {
			log.Fatalf("MAC address %s not in OUI database: %s\n", mac, err)
		}
		fmt.Printf("What is (%s, %s, %s)? (Or 'SKIP') ", mac.String(), entry.Manufacturer, devInfo.GetDhcpName())
		trueIdentity := 0
		name := ""
		for {
			entry, _ := userInput.ReadString('\n')
			entry = strings.TrimSpace(entry)
			if strings.Contains(entry, "SKIP") || strings.Contains(entry, "NEW") {
				break
			}
			if tmp, err := strconv.Atoi(entry); err == nil {
				if d, ok := devices[uint32(tmp)]; ok {
					trueIdentity = tmp
					name = d.Vendor + " " + d.ProductName
					break
				}
			}
			fmt.Printf("Invalid entry: %s\n", entry)
		}

		if trueIdentity == 0 {
			continue
		}

		if _, ok := mfgidDB[entry.Manufacturer]; !ok {
			maxMfg++
			mfgidDB[entry.Manufacturer] = maxMfg
		}

		s := &sample{
			name:     name,
			identity: trueIdentity,
			attrs:    make(map[string]string),
		}

		mfgStr := model.FormatMfgString(mfgidDB[entry.Manufacturer])
		s.attrs[mfgStr] = "1"
		attrUnion[mfgStr] = true

		addDNS(devInfo, userInput, s)
		addScan(devInfo, userInput, s)
		addListen(devInfo, userInput, s)

		output = append(output, s)
	}
}

func readIdentities(path string) {
	f, err := os.Open(path)
	if err != nil {
		log.Fatalf("failed to open data file %s\n", path)
	}
	defer f.Close()

	r := csv.NewReader(f)
	header, err := r.Read()
	if err != nil {
		log.Fatalf("failed to read header from %s: %s\n", path, err)
	}

	line := 0
	for {
		line++
		record, err := r.Read()
		if err == io.EOF {
			break
		} else if err != nil {
			log.Fatalf("failed to read from %s: %s\n", path, err)
		}

		last := len(record) - 1
		id, err := strconv.Atoi(record[last])
		if err != nil || id == 0 {
			fmt.Printf("Bad id at %s: %d\n", record[last], line)
			continue
		}

		s := &sample{
			identity: id,
			attrs:    make(map[string]string),
		}

		for i, feat := range record[:last] {
			if feat == "0" {
				continue
			}
			s.attrs[header[i]] = feat
			attrUnion[header[i]] = true
		}
		output = append(output, s)
	}

	// We want to iterate over the devices in ascending order - not in the
	// default map order.  To do that, we need to find the maximum ID.
	max := uint32(2)
	for i := range devices {
		if i > max {
			max = i
		}
	}

	fmt.Println("The identities we know about are:")
	for i := uint32(2); i <= max; i++ {
		if d, ok := devices[i]; ok {
			fmt.Printf("\t%-2d  %s %s\n", i, d.Vendor, d.ProductName)
		}
	}
}

func writeIdentities(path string) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		log.Fatalf("failed to open %s: %s\n", path, err)
	}
	defer f.Close()
	w := csv.NewWriter(f)

	attrMap := make(map[string]int)
	header := make([]string, 0)
	for a := range attrUnion {
		header = append(header, a)
		attrMap[a] = len(header) - 1
	}
	header = append(header, "Identity")
	w.Write(header)

	for _, s := range output {
		row := make([]string, len(header))
		for i := range header {
			row[i] = "0"
		}

		for a, v := range s.attrs {
			row[attrMap[a]] = v
		}
		row[len(row)-1] = strconv.Itoa(s.identity)
		w.Write(row)
	}

	w.Flush()
	if w.Error() != nil {
		log.Fatalf("failed to write to %s: %s\n", path, err)
	}
}

func writeMfgs(path string) {
	s, err := json.MarshalIndent(mfgidDB, "", "  ")
	if err != nil {
		log.Fatalf("failed to construct JSON: %s\n", err)
	}

	err = ioutil.WriteFile(path, s, 0644)
	if err != nil {
		log.Fatalf("Failed to write file %s: %s\n", path, err)
	}
}

func filemunge(path string) string {
	return strings.Replace(path, "$ETC", *etcDir, 1)
}

func main() {
	var err error
	flag.Parse()

	path := filemunge(*ouiFile)
	ouiDB, err = oui.OpenFile(path)
	if err != nil {
		log.Fatalf("failed to open OUI file %s: %s", path, err)
	}

	path = filemunge(*mfgFile)
	file, err := ioutil.ReadFile(path)
	if err != nil {
		log.Fatalf("failed to open manufacturer ID file %s: %s\n", path, err)
	}

	err = json.Unmarshal(file, &mfgidDB)
	if err != nil {
		log.Fatalf("failed to import manufacturer IDs from %s: %s\n", path, err)
	}

	for _, v := range mfgidDB {
		if v > maxMfg {
			maxMfg = v
		}
	}

	path = filemunge(*devFile)
	devices, err = device.DevicesLoad(path)
	if err != nil {
		log.Fatalf("failed to import devices from %s: %s\n", path, err)
	}

	path = filemunge(*identFile)
	readIdentities(path)

	files := strings.Split(*observedFiles, ",")
	for _, f := range files {
		readObservations(f)
	}

	writeIdentities(identOut)
	writeMfgs(mfgOut)
}
