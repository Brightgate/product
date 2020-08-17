/*
 * Copyright 2020 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package main

import (
	"bg/cl-obs/extract"
	"bg/cl_common/deviceinfo"
	"context"
	"fmt"
	"os"
	"regexp"
	"time"

	"cloud.google.com/go/storage"
	"github.com/pkg/errors"
	"github.com/satori/uuid"
	"golang.org/x/sync/semaphore"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
)

const (
	bucketPattern = `bg-appliance-data-(?P<uuid>[a-f0-9-]*)`
	objectPattern = `obs/(?P<mac>[a-f0-9:]*)/device_info.(?P<ts>[0-9]*).pb`

	bucketPrefix          = "bg-appliance-data-"
	progressEveryNObjects = 1000
)

var (
	bucketRE = regexp.MustCompile(bucketPattern)
	objectRE = regexp.MustCompile(objectPattern)
)

type cloudIngester struct {
	project              string
	storageClient        *storage.Client
	bucketWorkers        int64
	objectWorkersPerSite int64
	allObjectWorkers     *semaphore.Weighted
}

func (c *cloudIngester) ingestSiteBucket(B *backdrop, siteUUID uuid.UUID,
	prevIngestTime time.Time, bucketName string) error {

	bucket := c.storageClient.Bucket(bucketName)
	q := storage.Query{Prefix: "obs/"}
	if err := q.SetAttrSelection([]string{"Name", "Updated"}); err != nil {
		return errors.Wrap(err, "setting up GCS query")
	}

	ingestStats := RecordedIngest{
		SiteUUID:   siteUUID.String(),
		IngestDate: prevIngestTime,
	}
	slog.Infof("start bucket: %s", bucketName)
	slog.Debugf("previous cursor: %s", prevIngestTime.Format(time.RFC3339Nano))
	slog.Debugf("ingest stats %v", &ingestStats)

	startOtherSentenceV, err := countOtherSentenceVersions(B.db, siteUUID, extract.CombinedVersion)
	if err != nil {
		return errors.Wrap(err, "checking for old sentences")
	}
	if startOtherSentenceV > 0 {
		slog.Warnf("%s: old/version-mismatched sentences were detected.  Will do a full reingest.", siteUUID)
		// Zero out the prevIngestTime to force full refresh
		prevIngestTime = time.Time{}
	} else {
		slog.Debugf("%s: zero old/version-mismatched sentences were detected.", siteUUID)
	}

	// controls the worker goroutines for this bucket
	objectIngestSem := semaphore.NewWeighted(c.objectWorkersPerSite)

	nObjs := 0
	ingest := 0
	skipped := 0
	objs := bucket.Objects(context.Background(), &q)
	for {
		// We track and report stats using the loop iterations, (i.e. synchronously)
		// even though some of the ingest proceeds async.
		if nObjs > 0 && nObjs%progressEveryNObjects == 0 {
			slog.Infof("%s: ingested %d of %d examined objects (%d skips)", siteUUID, ingest, nObjs, skipped)
		}
		oattrs, err := objs.Next()
		if err == iterator.Done {
			break
		}
		nObjs++

		// If before, or same, we've already got this one
		if !oattrs.Updated.After(prevIngestTime) {
			skipped++
			continue
		}

		om := objectRE.FindAllStringSubmatch(oattrs.Name, -1)
		if om == nil {
			slog.Warnf("object '%s' doesn't match pattern", oattrs.Name)
			continue
		}

		tuple, err := deviceinfo.NewTupleFromStrings(siteUUID.String(), om[0][1], om[0][2])
		if err != nil {
			slog.Fatalf("error building tuple: %v", err)
		}

		if err := objectIngestSem.Acquire(context.TODO(), 1); err != nil {
			slog.Fatalf("error getting objectIngest semaphore: %v", err)
		}
		if err := c.allObjectWorkers.Acquire(context.TODO(), 1); err != nil {
			slog.Fatalf("error getting allObjectWorkers semaphore: %v", err)
		}
		ingest++

		go func() {
			defer objectIngestSem.Release(1)
			defer c.allObjectWorkers.Release(1)
			slog.Debugf("%s: starting DeviceInfo %s", siteUUID, tuple)

			di, err := B.store.ReadTuple(context.Background(), tuple)
			if err != nil {
				slog.Errorf("couldn't get DeviceInfo %s: %v", tuple, err)
				return
			}

			err = RecordInventory(B.db, B.ouidb,
				B.store, tuple, oattrs.Updated, di, &ingestStats)
			if err != nil {
				slog.Fatalf("couldn't record inventory %s: %v", tuple, err)
			}
			slog.Debugf("%s: finished DeviceInfo %s", siteUUID, tuple)
		}()
	}
	// Wait for all workers to finish
	_ = objectIngestSem.Acquire(context.TODO(), c.objectWorkersPerSite)
	slog.Infof("%s: ingested %d of %d examined objects (%d skips) [done]", siteUUID, ingest, nObjs, skipped)

	if ingestStats.NewInventories != 0 {
		// Record the results of the ingest.
		err = insertSiteIngest(B.db, &ingestStats)
		if err != nil {
			slog.Fatalf("insert Site Ingest %v failed: %v", &ingestStats, err)
		} else {
			slog.Debugf("recorded ingest: %v", &ingestStats)
		}
	}

	// We re-count the non-matching sentences here, in order to see if
	// there are some which, despite the re-import, are still from other
	// versions.  This could happen if the population of ingestable
	// records changes from run to run (i.e. one or more got deleted),
	// leaving an unfixable sentence.
	endOtherSentenceV, err := countOtherSentenceVersions(B.db, siteUUID,
		extract.CombinedVersion)
	if err != nil {
		return errors.Wrap(err, "checking for old sentences")
	}
	if endOtherSentenceV > 0 {
		slog.Warnf("%s: after reingest %d old/version-mismatched sentences were seen (%d at start). Purging.",
			siteUUID, endOtherSentenceV, startOtherSentenceV)
		err = removeOtherSentenceVersions(B.db, siteUUID, extract.CombinedVersion)
		if err != nil {
			return errors.Wrap(err, "removing old sentences")
		}
	}
	slog.Infof("end bucket: %s", bucketName)
	return nil
}

func (c *cloudIngester) Ingest(B *backdrop, selectedUUIDs map[uuid.UUID]bool) error {
	slog.Debugf("backdrop: %+v", B)

	cenv := os.Getenv(googleCredentialsEnvVar)
	if cenv == "" {
		return fmt.Errorf("Provide cloud credentials through %s envvar",
			googleCredentialsEnvVar)
	}

	bkts := c.storageClient.Buckets(context.Background(), c.project)
	bkts.Prefix = bucketPrefix

	prevIngestTimes, err := getSiteIngestTimes(B.db)
	if err != nil {
		return err
	}
	newSites := 0

	bucketIngestSem := semaphore.NewWeighted(c.bucketWorkers)

	slog.Infof("begin bucket walk")
	// Walk the set of buckets
	for {
		battrs, err := bkts.Next()
		if err == iterator.Done {
			break
		} else if err != nil {
			return errors.Wrap(err, "bkts next")
		}

		bm := bucketRE.FindAllStringSubmatch(battrs.Name, -1)
		if bm == nil {
			slog.Warnf("bucket '%s' doesn't match pattern", battrs.Name)
			continue
		}
		siteUUID := uuid.Must(uuid.FromString(bm[0][1]))

		// This is awkward because we're walking buckets, and matching to
		// passed-in sites.  In the future a better strategy is to get the
		// sites/buckets from the appliancedb.
		if selectedUUIDs != nil && !selectedUUIDs[siteUUID] {
			continue
		}

		// XXX consider expunging all of the "newsite-ness tracking"?
		// XXX also error semantics here are weird
		newSites += insertNewSiteByUUID(B.db, siteUUID)

		if err := bucketIngestSem.Acquire(context.TODO(), 1); err != nil {
			slog.Fatalf("couldn't acquire semaphore: %v", err)
		}

		go func() {
			defer bucketIngestSem.Release(1)
			// Ingest the bucket.
			err = c.ingestSiteBucket(B, siteUUID, prevIngestTimes[siteUUID], battrs.Name)
			if err != nil {
				slog.Errorf("failed ingesting bucket %s", battrs.Name)
			}
		}()
	}
	// Make sure all workers are done.
	_ = bucketIngestSem.Acquire(context.TODO(), c.bucketWorkers)

	slog.Infof("Discovered %d new sites", newSites)
	return nil
}

func newCloudIngester(project string, workers int) (*cloudIngester, error) {
	cenv := os.Getenv(googleCredentialsEnvVar)
	if cenv == "" {
		return nil, fmt.Errorf("Provide cloud credentials through %s envvar",
			googleCredentialsEnvVar)
	}
	storageClient, err := storage.NewClient(context.Background(),
		option.WithCredentialsFile(cenv))
	if err != nil {
		return nil, errors.Wrap(err, "storage client")
	}

	var bucketWorkers int64
	var objectWorkers int64
	var totalWorkers int64

	if workers == 0 {
		bucketWorkers = 25
		objectWorkers = 25
		totalWorkers = 200
	} else if workers <= 4 {
		bucketWorkers = 1
		objectWorkers = 1
		totalWorkers = 1
	} else {
		bucketWorkers = int64(workers / 4)
		objectWorkers = int64(workers / 4)
		totalWorkers = int64(workers)
	}
	return &cloudIngester{
		project:              project,
		bucketWorkers:        bucketWorkers,
		objectWorkersPerSite: objectWorkers,
		allObjectWorkers:     semaphore.NewWeighted(totalWorkers),
		storageClient:        storageClient,
	}, nil
}

