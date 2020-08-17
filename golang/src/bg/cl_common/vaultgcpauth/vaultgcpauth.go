/*
 * Copyright 2020 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


// Package vaultgcpauth contains routines to help authenticate to Vault using
// the GCE-type GCP authentication.
package vaultgcpauth

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"bg/cl_common/daemonutils"

	"github.com/hashicorp/go-hclog"
	vault "github.com/hashicorp/vault/api"
	"github.com/hashicorp/vault/command/agent/auth"
	"github.com/hashicorp/vault/command/agent/auth/gcp"
	"github.com/hashicorp/vault/command/agent/sink"
)

type vaultClientSink struct {
	client   *vault.Client
	log      hclog.Logger
	notifier *daemonutils.FanOut
}

func (s *vaultClientSink) WriteToken(token string) error {
	s.client.SetToken(token)
	// This is a different token each time.  We need to signal to the other
	// users of s.client that they should get *new* leases.
	s.notifier.Notify()
	return nil
}

// VaultAuth sets up authentication to Vault using GCP.  It will renew the token
// until expiration, and then fetch a new one.  The tokens will be set on the
// passed-in Vault client.
func VaultAuth(ctx context.Context, hclogger hclog.Logger, vc *vault.Client, path, role string) (*daemonutils.FanOut, error) {
	hclogger = hclogger.Named("vault-authenticator")
	authConfig := &auth.AuthConfig{
		Logger:    hclogger,
		MountPath: path,
		Config: map[string]interface{}{
			"type":            "gce",
			"role":            role,
			"service_account": "default", // configurable?
		},
	}
	authMethod, err := gcp.NewGCPAuthMethod(authConfig)
	if err != nil {
		return nil, err
	}

	authHandlerConfig := &auth.AuthHandlerConfig{
		Client:                       vc,
		Logger:                       hclogger,
		EnableReauthOnNewCredentials: true,
	}
	authHandler := auth.NewAuthHandler(authHandlerConfig)

	vcSink := &vaultClientSink{
		client:   vc,
		log:      hclogger,
		notifier: daemonutils.NewFanOut(make(chan struct{})),
	}
	sinkConfig := &sink.SinkConfig{
		Client: vc,
		Logger: hclogger,
		Sink:   vcSink,
	}

	ssConfig := &sink.SinkServerConfig{
		Context: ctx,
		Client:  vc,
		Logger:  hclogger,
	}
	sinkServer := sink.NewSinkServer(ssConfig)

	go authHandler.Run(ctx, authMethod)
	go sinkServer.Run(ctx, authHandler.OutputCh, []*sink.SinkConfig{sinkConfig})

	// Don't return until we've gotten our first token.
	for count := 0; vc.Token() == ""; count++ {
		time.Sleep(250 * time.Millisecond)
		if count > 20 {
			return nil, fmt.Errorf("Couldn't authenticate to Vault within 5 seconds")
		}
	}

	return vcSink.notifier, nil
}

// VaultAuthOnce sets up authentication to Vault using GCP, setting the
// retrieved token on the passed-in Vault client.  No renewal machinery is
// started, so once the token expires, the caller will have to log in again.
func VaultAuthOnce(ctx context.Context, hclogger hclog.Logger, vc *vault.Client, path, role string) error {
	hclogger = hclogger.Named("vault-authenticator")
	authConfig := &auth.AuthConfig{
		Logger:    hclogger,
		MountPath: path,
		Config: map[string]interface{}{
			"type":            "gce",
			"role":            role,
			"service_account": "default", // configurable?
		},
	}

	gcpAuth, err := gcp.NewGCPAuthMethod(authConfig)
	if err != nil {
		return err
	}

	// header (retval #2) is always nil for this provider
	vLoginURL, _, data, err := gcpAuth.Authenticate(ctx, vc)
	if err != nil {
		return err
	}

	vReq := vc.NewRequest(http.MethodPost, "/v1/"+vLoginURL)
	vReq.SetJSONBody(data)
	vResp, err := vc.RawRequestWithContext(ctx, vReq)
	if err != nil {
		return err
	}
	secret, err := vault.ParseSecret(vResp.Body)
	if err != nil {
		return err
	}
	token, err := secret.TokenID()
	if err != nil {
		return err
	}
	vc.SetToken(token)

	return nil
}

