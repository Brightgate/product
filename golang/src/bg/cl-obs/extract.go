//
// COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
//
// This copyright notice is Copyright Management Information under 17 USC 1202
// and is included to protect this work and deter copyright infringement.
// Removal or alteration of this Copyright Management Information without the
// express written permission of Brightgate Inc is prohibited, and any
// such unauthorized removal or alteration will be a violation of federal law.
//

// Support for extraction and display of DeviceInfo fields.

// Extraction of data used in the Bayesian classifier results in the
// concatenation of a sequence of synthetic terms (representing various
// features of each data type) into a "sentence".  Since we may evolve
// what features are extracted over time and we may choose to cache
// sentences as our dataset becomes large, each extractor has a version,
// represented by a single character.  The concatenation of these
// extractor versions is then the version of the generated sentence.
package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"log"
	"regexp"
	"strings"

	"bg/base_msg"
	"bg/common/network"

	"github.com/fatih/color"
	"github.com/golang/protobuf/proto"
	"github.com/klauspost/oui"
	"github.com/pkg/errors"
)

const (
	termMacMfgFmt = "hw-mac-mfg-%s"

	termDHCPVendorFmt      = "dh-vendor-agent-%s"
	termDHCPOptionsFmt     = "dh-vendor-options-%s"
	termDHCPAAPLSpecialFmt = "dh-aapl-special-%s"

	termDNSHitFmt = "dns-%s"
)

func extractDNSRecords(B *backdrop, dpath string) error {
	dnss := make(map[string]hostBucket)

	rows, err := B.db.Queryx("SELECT * FROM training;")
	if err != nil {
		return errors.Wrap(err, "select training failed")
	}

	for rows.Next() {
		var dt RecordedTraining

		err = rows.StructScan(&dt)
		if err != nil {
			log.Printf("training scan failed: %v\n", err)
			continue
		}

		buf, rerr := ioutil.ReadFile(infofileFromTraining(dt, dpath))
		if rerr != nil {
			log.Printf("couldn't read %s: %v", infofileFromTraining(dt, dpath), err)
			continue
		}
		di := &base_msg.DeviceInfo{}
		err = proto.Unmarshal(buf, di)
		if err != nil {
			log.Printf("unmarshal of '%s' failed: %v\n", infofileFromTraining(dt, dpath), err)
			continue
		}

		// Need to ignore non-DNS protocols.
		for r := range di.Request {
			for q := range di.Request[r].Request {
				host := di.Request[r].Request[q]
				vals := dnsINRequestRE.FindStringSubmatch(host)
				log.Printf("name = %s, query = '%s'\n", vals[1], vals[2])

				hb, present := dnss[vals[1]]
				if !present {
					hb = hostBucket{}
				}
				switch vals[2] {
				case "A":
					hb.ACount++
				case "AAAA":
					hb.AAAACount++
				default:
					hb.OtherCount++
				}
				dnss[vals[1]] = hb
			}
		}
	}

	used := make(map[string]int)

	for d := range dnss {
		found := false
		for m := range dnsAttributes {
			if strings.Contains(d, dnsAttributes[m]) {
				found = true
				used[dnsAttributes[m]]++
				break
			}
		}

		if found {
			fmt.Printf("+%60s %5d %5d %5d\n", d, dnss[d].ACount, dnss[d].AAAACount, dnss[d].OtherCount)
		} else {
			fmt.Printf("-%60s %5d %5d %5d\n", d, dnss[d].ACount, dnss[d].AAAACount, dnss[d].OtherCount)
		}
	}

	for m := range dnsAttributes {
		_, present := used[dnsAttributes[m]]
		if !present {
			fmt.Printf("unmatched attribute: %v\n", dnsAttributes[m])
		}
	}

	return nil
}

func smashMfg(mfg string) string {
	l := strings.ToLower(mfg)
	l = strings.Replace(l, "(", "-", -1)
	l = strings.Replace(l, ")", "-", -1)
	l = strings.Replace(l, ",", "-", -1)
	l = strings.Replace(l, ".", "-", -1)
	l = strings.Replace(l, "\u00a0", "-", -1) // Non-breaking space.
	l = strings.Replace(l, " ", "-", -1)
	return strings.Trim(l, "-")
}

func macToMfgAlias(ouiDB oui.OuiDB, smac string) string {
	var mfg string

	if strings.HasPrefix(strings.ToLower(smac), "60:90:84:a") {
		return smashMfg("Brightgate, Inc.")
	}

	entry, err := ouiDB.Query(smac)

	if err != nil {
		mfg = unknownMfg
		return mfg
	}

	return smashMfg(entry.Manufacturer)
}

func wordifyDHCPOptions(opts []byte) string {
	stropt := make([]string, 0)

	for _, b := range opts {
		stropt = append(stropt, fmt.Sprintf("%d", b))
	}

	return strings.Join(stropt, "-")
}

func appendOnlyNew(sentence []string, terms ...string) []string {
	for _, term := range terms {
		sterm := strings.Replace(term, ".", "-", -1)

		for _, t := range sentence {
			if t == sterm {
				return sentence
			}
		}

		sentence = append(sentence, sterm)
	}

	return sentence
}

const ediEntityVersion = "0"

func extractDeviceInfoEntity(di *base_msg.DeviceInfo) []string {
	sentence := make([]string, 0)

	// for e := range di.Entity {
	// 	continue
	// }

	return sentence
}

const ediDHCPVersion = "1"

var emptyDHCPOptions string

func extractDeviceInfoDHCP(di *base_msg.DeviceInfo) ([]string, string) {
	var vendor string

	sentence := make([]string, 0)

	for o := range di.Options {
		vc := string(di.Options[o].VendorClassId)
		if len(vc) > 0 {
			vendor = vc
		}

		if len(vc) > 0 {
			vendorMatch, err := matchDHCPVendor(vc)
			if err != nil {
				log.Printf("unknown DHCP vendor: %v", vc)
			}

			sentence = appendOnlyNew(sentence,
				fmt.Sprintf(termDHCPVendorFmt, vendorMatch))
		} else {
			sentence = appendOnlyNew(sentence,
				fmt.Sprintf(termDHCPVendorFmt, "empty"))
		}

		options := di.Options[o].ParamReqList

		dhcpOptions := fmt.Sprintf(termDHCPOptionsFmt, wordifyDHCPOptions(options))
		if dhcpOptions != emptyDHCPOptions {
			sentence = appendOnlyNew(sentence, dhcpOptions)
		}

		if bytes.Equal(options, []byte{1, 121, 3, 6, 15, 119, 252, 95, 44, 46}) {
			// Apple, long.
			sentence = appendOnlyNew(sentence,
				fmt.Sprintf(termDHCPAAPLSpecialFmt, "long"))
		} else if bytes.Equal(options, []byte{1, 121, 3, 6, 15, 119, 252}) {
			// Apple, short.
			sentence = appendOnlyNew(sentence,
				fmt.Sprintf(termDHCPAAPLSpecialFmt, "short"))
		}
	}

	return sentence, vendor
}

const ediDNSVersion = "0"

func extractDeviceInfoDNS(di *base_msg.DeviceInfo) []string {
	sentence := make([]string, 0)

	for r := range di.Request {
		for q := range di.Request[r].Request {
			query := di.Request[r].Request[q]
			vals := dnsINRequestRE.FindStringSubmatch(query)
			if len(vals) < 2 {
				continue
			}
			host := vals[1]

			for i := range dnsAttributes {
				if strings.Contains(host, dnsAttributes[i]) {
					sentence = appendOnlyNew(sentence,
						fmt.Sprintf(termDNSHitFmt,
							dnsAttributes[i]))
				}
			}
		}
	}

	return sentence
}

const ediListenVersion = "0"

func extractDeviceInfoListen(di *base_msg.DeviceInfo) []string {
	sentence := make([]string, 0)

	for l := range di.Listen {

		if *di.Listen[l].Type == base_msg.EventListen_SSDP {
			// XXX We only want to add this if a device is
			// publishing, not using SEARCH or DISCOVER.
			if *di.Listen[l].Ssdp.Type == base_msg.EventSSDP_ALIVE {
				sentence = appendOnlyNew(sentence, "listen-ssdp")
			}
		} else if *di.Listen[l].Type == base_msg.EventListen_mDNS {
			// XXX We only want to add this if a device is
			// publishing, not querying.
			sentence = appendOnlyNew(sentence, "listen-mdns")
		}
	}

	return sentence
}

const ediScanVersion = "0"

func extractDeviceInfoScan(di *base_msg.DeviceInfo) []string {
	sentence := make([]string, 0)

	for s := range di.Scan {

		for h := range di.Scan[s].Hosts {
			for p := range di.Scan[s].Hosts[h].Ports {
				sentence = appendOnlyNew(sentence,
					fmt.Sprintf("scan-port-%s-%d",
						*di.Scan[s].Hosts[h].Ports[p].Protocol,
						*di.Scan[s].Hosts[h].Ports[p].PortId))
			}
		}
	}

	return sentence
}

func getCombinedVersion() string {
	return ediEntityVersion + ediDHCPVersion + ediDNSVersion + ediListenVersion + ediScanVersion
}

// The Bayesian sentence we compute from a DeviceInfo is composed of the
// concatenation of the "version term", followed by the subsentences
// from each of the extractors.
func genBayesSentenceFromDeviceInfo(ouiDB oui.OuiDB, di *base_msg.DeviceInfo) (string, string) {
	sentence := make([]string, 0)

	if di.MacAddress == nil {
		return getCombinedVersion(), ""
	}

	mac := network.Uint64ToMac(*di.MacAddress)

	mfg := macToMfgAlias(ouiDB, mac)

	sentence = appendOnlyNew(sentence, fmt.Sprintf(termMacMfgFmt, smashMfg(mfg)))

	entitySentence := extractDeviceInfoEntity(di)
	sentence = appendOnlyNew(sentence, entitySentence...)

	dhcpSentence, _ := extractDeviceInfoDHCP(di)
	sentence = appendOnlyNew(sentence, dhcpSentence...)

	dnsSentence := extractDeviceInfoDNS(di)
	sentence = appendOnlyNew(sentence, dnsSentence...)

	listenSentence := extractDeviceInfoListen(di)
	sentence = appendOnlyNew(sentence, listenSentence...)

	scanSentence := extractDeviceInfoScan(di)
	sentence = appendOnlyNew(sentence, scanSentence...)

	return getCombinedVersion(), strings.Join(sentence, " ")
}

func genBayesSentenceFromDeviceInfoFile(ouiDB oui.OuiDB, diPath string) (string, string) {
	buf, err := ioutil.ReadFile(diPath)
	if err != nil {
		log.Fatalf("couldn't read %s: %v; new ingest needed?\n", diPath, err)
	}

	di := &base_msg.DeviceInfo{}
	err = proto.Unmarshal(buf, di)
	if err != nil {
		log.Fatalf("couldn't unmarshal %s: %v\n", diPath, err)
	}

	return genBayesSentenceFromDeviceInfo(ouiDB, di)
}

func matchDHCPVendor(vendor string) (string, error) {
	for p := range dhcpVendorPatterns {
		matched, _ := regexp.MatchString(p, vendor)
		if matched {
			return dhcpVendorPatterns[p], nil
		}
	}
	return unknownDHCPVendor, fmt.Errorf("matchDHCPVendor no match '%s'", vendor)
}

func extractDHCPRecords(B *backdrop, dpath string) error {
	dhcpvs := make(map[int]dhcpBucket)

	rows, err := B.db.Queryx("SELECT * FROM training;")
	if err != nil {
		log.Fatalf("select training failed: %v\n", err)
	}

	n := 0

	for rows.Next() {
		var dt RecordedTraining

		err = rows.StructScan(&dt)
		if err != nil {
			log.Printf("training scan failed: %v\n", err)
			continue
		}

		buf, rerr := ioutil.ReadFile(infofileFromTraining(dt, dpath))
		if rerr != nil {
			log.Printf("couldn't read %s: %v", infofileFromTraining(dt, dpath), rerr)
			continue
		}
		di := &base_msg.DeviceInfo{}
		err = proto.Unmarshal(buf, di)
		if err != nil {
			log.Printf("unmarshal failed: %v\n", err)
			continue
		}

		for o := range di.Options {
			dhb, present := dhcpvs[n]
			if !present {
				dhb = dhcpBucket{}
			}
			dhb.Options = di.Options[o].ParamReqList
			dhb.Vendor = string(di.Options[o].VendorClassId)
			if len(dhb.Vendor) > 0 {
				dhb.VendorMatch, err = matchDHCPVendor(dhb.Vendor)
				if err != nil {
					log.Printf("unknown DHCP vendor: %v", dhb.Vendor)
				}
			}
			dhcpvs[n] = dhb
		}

		n++
	}

	// Header
	for d := range dhcpvs {
		// Per bucket output
		fmt.Printf("%v %v %v\n", d, len(dhcpvs[d].Options), dhcpvs[d])
	}

	return nil
}

func extractMfgs(B *backdrop, dpath string) error {
	mfgs := make(map[string]mfgBucket)

	rows, err := B.db.Queryx("SELECT * FROM training;")

	if err != nil {
		log.Fatalf("select device failed: %v\n", err)
	}

	for rows.Next() {
		var dt RecordedTraining

		err = rows.StructScan(&dt)
		if err != nil {
			log.Printf("device scan failed: %v\n", err)
			continue
		}

		pcs := strings.Split(infofileFromTraining(dt, dpath), "/")
		if len(pcs) == 0 {
			continue
		}

		dmac := pcs[len(pcs)-2]

		entry, err := B.ouidb.Query(dmac)
		if err != nil {
			log.Printf("%v unknown manufacturer: %+v\n", dmac, infofileFromTraining(dt, dpath))
			continue
		}

		mb, present := mfgs[entry.Prefix.String()]
		if !present {
			mb = mfgBucket{entry.Prefix.String(), entry.Manufacturer, 1}
		} else {
			mb.Count++
		}
		mfgs[entry.Prefix.String()] = mb
	}

	fmt.Printf("%8s\t%-24s\t%-40s\t%1s\t%5s\n", "PREFIX", "ALIAS", "MANUFACTURER", "?", "COUNT")
	for m := range mfgs {
		name := mfgs[m].Name
		alias := mfgReverseAliasMap[name]
		missing := " "

		fmt.Printf("%8s\t%-24s\t%-40s\t%1s\t%5d\n", mfgs[m].Prefix, alias, name, missing, mfgs[m].Count)
	}

	return nil
}

func extractDevices(B *backdrop) error {
	rows, err := B.db.Queryx("SELECT * FROM device;")
	if err != nil {
		log.Fatalf("select device failed: %v", err)
	}

	for rows.Next() {
		var rdi RecordedDeviceInfo
		deviceFound := false
		osFound := false

		err = rows.StructScan(&rdi)
		if err != nil {
			log.Printf("device struct scan failed: %v\n", err)
			continue
		}

		for _, v := range deviceGenusMap {
			if rdi.AssignedDeviceGenus == v {
				deviceFound = true
				break
			}
		}

		for _, v := range osGenusMap {
			if rdi.AssignedOSGenus == v {
				osFound = true
				break
			}
		}

		if !deviceFound {
			log.Fatalf(color.RedString("!! no device match '%s'", rdi.AssignedDeviceGenus))
		}
		if !osFound {
			log.Fatalf(color.RedString("!! no OS match '%s'", rdi.AssignedOSGenus))
		}
	}

	return nil
}

func init() {
	emptyDHCPOptions = fmt.Sprintf(termDHCPOptionsFmt, "")
}
