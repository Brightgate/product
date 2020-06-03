/*
 * COPYRIGHT 2020 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package vaulttokensource

import (
	"encoding/json"
	"fmt"
	"time"

	vault "github.com/hashicorp/vault/api"
	"github.com/pkg/errors"
	"golang.org/x/oauth2"
)

// VaultTokenSource is an oauth2.TokenSource that returns an access token pulled
// from Vault for a GCP service account.
type VaultTokenSource struct {
	vaultClient *vault.Client
	enginePath  string
	roleName    string
}

// Token reads an access token from Vault and returns an oauth2.Token based on
// that.
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

	return &oauth2.Token{
		AccessToken: token,
		Expiry:      time.Unix(expSec, 0),
	}, nil
}

// NewVaultTokenSource returns a VaultTokenSource which will read from the path
// defined by the given secrets engine and role.  It is recommended to wrap this
// with oauth2.ReuseTokenSource().
func NewVaultTokenSource(vc *vault.Client, engine, role string) *VaultTokenSource {
	return &VaultTokenSource{
		vaultClient: vc,
		enginePath:  engine,
		roleName:    role,
	}
}
