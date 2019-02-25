/*
 * COPYRIGHT 2019 Brightgate Inc. All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"bg/ap_common/certificate"
	"bg/cloud_rpc"
	"bg/common/cfgapi"
	"bg/common/zaperr"

	"google.golang.org/grpc"
)

func downloadCert(ctx context.Context, client cloud_rpc.CertificateManagerClient, fpstr string) error {
	if fpstr != "" {
		slog.Infof("Downloading key and certificate: fingerprint=%s", fpstr)
	} else {
		slog.Info("Requesting new key and certificate")
	}

	var err error
	ctx, err = applianceCred.MakeGRPCContext(ctx)
	if err != nil {
		slog.Fatalf("Failed to make GRPC credential: %v", err)
	}

	clientDeadline := time.Now().Add(*rpcDeadline)
	ctx, ctxcancel := context.WithDeadline(ctx, clientDeadline)
	defer ctxcancel()

	fpbytes, err := hex.DecodeString(fpstr)
	if err != nil {
		return err
	}
	req := &cloud_rpc.CertificateRequest{
		CertFingerprint: fpbytes,
	}

	resp, err := client.Download(ctx, req)
	var fpMsg, errMsg string
	if err == nil {
		fpMsg = fmt.Sprintf(" fingerprint=%s",
			hex.EncodeToString(resp.Fingerprint))
	} else {
		errMsg = fmt.Sprintf(" error=%s", err)
	}
	slog.Debugf("Download response%s%s", errMsg, fpMsg)
	if err != nil {
		return err
	}

	if err = certificate.InstallCert(resp.Key, resp.Certificate,
		resp.IssuerCert, fpstr, config); err != nil {
		// Technically, this can also be a failure to notify listeners,
		// but if we have to download again, it's not the end of the
		// world.
		return zaperr.Errorw("Failed to install key/cert", "error", err)
	}
	return nil
}

func certificateInitCheck(ctx context.Context, client cloud_rpc.CertificateManagerClient) error {
	node, err := config.GetProps("@/certs")
	if err == cfgapi.ErrNoProp {
		// If there is no such property, then we don't have a cert; go
		// ask for one.
		slog.Info("No certs known; requesting new cert")
		return downloadCert(ctx, client, "")
	} else if err != nil {
		return err
	}

	// Applicable states are "installed" and "available".  We request a new
	// cert if there aren't any at all, or all certs have expired.  If there
	// aren't any installed certs but there are ones available that are not
	// expired, download the latest.
	//
	// If we find an installed cert where the origin is not "cloud", we
	// don't stop looking; consumers will use the existing certificate until
	// a new one is available.
	type tuple struct {
		fp  string
		exp *time.Time
	}
	var available []tuple
	for fp, node := range node.Children {
		stateNode := node.Children["state"]
		if stateNode != nil && !stateNode.Expires.Before(time.Now()) {
			tup := tuple{fp, stateNode.Expires}
			if stateNode.Value == "installed" {
				originNode := node.Children["origin"]
				if originNode == nil || originNode.Value != "cloud" {
					slog.Warnf("Found an installed non-cloud "+
						"certificate: %s", fp)
					continue
				}
				// If we already have an installed key, there's
				// no need to continue.
				slog.Infof("Found at least one installed certificate: %s",
					fp)
				return nil
			} else if stateNode.Value == "available" {
				available = append(available, tup)
			}
		}
	}

	// If there are any available certs, find the one with the latest
	// expiration; otherwise, we'll just get a new one.
	var fp string
	if len(available) > 0 {
		// Sort by decreasing expiration time
		sort.Slice(available, func(i, j int) bool {
			return (available[i].exp).After(*available[j].exp)
		})
		fp = available[0].fp
	}

	return downloadCert(ctx, client, fp)
}

func certificateInit(ctx context.Context, conn *grpc.ClientConn) {
	client := cloud_rpc.NewCertificateManagerClient(conn)

	// Set up a handler to download new certs when the cloud tells us
	// they're available and one to clean up the parent nodes when the
	// "state" property expires.
	certStateChange := func(path []string, val string, expires *time.Time) {
		if val == "available" {
			downloadCert(ctx, client, path[1])
		}
	}
	certStateExpire := func(path []string) {
		subtree := "@/" + strings.Join(path[:len(path)-1], "/")
		if err := config.DeleteProp(subtree); err != nil {
			slog.Errorf("Failed to delete old certificate subtree: "+
				"path=%s, error=%v", subtree, err)
		}
	}
	config.HandleChange(`^@/certs/.*/state`, certStateChange)
	config.HandleExpire(`^@/certs/.*/state`, certStateExpire)

	// Download and install a certificate if we need one and one is
	// available.  If one isn't available (or the cloud isn't upgraded with
	// this functionality yet), we'll have to wait until one is created.
	go func() {
		sleep := time.Second
		for {
			if err := certificateInitCheck(ctx, client); err != nil {
				slog.Errorf("Failed initial cert retrieval "+
					"(will retry in %s): %v", sleep, err)
			} else {
				return
			}
			time.Sleep(sleep)
			sleep = 2 * sleep
			if sleep > 10*time.Minute {
				sleep = 10 * time.Minute
			}
		}
	}()
}
