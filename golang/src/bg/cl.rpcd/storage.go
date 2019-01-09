/*
 * COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 *
 */

package main

import (
	"context"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"bg/cloud_models/appliancedb"
	"bg/cloud_rpc"
	"bg/common/archive"

	"github.com/pkg/errors"
	"github.com/satori/uuid"

	"cloud.google.com/go/storage"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/googleapi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type cloudStorageServer struct {
	serviceID     string
	privateKey    []byte
	projectID     string
	storageClient *storage.Client
	applianceDB   appliancedb.DataStore
}

func init() {
	rand.Seed(time.Now().UnixNano())
}

func defaultCloudStorageServer(applianceDB appliancedb.DataStore) *cloudStorageServer {
	ctx := context.Background()
	creds, _ := google.FindDefaultCredentials(ctx, storage.ScopeFullControl)
	if creds == nil {
		slog.Fatalf("no cloud credentials defined")
	}

	jwt, err := google.JWTConfigFromJSON(creds.JSON)
	if err != nil {
		slog.Fatalf("bad cloud credentials: %v", err)
	}

	client, err := storage.NewClient(context.Background())
	if err != nil {
		slog.Fatalf("failed to make storage client")
	}
	return newCloudStorageServer(client, jwt.Email, creds.ProjectID, jwt.PrivateKey, applianceDB)
}

func newCloudStorageServer(client *storage.Client, serviceID string, projectID string, privateKey []byte, appliancedb appliancedb.DataStore) *cloudStorageServer {
	c := &cloudStorageServer{
		serviceID:     serviceID,
		privateKey:    privateKey,
		projectID:     projectID,
		storageClient: client,
		applianceDB:   appliancedb,
	}
	return c
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

func (cs *cloudStorageServer) makeBucket(ctx context.Context, applianceID *appliancedb.ApplianceID) (string, error) {
	var bktName string
	var bkt *storage.BucketHandle
	var err error

	// See if the bucket was created already and we seem to have the right
	// to access it.  If yes, update its attributes and return it so that
	// the database can be updated.
	// This case should be rare, but helps handle manually provisioned
	// buckets.
	bktName = bktPrefix + applianceID.SiteUUID.String()
	bkt = cs.storageClient.Bucket(bktName)
	_, err = bkt.Attrs(ctx)
	if err != nil {
		// The first time around, just try the appliance name; if something goes wrong,
		// try again with a different name.
		for suffix := ""; ; suffix = "-" + mkRandString(8) {
			bktName = bktPrefix + applianceID.SiteUUID.String() + suffix
			bkt = cs.storageClient.Bucket(bktName)
			err = bkt.Create(ctx, cs.projectID, nil)
			if err == nil {
				break
			}
			e, ok := err.(*googleapi.Error)
			if !ok {
				return "", errors.Wrap(err, "Failed to create bucket, unknown error")
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
				return "", errors.Wrap(err, "Failed to create bucket, unexpected http error")
			}
		}
	}

	uattr := storage.BucketAttrsToUpdate{}
	// These are intended to be informational, and an aid to debugging.  The set of
	// allowed characters in labels and values are very restrictive.
	uattr.SetLabel("appliance_reg_id", strings.ToLower(applianceID.ApplianceRegID))
	uattr.SetLabel("site_uuid", applianceID.SiteUUID.String())
	_, err = bkt.Update(ctx, uattr)
	if err != nil {
		return "", errors.Wrap(err, "Failed to update bucket attrs")
	}

	cloudStor := &appliancedb.SiteCloudStorage{
		Bucket:   bktName,
		Provider: "gcs",
	}
	if err = cs.applianceDB.UpsertCloudStorage(ctx, applianceID.SiteUUID, cloudStor); err != nil {
		return "", errors.Wrap(err, "Failed to upsert CloudStorage record")
	}
	return bktName, nil
}

// If use of this becomes more widespread, the next step is to move this
// function into a package of its own, or into appliancedb code.
func (cs *cloudStorageServer) GetBucketName(ctx context.Context, siteUUID uuid.UUID) (string, error) {
	// An unsolved problem here is how to manage appliances which move from
	// one GCP project to another; the bucket namespace is global but the
	// new GCP project won't have access rights to the old bucket.  For now
	// we simply fail if there is a mismatch between what is in the
	// registry and the storage client's Project ID.  This helps to prevent
	// weird cases from happening in the first place, but the code is still
	// fragile if things become misaligned.
	applianceID, err := cs.applianceDB.ApplianceIDByUUID(ctx, siteUUID)
	if err != nil {
		return "", err
	}
	if cs.projectID != applianceID.GCPProject {
		return "", errors.Errorf("Appliance Project (%s) != Storage Project (%s)",
			applianceID.GCPProject, cs.projectID)
	}

	cloudStor, err := cs.applianceDB.CloudStorageByUUID(ctx, siteUUID)
	if err == nil {
		return cloudStor.Bucket, nil
	}

	_, ok := err.(appliancedb.NotFoundError)
	if !ok {
		return "", errors.Wrap(err, "GetBucketName: unexpected failure")
	}
	// Else, go make the bucket
	return cs.makeBucket(ctx, applianceID)
}

func (cs *cloudStorageServer) GenerateURL(ctx context.Context, req *cloud_rpc.GenerateURLRequest) (*cloud_rpc.GenerateURLResponse, error) {
	_, slog := endpointLogger(ctx)
	slog.Debugw("incoming URL request", "req", req)

	siteUUID, err := getSiteUUID(ctx, false)
	if err != nil {
		return nil, err
	}

	exp := time.Now().Add(10 * time.Minute)

	resp := cloud_rpc.GenerateURLResponse{
		Urls: make([]*cloud_rpc.SignedURL, 0),
	}

	// Lookup (or create) bucket for appliance using site UUID
	bucket, err := cs.GetBucketName(ctx, siteUUID)
	if err != nil {
		slog.Errorf("GenerateURL: couldn't get bucket for %s: %v", siteUUID, err)
		return nil, status.Errorf(codes.FailedPrecondition, "storage not available")
	}

	if req.HttpMethod != "PUT" {
		return nil, status.Errorf(codes.FailedPrecondition, "method not available")
	}
	options := &storage.SignedURLOptions{
		GoogleAccessID: cs.serviceID,
		PrivateKey:     cs.privateKey,
		Method:         req.HttpMethod,
		ContentType:    req.ContentType,
		Expires:        exp,
	}

	// Specific prefixes have specific requirements
	if req.Prefix == "drops" {
		if req.ContentType != archive.DropContentType {
			return nil, status.Errorf(codes.FailedPrecondition, "bad content-type for drops")
		}
	} else if req.Prefix == "stats" {
		if req.ContentType != archive.StatContentType {
			return nil, status.Errorf(codes.FailedPrecondition, "bad content-type for stats")
		}
	} else if req.Prefix == "" {
		return nil, status.Errorf(codes.FailedPrecondition, "bad prefix")
	}

	for _, obj := range req.Objects {
		var fullName string
		if req.Prefix == "drops" || req.Prefix == "stats" {
			// We liberally accept any timezone, but store everything as UTC
			t, err := time.Parse(time.RFC3339, strings.TrimSuffix(obj, ".json"))
			if err != nil {
				slog.Warnf("GenerateURL: invalid object name %v", obj)
				return nil, status.Errorf(codes.FailedPrecondition, "invalid object name")
			}
			fullName = req.Prefix + "/" + t.UTC().Format(time.RFC3339) + ".json"
		} else {
			fullName = req.Prefix + "/" + obj
		}
		slog.Debugf("URL request for obj=%v -> fullName=%v", obj, fullName)

		genurl, err := storage.SignedURL(bucket, fullName, options)
		if err != nil {
			slog.Errorf("GenerateURL: failed SignedURL: %v", err)
			return nil, status.Errorf(codes.Internal, "could not Sign URL")
		}
		r := &cloud_rpc.SignedURL{
			Object: obj,
			Url:    genurl,
		}
		slog.Debugw("Appending to response", "SignedURL", r)
		resp.Urls = append(resp.Urls, r)
	}
	slog.Infof("Generated %d '%s' URLs", len(resp.Urls), req.Prefix)

	return &resp, nil
}
