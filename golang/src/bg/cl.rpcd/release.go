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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"bg/cl_common/daemonutils"
	"bg/cl_common/release"
	"bg/cl_common/vaulttokensource"
	"bg/cloud_models/appliancedb"
	"bg/cloud_rpc"

	"cloud.google.com/go/storage"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/option"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	minDescriptorVersion = 0
	maxDescriptorVersion = 0
)

type releaseServer struct {
	tokenSource *vaulttokensource.VaultTokenSource
	clientOpts  []option.ClientOption
	serviceID   string
	privateKey  []byte
	projectID   string
	applianceDB appliancedb.DataStore
}

func newReleaseServer(applianceDB appliancedb.DataStore, vts *vaulttokensource.VaultTokenSource) *releaseServer {
	ctx := context.Background()

	var privateKey []byte
	var opts []option.ClientOption
	var email, project string
	if vts == nil {
		creds, err := google.FindDefaultCredentials(ctx)
		if err != nil {
			slog.Fatalw("failed to find cloud credentials", "error", err)
		}

		jwt, err := google.JWTConfigFromJSON(creds.JSON)
		if err != nil {
			slog.Fatalw("bad cloud credentials", "error", err)
		}
		email = jwt.Email
		privateKey = jwt.PrivateKey
		project = creds.ProjectID
	} else {
		email = vts.ServiceAccountEmail()
		project = vts.Project()
		opts = append(opts, option.WithTokenSource(oauth2.ReuseTokenSource(nil, vts)))
	}

	return &releaseServer{
		tokenSource: vts,
		clientOpts:  opts,
		serviceID:   email,
		privateKey:  privateKey,
		projectID:   project,
		applianceDB: applianceDB,
	}
}

func (rs *releaseServer) signBytes(ctx context.Context, input []byte) ([]byte, error) {
	return signBytes(ctx, input, rs.serviceID, rs.clientOpts)
}

func (rs *releaseServer) updateMetadata() error {
	err := rs.tokenSource.UpdateMetadata()
	if err != nil {
		return err
	}
	rs.serviceID = rs.tokenSource.ServiceAccountEmail()
	rs.projectID = rs.tokenSource.Project()
	return nil
}

func (rs *releaseServer) FetchDescriptor(ctx context.Context, req *cloud_rpc.ReleaseRequest) (*cloud_rpc.ReleaseResponse, error) {
	_, slog := daemonutils.EndpointLogger(ctx)

	_, err := getSiteUUID(ctx, false)
	if err != nil {
		slog.Errorw("Failed to process release descriptor retrieval",
			"error", err)
		return nil, err
	}
	appUU, err := getApplianceUUID(ctx, false)
	if err != nil {
		slog.Errorw("Failed to process release descriptor retrieval",
			"error", err)
		return nil, err
	}

	if req.MaxVersion < minDescriptorVersion || req.MinVersion > maxDescriptorVersion {
		slog.Warnw("Descriptor version range unsupported",
			"client_min", req.MinVersion, "client_max", req.MaxVersion,
			"server_min", minDescriptorVersion, "server_max", maxDescriptorVersion)
		return nil, status.Error(codes.Unimplemented,
			"descriptor version range unsupported")
	}

	relUU, err := rs.applianceDB.GetTargetRelease(ctx, appUU)
	if err != nil {
		slog.Errorw("Failed to process release descriptor retrieval: unable to determine target release",
			"error", err)
		return nil, status.Error(codes.Internal, "unable to determine target release")
	}

	// If we fail prior to this, we won't be able to record that failure in
	// the database, because we don't have the release UUID.

	dbRel, err := rs.applianceDB.GetRelease(ctx, relUU)
	if err != nil {
		slog.Errorw("Failed to process release descriptor retrieval: DB error",
			"error", err, "target_release_uuid", relUU.String())
		rs.applianceDB.SetUpgradeStage(ctx, appUU, relUU, time.Now(), "manifest_retrieved",
			false, "unable to retrieve release")
		return nil, status.Error(codes.Internal, "unable to retrieve release")
	}

	desc := release.FromDBRelease(dbRel)

	// Replace the URLs with signed URLs
	options := &storage.SignedURLOptions{
		GoogleAccessID: rs.serviceID,
		PrivateKey:     rs.privateKey,
		Method:         "GET",
		Expires:        time.Now().Add(1 * time.Hour),
	}
	if rs.privateKey == nil {
		options.SignBytes = func(input []byte) ([]byte, error) {
			return rs.signBytes(ctx, input)
		}
	}

	for i, artifact := range desc.Artifacts {
		u, err := url.Parse(artifact.URL)
		if err != nil {
			slog.Errorw("Failed to process release descriptor retrieval: unparseable artifact URL",
				"error", err, "target_release_uuid", relUU.String(), "url", artifact.URL)
			rs.applianceDB.SetUpgradeStage(ctx, appUU, relUU, time.Now(),
				"manifest_retrieved", false, "unparseable artifact URL")
			return nil, status.Error(codes.Internal, "unparseable artifact URL")
		}
		if u.Scheme != "gs" {
			slog.Errorw("Failed to process release descriptor retrieval: unknown artifact URL scheme",
				"error", "GCS prefix scheme must be 'gs'", "url", artifact.URL,
				"target_release_uuid", relUU.String())
			rs.applianceDB.SetUpgradeStage(ctx, appUU, relUU, time.Now(),
				"manifest_retrieved", false, "unknown artifact URL scheme")
			return nil, status.Error(codes.Internal, "unknown artifact URL scheme")
		}
		bucketName := u.Hostname()
		objectName := strings.TrimPrefix(u.Path, "/")
		var surl string
		op := func() (err error) {
			surl, err = storage.SignedURL(bucketName, objectName, options)
			return
		}
		err = vaulttokensource.Retry(op, rs.updateMetadata, func(msg string, e error) {
			slog.Warnw(fmt.Sprintf("FetchDescriptor: %s", msg), "error", e)
		})
		if err != nil {
			slog.Errorw("Failed to process release descriptor retrieval: failed to create signed URL",
				"error", err, "target_release_uuid", relUU.String())
			rs.applianceDB.SetUpgradeStage(ctx, appUU, relUU, time.Now(),
				"manifest_retrieved", false, "failed to create signed URL")
			return nil, status.Error(codes.Internal, "failed to create signed URL")
		}
		artifact.URL = surl
		desc.Artifacts[i] = artifact
	}

	// We need to disable HTML escaping or the &s in the signed URLs will
	// get mangled to \0026.
	buf := &bytes.Buffer{}
	encoder := json.NewEncoder(buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(desc); err != nil {
		slog.Errorw("Failed to process release descriptor retrieval: JSON encoding failure",
			"error", err, "target_release_uuid", relUU.String())
		rs.applianceDB.SetUpgradeStage(ctx, appUU, relUU, time.Now(),
			"manifest_retrieved", false, "JSON encoding failure")
		return nil, status.Error(codes.Internal, "JSON encoding failure")
	}

	rs.applianceDB.SetUpgradeStage(ctx, appUU, relUU, time.Now(), "manifest_retrieved", true, "")
	return &cloud_rpc.ReleaseResponse{
		Release: buf.String(),
	}, nil
}
