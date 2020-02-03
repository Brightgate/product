//
// COPYRIGHT 2020 Brightgate Inc.  All rights reserved.
//
// This copyright notice is Copyright Management Information under 17 USC 1202
// and is included to protect this work and deter copyright infringement.
// Removal or alteration of this Copyright Management Information without the
// express written permission of Brightgate Inc is prohibited, and any
// such unauthorized removal or alteration will be a violation of federal law.
//

package main

import (
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/pkg/errors"
	"github.com/satori/uuid"
)

const (
	infofileFmt     = "%s/%s/%s/device_info.%s.pb"
	filepathPattern = `([[:xdigit:]]+-[[:xdigit:]]+-[[:xdigit:]]+-[[:xdigit:]]+-[[:xdigit:]]+)/([[:xdigit:]]+:[[:xdigit:]]+:[[:xdigit:]]+:[[:xdigit:]]+:[[:xdigit:]]+:[[:xdigit:]]+)/device_info.(\d+).pb`
)

type fileIngester struct {
	ingestDir       string
	selectedUUIDs   map[uuid.UUID]bool
	prevIngestTimes map[uuid.UUID]time.Time
	ingestRecords   map[uuid.UUID]*RecordedIngest
	// Need to pick up this lock to access ingestRecords
	ingestRecordsLock sync.Mutex
}

func (f *fileIngester) SiteExists(B *backdrop, siteUUID string) (bool, error) {
	ud := fmt.Sprintf("%s/%s", f.ingestDir, siteUUID)
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
	fn := fmt.Sprintf(infofileFmt, f.ingestDir, siteUUID, deviceMac, unixTimestamp)

	return os.Open(fn)
}

func (f *fileIngester) ingestFile(B *backdrop, fpath string, info os.FileInfo) error {
	// Remove prefix from fpath.
	cpath := strings.TrimPrefix(fpath, f.ingestDir)
	cpath = strings.TrimPrefix(cpath, "/")
	fpMatch := filepathRe.FindAllStringSubmatch(cpath, -1)
	if len(fpMatch) < 1 {
		return errors.Errorf("path not compliant with pattern: %q, %d", cpath, len(fpMatch))
	}

	uuStr := fpMatch[0][1]
	deviceMAC := fpMatch[0][2]
	diTimestamp := fpMatch[0][3]

	siteUUID, err := uuid.FromString(uuStr)
	if err != nil {
		return errors.Wrapf(err, "Couldn't get site UUID from %s", cpath)
	}
	// Weed out any sites we're not interested in; mostly this should get
	// handled when we weed out sites by directory name, but this is here
	// as a double-check.
	if f.selectedUUIDs != nil && !f.selectedUUIDs[siteUUID] {
		return nil
	}
	if !info.ModTime().After(f.prevIngestTimes[siteUUID]) {
		return nil
	}

	f.ingestRecordsLock.Lock()
	ingestRecord := f.ingestRecords[siteUUID]
	if ingestRecord == nil {
		ingestRecord = &RecordedIngest{}
		f.ingestRecords[siteUUID] = ingestRecord
	}
	f.ingestRecordsLock.Unlock()

	slog.Debugf("%s: starting DeviceInfo %s %s", siteUUID, deviceMAC, diTimestamp)
	// XXX We only want to perform the .ReadFile() when the line is
	// absent, or the sentence versions differ.
	fd, err := os.Open(fpath)
	if err != nil {
		return errors.Wrapf(err, "open %q failed: %v", fpath, err)
	}
	defer fd.Close()

	inventoryRecord := RecordedInventory{
		Storage:       "files",
		InventoryDate: info.ModTime(),
		UnixTimestamp: diTimestamp,
		SiteUUID:      siteUUID.String(),
		DeviceMAC:     deviceMAC,
	}

	err = inventoryRecord.addInfoFromReader(B.ouidb, fd)
	if err != nil {
		return errors.Wrapf(err, "couldn't add info to inventory %v: %v", inventoryRecord, err)
	}

	slog.Debugf("%s: recording %v", siteUUID, inventoryRecord)
	err = recordInventory(B.db, ingestRecord, &inventoryRecord)
	if err != nil {
		slog.Fatalf("couldn't record inventory: %v", err)
	}
	slog.Debugf("%s: finished work on %s %s", siteUUID, deviceMAC, diTimestamp)
	return nil
}

func (f *fileIngester) walk(B *backdrop) error {
	newSites := 0
	werr := filepath.Walk(f.ingestDir, func(fpath string, info os.FileInfo, err error) error {
		if err != nil {
			slog.Warnf("got error from filepath.Walk: %v", err)
			return err
		}
		// The structure is <...stuff.../uuid/macaddr/record.pb>
		if info.IsDir() {
			// If the basename parses as a UUID, this component is
			// the reporting site's UUID.
			siteUUID, err := uuid.FromString(info.Name())
			if err == nil {
				if f.selectedUUIDs != nil && !f.selectedUUIDs[siteUUID] {
					// Forces filepath.Walk() to skip this dir
					return filepath.SkipDir
				}
				newSites += insertNewSiteByUUID(B.db, siteUUID)
				return nil
			}

			// If the basename parses as a MAC address, this component is the device.
			_, err = net.ParseMAC(info.Name())
			if err != nil {
				// This directory isn't a site or a macaddr, but it may contain those,
				// so keep looking.
				slog.Warnf("directory %q not parseable as UUID or MAC: %s", info.Name(), err)
				return nil
			}
			return nil
		}

		// Now handle individual files we encounter on our walk
		err = f.ingestFile(B, fpath, info)
		if err != nil {
			// Report on errors but keep going.
			slog.Warnf("Couldn't ingest %s: %v", fpath, err)
		}
		return nil
	})
	slog.Infof("discovered %d new sites", newSites)
	return werr
}

func (f *fileIngester) Ingest(B *backdrop, selectedUUIDs map[uuid.UUID]bool) error {
	var err error
	fmt.Print(f.ingestDir, ":")

	f.selectedUUIDs = selectedUUIDs
	f.ingestRecords = make(map[uuid.UUID]*RecordedIngest)
	f.prevIngestTimes, err = getSiteIngestTimes(B.db)
	if err != nil {
		return err
	}

	err = f.walk(B)
	if err != nil {
		return errors.Wrap(err, "filesystem walk failed")
	}

	// Write out all of the site ingest records
	for siteUUID, record := range f.ingestRecords {
		record.SiteUUID = siteUUID.String()
		if record.NewInventories != 0 {
			err = insertSiteIngest(B.db, record)
			if err != nil {
				slog.Fatalf("Failed to insert ingest record %v: %v", record, err)
			} else {
				slog.Debugf("recorded ingest %v", record)
			}
		}
	}

	slog.Infof("finished ingest")
	return nil
}

func newFileIngester(ingestDir string) *fileIngester {
	return &fileIngester{ingestDir: ingestDir}
}
