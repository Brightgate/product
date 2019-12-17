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
	"io"
	"log"
	"net"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/satori/uuid"
)

const (
	infofileFmt     = "%s/%s/%s/device_info.%s.pb"
	filepathPattern = `([[:xdigit:]]+-[[:xdigit:]]+-[[:xdigit:]]+-[[:xdigit:]]+-[[:xdigit:]]+)/([[:xdigit:]]+:[[:xdigit:]]+:[[:xdigit:]]+:[[:xdigit:]]+:[[:xdigit:]]+:[[:xdigit:]]+)/device_info.(\d+).pb`
)

type fileIngester struct {
	fName string
}

func (f *fileIngester) SiteExists(B *backdrop, siteUUID string) (bool, error) {
	ud := fmt.Sprintf("%s/%s", f.fName, siteUUID)
	us, err := os.Stat(ud)

	if err == nil && us.IsDir() {
		return true, nil
	}

	if err != nil && os.IsNotExist(err) {
		return false, nil
	}

	return false, err
}

func (f *fileIngester) DeviceInfoOpen(B *backdrop, siteUUID string, deviceMac string, unixTimestamp string) (io.Reader, error) {
	fn := fmt.Sprintf(infofileFmt, ingestDir, siteUUID, deviceMac, unixTimestamp)

	return os.Open(fn)
}

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
			stats.NewSites += insertNewSiteByUUID(B.db, uu)

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
	fd, err := os.Open(fpath)
	if err != nil {
		log.Printf("open failed %s: %v", fpath, err)
		return newer
	}

	defer fd.Close()

	fileSupport := ingestReaderSupport{
		storage:     "files",
		deviceMac:   deviceMac,
		siteUUID:    siteUUID,
		diTimestamp: diTimestamp,
		modTime:     mt,
	}

	return ingestFromReader(B, stats, fd, newer, fileSupport)
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

func (f *fileIngester) Ingest(B *backdrop) error {
	fmt.Print(f.fName, ":")

	// Part 1.  Tree walk.
	row := B.db.QueryRowx("SELECT * FROM ingest ORDER BY ingest_date DESC LIMIT 1;")

	prevStats := RecordedIngest{}
	stats := RecordedIngest{}
	err := row.StructScan(&prevStats)
	if err != nil && err != sql.ErrNoRows {
		return errors.Wrap(err, "select ingest scan failed")
	}

	ingestFile(B, &stats, f.fName, prevStats.IngestDate)

	ingestCount = 0

	filepath.Walk(f.fName, func(path string, info os.FileInfo, err error) error {
		countFile(path, prevStats.IngestDate)
		return nil
	})

	log.Printf("%d files to consider", ingestCount)

	if ingestCount < 1 {
		ingestCount = 1
	}

	newest := prevStats.IngestDate

	filepath.Walk(f.fName, func(path string, info os.FileInfo, err error) error {
		ift := ingestFile(B, &stats, path, prevStats.IngestDate)
		if newest.Before(ift) {
			newest = ift
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

func newFileIngester(fname string) *fileIngester {
	return &fileIngester{fName: fname}
}
