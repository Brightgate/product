/*
 * COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

/*
 * This daemon will periodically check the Brightgate cloud for updates to
 * blocklists, etc.  We currently assume that each dataset to be updated has a
 * 'latest' file, which contains the URL of the freshest data.  If an update is
 * found, the daemon will download it and modify the @/updates/<name> config
 * property, which will trigger the consuming daemon to refresh its internal
 * state.
 *
 * Future work:
 *
 *    - Rather than having a 'latest' file for each dataset, we might want to
 *      investigate having a single manifest itemizing the latest versions of
 *      each dataset.
 *
 *    - We currently have no versioning on the datasets.  There should be some
 *      annotation indicating minimum/maximum client versions a dataset can be
 *      consumed by.  Alternatively, each dataset could have a format version
 *      and the consuming software would specify the formats that it supports.
 *      Either way, we would need some sort of manifest rather than the simple
 *      'latest' file.
 *
 *    - There should be some dataset-specific callback to invoke when a dataset
 *      is refreshed.  This will probably be needed when we start downloading
 *      the vulnerability database, as we will need the ability to download new
 *      exploit-detection scripts as well.
 */

package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"bg/ap_common/apcfg"
	"bg/ap_common/aputil"
	"bg/ap_common/broker"
	"bg/ap_common/mcp"
	"bg/common"
)

const (
	pname = "ap.updated"

	googleStorage = "https://storage.googleapis.com/"
	googleBucket  = "bg-blocklist-a198e4a0-5823-4d16-8950-ad34b32ace1c"
)

var (
	brokerd *broker.Broker
	config  *apcfg.APConfig

	period = flag.Duration("period", 10*time.Minute,
		"frequency with which to check updates")
)

type updateInfo struct {
	localDir   string
	localName  string
	urlBase    string
	latestName string
}

var updates = map[string]updateInfo{
	"ip_blocklist": {
		localDir:   "/var/spool/watchd/",
		localName:  "ip_blocklist.csv",
		urlBase:    googleStorage + googleBucket + "/",
		latestName: "ip_blocklist.latest",
	},
	"dns_blocklist": {
		localDir:   "/var/spool/antiphishing/",
		localName:  "dns_blocklist.csv",
		urlBase:    googleStorage + googleBucket + "/",
		latestName: "dns_blocklist.latest",
	},
	"dns_allowlist": {
		localDir:   "/var/spool/antiphishing/",
		localName:  "dns_allowlist.csv",
		urlBase:    googleStorage + googleBucket + "/",
		latestName: "dns_allowlist.latest",
	},
	// "vulnerabilities": {
	// localDir:   "/var/spool/watchd/",
	// localName:  "vuln-db.json",
	// urlBase:    googleStorage + googleBucket + "/",
	// latestName: "vuln-db.latest",
	// },
}

func refresh(u *updateInfo) (bool, error) {
	targetDir := aputil.ExpandDirPath(u.localDir)
	target := targetDir + u.localName
	latestFile := "/var/tmp/" + u.latestName
	metaFile := latestFile + ".meta"

	if !aputil.FileExists(targetDir) {
		if err := os.MkdirAll(targetDir, 0755); err != nil {
			return false, fmt.Errorf("failed to create %s: %v",
				targetDir, err)
		}
	}

	url := u.urlBase + u.latestName
	metaRefreshed, err := common.FetchURL(url, latestFile, metaFile)
	if err != nil {
		return false, fmt.Errorf("unable to download %s: %v", url, err)
	}

	dataRefreshed := false
	if metaRefreshed || !aputil.FileExists(target) {
		b, rerr := ioutil.ReadFile(latestFile)
		if rerr != nil {
			err = fmt.Errorf("unable to read %s: %v", latestFile, err)
		} else {
			sourceName := string(b)
			url = u.urlBase + sourceName
			_, err = common.FetchURL(url, target, "")
		}
		if err == nil {
			log.Printf("Updated %s\n", target)
			dataRefreshed = true
		} else {
			os.Remove(metaFile)
		}
	}

	return dataRefreshed, err
}

func refreshOne(name string) {
	update := updates[name]
	refreshed, err := refresh(&update)
	if err != nil {
		log.Printf("Failed to update %s: %v\n", name, err)
	} else if refreshed {
		log.Printf("%s refreshed\n", name)
		tstr := time.Now().Format(time.RFC3339)
		prop := "@/updates/" + name
		config.CreateProp(prop, tstr, nil)
	}
}

func mainLoop() {
	exitSig := make(chan os.Signal, 2)
	signal.Notify(exitSig, syscall.SIGINT, syscall.SIGTERM)

	refreshSig := make(chan os.Signal)
	signal.Notify(refreshSig, syscall.SIGHUP)

	ticker := time.NewTicker(*period)
	for {
		for name := range updates {
			refreshOne(name)
		}

		select {
		case s := <-exitSig:
			log.Printf("Received signal '%v'.  Exiting.\n", s)
			return
		case <-refreshSig:
			log.Printf("Received SIGHUP.  Refreshing updates.\n")
		case <-ticker.C:
		}
	}
}

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	flag.Parse()

	mcpd, err := mcp.New(pname)
	if err != nil {
		log.Printf("cannot connect to mcp\n")
	}

	brokerd := broker.New(pname)
	defer brokerd.Fini()

	if config, err = apcfg.NewConfig(brokerd, pname); err != nil {
		log.Fatalf("cannot connect to configd: %v\n", err)
	}

	mcpd.SetState(mcp.ONLINE)
	mainLoop()
}
