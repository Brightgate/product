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
	"strings"
	"sync"
	"time"

	"bg/ap_common/certificate"
	"bg/cloud_rpc"
	"bg/common/cfgapi"
	"bg/common/zaperr"

	"github.com/pkg/errors"

	"google.golang.org/grpc"
)

func downloadCert(ctx context.Context, client cloud_rpc.CertificateManagerClient, fpstr string) error {
	if fpstr != "" {
		slog.Info("Downloading key and certificate")
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

	req := &cloud_rpc.CertificateRequest{}

	resp, err := client.Download(ctx, req)
	var fpResp, fpMsg, errMsg string
	if err == nil {
		fpResp = hex.EncodeToString(resp.Fingerprint)
		fpMsg = fmt.Sprintf(" requested=%s received=%s", fpstr, fpResp)
	} else {
		errMsg = fmt.Sprintf(" error=%s", err)
	}
	slog.Debugf("Download response%s%s", errMsg, fpMsg)
	if err != nil {
		return err
	}

	// If the server gave us the cert we already had, there's no need to
	// install it and restart daemons.
	if fpResp == fpstr {
		slog.Info("Key and certificate haven't changed")
		return nil
	}

	slog.Infof("Installing new cloud certificate: fingerprint=%s", fpResp)
	if err = certificate.InstallCert(resp.Key, resp.Certificate,
		resp.IssuerCert, config); err != nil {
		// Technically, this can also be a failure to notify listeners,
		// but if we have to download again, it's not the end of the
		// world.
		return zaperr.Errorw("Failed to install key/cert", "error", err)
	}
	return nil
}

// findInstalledCert looks through the config tree at @/certs and finds an
// installed, unexpired cloud certificate.  It may return an error if there's a
// problem with configd.
func findInstalledCert(quiet, cloudOnly bool) (string, error) {
	node, err := config.GetProps("@/certs")
	if err == cfgapi.ErrNoProp {
		// If there is no such property, then we don't have a cert; go
		// ask for one.
		if !quiet {
			slog.Info("No certs known; requesting new cert")
		}
		return "", nil
	} else if err != nil {
		return "", err
	}

	var found string
	for fp, node := range node.Children {
		stateNode := node.Children["state"]
		if stateNode == nil || stateNode.Expires == nil ||
			stateNode.Expires.Before(time.Now()) ||
			stateNode.Value != "installed" {
			if stateNode != nil && stateNode.Expires == nil {
				slog.Warnf("@/certs/%s/state unexpectedly has "+
					"nil expiration", fp)
			}
			continue
		}

		originNode := node.Children["origin"]
		// If we find an installed cert where the origin is not "cloud",
		// we don't stop looking; consumers will use the existing cert
		// until a new one is available.
		if cloudOnly && (originNode == nil || originNode.Value != "cloud") {
			if !quiet {
				slog.Warnf("Found an installed non-cloud "+
					"certificate: %s", fp)
			}
			continue
		}

		// If we already have an installed key, there's no need to
		// continue.
		if !quiet {
			slog.Infof("Found at least one installed certificate: %s", fp)
		}
		found = fp
		break
	}

	return found, nil
}

func sleepOrDone(t time.Duration, doneChan chan bool) bool {
	done := false
	timer := time.NewTimer(t)
	defer timer.Stop()

	select {
	case <-timer.C:
	case <-doneChan:
		done = true
	}
	return done
}

func ssCertGen(genNo string) {
	domain, err := config.GetDomain()
	if err != nil {
		slog.Fatalf("failed to fetch gateway domain for use in self-signed cert: %v", err)
	}

	retryPeriod := 30 * time.Second
	for {
		_, err = certificate.CreateSSKeyCert(config, domain, genNo)
		if err == nil {
			break
		}

		// If the error is that the cloud cert beat us to the
		// punch, it's not really an error.
		if errors.Cause(err) == cfgapi.ErrNotEqual {
			slog.Infof("Self-signed certificate generation " +
				"canceled by new cloud certificate")
			break
		}
		slog.Errorf("Self-signed certificate generation failed "+
			"(retry in %s): %v", retryPeriod, err)
		time.Sleep(retryPeriod)
	}
}

func setCertExpirationHandler() {
	// Set up a handler to clean up the parent nodes when the "state"
	// property expires.  Also, if we don't still have an installed cert, go
	// make a new self-signed cert.
	certStateExpire := func(path []string) {
		subtree := "@/" + strings.Join(path[:len(path)-1], "/")
		if err := config.DeleteProp(subtree); err != nil {
			slog.Errorf("Failed to delete old certificate subtree: "+
				"path=%s, error=%v", subtree, err)
		}

		fp, err := findInstalledCert(true, false)
		if err != nil {
			slog.Errorf("Error finding installed cert: %v", err)
			return
		}
		if fp == "" {
			slog.Info("Generating replacement self-signed cert")
			genNo, err := config.GetProp("@/cert_generation")
			if err == nil {
				go ssCertGen(genNo)
			}
		}
	}
	config.HandleExpire(`^@/certs/.*/state`, certStateExpire)
}

func cloudCertLoop(ctx context.Context, conn *grpc.ClientConn, wg *sync.WaitGroup, doneChan chan bool) {
	slog.Infof("certificate loop starting")
	client := cloud_rpc.NewCertificateManagerClient(conn)

	// A curated sleep schedule
	sleepSched := []int{
		1, 2, 2, // up to 5 seconds
		5, 5, // 5 second sleeps up to 15 seconds
		15, 15, 15, // 15 second sleeps up to 1 minute
		60, 60, 60, 60, // 1 minute sleeps up to 5 minutes
		300, 300, // 5 minute sleeps up to 15 minutes
		900, 900, 900, // 15 minute sleeps up to 1 hour
		3600, // 1 hour thereafter
	}

	done := false
	for !done {
		for i := 0; !done; i++ {
			if i == len(sleepSched) {
				i--
			}

			var err error
			var fp, msg string

			// Download and install a certificate if we need one and
			// one is available.  If one isn't available, wait until
			// one is created.
			if fp, err = findInstalledCert(false, true); err != nil {
				msg = "find installed"
			} else {
				if err = downloadCert(ctx, client, fp); err == nil {
					slog.Info("Downloaded cert")
					break
				}
				msg = "download"
			}

			sleep := time.Duration(sleepSched[i]) * time.Second
			slog.Errorf("Failed to %s cert (will retry in %s): %v",
				msg, sleep, err)
			done = sleepOrDone(sleep, doneChan)
		}
		if done {
			break
		}

		// Check back a day after a successful cloud connection to see
		// if there's a new cert for us.
		done = sleepOrDone(24*time.Hour, doneChan)
	}

	slog.Infof("certificate loop done")
	wg.Done()
}
