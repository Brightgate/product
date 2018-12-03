/*
 * COPYRIGHT 2018 Brightgate Inc. All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package main

import (
	"flag"
	"log"
	"time"

	"bg/ap_common/apcfg"
	"bg/ap_common/broker"
	"bg/ap_common/certificate"
	"bg/common/cfgapi"
)

var (
	force   bool
	verbose bool
)

func certFlagInit() {
	flag.BoolVar(&force, "force", false, "Force refresh self-signed")
	flag.BoolVar(&verbose, "verbose", false, "Verbose output")
	flag.Parse()
}

func certcheck() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	certFlagInit()
	validDuration, err := time.ParseDuration("25h")
	if err != nil {
		log.Fatalf("could not parse '25h' duration: %v\n", err)
	}

	brokerd := broker.New(pname)
	configd, err := apcfg.NewConfigd(brokerd, pname, cfgapi.AccessInternal)
	if err != nil {
		log.Fatalf("cannot connect to configd: %v\n", err)
	}

	domainName, err := configd.GetDomain()
	if err != nil {
		log.Fatalf("failed to fetch gateway domain: %v\n", err)
	}
	gatewayName := "gateway." + domainName
	if verbose {
		log.Printf("gateway = %v\n", gatewayName)
	}

	keyfn, certfn, chainfn, fullchainfn, err := certificate.GetKeyCertPaths(brokerd, gatewayName,
		time.Now().Add(validDuration), force)

	if err != nil {
		log.Fatalf("GetKeyCertPaths failed: %v", err)
	}

	if verbose {
		log.Printf("key = %v\ncertificate = '%v'\nchain = '%v'\nfullchain = '%v'\n", keyfn, certfn, chainfn, fullchainfn)
	}
}

func init() {
	addTool("ap-certcheck", certcheck)
}
