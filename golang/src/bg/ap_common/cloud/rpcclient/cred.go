/*
 * COPYRIGHT 2018 Brightgate Inc. All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package rpcclient

import (
	"context"
	"crypto/rsa"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"strings"
	"time"

	"bg/ap_common/aputil"

	jwt "github.com/dgrijalva/jwt-go"
	"github.com/pkg/errors"

	"google.golang.org/grpc/metadata"
)

var (
	credPathFlag = flag.String("cloud-cred-path",
		"/etc/secret/cloud/cloud.secret.json",
		"cloud service JSON credential")
)

// Credential contains the necessary identifiers to connect the
// Appliance Cloud endpoint
type Credential struct {
	Project     string `json:"project"`
	Region      string `json:"region"`
	Registry    string `json:"registry"`
	ApplianceID string `json:"appliance_id"`
	PrivateKey  *rsa.PrivateKey
	jwtClaims   jwt.Claims
}

type credentialJSON struct {
	Credential
	PrivateKeyPEM string `json:"private_key"`
}

// NewCredential creates a credential from each sub-part
func NewCredential(project, region, registry, applianceID string,
	privateKey *rsa.PrivateKey) *Credential {

	return &Credential{
		Project:     project,
		Region:      region,
		Registry:    registry,
		ApplianceID: applianceID,
		PrivateKey:  privateKey,
	}
}

// NewCredentialFromJSON creates a new credential from a JSON representation
// of the credential.  The RSA Private key must be PEM encoded.  The PEM block
// must have '\n' substituted for newlines.
func NewCredentialFromJSON(data []byte) (*Credential, error) {
	var credj credentialJSON
	err := json.Unmarshal(data, &credj)
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to parse Credential from JSON")
	}
	// Put escaped \n's back into the RFC1421 PEM block
	pem := strings.Replace(credj.PrivateKeyPEM, "\\n", "\n", -1)
	pk, err := jwt.ParseRSAPrivateKeyFromPEM([]byte(pem))
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to create Credential from JSON")
	}
	credj.Credential.PrivateKey = pk
	return &credj.Credential, nil
}

// SystemCredential creates a new credential based on the system default
// storage location, or -cred-path, if given.
func SystemCredential() (*Credential, error) {
	credPath := aputil.ExpandDirPath(*credPathFlag)

	credFile, err := ioutil.ReadFile(credPath)
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to read credential file '%s'", credPath)
	}
	cred, err := NewCredentialFromJSON(credFile)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to build credential")
	}
	return cred, nil
}

// makeJWT creates a JSON Web Token (RFC 7519) authentication token
// corresponding to the credential.
func (cred *Credential) makeJWT() (string, error) {
	jwtSigningMethod := jwt.GetSigningMethod("RS256")
	if jwtSigningMethod == nil {
		panic("couldn't get RS256 signing method")
	}
	cred.jwtClaims = jwt.StandardClaims{
		IssuedAt:  time.Now().Unix(),
		ExpiresAt: time.Now().Add(cJWTExpiry).Unix(),
		Audience:  cred.Project,
	}
	j := jwt.NewWithClaims(jwtSigningMethod, cred.jwtClaims)
	signedJWT, err := j.SignedString(cred.PrivateKey)
	if err != nil {
		return "", errors.Wrap(err, "Couldn't sign JWT")
	}
	return signedJWT, nil
}

// ClientID creates the appropriate Client ID for connecting to the CloudAppliance Service
func (cred *Credential) ClientID() string {
	return fmt.Sprintf(
		"projects/%s/locations/%s/registries/%s/appliances/%s",
		cred.Project, cred.Region, cred.Registry, cred.ApplianceID)
}

func (cred *Credential) String() string {
	return fmt.Sprintf("%s %+v", cred.ClientID(), cred.jwtClaims)
}

// MakeGRPCContext adds 'authorization:' and 'clientid:' metadata.  This
// information flows into the gRPC headers and is used by the server to
// validate the identity of the client.
func (cred *Credential) MakeGRPCContext(ctx context.Context) (context.Context, error) {
	jwt, err := cred.makeJWT()
	if err != nil {
		return nil, errors.Wrap(err, "Failed to make context")
	}
	c := metadata.AppendToOutgoingContext(ctx,
		"authorization", "bearer "+jwt,
		"clientid", cred.ClientID(),
	)
	return c, nil
}
