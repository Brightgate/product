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
	output_fmt = "%-18s %s\n"
)

var (
	cli_db_path = flag.String("oui-db-path", "", "Path to OUI database file")
	db_path     string
	db          oui.StaticDB
)

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	flag.Parse()

	ap_root, root_defined := os.LookupEnv("APROOT")

	// If command line option defined, open that.
	if *cli_db_path != "" {
		db_path = *cli_db_path
	} else if root_defined {
		// Else if APROOT defined, construct path and open that.
		db_path = fmt.Sprintf("%s/etc/oui.txt", ap_root)
	} else {
		log.Println("No OUI database found; use -oui-db-path or APROOT")
		os.Exit(2)
	}

	// Open a copy of the IEEE OUI database.
	db, err := oui.OpenStaticFile(db_path)
	if err != nil {
		log.Fatalf("couldn't open '%s': %v\n", db_path, err)
	}

	log.Println("generated %v\n", db.Generated())

	// Query each MAC address given on the command line.
	fmt.Printf(output_fmt, "HWADDR", "MANUFACTURER")
	for _, mac := range flag.Args() {
		entry, err := db.Query(mac)

		// If error is nil, we have a result in "entry"
		if err == nil {
			fmt.Printf(output_fmt, mac, entry.Manufacturer)
		} else {
			log.Println("Query() err", err)
		}
	}
}
