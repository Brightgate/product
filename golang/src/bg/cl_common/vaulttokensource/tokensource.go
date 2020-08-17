/*
 * Copyright 2020 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package vaulttokensource

import (
	"encoding/json"
	"fmt"
	"time"

	vault "github.com/hashicorp/vault/api"
	"github.com/pkg/errors"
	"golang.org/x/oauth2"
	"google.golang.org/api/googleapi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// VaultTokenSource is an oauth2.TokenSource that returns an access token pulled
// from Vault for a GCP service account.
type VaultTokenSource struct {
	vaultClient *vault.Client
	enginePath  string
	roleName    string
	project     string
	email       string

	prevEmail string
	prevToken *oauth2.Token
}

// Token reads an access token from Vault and returns an oauth2.Token based on
// that.  This can be buffered by oauth2.ReuseTokenSource, but it may not be
// shared between multiple instances.
func (vts *VaultTokenSource) Token() (*oauth2.Token, error) {
	vcl := vts.vaultClient.Logical()
	path := fmt.Sprintf("%s/token/%s", vts.enginePath, vts.roleName)
	creds, err := vcl.Read(path)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read access token from Vault")
	}
	if creds == nil || creds.Data == nil {
		return nil, errors.New("no data reading access token from Vault")
	}

	tokenData, ok := creds.Data["token"]
	if !ok {
		return nil, errors.New("missing token in access token response from Vault")
	}
	token, ok := tokenData.(string)
	if !ok {
		return nil, errors.Errorf("unexpected type '%T' for token '%s'",
			tokenData, tokenData)
	}

	expiresData, ok := creds.Data["expires_at_seconds"]
	if !ok {
		return nil, errors.New("missing expiration for access token from Vault")
	}
	expires, ok := expiresData.(json.Number)
	if !ok {
		return nil, errors.Errorf("unexpected type '%T' for expiration '%s'",
			expiresData, expiresData)
	}
	expSec, err := expires.Int64()
	if err != nil {
		return nil, errors.Wrapf(err, "access token from Vault has malformed expiration")
	}

	vts.prevToken = &oauth2.Token{
		AccessToken: token,
		Expiry:      time.Unix(expSec, 0),
	}
	return vts.prevToken, nil
}

// Project returns the GCP project associated with the token source.
func (vts *VaultTokenSource) Project() string {
	return vts.project
}

// ServiceAccountEmail returns the email of the service account whose tokens are
// retrieved by the token source.
func (vts *VaultTokenSource) ServiceAccountEmail() string {
	return vts.email
}

// UpdateMetadata reads the metadata from the roleset configuration and stashes
// it in the object.
func (vts *VaultTokenSource) UpdateMetadata() error {
	vcl := vts.vaultClient.Logical()
	path := fmt.Sprintf("%s/roleset/%s", vts.enginePath, vts.roleName)
	data, err := vcl.Read(path)
	if err != nil {
		return errors.Wrapf(err, "failed to read roleset config from Vault")
	}
	if data == nil || data.Data == nil {
		return errors.New("no data reading roleset config from Vault")
	}

	projectData, ok := data.Data["project"]
	if !ok {
		return errors.New("missing 'project' key in roleset config from Vault")
	}
	project, ok := projectData.(string)
	if !ok {
		return errors.Errorf("unexpected type '%T' for 'project' key '%s'",
			projectData, projectData)
	}

	emailData, ok := data.Data["service_account_email"]
	if !ok {
		return errors.New("missing 'service_account_email' key in roleset config from Vault")
	}
	email, ok := emailData.(string)
	if !ok {
		return errors.Errorf("unexpected type '%T' for 'service_account_email' key '%s'",
			emailData, emailData)
	}

	// Assuming that the email has changed, we need to expire the token
	// immediately (this will be picked up by ReuseTokenSource, at least).
	// The use of oldToken prevents a race where one thread expires the
	// token that the first one created, but hadn't used yet.
	oldToken := vts.prevToken
	if oldToken != nil && email != vts.email {
		oldToken.Expiry = time.Now()
	}
	vts.email = email
	vts.project = project

	return nil
}

// NewVaultTokenSource returns a VaultTokenSource which will read from the path
// defined by the given secrets engine and role.  It is recommended to wrap this
// with oauth2.ReuseTokenSource().
func NewVaultTokenSource(vc *vault.Client, engine, role string) (*VaultTokenSource, error) {
	vts := &VaultTokenSource{
		vaultClient: vc,
		enginePath:  engine,
		roleName:    role,
	}

	if err := vts.UpdateMetadata(); err != nil {
		return nil, err
	}
	return vts, nil
}

// Copy copies a VaultTokenSource, excluding any state used to track token
// validity; this will return a separate stream of tokens from the original,
// suitable for separate caching by oauth2.ReuseTokenSource().
func (vts *VaultTokenSource) Copy() *VaultTokenSource {
	if vts == nil {
		return nil
	}
	return &VaultTokenSource{
		vaultClient: vts.vaultClient,
		enginePath:  vts.enginePath,
		roleName:    vts.roleName,
		project:     vts.project,
		email:       vts.email,
	}
}

type statusError interface {
	GRPCStatus() *status.Status
}

// Retry will perform an operation, and if it fails because the token it used
// was no longer valid, will call another function to update the token (and do
// anything else the caller needs when that happens), and try again.  If
// updating the token fails, that error will be logged.  The error return of the
// last execution of the operation will be returned.  If other values need to be
// returned, they must be closed over and set in the function.
func Retry(op func() error, update func() error, log func(string, error)) error {
	err := op()
	switch e := err.(type) {
	case statusError:
		// Unauthenticated and PermissionDenied are the obvious error
		// codes indicating we might be using the wrong service account,
		// but we get NotFound while trying to sign a URL.
		code := e.GRPCStatus().Code()
		if code != codes.Unauthenticated && code != codes.PermissionDenied &&
			code != codes.NotFound {
			return err
		}

	case *googleapi.Error:
		if e.Code != 403 {
			return err
		}
		forbidden := false
		for _, ge := range e.Errors {
			if ge.Reason == "forbidden" {
				forbidden = true
				break
			}
		}
		if !forbidden {
			return err
		}
	case nil:
		return nil
	default:
		return err
	}

	if ume := update(); ume == nil {
		err = op()
	} else {
		log("failed to update token source metadata", ume)
	}

	return err
}

