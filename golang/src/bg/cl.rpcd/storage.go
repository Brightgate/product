/*
 * COPYRIGHT 2020 Brightgate Inc.  All rights reserved.
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
	"path/filepath"
	"strings"
	"time"

	"bg/cl_common/daemonutils"
	"bg/cl_common/registry"
	"bg/cl_common/vaulttokensource"
	"bg/cloud_models/appliancedb"
	"bg/cloud_rpc"
	"bg/common/archive"

	credentials "cloud.google.com/go/iam/credentials/apiv1"
	"cloud.google.com/go/storage"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/option"
	credentialspb "google.golang.org/genproto/googleapis/iam/credentials/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type cloudStorageServer struct {
	tokenSource *vaulttokensource.VaultTokenSource
	clientOpts  []option.ClientOption
	serviceID   string
	privateKey  []byte
	projectID   string
	applianceDB appliancedb.DataStore
}

func defaultCloudStorageServer(applianceDB appliancedb.DataStore, vts *vaulttokensource.VaultTokenSource) *cloudStorageServer {
	ctx := context.Background()

	var privateKey []byte
	var email, project string
	if vts == nil {
		creds, _ := google.FindDefaultCredentials(ctx, storage.ScopeFullControl)
		if creds == nil {
			slog.Fatalf("no cloud credentials defined")
		}

		jwt, err := google.JWTConfigFromJSON(creds.JSON)
		if err != nil {
			slog.Fatalf("bad cloud credentials: %v", err)
		}
		email = jwt.Email
		privateKey = jwt.PrivateKey
		project = creds.ProjectID
	} else {
		email = vts.ServiceAccountEmail()
		project = vts.Project()
	}

	return newCloudStorageServer(vts, email, project, privateKey, applianceDB)
}

func newCloudStorageServer(vts *vaulttokensource.VaultTokenSource, serviceID string, projectID string, privateKey []byte, appliancedb appliancedb.DataStore) *cloudStorageServer {
	var opts []option.ClientOption
	if vts != nil {
		opts = append(opts, option.WithTokenSource(oauth2.ReuseTokenSource(nil, vts)))
	}
	c := &cloudStorageServer{
		tokenSource: vts,
		clientOpts:  opts,
		serviceID:   serviceID,
		privateKey:  privateKey,
		projectID:   projectID,
		applianceDB: appliancedb,
	}
	return c
}

func signBytes(ctx context.Context, input []byte, serviceID string, opts []option.ClientOption) ([]byte, error) {
	iamClient, err := credentials.NewIamCredentialsClient(ctx, opts...)
	if err != nil {
		return nil, err
	}
	defer iamClient.Close()

	req := &credentialspb.SignBlobRequest{
		Payload: input,
		Name:    serviceID,
	}

	resp, err := iamClient.SignBlob(ctx, req)
	if err != nil {
		return nil, err
	}
	return resp.SignedBlob, nil
}

func (cs *cloudStorageServer) signBytes(ctx context.Context, input []byte) ([]byte, error) {
	return signBytes(ctx, input, cs.serviceID, cs.clientOpts)
}

func (cs *cloudStorageServer) updateMetadata() error {
	err := cs.tokenSource.UpdateMetadata()
	if err != nil {
		return err
	}
	cs.serviceID = cs.tokenSource.ServiceAccountEmail()
	cs.projectID = cs.tokenSource.Project()
	return nil
}

func (cs *cloudStorageServer) GenerateURL(ctx context.Context, req *cloud_rpc.GenerateURLRequest) (*cloud_rpc.GenerateURLResponse, error) {
	_, slog := daemonutils.EndpointLogger(ctx)
	slog.Debugw("incoming URL request", "req", req)

	siteUUID, err := getSiteUUID(ctx, false)
	if err != nil {
		return nil, err
	}
	applianceUUID, err := getApplianceUUID(ctx, false)
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
	if cs.privateKey == nil {
		options.SignBytes = func(input []byte) ([]byte, error) {
			return cs.signBytes(ctx, input)
		}
	}

	// Specific prefixes have specific requirements
	ct := req.ContentType
	errmsg := ""
	if req.Prefix == "drops" {
		if ct == "" {
			errmsg = "missing content-type for drops"
		} else if ct != archive.DropContentType && ct != archive.DropBinaryType {
			errmsg = "bad content-type for drops: " + ct
		}
	} else if req.Prefix == "stats" {
		if ct == "" {
			errmsg = "missing content-type for stats"
		} else if ct != archive.StatContentType && ct != archive.StatBinaryType {
			errmsg = "bad content-type for stats: " + ct
		}
	} else if req.Prefix == "" {
		errmsg = "missing prefix"
	}
	if errmsg != "" {
		return nil, status.Errorf(codes.FailedPrecondition, errmsg)
	}

	for _, obj := range req.Objects {
		var fullName string
		if req.Prefix == "drops" || req.Prefix == "stats" {
			suffix := filepath.Ext(obj)

			if suffix != ".json" && suffix != ".gob" {
				slog.Warnf("GenerateURL: invalid object suffix %v", obj)
				return nil, status.Errorf(codes.FailedPrecondition,
					"invalid object suffix")
			}

			// We liberally accept any timezone, but store everything as UTC
			t, err := time.Parse(time.RFC3339, strings.TrimSuffix(obj, suffix))
			if err != nil {
				slog.Warnf("GenerateURL: invalid object timestamp %v", obj)
				return nil, status.Errorf(codes.FailedPrecondition,
					"invalid object timestamp")
			}
			fullName = req.Prefix + "/" + t.UTC().Format(time.RFC3339) + suffix
		} else {
			fullName = req.Prefix + "/" + obj
		}
		slog.Debugf("URL request for obj=%v -> fullName=%v", obj, fullName)

		var genurl string
		op := func() (err error) {
			options.GoogleAccessID = cs.serviceID
			genurl, err = storage.SignedURL(cloudStor.Bucket, fullName, options)
			return
		}
		err = vaulttokensource.Retry(op, cs.updateMetadata, func(msg string, e error) {
			slog.Warnf("GenerateURL: %s: %v", msg, e)
		})
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
