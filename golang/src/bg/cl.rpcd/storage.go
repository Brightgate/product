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
	"strings"
	"time"

	"bg/cl_common/daemonutils"
	"bg/cl_common/registry"
	"bg/cloud_models/appliancedb"
	"bg/cloud_rpc"
	"bg/common/archive"

	"github.com/grpc-ecosystem/go-grpc-middleware/util/metautils"
	"github.com/satori/uuid"

	"cloud.google.com/go/storage"
	"golang.org/x/oauth2/google"
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

func (cs *cloudStorageServer) GenerateURL(ctx context.Context, req *cloud_rpc.GenerateURLRequest) (*cloud_rpc.GenerateURLResponse, error) {
	_, slog := daemonutils.EndpointLogger(ctx)
	slog.Debugw("incoming URL request", "req", req)

	siteUUID, err := getSiteUUID(ctx, false)
	if err != nil {
		return nil, err
	}
	applianceUUIDStr := metautils.ExtractIncoming(ctx).Get("appliance_uuid")
	applianceUUID, err := uuid.FromString(applianceUUIDStr)
	if err != nil {
		return nil, err
	}
	exp := time.Now().Add(10 * time.Minute)

	resp := cloud_rpc.GenerateURLResponse{
		Urls: make([]*cloud_rpc.SignedURL, 0),
	}

	// Lookup bucket for site using appliance and site UUIDs
	cloudStor, err := registry.GetCloudStorage(ctx, cs.applianceDB, cs.projectID, applianceUUID, siteUUID)
	if err != nil {
		slog.Errorf("GenerateURL: couldn't get cloud storage for %s: %v", siteUUID, err)
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

		genurl, err := storage.SignedURL(cloudStor.Bucket, fullName, options)
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
