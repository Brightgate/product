//
// COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
//
// This copyright notice is Copyright Management Information under 17 USC 1202
// and is included to protect this work and deter copyright infringement.
// Removal or alteration of this Copyright Management Information without the
// express written permission of Brightgate Inc is prohibited, and any
// such unauthorized removal or alteration will be a violation of federal law.
//

package main

import (
	"database/sql"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"bg/base_msg"

	"github.com/golang/protobuf/proto"
	"github.com/gosuri/uiprogress"
	"github.com/jmoiron/sqlx"
	"github.com/pkg/errors"
	"github.com/satori/uuid"
	"github.com/yourbasic/bloom"
)

var (
	siteBF     *bloom.Filter
	filepathRe *regexp.Regexp
)

func getContentStatusFromDeviceInfo(difn string) string {
	entityPresent := "-"
	dhcpPresent := "-"
	dnsRecordsPresent := "-"
	networkScanPresent := "-"

	buf, err := ioutil.ReadFile(difn)
	if err != nil {
		log.Printf("could not readfile '%s': %v\n", difn, err)
		return "????"
	}

	di := &base_msg.DeviceInfo{}
	err = proto.Unmarshal(buf, di)
	if err != nil {
		log.Printf("could not unmarshal '%s' content: %v\n", difn, err)
		return "????"
	}

	if di.Entity != nil {
		entityPresent = "E"
	}

	if len(di.Options) > 0 {
		dhcpPresent = "D"
	}

	if len(di.Request) > 0 {
		dnsRecordsPresent = "N"
	}

	if len(di.Scan) > 0 {
		networkScanPresent = "S"
	}

	return fmt.Sprintf("%s%s%s%s", entityPresent, dhcpPresent,
		dnsRecordsPresent, networkScanPresent)
}

func insertNewSiteByUUID(db *sqlx.DB, UUID uuid.UUID) {
	var ruuid string

	if siteBF.TestByte(UUID.Bytes()) {
		return
	}

	row := db.QueryRow("SELECT site_uuid FROM site WHERE site_uuid = $1;", UUID.String())
	err := row.Scan(&ruuid)
	if err == nil {
		return
	} else if err == sql.ErrNoRows {
		_, err := db.Exec("INSERT INTO site (site_uuid, site_name) VALUES ($1, $2);", UUID.String(), unknownSite)
		if err != nil {
			log.Printf("insert site failed: %v\n", err)
		}

		siteBF.AddByte(UUID.Bytes())
	} else {
		log.Printf("site scan err %v\n", err)
	}
}

const infofileFmt = "%s/%s/%s/device_info.%s.pb"

func infofileFromTraining(rt RecordedTraining, lookup string) string {
	return fmt.Sprintf(infofileFmt, lookup, rt.SiteUUID, rt.DeviceMAC, rt.UnixTimestamp)
}

func infofileFromRecord(rdi RecordedInventory, lookup string) string {
	return fmt.Sprintf(infofileFmt, lookup, rdi.SiteUUID, rdi.DeviceMAC, rdi.UnixTimestamp)
}

const filepathPattern = `([0-9a-fA-F]+-[0-9a-fA-F]+-[0-9a-fA-F]+-[0-9a-fA-F]+-[0-9a-fA-F]+)/([0-9a-fA-F]+:[0-9a-fA-F]+:[0-9a-fA-F]+:[0-9a-fA-F]+:[0-9a-fA-F]+:[0-9a-fA-F]+)/device_info.(\d+).pb`

func ingestFile(B *backdrop, stats *RecordedIngest, fpath string, newer time.Time) time.Time {
	// Is a dir?
	fi, err := os.Stat(fpath)
	if err != nil {
		log.Fatalf("stat somehow failed after walk: %v", err)
	}

	if fi.IsDir() {
		bn := path.Base(fpath)

		// If the basename parses as a UUID, this component is
		// the reporting appliance UUID.
		uu, err := uuid.FromString(bn)
		if err == nil {
			insertNewSiteByUUID(B.db, uu)
			stats.NewSites++

			return newer
		}

		// If the basename parses as a MAC address, this
		// component is the device.
		_, err = net.ParseMAC(bn)
		if err != nil {
			log.Printf("directory not parseable as UUID or MAC: %s", bn)
		}

		return newer
	}

	mt := fi.ModTime()

	if mt.Before(newer) {
		return mt
	}

	// Remove prefix from fpath.
	cpath := strings.TrimPrefix(fpath, ingestDir)
	cpath = strings.TrimPrefix(cpath, "/")
	fpMatch := filepathRe.FindAllStringSubmatch(cpath, -1)
	if len(fpMatch) < 1 {
		log.Printf("path not compliant with pattern: '%s', %d", cpath, len(fpMatch))
		return newer
	}

	siteUUID := fpMatch[0][1]
	deviceMac := fpMatch[0][2]
	diTimestamp := fpMatch[0][3]

	row := B.db.QueryRowx("SELECT * FROM inventory WHERE site_uuid = $1 AND device_mac = $2 AND unix_timestamp = $3;", siteUUID, deviceMac, diTimestamp)
	ri := RecordedInventory{}
	sqlerr := row.StructScan(&ri)

	if sqlerr == nil && ri.BayesSentenceVersion == getCombinedVersion() {
		return mt
	}

	if sqlerr != sql.ErrNoRows {
		log.Printf("select inventory failed: %v", sqlerr)
		return mt
	}

	// We only want to perform the .ReadFile() when the line is
	// absent, or the sentence versions differ.
	buf, err := ioutil.ReadFile(fpath)
	if err != nil {
		log.Printf("couldn't readfile %s: %s", fpath, err)
		return newer
	}

	// Extract DHCP vendor raw string.
	di := &base_msg.DeviceInfo{}
	err = proto.Unmarshal(buf, di)
	if err != nil {
		log.Printf("unmarshal failed: %+v", err)
		return newer
	}

	_, rawVendor := extractDeviceInfoDHCP(di)
	sversion, sentence := genBayesSentenceFromDeviceInfo(B.ouidb, di)

	// The device information is either new, or the sentence
	// generator version is different.
	if sqlerr != nil {
		_, err := B.db.Exec("INSERT OR REPLACE INTO inventory (inventory_date, unix_timestamp, site_uuid, device_mac, dhcp_vendor, bayes_sentence_version, bayes_sentence) VALUES ($1, $2, $3, $4, $5, $6, $7);",
			fi.ModTime(), diTimestamp, siteUUID,
			deviceMac, rawVendor, sversion, sentence)
		if err != nil {
			log.Printf("insert inventory failed: %v\n", err)
		}
		stats.NewInventories++
	} else {
		_, err := B.db.Exec("UPDATE inventory SET bayes_sentence_version = $1, bayes_sentence = $2 WHERE site_uuid = $3 AND device_mac = $4 AND unix_timestamp = $5;", sversion, sentence, siteUUID, deviceMac, diTimestamp)
		if err != nil {
			log.Printf("update inventory failed: %v\n", err)
		}
		stats.UpdatedInventories++
	}

	return mt
}

var ingestCount int

func countFile(fpath string, newer time.Time) {
	fi, _ := os.Stat(fpath)

	if fi.IsDir() {
		return
	}

	if fi.ModTime().Before(newer) {
		return
	}
	ingestCount++
}

// TODO We want to make ingestTree more aware of the previous ingest
// state.  We use the timestamp of the last run to filter out old files,
// which should have been successfully ingested.  We only update those old
// files which have mismatches on the sentence version.
func ingestTree(B *backdrop, fname string) error {
	fmt.Print(fname, ":")

	// Part 1.  Tree walk.
	row := B.db.QueryRowx("SELECT * FROM ingest ORDER BY ingest_date DESC LIMIT 1;")

	prevStats := RecordedIngest{}
	stats := RecordedIngest{}
	err := row.StructScan(&prevStats)
	if err != nil && err != sql.ErrNoRows {
		return errors.Wrap(err, "select ingest scan failed")
	}

	ingestFile(B, &stats, fname, prevStats.IngestDate)

	ingestCount = 0

	filepath.Walk(fname, func(path string, info os.FileInfo, err error) error {
		countFile(path, prevStats.IngestDate)
		return nil
	})

	log.Printf("%d files to consider", ingestCount)

	if ingestCount < 1 {
		ingestCount = 1
	}

	uiprogress.Start()
	bar := uiprogress.AddBar(ingestCount)
	bar.PrependCompleted().AppendElapsed()
	barValid := true
	past := 0
	newest := prevStats.IngestDate

	filepath.Walk(fname, func(path string, info os.FileInfo, err error) error {
		ift := ingestFile(B, &stats, path, prevStats.IngestDate)
		if newest.Before(ift) {
			newest = ift

			barValid = bar.Incr()
			if !barValid {
				past++
				log.Printf("progress no longer valid: %d %d", bar.Current(), past)
			}
		}
		return nil
	})

	// Part 2.  Table scan for incorrect sentence versions.
	rows, err := B.db.Queryx("SELECT * FROM inventory WHERE bayes_sentence_version != $1;", getCombinedVersion())

	if err == nil {
		for rows.Next() {
			ri := RecordedInventory{}
			rows.StructScan(&ri)
			log.Printf("would update %v", ri)
		}

		rows.Close()
	} else {
		log.Printf("select inventory failed: %v", err)
	}

	// The time here should be the newest of the ModTime() values
	// we've seen.
	stats.IngestDate = newest

	_, err = B.db.Exec("INSERT INTO ingest (ingest_date, new_sites, new_inventories, updated_inventories) VALUES ($1, $2, $3, $4)", stats.IngestDate, stats.NewSites, stats.NewInventories, stats.UpdatedInventories)
	if err != nil {
		log.Printf("ingest insert failed: %v", err)
	}

	log.Printf("ingest stats %+v", stats)

	return nil
}

func init() {
	siteBF = bloom.New(10000, 500)

	filepathRe = regexp.MustCompile(filepathPattern)
}
