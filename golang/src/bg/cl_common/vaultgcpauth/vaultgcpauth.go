/*
 * COPYRIGHT 2020 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

// Package vaultgcpauth contains routines to help authenticate to Vault using
// the GCE-type GCP authentication.
package vaultgcpauth

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"time"

	"bg/cl_common/daemonutils"

	"github.com/hashicorp/go-hclog"
	vault "github.com/hashicorp/vault/api"
	"github.com/hashicorp/vault/command/agent/auth"
	"github.com/hashicorp/vault/command/agent/auth/gcp"
	"github.com/hashicorp/vault/command/agent/sink"
)

func vaultAuthManual(vc *vault.Client, path, role string) error {
	baseURL := "http://metadata/computeMetadata/v1/instance/service-accounts/default/identity"
	u, err := url.Parse(baseURL)
	if err != nil {
		panic(err)
	}
	urlData := u.Query()
	urlData.Set("audience", "vault/"+role)
	urlData.Set("format", "full")
	u.RawQuery = urlData.Encode()

	request, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	request.Header.Set("Metadata-Flavor", "Google")
	resp, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}

	jwt, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	vLoginURL := fmt.Sprintf("/v1/%s/login", path)
	vReq := vc.NewRequest(http.MethodPost, vLoginURL)
	vReq.BodyBytes = []byte(fmt.Sprintf(`{"role": "%s", "jwt": "%s"}`, role, jwt))
	vResp, err := vc.RawRequest(vReq)
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
