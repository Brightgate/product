//
// COPYRIGHT 2020 Brightgate Inc.  All rights reserved.
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
	"io"
	"io/ioutil"
	"regexp"
	"strings"

	"bg/base_msg"
	"bg/cl-obs/sentence"
	"bg/common/network"

	"github.com/golang/protobuf/proto"
	"github.com/klauspost/oui"
	"github.com/pkg/errors"
)

const (
	// Update ediSeparatorVersion when either the separator constant or one
	// of the format constants is modified.
	//   The separator character, '_', is chosen to neutralize the
	//   tokenizing and stemming implementations in the underlying
	//   third party Bayesian algorithm.  That is, our vocabulary
	//   passes through its preprocessing unchanged.
	ediSeparatorVersion = "0"
	separator           = "_"

	// The trailing separators in the following formats prevent
	// stemming.
	termMacMfgFmt    = "hw_mac_mfg_%s_"
	termMacTripleFmt = "hw_mac_triple_%s_"

	termDHCPVendorFmt      = "dh_vendor_agent_%s_"
	termDHCPOptionsFmt     = "dh_vendor_options_%s_"
	termDHCPAAPLSpecialFmt = "dh_aapl_special_%s_"

	termDNSHitFmt = "dns_%s_"

	dnsINRequestPat = ";(.*)\tIN\t (.*)"
)

var dnsINRequestRE = regexp.MustCompile(dnsINRequestPat)

func extractDNSRecords(B *backdrop) error {
	dnss := make(map[string]hostBucket)

	rows, err := B.db.Queryx("SELECT * FROM training;")
	if err != nil {
		return errors.Wrap(err, "select training failed")
	}

	for rows.Next() {
		var dt RecordedTraining

		err = rows.StructScan(&dt)
		if err != nil {
			slog.Warnf("training scan failed: %v\n", err)
			continue
		}

		rdr, rerr := readerFromTraining(B, dt)
		if rerr != nil {
			slog.Warnf("couldn't get reader: %v", err)
			continue
		}

		buf, err := ioutil.ReadAll(rdr)
		if err != nil {
			slog.Warnf("couldn't ReadAll %v: %s", rdr, err)
			continue
		}

		di := &base_msg.DeviceInfo{}
		err = proto.Unmarshal(buf, di)
		if err != nil {
			slog.Warnf("unmarshal failed: %v\n", err)
			continue
		}

		// Need to ignore non-DNS protocols.
		for r := range di.Request {
			for q := range di.Request[r].Request {
				host := di.Request[r].Request[q]
				vals := dnsINRequestRE.FindStringSubmatch(host)

				if len(vals) == 0 {
					slog.Infof("no re match: %s", host)
					continue
				}

				hb, present := dnss[vals[1]]
				if !present {
					hb = hostBucket{}
				}

				if len(vals) == 3 {
					slog.Infof("name = %s, query = '%s'", vals[1], vals[2])
					switch vals[2] {
					case "A":
						hb.ACount++
					case "AAAA":
						hb.AAAACount++
					default:
						hb.OtherCount++
					}
				} else {
					slog.Info("unusual re match length: %d %+v", len(vals), vals)
				}
				dnss[vals[1]] = hb
			}
		}
	}

	used := make(map[string]int)

	fmt.Printf(" %60s %5s %5s %5s\n", "DOMAIN", "#A", "#AAAA", "#OTH")
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
	l = strings.Replace(l, "(", separator, -1)
	l = strings.Replace(l, ")", separator, -1)
	l = strings.Replace(l, ",", separator, -1)
	l = strings.Replace(l, ".", separator, -1)
	l = strings.Replace(l, "-", separator, -1)
	l = strings.Replace(l, "\u00a0", separator, -1) // Non-breaking space.
	l = strings.Replace(l, " ", separator, -1)
	return strings.Trim(l, separator)
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

	return strings.Join(stropt, separator)
}

func appendOnlyNew(sentence []string, terms ...string) []string {
	for _, term := range terms {
		sterm := strings.Replace(term, ".", separator, -1)

		for _, t := range sentence {
			if t == sterm {
				return sentence
			}
		}

		sentence = append(sentence, sterm)
	}

	return sentence
}

const ediBaseVersion = "1"

// extract information from the top level of the DeviceInfo record.
func extractDeviceInfoBase(ouiDB oui.OuiDB, di *base_msg.DeviceInfo) sentence.Sentence {
	s := sentence.New()

	mac := network.Uint64ToMac(*di.MacAddress)

	// Manufacturer's name.
	mfg := macToMfgAlias(ouiDB, mac)
	s.AddTermf(termMacMfgFmt, smashMfg(mfg))

	// First three octets of MAC.
	s.AddTermf(termMacTripleFmt, strings.ReplaceAll(mac[0:8], ":", separator))

	return s
}

const ediDHCPVersion = "0"

var emptyDHCPOptions string

func extractDeviceInfoDHCP(di *base_msg.DeviceInfo) (sentence.Sentence, string) {
	var vendor string

	s := sentence.New()

	for o := range di.Options {
		vc := string(di.Options[o].VendorClassId)
		if len(vc) > 0 {
			vendor = vc
		}

		if len(vc) > 0 {
			vendorMatch, err := matchDHCPVendor(vc)
			if err != nil {
				slog.Warnf("unknown DHCP vendor: %v", vc)
			}

			s.AddTermf(termDHCPVendorFmt, vendorMatch)
		}

		options := di.Options[o].ParamReqList

		dhcpOptions := fmt.Sprintf(termDHCPOptionsFmt, wordifyDHCPOptions(options))
		if dhcpOptions != emptyDHCPOptions {
			s.AddTerm(dhcpOptions)
		}

		if bytes.Equal(options, []byte{1, 121, 3, 6, 15, 119, 252, 95, 44, 46}) {
			// Apple, long.
			s.AddTermf(termDHCPAAPLSpecialFmt, "long")
		} else if bytes.Equal(options, []byte{1, 121, 3, 6, 15, 119, 252}) {
			// Apple, short.
			s.AddTermf(termDHCPAAPLSpecialFmt, "short")
		}
	}

	return s, vendor
}

const ediDNSVersion = "1"

func extractDeviceInfoDNS(di *base_msg.DeviceInfo) sentence.Sentence {
	s := sentence.New()

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
					l := strings.Replace(dnsAttributes[i], ".", separator, -1)
					l = strings.Replace(l, "-", separator, -1)
					s.AddTermf(termDNSHitFmt, l)
				}
			}
		}
	}

	return s
}

const ediListenVersion = "1"

func extractDeviceInfoListen(di *base_msg.DeviceInfo) sentence.Sentence {
	s := sentence.New()

	for l := range di.Listen {

		if *di.Listen[l].Type == base_msg.EventListen_SSDP {
			// XXX We only want to add this if a device is
			// publishing, not using SEARCH or DISCOVER.
			if *di.Listen[l].Ssdp.Type == base_msg.EventSSDP_ALIVE {
				s.AddTerm("listen_ssdp")
			}
		} else if *di.Listen[l].Type == base_msg.EventListen_mDNS {
			// XXX We only want to add this if a device is
			// publishing, not querying.
			s.AddTerm("listen_mdns")
		}
	}

	return s
}

const ediScanVersion = "1"

func extractDeviceInfoScan(di *base_msg.DeviceInfo) sentence.Sentence {
	s := sentence.New()

	for sc := range di.Scan {
		for h := range di.Scan[sc].Hosts {
			for p, port := range di.Scan[sc].Hosts[h].Ports {
				if *port.PortId > 10000 {
					continue
				}
				s.AddTermf("scan_port_%s_%d",
					*di.Scan[sc].Hosts[h].Ports[p].Protocol,
					*di.Scan[sc].Hosts[h].Ports[p].PortId)
			}
		}
	}

	return s
}

func getCombinedVersion() string {
	return ediSeparatorVersion + ediBaseVersion + ediDHCPVersion + ediDNSVersion + ediListenVersion + ediScanVersion
}

// The Bayesian sentence we compute from a DeviceInfo is composed of the
// concatenation of the "version term", followed by the subsentences
// from each of the extractors.
func genBayesSentenceFromDeviceInfo(ouiDB oui.OuiDB, di *base_msg.DeviceInfo) (string, sentence.Sentence) {
	s := sentence.New()

	if di.MacAddress == nil {
		return getCombinedVersion(), s
	}

	baseSentence := extractDeviceInfoBase(ouiDB, di)
	s.AddSentence(baseSentence)

	dhcpSentence, _ := extractDeviceInfoDHCP(di)
	s.AddSentence(dhcpSentence)

	dnsSentence := extractDeviceInfoDNS(di)
	s.AddSentence(dnsSentence)

	listenSentence := extractDeviceInfoListen(di)
	s.AddSentence(listenSentence)

	scanSentence := extractDeviceInfoScan(di)
	s.AddSentence(scanSentence)

	return getCombinedVersion(), s
}

func genBayesSentenceFromReader(ouiDB oui.OuiDB, rdr io.Reader) (string, sentence.Sentence) {
	buf, err := ioutil.ReadAll(rdr)
	if err != nil {
		slog.Fatalf("couldn't read: %v; new ingest needed?\n", err)
	}

	di := &base_msg.DeviceInfo{}
	err = proto.Unmarshal(buf, di)
	if err != nil {
		slog.Fatalf("couldn't unmarshal: %v\n", err)
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

func extractDHCPRecords(B *backdrop) error {
	type dhcpBucket struct {
		Options     []byte
		Vendor      string
		VendorMatch string
	}

	dhcpvs := make(map[int]dhcpBucket)

	rows, err := B.db.Queryx("SELECT * FROM training;")
	if err != nil {
		slog.Fatalf("select training failed: %v\n", err)
	}

	n := 0

	for rows.Next() {
		var dt RecordedTraining

		err = rows.StructScan(&dt)
		if err != nil {
			slog.Warnf("training scan failed: %v\n", err)
			continue
		}

		rdr, rerr := readerFromTraining(B, dt)
		if rerr != nil {
			slog.Warnf("couldn't get reader for %v: %v", dt, rerr)
			continue
		}
		buf, rerr := ioutil.ReadAll(rdr)
		if rerr != nil {
			slog.Warnf("couldn't read: %v", rerr)
			continue
		}
		di := &base_msg.DeviceInfo{}
		err = proto.Unmarshal(buf, di)
		if err != nil {
			slog.Warnf("unmarshal failed: %v\n", err)
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
					slog.Warnf("unknown DHCP vendor: %v", dhb.Vendor)
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

func extractMfgs(B *backdrop) error {
	type mfgBucket struct {
		Prefix string
		Name   string
		Count  int
	}

	mfgs := make(map[string]mfgBucket)

	rows, err := B.db.Queryx("SELECT * FROM training;")
	if err != nil {
		slog.Fatalf("select device failed: %v\n", err)
	}

	for rows.Next() {
		var dt RecordedTraining

		err = rows.StructScan(&dt)
		if err != nil {
			slog.Warnf("device scan failed: %v\n", err)
			continue
		}

		dmac := dt.DeviceMAC

		entry, err := B.ouidb.Query(dmac)
		if err != nil {
			slog.Warnf("%v unknown manufacturer: %+v\n", dmac, dt)
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
		slog.Fatalf("select device failed: %v", err)
	}

	for rows.Next() {
		var rdi RecordedDevice
		deviceFound := false
		osFound := false

		err = rows.StructScan(&rdi)
		if err != nil {
			slog.Warnf("device struct scan failed: %v\n", err)
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
			slog.Fatalf("!! no device match '%s'", rdi.AssignedDeviceGenus)
		}
		if !osFound {
			slog.Fatalf("!! no OS match '%s'", rdi.AssignedOSGenus)
		}
	}

	return nil
}

func init() {
	emptyDHCPOptions = fmt.Sprintf(termDHCPOptionsFmt, "")
}
