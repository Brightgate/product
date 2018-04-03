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
)

const (
	pname = "ap-certcheck"
)

var (
	aproot = flag.String("root", "proto.armv7l/appliance/opt/com.brightgate",
		"Root of AP installation")
	force   = flag.Bool("force", false, "Force refresh self-signed")
	verbose = flag.Bool("verbose", false, "Verbose output")

	config *apcfg.APConfig

	// ApVersion will be replaced by go build step.
	ApVersion = "undefined"
)

func main() {
	var (
		err           error
		validDuration time.Duration
		configd       *apcfg.APConfig
	)

	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	flag.Parse()

	validDuration, err = time.ParseDuration("25h")
	if err != nil {
		log.Fatalf("could not parse '25h' duration: %v\n", err)
	}

	brokerd := broker.New(pname)
	configd, err = apcfg.NewConfig(brokerd, pname)
	if err != nil {
		log.Fatalf("cannot connect to configd: %v\n", err)
	}

	domainName, err := configd.GetDomain()
	if err != nil {
		log.Fatalf("failed to fetch gateway domain: %v\n", err)
	}
	gatewayName := "gateway." + domainName

	keyfn, certfn, chainfn, fullchainfn, err := certificate.GetKeyCertPaths(brokerd, gatewayName,
		time.Now().Add(validDuration), *force)

	if err != nil {
		log.Fatalf("GetKeyCertPaths failed: %v", err)
	}

	if *verbose {
		log.Printf("key = %v\ncertificate = '%v'\nchain = '%v'\nfullchain = '%v'\n", keyfn, certfn, chainfn, fullchainfn)
	}
}
