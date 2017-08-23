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
	"net"
	"os"
	"strings"

	"ap.identifierd/model"

	"github.com/klauspost/oui"
)

const (
	identOut = "new_ap_identities.csv"
	mfgOut   = "new_ap_mfgid.json"
)

var (
	observedFiles = flag.String("observed", "",
		"A comma separated list of CSV files containing observations.")
	identFile = flag.String("identities", "", "The CSV file of identities to update.")
	mfgFile   = flag.String("mfgIDs", "", "The JSON file of MFG IDs to update.")
	ouiFile   = flag.String("oui", "", "The path to oui.txt.")

	ouiDB   oui.DynamicDB
	mfgidDB = make(map[string]int)
	maxID   int

	attrUnion = make(map[string]bool)
	output    = make([]*sample, 0)
)

type sample struct {
	identity string
	attrs    map[string]string
}

func readObservations(path string) {
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

	userInput := bufio.NewReader(os.Stdin)

	// header[0] is "MAC Address".
	header = header[1:]
	for {
		record, err := r.Read()
		if err == io.EOF {
			break
		} else if err != nil {
			log.Fatalf("failed to read from %s: %s\n", path, err)
		}

		mac, err := net.ParseMAC(record[0])
		if err != nil {
			log.Fatalf("invalid MAC address %s: %s\n", record[0], err)
		}

		entry, err := ouiDB.Query(mac.String())
		if err != nil {
			log.Fatalf("MAC address %s not in OUI database: %s\n", mac, err)
		}

		last := len(record) - 1
		fmt.Printf("What is (%s, %s, %s)? (Or 'SKIP') ", mac.String(), entry.Manufacturer, record[last])
		trueIdentity, _ := userInput.ReadString('\n')
		trueIdentity = strings.TrimSpace(trueIdentity)

		if strings.Contains(trueIdentity, "SKIP") {
			continue
		}

		if _, ok := mfgidDB[entry.Manufacturer]; !ok {
			maxID++
			mfgidDB[entry.Manufacturer] = maxID
		}

		s := &sample{
			identity: trueIdentity,
			attrs:    make(map[string]string),
		}
		mfgStr := model.FormatMfgString(mfgidDB[entry.Manufacturer])
		s.attrs[mfgStr] = "1"
		attrUnion[mfgStr] = true

		for i, feat := range record[1:last] {
			if feat == "0" {
				continue
			}

			fmt.Printf("\tAdd feature %q to %q? (Y/n) ", header[i], trueIdentity)
			response, _ := userInput.ReadString('\n')
			if strings.Contains(response, "n") {
				continue
			}

			s.attrs[header[i]] = feat
			attrUnion[header[i]] = true
		}
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

	known := make(map[string]bool)
	for {
		record, err := r.Read()
		if err == io.EOF {
			break
		} else if err != nil {
			log.Fatalf("failed to read from %s: %s\n", path, err)
		}

		last := len(record) - 1

		s := &sample{
			identity: record[last],
			attrs:    make(map[string]string),
		}

		known[s.identity] = true
		for i, feat := range record[:last] {
			if feat == "0" {
				continue
			}
			s.attrs[header[i]] = feat
			attrUnion[header[i]] = true
		}
		output = append(output, s)
	}

	fmt.Println("The identities we know about are:")
	for i := range known {
		fmt.Printf("\t%q\n", i)
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
		row[len(row)-1] = s.identity
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

func main() {
	var err error
	flag.Parse()

	ouiDB, err = oui.OpenFile(*ouiFile)
	if err != nil {
		log.Fatalf("failed to open OUI file %s: %s", *ouiFile, err)
	}

	file, err := ioutil.ReadFile(*mfgFile)
	if err != nil {
		log.Fatalf("failed to open manufacturer ID file %s: %s\n", *mfgFile, err)
	}

	err = json.Unmarshal(file, &mfgidDB)
	if err != nil {
		log.Fatalf("failed to import manufacturer IDs from %s: %s\n", *mfgFile, err)
	}

	for _, v := range mfgidDB {
		if v > maxID {
			maxID = v
		}
	}

	readIdentities(*identFile)

	files := strings.Split(*observedFiles, ",")
	for _, f := range files {
		readObservations(f)
	}

	writeIdentities(identOut)
	writeMfgs(mfgOut)
}
