//
// COPYRIGHT 2017 Brightgate Inc. All rights reserved.
//
// This copyright notice is Copyright Management Information under 17 USC 1202
// and is included to protect this work and deter copyright infringement.
// Removal or alteration of this Copyright Management Information without the
// express written permission of Brightgate Inc is prohibited, and any
// such unauthorized removal or alteration will be a violation of federal law.
//

package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/klauspost/oui"
)

const (
	outputFmt = "%-18s %s\n"
)

var (
	cliDbPath = flag.String("oui-db-path", "", "Path to OUI database file")
	dbPath    string
	db        oui.StaticDB
)

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	flag.Parse()

	apRoot, ok := os.LookupEnv("APROOT")

	// If command line option defined, open that.
	if *cliDbPath != "" {
		dbPath = *cliDbPath
	} else if ok {
		// Else if APROOT defined, construct path and open that.
		dbPath = fmt.Sprintf("%s/etc/oui.txt", apRoot)
	} else {
		log.Println("No OUI database found; use -oui-db-path or APROOT")
		os.Exit(2)
	}

	// Open a copy of the IEEE OUI database.
	db, err := oui.OpenStaticFile(dbPath)
	if err != nil {
		log.Fatalf("couldn't open '%s': %v\n", dbPath, err)
	}

	log.Printf("generated %v\n", db.Generated())

	// Query each MAC address given on the command line.
	fmt.Printf(outputFmt, "HWADDR", "MANUFACTURER")
	for _, mac := range flag.Args() {
		entry, err := db.Query(mac)

		// If error is nil, we have a result in "entry"
		if err == nil {
			fmt.Printf(outputFmt, mac, entry.Manufacturer)
		} else {
			log.Println("Query() err", err)
		}
	}
}
