/*
 * Copyright 2020 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


// When running on the same node as cl.configd (the likely scenario for
// testing), the following environment variables should be set:
//
//       export B10E_CLCONFIGD_CONNECTION=127.0.0.1:4431
//       export B10E_CLCONFIGD_DISABLE_TLS=true

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"bg/cl_common/clcfg"
	"bg/cl_common/registry"
	"bg/common/cfgapi"
	"bg/common/vpntool"

	"github.com/tomazk/envcfg"

	"google.golang.org/grpc/metadata"
)

const pname = "cl-vpntool"

var environ struct {
	ConfigdConnection  string `envcfg:"B10E_CLCONFIGD_CONNECTION"`
	DisableTLS         bool   `envcfg:"B10E_CLCONFIGD_DISABLE_TLS"`
	PostgresConnection string `envcfg:"REG_DBURI"`
}

var (
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

	args := flag.Args()

	u, err := registry.SiteUUIDByNameFuzzy(
		context.Background(), environ.PostgresConnection, *uuid)
	if err != nil {
		if ase, ok := err.(registry.AmbiguousSiteError); ok {
			log.Fatal(ase.Pretty())
		}
		log.Fatal(err)
	}
	if u.Name != "" {
		log.Printf("%q matched more than one site, but %q (%s) seemed the most likely",
			*uuid, u.Name, u.UUID)
	}

	url := environ.ConfigdConnection
	tls := !environ.DisableTLS
	conn, err := clcfg.NewConfigd(pname, u.UUID.String(), url, tls)
	if err != nil {
		log.Fatalf("connection failure: %s", err)
	}
	conn.SetTimeout(*timeout)
	conn.SetVerbose(*verbose)
	conn.SetLevel(cfgapi.AccessInternal)
	conn.Ping(nil)

	cfg := cfgapi.NewHandle(conn)
	ctx := context.Background()
	ctx = metadata.AppendToOutgoingContext(ctx, "site_uuid", *uuid)

	err = vpntool.Exec(ctx, pname, cfg, args)

	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	if err != nil {
		fmt.Printf("%v\n", err)
		os.Exit(1)
	}
}

