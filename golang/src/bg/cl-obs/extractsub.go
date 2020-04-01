/*
 * COPYRIGHT 2020 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

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
	"context"
	"fmt"
	"regexp"
	"strings"

	"bg/cl-obs/defs"
	"bg/cl-obs/extract"

	"github.com/pkg/errors"
)

const (
	dnsINRequestPat = ";(.*)\tIN\t (.*)"
)

var dnsINRequestRE = regexp.MustCompile(dnsINRequestPat)

func extractDNSRecords(B *backdrop) error {
	type hostBucket struct {
		ACount     int
		AAAACount  int
		OtherCount int
	}

	dnss := make(map[string]hostBucket)

	rows, err := B.db.Queryx("SELECT * FROM training;")
	if err != nil {
		return errors.Wrap(err, "select training failed")
	}

	for rows.Next() {
		var rt RecordedTraining

		err = rows.StructScan(&rt)
		if err != nil {
			slog.Warnf("training scan failed: %v\n", err)
			continue
		}

		di, err := B.store.ReadTuple(context.Background(), rt.Tuple())
		if err != nil {
			slog.Warnf("couldn't get DeviceInfo %s: %v", rt.Tuple(), err)
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
		for _, dnsAttr := range defs.DNSAttributes {
			if strings.Contains(d, dnsAttr) {
				found = true
				used[dnsAttr]++
				break
			}
		}

		if found {
			fmt.Printf("+%60s %5d %5d %5d\n", d, dnss[d].ACount, dnss[d].AAAACount, dnss[d].OtherCount)
		} else {
			fmt.Printf("-%60s %5d %5d %5d\n", d, dnss[d].ACount, dnss[d].AAAACount, dnss[d].OtherCount)
		}
	}

	for _, dnsAttr := range defs.DNSAttributes {
		_, present := used[dnsAttr]
		if !present {
			fmt.Printf("unmatched attribute: %v\n", dnsAttr)
		}
	}

	return nil
}

func extractDHCPRecords(B *backdrop) error {
	type dhcpBucket struct {
		Options    []byte
		Vendor     string
		VendorNorm string
	}

	dhcpvs := make(map[int]dhcpBucket)

	rows, err := B.db.Queryx("SELECT * FROM training;")
	if err != nil {
		slog.Fatalf("select training failed: %v\n", err)
	}

	n := 0

	for rows.Next() {
		var rt RecordedTraining

		err = rows.StructScan(&rt)
		if err != nil {
			slog.Warnf("training scan failed: %v\n", err)
			continue
		}

		di, err := B.store.ReadTuple(context.Background(), rt.Tuple())
		if err != nil {
			slog.Warnf("couldn't get DeviceInfo %s: %v", rt.Tuple(), err)
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
				dhb.VendorNorm, err = extract.NormalizeDHCPVendor(dhb.Vendor)
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
		var rt RecordedTraining

		err = rows.StructScan(&rt)
		if err != nil {
			slog.Warnf("device scan failed: %v\n", err)
			continue
		}

		dmac := rt.DeviceMAC

		entry, err := B.ouidb.Query(dmac)
		if err != nil {
			slog.Warnf("%v unknown manufacturer: %+v\n", dmac, rt)
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
		alias := defs.MfgReverseAliasMap[name]
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

		for _, v := range defs.DeviceGenusMap {
			if rdi.AssignedDeviceGenus == v {
				deviceFound = true
				break
			}
		}

		for _, v := range defs.OSGenusMap {
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
