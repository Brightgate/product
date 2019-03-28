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

// To create the device database on Ubuntu:
//   $ export BGUSER=<name>
//   $ sudo apt-get install postgresql postgresql-contrib postgresql-client
//   $ sudo -u postgres createuser --createdb $BGUSER
//   $ createdb $BGUSER
//   # Edit /etc/postgresql/9.5/main/pg_hba.conf to
//         host    all             all             127.0.0.1/32            trust
//   $ sudo systemctl restart postgresql
//   $ ./proto.x86_64/util/bin/build_device_db -import \
//     -dev golang/src/bg/ap.configd/devices.json -id ap_identities.csv \
//     -mfg ap_mfgid.json
package main

import (
	"flag"
	"fmt"
	"os"
)

var (
	devFile = flag.String("dev", "", "device JSON file")
	idFile  = flag.String("id", "", "identifier CSV")
	mfgFile = flag.String("mfg", "", "manufacturer json")
	impFlag = flag.Bool("import", false, "import into database")
	expFlag = flag.Bool("export", false, "export from database")
)

func usage(name string) {
	fmt.Printf("Usage: %s < -import | -export > -dev <device.json> -id <id.csv> -mfg <mfg.json>\n",
		name)
	os.Exit(1)
}

func main() {
	flag.Parse()

	imp := (impFlag != nil) && *impFlag
	exp := (expFlag != nil) && *expFlag
	if imp == exp {
		usage(os.Args[0])
	}

	db, err := ConnectDB(os.Getenv("BGUSER"), os.Getenv("BGPASSWORD"))
	if err != nil {
		fmt.Printf("Failed to connect to DB: %v\n", err)
		os.Exit(1)
	}

	if imp {
		if err = ImportData(db, *devFile, *idFile, *mfgFile); err == nil {
			err = PopulateDatabase(db)
		}
	} else {
		if err = FetchData(db); err == nil {
			err = ExportData(db, *devFile, *idFile, *mfgFile)
		}
	}

	if err != nil {
		fmt.Printf("%v\n", err)
		os.Exit(1)
	}
}
