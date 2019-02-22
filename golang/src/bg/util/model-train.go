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

// model-train uses observations collected from identifierd to update the
// various tables in the device database. The tables combine to form the
// training data for new models. Create the device DB locally before using this
// script.
//
// Currently a human is required to select features, but eventually we can try
// automatic feature selection through some statistical measure of significance.
package main

import (
	"bufio"
	"database/sql"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"bg/ap_common/model"
	"bg/base_msg"
	"bg/common/deviceid"
	"bg/common/network"
	"bg/util/deviceDB"

	"github.com/golang/protobuf/proto"
	"github.com/klauspost/oui"
)

var (
	// The infoDir should be a directory containing protobuf files, each file
	// containing a single DeviceInfo. We assume (and enforce) that all files in
	// the infoDir reference the same client. Because of the streaming nature
	// of data collection (see ap.identifierd) the entire directory is treated
	// as one sample.
	infoDir = flag.String("info", "",
		"A directory of .pb files for a specific client, each file containing a single DeviceInfo.")
	ouiFile = flag.String("oui", "$ETC/oui.txt",
		"The path to oui.txt.")

	db       *sql.DB
	ouiDB    oui.DynamicDB
	maxMfg   int
	maxChar  int
	maxMatch int
	maxDev   uint32

	added = make(map[string]bool)
)

type sample struct {
	name  string
	match deviceDB.Match
}

// Ask the user if feature 'feat' should be included as an identifying feature
// of the sample. If so, add the feature to the characteristics table if it
// doesn't already exist.
func addFeat(user *bufio.Reader, s *sample, feat string) bool {
	var name string
	if s.name == "" {
		name = "(unknown)"
	} else {
		name = s.name
	}

	fmt.Printf("\tAdd feature %q to %s? (Y/n) ", feat, name)
	response, _ := user.ReadString('\n')
	if strings.Contains(response, "n") {
		return false
	}

	var index int
	row := db.QueryRow("SELECT index FROM "+deviceDB.CharTable+" WHERE characteristic=$1", feat)
	if err := row.Scan(&index); err != nil && err != sql.ErrNoRows {
		log.Fatalf("failed to to fetch feature %s: %s\n", feat, err)
	} else if err == sql.ErrNoRows {
		maxChar++
		index = maxChar
		if err := deviceDB.InsertOneCharacteristic(db, maxChar, feat); err != nil {
			log.Fatalf("failed to insert characteristic %s: %s\n", feat, err)
		}
	}

	if len(s.match.Charstr) != 0 {
		s.match.Charstr += ","
	}

	s.match.Charstr += strconv.Itoa(index)
	return true
}

func addMfg(mfg string, user *bufio.Reader, s *sample) {
	var mfgid int
	row := db.QueryRow("SELECT mfgid FROM "+deviceDB.MfgTable+" WHERE name=$1", mfg)
	if err := row.Scan(&mfgid); err != nil && err != sql.ErrNoRows {
		log.Fatalf("failed to to fetch mfg %s: %s\n", mfg, err)
	} else if err == sql.ErrNoRows {
		maxMfg++
		mfgid = maxMfg
		if err := deviceDB.InsertOneMfg(db, maxMfg, mfg); err != nil {
			log.Fatalf("failed to insert mfg %s: %s\n", mfg, err)
		}
	}

	mfgStr := model.FormatMfgString(mfgid)
	if !added[mfgStr] {
		added[mfgStr] = addFeat(user, s, mfgStr)
	}
}

func addDNS(devInfo *base_msg.DeviceInfo, user *bufio.Reader, s *sample) {
	if devInfo.Request == nil {
		return
	}

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

func promptUser(user *bufio.Reader, prompt string) string {
	fmt.Printf("%s", prompt)
	entry, _ := user.ReadString('\n')
	entry = strings.TrimSpace(entry)
	return entry
}

func newDevice(user *bufio.Reader, entry *oui.Entry, s *sample) {
	productName := promptUser(user, "Enter product name: ")
	productVer := promptUser(user, "Enter product version: ")
	devType := promptUser(user, "Enter device type: ")

	maxDev++
	d := &deviceid.Device{
		Obsolete:       false,
		UpdateTime:     time.Now(),
		Devtype:        devType,
		Vendor:         entry.Manufacturer,
		ProductName:    productName,
		ProductVersion: productVer,
	}

	if err := deviceDB.InsertOneDevice(db, maxDev, d); err != nil {
		log.Fatalf("failed to insert device: %s\n", err)
	}

	s.match.Devid = maxDev
	s.name = d.Vendor + " " + d.ProductName
}

func readObservations(hwMatch uint64, file os.FileInfo, s *sample) {
	path := filepath.Join(*infoDir, file.Name())
	in, err := ioutil.ReadFile(path)
	if err != nil {
		log.Fatalf("failed to open data file %s: %s\n", path, err)
	}

	devInfo := &base_msg.DeviceInfo{}
	proto.Unmarshal(in, devInfo)

	hwaddr := devInfo.GetMacAddress()
	if hwaddr != hwMatch {
		mac1 := network.Uint64ToHWAddr(hwaddr)
		mac2 := network.Uint64ToHWAddr(hwMatch)
		log.Fatalf("MAC addresses don't match: %s != %s\n", mac1, mac2)
	}

	mac := network.Uint64ToHWAddr(hwaddr)
	ouiEntry, err := ouiDB.Query(mac.String())
	if err != nil {
		log.Fatalf("MAC address %s not in OUI database: %s\n", mac, err)
	}

	userInput := bufio.NewReader(os.Stdin)
	if s.match.Devid == 0 {
		fmt.Printf("What is (%s, %s, %s)?\n"+
			"\tIf you don't know just hit enter.\n"+
			"\tFor new device IDs enter 'NEW'.\n"+
			"\tTo skip this file (%s) enter 'SKIP'\n",
			mac.String(), ouiEntry.Manufacturer, devInfo.GetDhcpName(), path)

		for {
			entry, _ := userInput.ReadString('\n')
			entry = strings.TrimSpace(entry)

			if entry == "" {
				break
			}

			if strings.Contains(entry, "SKIP") {
				return
			}

			if strings.Contains(entry, "NEW") {
				newDevice(userInput, ouiEntry, s)
				break
			}

			if tmp, err := strconv.ParseUint(entry, 10, 32); err == nil {
				var vendor string
				var productName string
				row := db.QueryRow("SELECT Vendor, ProductName FROM "+deviceDB.DevTable+" WHERE Devid=$1", tmp)
				if err := row.Scan(&vendor, &productName); err == nil {
					s.match.Devid = uint32(tmp)
					s.name = vendor + " " + productName
					break
				} else {
					fmt.Printf("DB Query failed: %v\n", err)
				}
			}
			fmt.Printf("Invalid entry: %s\n", entry)
		}
	}

	addMfg(ouiEntry.Manufacturer, userInput, s)
	addDNS(devInfo, userInput, s)
	addScan(devInfo, userInput, s)
	addListen(devInfo, userInput, s)
}

func readInfo(dir string) {
	files, err := ioutil.ReadDir(dir)
	if err != nil {
		log.Fatalf("failed to read dir %s: %s\n", dir, err)
	}

	// Read the first MacAddress. All subsequent files should match.
	path := filepath.Join(dir, files[0].Name())
	in, err := ioutil.ReadFile(path)
	if err != nil {
		log.Fatalf("failed to open data file %s: %s\n", path, err)
	}

	devInfo := &base_msg.DeviceInfo{}
	proto.Unmarshal(in, devInfo)
	hwaddr := devInfo.GetMacAddress()
	if hwaddr == 0 {
		log.Fatalf("failed to get MAC address in %s\n", path)
	}

	// All files consitute a single sample.
	maxMatch++
	s := &sample{
		match: deviceDB.Match{Matchid: maxMatch},
	}

	for _, f := range files {
		readObservations(hwaddr, f, s)
	}

	if s.match.Devid != 0 {
		if err := deviceDB.InsertOneMatch(db, s.match); err != nil {
			log.Fatalf("failed to insert match %s: %s\n", s.name, err)
		}
	} else {
		log.Fatalf("cannot create a sample with no identity!\n")
	}
}

func readIdentities() {
	var devid uint32
	var vendor string
	var productName string

	fmt.Println("The identities we know about are:")
	rows, err := db.Query("SELECT Devid, Vendor, ProductName FROM " + deviceDB.DevTable + " ORDER BY Devid ASC")
	if err != nil {
		log.Fatalf("failed to retrieve Vendor and ProductName data: %s", err)
	}
	defer rows.Close()

	for rows.Next() {
		if err := rows.Scan(&devid, &vendor, &productName); err != nil {
			log.Fatalf("failed to scan for Vendor and ProductName: %s", err)
		}
		fmt.Printf("\t%-2d  %s %s\n", devid, vendor, productName)
	}
}

func main() {
	var err error
	flag.Parse()

	db, err = deviceDB.ConnectDB(os.Getenv("BGUSER"), os.Getenv("BGPASSWORD"))
	if err != nil {
		log.Fatalf("Failed to connect to DB: %s\n", err)
	}

	ouiDB, err = oui.OpenFile(*ouiFile)
	if err != nil {
		log.Fatalf("failed to open OUI file %s: %s", *ouiFile, err)
	}

	if err = db.QueryRow("SELECT MAX(mfgid) FROM " + deviceDB.MfgTable).Scan(&maxMfg); err != nil {
		log.Fatalf("failed to get maxMfg: %s\n", err)
	}

	if err = db.QueryRow("SELECT MAX(index) FROM " + deviceDB.CharTable).Scan(&maxChar); err != nil {
		log.Fatalf("failed to get maxChar: %s\n", err)
	}

	if err = db.QueryRow("SELECT MAX(matchid) FROM " + deviceDB.MatchTable).Scan(&maxMatch); err != nil {
		log.Fatalf("failed to get maxMatch: %s\n", err)
	}

	if err = db.QueryRow("SELECT MAX(devid) FROM " + deviceDB.DevTable).Scan(&maxDev); err != nil {
		log.Fatalf("failed to get maxDev: %s\n", err)
	}

	readIdentities()
	readInfo(*infoDir)
}
