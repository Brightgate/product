/*
 * COPYRIGHT 2017 Brightgate Inc. All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package iotcore

import (
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	jwt "github.com/dgrijalva/jwt-go"
	"github.com/pkg/errors"
)

// IoTCredential contains the necessary identifiers to connect the Google
// Cloud IoT Core MQTT broker.
type IoTCredential struct {
	Project    string `json:"project"`
	Region     string `json:"region"`
	Registry   string `json:"registry"`
	DeviceID   string `json:"device_id"`
	PrivateKey *rsa.PrivateKey
	jwtClaims  jwt.Claims
}

type iotCredentialJSON struct {
	IoTCredential
	PrivateKeyPEM string `json:"private_key"`
}

// NewCredential creates a credential from each sub-part
func NewCredential(project, region, registry, deviceID string,
	privateKey *rsa.PrivateKey) *IoTCredential {

	return &IoTCredential{
		Project:    project,
		Region:     region,
		Registry:   registry,
		DeviceID:   deviceID,
		PrivateKey: privateKey,
	}
}

// NewCredentialFromJSON creates a new credential from a JSON representation
// of the credential.  The RSA Private key must be PEM encoded.  The PEM block
// must have '\n' substituted for newlines.
func NewCredentialFromJSON(data []byte) (*IoTCredential, error) {
	var iotj iotCredentialJSON
	err := json.Unmarshal(data, &iotj)
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to parse Credential from JSON")
	}
	// Put escaped \n's back into the RFC1421 PEM block
	pem := strings.Replace(iotj.PrivateKeyPEM, "\\n", "\n", -1)
	pk, err := jwt.ParseRSAPrivateKeyFromPEM([]byte(pem))
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to create Credential from JSON")
	}
	iotj.PrivateKey = pk
	return &iotj.IoTCredential, nil
}

func (cred *IoTCredential) makeJWT() (string, error) {
	jwtSigningMethod := jwt.GetSigningMethod("RS256")
	if jwtSigningMethod == nil {
		return "", errors.New("Couldn't find signing method")
	}
	cred.jwtClaims = jwt.StandardClaims{
		IssuedAt:  time.Now().Unix(),
		ExpiresAt: time.Now().Unix() + cJWTExpiry,
		Audience:  cred.Project,
	}
	j := jwt.NewWithClaims(jwtSigningMethod, cred.jwtClaims)
	signedJWT, err := j.SignedString(cred.PrivateKey)
	if err != nil {
		return "", errors.Wrap(err, "Couldn't sign JWT")
	}
	return signedJWT, nil
}

// clientID creates the appropriate Client ID for connecting to IoT Core.
func (cred *IoTCredential) clientID() string {
	return fmt.Sprintf(
		"projects/%s/locations/%s/registries/%s/devices/%s",
		cred.Project, cred.Region, cred.Registry, cred.DeviceID)
}

func (cred *IoTCredential) String() string {
	return fmt.Sprintf("%s %+v", cred.clientID(), cred.jwtClaims)
}
