/*
 * COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

// When running on the same node as cl.configd (the likely scenario for
// testing), the following environment variables should be set:
//
//       export B10E_CLCONFIGD_CONNECTION=127.0.0.1:4431
//       export B10E_CLCONFIGD_DISABLE_TLS=true

package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"bg/cl_common/clcfg"
	"bg/common/cfgapi"
	"bg/common/configctl"

	"github.com/tomazk/envcfg"
)

const pname = "cl-configctl"

var environ struct {
	// XXX: this will eventually be a postgres connection, and we will look
	// up the per-site cl.configd connection via the database
	ConfigdConnection string `envcfg:"B10E_CLCONFIGD_CONNECTION"`
	DisableTLS        bool   `envcfg:"B10E_CLCONFIGD_DISABLE_TLS"`
}

var (
	level   = flag.String("l", "user", "change configd access level")
	uuid    = flag.String("u", "", "uuid of site to configure")
	timeout = flag.Duration("t", 10*time.Second, "timeout")
	verbose = flag.Bool("v", false, "verbose output")
)

func main() {
	var err error

	flag.Parse()

	err = envcfg.Unmarshal(&environ)
	if err != nil {
		log.Fatalf("Environment Error: %s", err)
	}
	if environ.ConfigdConnection == "" {
		log.Fatalf("B10E_CLCONFIGD_CONNECTION must be set")
	}

	l, ok := cfgapi.AccessLevels[*level]
	if !ok {
		fmt.Printf("no such access level: %s\n", *level)
		os.Exit(1)
	}
	args := flag.Args()

	url := environ.ConfigdConnection
	tls := !environ.DisableTLS
	conn, err := clcfg.NewConfigd(pname, *uuid, url, tls)
	if err != nil {
		log.Fatalf("connection failure: %s", err)
	}
	conn.SetTimeout(*timeout)
	conn.SetVerbose(*verbose)
	conn.SetLevel(l)
	conn.Ping(nil)

	cfg := cfgapi.NewHandle(conn)
	err = configctl.Exec(pname, cfg, args)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	if err != nil {
		fmt.Printf("%v\n", err)
		os.Exit(1)
	}
}
