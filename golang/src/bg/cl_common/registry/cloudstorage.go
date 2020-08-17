/*
 * Copyright 2019 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package registry

import (
	"context"
	"math/rand"
	"net/http"
	"regexp"
	"strings"
	"time"

	"cloud.google.com/go/storage"
	"github.com/pkg/errors"
	"github.com/satori/uuid"
	"google.golang.org/api/googleapi"

	"bg/cloud_models/appliancedb"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

const bktPrefix = "bg-appliance-data-"

func mkRandString(n uint) string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	bytes := make([]byte, n)
	for i := uint(0); i < n; i++ {
		bytes[i] = letters[rand.Intn(len(letters))]
	}
	return string(bytes)
}

// Cleanup GCS bucket label values, as per the spec in
// https://cloud.google.com/storage/docs/key-terms#bucket-labels
// - Keys and values cannot be longer than 63 characters each.
// - Keys and values can only contain lowercase letters, numeric characters, underscores, and dashes. International characters are allowed.
var labelRE = regexp.MustCompile("[^-a-z0-9]")

func cleanLabelValue(lv string) string {
	lv = strings.ToLower(lv)
	lv = string(labelRE.ReplaceAll([]byte(lv), []byte("_")))
	if len(lv) > 63 {
		lv = lv[0:62]
	}
	return lv
}

func newBucket(ctx context.Context, db appliancedb.DataStore,
	hostProject string, site *appliancedb.CustomerSite) (*appliancedb.SiteCloudStorage, error) {
	var bktName string
	var err error

	storageClient, err := storage.NewClient(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "preparing to make bucket; failed to make storage client")
	}
	organization, err := db.OrganizationByUUID(ctx, site.OrganizationUUID)
	if err != nil {
		return nil, errors.Wrapf(err, "preparing to make bucket; failed getting org %v", site.OrganizationUUID)
	}

	// See if the bucket was created already and we seem to have the right
	// to access it.  If yes, update its attributes and return it so that
	// the database can be updated.
	// This case should be rare, but helps handle manually provisioned
	// buckets.
	bktName = bktPrefix + site.UUID.String()
	bkt := storageClient.Bucket(bktName)
	_, err = bkt.Attrs(ctx)
	if err != nil {
		// The first time around, just try the appliance name; if something goes wrong,
		// try again with a different name.
		for suffix := ""; ; suffix = "-" + mkRandString(8) {
			bktName = bktPrefix + site.UUID.String() + suffix
			bkt = storageClient.Bucket(bktName)
			err = bkt.Create(ctx, hostProject, nil)
			if err == nil {
				break
			}
			e, ok := err.(*googleapi.Error)
			if !ok {
				return nil, errors.Wrap(err, "Failed to create bucket, unknown error")
			}
			// See https://cloud.google.com/storage/docs/json_api/v1/status-codes
			// and https://godoc.org/cloud.google.com/go/storage
			// Note that backoff is handled by the library for retryable errors.
			switch e.Code {
			case http.StatusConflict:
				// HTTP 409 -- conflict-- means the bucket already
				// exists; we don't know if we previously created it,
				// or if someone else has claimed it.  More logic could
				// be implemented here to search and reclaim orphaned
				// buckets, but it seems like overkill for now.
				continue
			default:
				return nil, errors.Wrap(err, "Failed to create bucket, unexpected http error")
			}
		}
	}

	uattr := storage.BucketAttrsToUpdate{}
	// These are intended to be informational, and an aid to debugging.  The set of
	// allowed characters in labels and values is very limited.
	uattr.SetLabel("site_uuid", cleanLabelValue(site.UUID.String()))
	uattr.SetLabel("site_name", cleanLabelValue(site.Name))
	uattr.SetLabel("org_name", cleanLabelValue(organization.Name))
	_, err = bkt.Update(ctx, uattr)
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to update bucket attrs to %#v", uattr)
	}

	cloudStor := &appliancedb.SiteCloudStorage{
		Bucket:   bktName,
		Provider: "gcs",
	}
	return cloudStor, nil
}

// GetCloudStorage returns the cloud storage record for the given site UUID.
// Additionally, it takes the appliance UUID in order to do some additional
// safety checks.
func GetCloudStorage(ctx context.Context, db appliancedb.DataStore, hostProject string,
	applianceUUID, siteUUID uuid.UUID) (*appliancedb.SiteCloudStorage, error) {
	// An unsolved problem here is how to manage appliances which move from
	// one GCP project to another; the bucket namespace is global but the
	// new GCP project won't have access rights to the old bucket.  For now
	// we simply fail if there is a mismatch between what is in the
	// registry and the storage client's Project ID.  This helps to prevent
	// weird cases from happening in the first place, but the code is still
	// fragile if things become misaligned.
	applianceID, err := db.ApplianceIDByUUID(ctx, applianceUUID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get ApplianceID")
	}
	if hostProject != applianceID.GCPProject {
		return nil, errors.Errorf("Appliance Project (%s) != Host Project (%s)",
			applianceID.GCPProject, hostProject)
	}
	return db.CloudStorageByUUID(ctx, siteUUID)
}

