/*
 * COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 *
 */

package data

import (
	"bufio"
	"log"
	"os"
	"regexp"
	"strings"

	"bg/ap_common/platform"
	"bg/common/network"
)

const (
	allowlistName = "dns_allowlist.csv"
	blocklistName = "dns_blocklist.csv"
)

var (
	// DefaultDataDir is the directory where antiphishing databases are
	// found by default.
	DefaultDataDir = "__APDATA__/antiphishing"

	plat *platform.Platform

	dnsAllowlist *dnsMatchList
	dnsBlocklist *dnsMatchList
)

type dnsMatchList struct {
	exactMatches  map[string]bool
	regexpMatches []*regexp.Regexp
}

func inList(name string, list *dnsMatchList) bool {
	if list == nil {
		return false
	}
	if _, ok := list.exactMatches[name]; ok {
		return true
	}

	for _, re := range list.regexpMatches {
		if re.MatchString(name) {
			return true
		}
	}

	return false
}

// BlockedHostname reports whether a given host/domain name is blocked.
func BlockedHostname(name string) bool {
	return inList(name, dnsBlocklist) && !inList(name, dnsAllowlist)
}

// Pull a list of DNS names from a CSV-like file (it allows for comment lines
// starting with "#" and has no header).  The first field of each line must be a
// legal dns name or a regular expression.  The rest of the line is ignored.
func ingestDNSFile(filename string) (*dnsMatchList, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}

	var list = dnsMatchList{
		exactMatches:  make(map[string]bool),
		regexpMatches: make([]*regexp.Regexp, 0),
	}

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()

		if idx := strings.Index(line, ","); idx > 0 {
			line = line[:idx]
		}

		if len(line) > 0 && line[0] != '#' {
			match := string(line)
			if network.ValidDNSName(match) {
				list.exactMatches[match] = true
			} else if re, err := regexp.Compile(match); err == nil {
				list.regexpMatches = append(list.regexpMatches, re)
			}
		}
	}
	file.Close()

	log.Printf("Ingested %d hostnames and %d regexps from %s\n",
		len(list.exactMatches), len(list.regexpMatches), filename)
	return &list, nil
}

// LoadDNSBlocklist loads the DNS antiphishing databases.
func LoadDNSBlocklist(dataDir string) {
	wfile := plat.ExpandDirPath(dataDir, allowlistName)
	bfile := plat.ExpandDirPath(dataDir, blocklistName)

	// The allowlist file has a single allowed DNS name on each line, or a
	// CSV with no Cs.  The blocklist file is a CSV-like file, where the
	// first field of each line is a DNS name and the second field is a
	// pipe-separated list of sources that have identified that site as
	// dangerous.

	list, err := ingestDNSFile(wfile)
	if err != nil {
		log.Printf("Unable to read DNS allowlist: %v\n", err)
	} else {
		dnsAllowlist = list
	}
	list, err = ingestDNSFile(bfile)
	if err != nil {
		log.Printf("Unable to read DNS blocklist: %v\n", err)
	} else {
		dnsBlocklist = list
	}
}

func init() {
	plat = platform.NewPlatform()
}
