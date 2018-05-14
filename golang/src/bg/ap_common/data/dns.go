/*
 * COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
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
	"regexp"
	"os"
	"strings"

	"bg/ap_common/aputil"
	"bg/ap_common/network"
)

const (
	whitelistName = "whitelist.csv"
	blacklistName = "dns_blocklist.csv"
)

var (
	// DefaultDataDir is the directory where antiphishing databases are
	// found by default.
	DefaultDataDir = "/var/spool/antiphishing"

	dnsWhitelist *dnsMatchList
	dnsBlacklist *dnsMatchList
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
	return inList(name, dnsBlacklist) && !inList(name, dnsWhitelist)
}

// Pull a list of DNS names from a CSV.  The first field of each line must be a
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

// LoadDNSBlacklist loads the DNS antiphishing databases.
func LoadDNSBlacklist(dataDir string) {
	wfile := aputil.ExpandDirPath(dataDir) + "/" + whitelistName
	bfile := aputil.ExpandDirPath(dataDir) + "/" + blacklistName

	// The whitelist file has a single whitelisted DNS name on each line, or
	// a CSV with no Cs.  The blacklist file is a CSV file, where the first
	// field of each line is a DNS name and the remaining fields are all
	// sources that have identified that site as dangerous.

	list, err := ingestDNSFile(wfile)
	if err != nil {
		log.Printf("Unable to read DNS whitelist %s: %v\n", wfile, err)
	} else {
		dnsWhitelist = list
	}
	list, err = ingestDNSFile(bfile)
	if err != nil {
		log.Printf("Unable to read DNS blacklist %s: %v\n", bfile, err)
	} else {
		dnsBlacklist = list
	}
}
