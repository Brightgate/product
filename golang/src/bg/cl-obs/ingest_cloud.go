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
	"context"
	"database/sql"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"

	"cloud.google.com/go/storage"
	"github.com/pkg/errors"
	"github.com/satori/uuid"
	"google.golang.org/api/iterator"
)

const (
	bucketFmt     = "bg-appliance-data-%s"
	bucketPattern = `bg-appliance-data-(?P<uuid>[a-f0-9-]*)`

	objectFmt     = "obs/%s/device_info.%s.pb"
	objectPattern = `obs/(?P<mac>[a-f0-9:]*)/device_info.(?P<ts>[0-9]*).pb`

	//	IngestProject = "staging-168518"
	bucketPrefix = "bg-appliance-data-"
)

type cloudIngester struct {
	project       string
	storageClient *storage.Client
}

func (c *cloudIngester) SiteExists(B *backdrop, siteUUID string) (bool, error) {
	if c.storageClient == nil {
		log.Fatalf("storage client not properly initialized")
	}

	bn := fmt.Sprintf(bucketFmt, siteUUID)
	b := c.storageClient.Bucket(bn)
	_, err := b.Attrs(context.Background())
	if err != nil {
		if err == storage.ErrObjectNotExist {
			return false, nil
		}
		return false, err
	}

	return true, nil
}

func (c *cloudIngester) DeviceInfoOpen(B *backdrop, siteUUID string, deviceMac string, unixTimestamp string) (io.Reader, error) {
	if c.storageClient == nil {
		log.Fatalf("storage client not properly initialized")
	}

	bn := fmt.Sprintf(bucketFmt, siteUUID)
	on := fmt.Sprintf(objectFmt, deviceMac, unixTimestamp)

	b := c.storageClient.Bucket(bn)
	o := b.Object(on)

	return o.NewReader(context.Background())
}

func (c *cloudIngester) Ingest(B *backdrop) error {
	log.Printf("backdrop: %+v", B)

	// How to log in, again?
	cenv := os.Getenv(googleCredentialsEnvVar)
	if cenv == "" {
		return fmt.Errorf("Provide cloud credentials through %s envvar",
			googleCredentialsEnvVar)
	}

	BucketRE := regexp.MustCompile(bucketPattern)
	ObjectRE := regexp.MustCompile(objectPattern)

	bkts := c.storageClient.Buckets(context.Background(), c.project)
	bkts.Prefix = bucketPrefix

	q := storage.Query{Prefix: "obs/"}

	// stats, newer should be initialized here.
	// Part 1.  Tree walk.
	row := B.db.QueryRowx("SELECT * FROM ingest ORDER BY ingest_date DESC LIMIT 1;")

	prevStats := RecordedIngest{}
	stats := RecordedIngest{}
	err := row.StructScan(&prevStats)
	if err != nil && err != sql.ErrNoRows {
		return errors.Wrap(err, "select ingest scan failed")
	}

	newer := prevStats.IngestDate
	log.Printf("ingest objects after %v", newer)
	newest := newer

	for {
		battrs, err := bkts.Next()
		if err == iterator.Done {
			break
		} else if err != nil {
			return errors.Wrap(err, "bkts next")
		}

		bm := BucketRE.FindAllStringSubmatch(battrs.Name, -1)
		if bm == nil {
			log.Printf("bucket '%s' doesn't match pattern", battrs.Name)
			continue
		}

		siteUUID := bm[0][1]
		uu, err := uuid.FromString(siteUUID)
		if err != nil {
			continue
		}

		stats.NewSites += insertNewSiteByUUID(B.db, uu)

		bucket := c.storageClient.Bucket(battrs.Name)

		objs := bucket.Objects(context.Background(), &q)

		for {
			oattrs, err := objs.Next()
			if err == iterator.Done {
				break
			}

			if oattrs.Updated.Before(newer) {
				continue
			}

			om := ObjectRE.FindAllStringSubmatch(oattrs.Name, -1)
			if om == nil {
				log.Printf("object '%s' doesn't match pattern", oattrs.Name)
				continue
			}

			deviceMac := om[0][1]
			diTimestamp := om[0][2]

			ordr, err := bucket.Object(oattrs.Name).NewReader(context.Background())

			objSupport := ingestReaderSupport{
				storage: "cloud",
				// XXX In principle, we could see an
				// update for a deviceInfo object.
				newRecord:   true,
				siteUUID:    siteUUID,
				deviceMac:   deviceMac,
				diTimestamp: diTimestamp,
				modTime:     oattrs.Updated,
			}

			rt := ingestFromReader(B, &stats, ordr, newest, objSupport)

			// rt is the returned time.  We will
			// want to update the ingest cache value
			// to the maximum rt we receive.  rt >=
			// newer by definition.
			if newest.Before(rt) {
				newest = rt
			}
		}

		log.Printf("stats bucket %s: %+v", battrs.Name, stats)
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

func newCloudIngester(project string) *cloudIngester {
	return &cloudIngester{project: project}
}
