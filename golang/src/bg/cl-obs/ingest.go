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
	"io/ioutil"
	"log"
	"regexp"
	"time"

	"bg/base_msg"

	"github.com/golang/protobuf/proto"
	"github.com/jmoiron/sqlx"
	"github.com/satori/uuid"
	"github.com/yourbasic/bloom"
)

var (
	siteBF     *bloom.Filter
	filepathRe *regexp.Regexp
)

func getContentStatusFromReader(rdr io.Reader) string {
	entityPresent := "-"
	dhcpPresent := "-"
	dnsRecordsPresent := "-"
	networkScanPresent := "-"

	buf, err := ioutil.ReadAll(rdr)
	if err != nil {
		log.Printf("could not read: %v\n", err)
		return "????"
	}

	di := &base_msg.DeviceInfo{}
	err = proto.Unmarshal(buf, di)
	if err != nil {
		log.Printf("could not unmarshal content: %v\n", err)
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

func insertNewSiteByUUID(db *sqlx.DB, UUID uuid.UUID) int {
	var ruuid string

	if siteBF.TestByte(UUID.Bytes()) {
		// Already present.
		return 0
	}

	row := db.QueryRow("SELECT site_uuid FROM site WHERE site_uuid = $1;", UUID.String())
	err := row.Scan(&ruuid)
	if err == nil {
		// Already present.
		return 0
	} else if err == sql.ErrNoRows {
		_, err := db.Exec("INSERT INTO site (site_uuid, site_name) VALUES ($1, $2);", UUID.String(), unknownSite)
		if err != nil {
			log.Printf("insert site failed: %v\n", err)
		}

		siteBF.AddByte(UUID.Bytes())
		return 1
	}

	log.Printf("site scan err %v\n", err)
	// No addition due to error.
	return 0
}

func readerFromTraining(B *backdrop, rt RecordedTraining) (io.Reader, error) {
	return B.ingester.DeviceInfoOpen(B, rt.SiteUUID, rt.DeviceMAC, rt.UnixTimestamp)
}

func readerFromRecord(B *backdrop, rdi RecordedInventory) (io.Reader, error) {
	return B.ingester.DeviceInfoOpen(B, rdi.SiteUUID, rdi.DeviceMAC, rdi.UnixTimestamp)
}

func inventoryFromTraining(B *backdrop, rt RecordedTraining) (*RecordedInventory, error) {
	row := B.db.QueryRowx("SELECT * FROM inventory WHERE site_uuid = $1 AND device_mac = $2 AND unix_timestamp = $3;", rt.SiteUUID, rt.DeviceMAC, rt.UnixTimestamp)
	ri := RecordedInventory{}
	sqlerr := row.StructScan(&ri)

	if sqlerr != nil {
		return nil, sqlerr
	}

	return &ri, nil
}

type ingestReaderSupport struct {
	newRecord       bool
	storage         string
	modTime         time.Time
	diTimestamp     string
	siteUUID        string
	deviceMac       string
	rawVendor       string
	sentenceVersion string
	sentence        string
}

func ingestFromReader(B *backdrop, stats *RecordedIngest, rdr io.Reader, newer time.Time, support ingestReaderSupport) time.Time {
	buf, err := ioutil.ReadAll(rdr)
	if err != nil {
		log.Printf("couldn't ReadAll %v: %s", rdr, err)
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
	if support.newRecord {
		_, err := B.db.Exec("INSERT OR REPLACE INTO inventory (storage, inventory_date, unix_timestamp, site_uuid, device_mac, dhcp_vendor, bayes_sentence_version, bayes_sentence) VALUES ($1, $2, $3, $4, $5, $6, $7, $8);",
			support.storage, support.modTime,
			support.diTimestamp, support.siteUUID,
			support.deviceMac, rawVendor, sversion,
			sentence.toString())
		if err != nil {
			log.Printf("insert inventory failed: %v\n", err)
		} else {
			stats.NewInventories++
		}
	} else {
		_, err := B.db.Exec("UPDATE inventory SET storage = $1, bayes_sentence_version = $2, bayes_sentence = $3 WHERE site_uuid = $4 AND device_mac = $5 AND unix_timestamp = $6;",
			support.storage, sversion, sentence.toString(),
			support.siteUUID, support.deviceMac, support.diTimestamp)
		if err != nil {
			log.Printf("update inventory failed: %v\n", err)
		} else {
			stats.UpdatedInventories++
		}
	}

	return support.modTime
}
func init() {
	siteBF = bloom.New(10000, 500)

	filepathRe = regexp.MustCompile(filepathPattern)
}
