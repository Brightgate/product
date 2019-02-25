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
	"encoding/hex"
	"fmt"
	"time"

	"bg/cl_common/clcfg"
	"bg/cloud_models/appliancedb"
	"bg/cloud_rpc"
	"bg/common/cfgapi"

	"github.com/grpc-ecosystem/go-grpc-middleware/util/metautils"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/satori/uuid"
)

type certServer struct {
	applianceDB appliancedb.DataStore
}

func newCertServer(applianceDB appliancedb.DataStore) *certServer {
	return &certServer{
		applianceDB: applianceDB,
	}
}

func getConfigClientHandle(cuuid string) (*cfgapi.Handle, error) {
	configd, err := clcfg.NewConfigd(pname, cuuid,
		environ.ConfigdConnection, !environ.ConfigdDisableTLS)
	if err != nil {
		return nil, err
	}
	configHandle := cfgapi.NewHandle(configd)
	return configHandle, nil
}

func errToCode(err error) codes.Code {
	var code codes.Code
	switch err.(type) {
	case appliancedb.NotFoundError:
		code = codes.NotFound
	case nil:
		code = codes.OK
	default:
		code = codes.Internal
	}
	return code
}

// claim makes sure that the site uuid is mapped to a "brightgate.net" domain,
// thus claiming it, and returns the certificate for that domain.
func (cs *certServer) claim(ctx context.Context, ustr string) (*cloud_rpc.CertificateResponse, error) {
	_, slog := endpointLogger(ctx)

	slog.Info("Processing new certificate request")

	u, err := uuid.FromString(ustr)
	if err != nil {
		slog.Errorw("Failed to convert string to UUID",
			"uuid-string", ustr, "error", err)
		return nil, status.Errorf(codes.InvalidArgument,
			"Failed to convert %q to UUID: %v", ustr, err)
	}

	jurisdiction := "" // XXX Need lookup
	domain, err := cs.applianceDB.RegisterDomain(ctx, u, jurisdiction)
	if err != nil {
		slog.Errorw("Failed to register or determine domain",
			"error", err)
		return nil, status.Errorf(codes.Internal,
			"Failed to register or determine domain: %v", err)
	}
	slog.Infow("Claimed domain for site", "domain", domain)

	certInfo, err := cs.applianceDB.ServerCertByUUID(ctx, u)
	if err != nil {
		slog.Errorw("Failed to find server certificate",
			"domain", domain, "error", err)
		return nil, status.Errorf(errToCode(err),
			"Failed to find server certificate: %v", err)
	}
	if certInfo.Expiration.Before(time.Now()) {
		expired := time.Now().Sub(certInfo.Expiration)
		slog.Errorw("Found already-expired certificate",
			"domain", domain, "expired", expired)
		return nil, status.Errorf(codes.Internal,
			"Found already-expired certificate")
	}

	return &cloud_rpc.CertificateResponse{
		Fingerprint: certInfo.Fingerprint,
		Certificate: certInfo.Cert,
		IssuerCert:  certInfo.IssuerCert,
		Key:         certInfo.Key,
	}, nil
}

func (cs *certServer) Download(ctx context.Context, req *cloud_rpc.CertificateRequest) (*cloud_rpc.CertificateResponse, error) {
	_, slog := endpointLogger(ctx)

	ustr := metautils.ExtractIncoming(ctx).Get("site_uuid")
	if ustr == "" {
		slog.Errorw("Failed to process certificate retrieval",
			"error", fmt.Errorf("missing site_uuid"))
		return nil, status.Errorf(codes.Internal, "missing site_uuid")
	}

	fp := req.CertFingerprint
	fpstr := hex.EncodeToString(fp)

	if len(fp) == 0 {
		return cs.claim(ctx, ustr)
	}

	slog = slog.With("fingerprint", fpstr)
	slog.Info("Processing certificate retrieval")
	certInfo, err := cs.applianceDB.ServerCertByFingerprint(ctx, fp)
	if err != nil {
		slog.Errorw("Failed to find server certificate", "error", err)
		return nil, status.Errorf(errToCode(err),
			"Failed to find server certificate: %v", err)
	}
	if certInfo.Expiration.Before(time.Now()) {
		expired := time.Now().Sub(certInfo.Expiration)
		slog.Errorw("Found already-expired certificate",
			"expired", expired)
		return nil, status.Errorf(codes.Internal,
			"Found already-expired certificate")
	}

	return &cloud_rpc.CertificateResponse{
		Fingerprint: certInfo.Fingerprint,
		Certificate: certInfo.Cert,
		IssuerCert:  certInfo.IssuerCert,
		Key:         certInfo.Key,
	}, nil
}
