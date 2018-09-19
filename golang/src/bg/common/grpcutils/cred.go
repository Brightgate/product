/*
 * COPYRIGHT 2018 Brightgate Inc. All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package grpcutils

import (
	"context"
	"crypto/rsa"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"strings"
	"sync"
	"time"

	"bg/ap_common/aputil"
	"bg/base_def"

	"github.com/dgrijalva/jwt-go"
	"github.com/pkg/errors"

	"google.golang.org/grpc/metadata"
)

const cJWTExpiry = base_def.BEARER_JWT_EXPIRY_SECS * time.Second

// Overrideable for testing
var timeNowFunc = time.Now

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
	jwtClaims   jwt.StandardClaims
	jwtSigned   string
	lock        sync.Mutex
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

// refreshJWT creates a JSON Web Token (RFC 7519) authentication token
// corresponding to the credential if the current token is not present
// or is close to expiry.
func (cred *Credential) refreshJWT() (string, error) {
	var err error
	var now = timeNowFunc()
	cred.lock.Lock()
	defer cred.lock.Unlock()

	if cred.jwtSigned != "" && !cred.nearlyExpired() {
		return cred.jwtSigned, nil
	}

	jwtSigningMethod := jwt.GetSigningMethod("RS256")
	if jwtSigningMethod == nil {
		panic("couldn't get RS256 signing method")
	}
	cred.jwtSigned = ""
	cred.jwtClaims = jwt.StandardClaims{
		IssuedAt:  now.Unix(),
		ExpiresAt: now.Add(cJWTExpiry).Unix(),
		Audience:  cred.Project,
	}
	j := jwt.NewWithClaims(jwtSigningMethod, cred.jwtClaims)
	cred.jwtSigned, err = j.SignedString(cred.PrivateKey)
	if err != nil {
		return "", errors.Wrap(err, "Couldn't sign JWT")
	}
	return cred.jwtSigned, nil
}

func (cred *Credential) clientID() string {
	return fmt.Sprintf(
		"projects/%s/locations/%s/registries/%s/appliances/%s",
		cred.Project, cred.Region, cred.Registry, cred.ApplianceID)
}

// ClientID creates the appropriate Client ID for connecting to the CloudAppliance Service
func (cred *Credential) ClientID() string {
	cred.lock.Lock()
	defer cred.lock.Unlock()
	return cred.clientID()
}

func (cred *Credential) String() string {
	cred.lock.Lock()
	defer cred.lock.Unlock()
	return fmt.Sprintf("%s %+v", cred.clientID(), cred.jwtClaims)
}

// nearlyExpired indicates if the credential is in its final quartile of life
func (cred *Credential) nearlyExpired() bool {
	secsLeft := time.Duration(cred.jwtClaims.ExpiresAt-timeNowFunc().Unix()) * time.Second
	return secsLeft < (cJWTExpiry / 4)
}

// MakeGRPCContext adds 'authorization:' and 'clientid:' metadata.  This
// information flows into the gRPC headers and is used by the server to
// validate the identity of the client.
func (cred *Credential) MakeGRPCContext(ctx context.Context) (context.Context, error) {
	jwtSigned, err := cred.refreshJWT()
	if err != nil {
		return nil, errors.Wrap(err, "Failed to refresh signed JWT")
	}
	c := metadata.AppendToOutgoingContext(ctx,
		"authorization", "bearer "+jwtSigned,
		"clientid", cred.ClientID(),
	)
	return c, nil
}

// TestCredentialPEM is a private key suitable for test cases.  It doesn't
// grant access to any real resource.
const TestCredentialPEM = `
-----BEGIN PRIVATE KEY-----
MIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcwggSjAgEAAoIBAQC5c7hsfUleaCjy
W3f9PExBjrXap0MLUpcsub5kO36/v/LUR3mQMUOfN9DsG/PdXdDGAxzHs3sH38Pt
mCAj0/YkpVED2jtGaYrAHpwbkDBRw8TYS8Zyv9kyT6rZ+2+ZHnGq95XQucOuf/o2
qkuNr6FDF48Ci17SfV+qpFaPq/EoD6n/WJ3w+ABWEynembpYr0ic0cpiZfX8RRCl
79pv0OemIXcvWtICAYSpG0F7TkP+yC5SMrrfzZxtUzu9GVYa2z9XSQ8wyfVQrcJe
/bAlJ1AaN0bPI2dJNjGmUI54GHS833jhV314XzGJrXdgMtsdcy9QL9ka7kO8r0CK
su7KrI0FAgMBAAECggEAaZPFxI22/TYTSZZlQxfW2eOjCC387y8/vUipaWqtiACA
//UI8dv6AWTHXgOz26yTNIeFFPPK8PqlElhuw7biBI7RBn5xDG79fM5wVQjLWWE4
aWMKQT2TKx9Lxvlr2SIJ2ClHcyKukmNtUT218Z2xEv8QfYRWoUKa+gzA8t4SVplK
Vo8ynC8ahpxbize4n2eFIwvxw2Y5qclfB/9m2O/5iRubzb+PeMAxyjbKMrHZFlYV
XRjifeFyeRUYPER+sxdYn4aKM+rUEP7dyCxYWW55fua3d6hKPrjuptftGcDQu9Wz
Z4cPfOX+0AQqQCvjxaUVzuv/F0JIvVV9CZZH5vWAKQKBgQD2dT3c2HoNUMji3Itl
ME/VF6Wamg0CpFfhWQkvnjJWOqxcB3fi1Wjb2AqrHb7p5PZXstNsgZOCK4tdViq5
csfzUwTAejH298zByrvLk2HnLvcI6SVMZzmvsOuF95MPgOhdIbGCiFrPBnvplvjk
ZLqg8QRQDygto4DIGn2fbPzHUwKBgQDAodIf90/j9/kQfXF7PqFfRmycJJCvf19l
AJXR8Ly6R21VKrC2xWcWuGMZoNC+LjCqWDCqOUILsYBkG7nMuwa+EzsBVzLzxJ/a
wHU2eBADwh8/3zcVP9s756dzxp43fdF8S1SHL30pZdjuBay29+UlPakA3Yu1kwn6
lBynTqoHRwKBgQCDDJR4eiNsMSigeOUmSSoqBQjpzEBex0RzbwSTbWsWrtw3k0EM
PK4lOBt0Ib0CYd0bhNsnNz9YWA8i8k6FjaMEn4BHWLJ4wAsAgOyasyO76h0xf8d1
eO4Tnd+evKZV+BWWb/QTlK20p53792shBu615XKFn4mduvMfc/aYbzt6QQKBgFTk
e8feo/Shib/8qJBZ76AfVyoQ6zqMdav7cAtPfrzRUZug7rP9lwrqQ7I9rwDBNm07
5GaASV0B4sU7esyA9924d96FYU0QsColewKAMv6VBFSPuKTCuYlS8/cP5xYperK+
OAhDo3MlEU8EbTNNWEzrOZnKCRICNPmbYG1TO5dtAoGASDiWo+NIuNz/WOC1ngie
G27Cp5ghBqAVohlNO+uV6tCeOMsCa0K+Yovx3IzyRhn2ujJfzTNcwf5mv17qb5C4
q9pvLui9jB+gdAkgAKeAcOcbVZmbt8dletuKcdIm4htTe0qnqZzLrVVDx8jf0kUo
IP5drVZE6NspLWJmKxeGPlw=
-----END PRIVATE KEY-----
`

// NewTestCredential produces a credential suitable for test cases involving
// Appliance<-->Cloud RPC.  The credential provides no real-world access to
// anything.
func NewTestCredential() *Credential {
	pk, err := jwt.ParseRSAPrivateKeyFromPEM([]byte(TestCredentialPEM))
	if err != nil {
		panic("test credential: Failed to make private key")
	}

	cred := NewCredential("testproj", "testregion", "testreg", "testappliance", pk)
	if err != nil {
		panic("test credential: Failed to make cred from JSON")
	}
	return cred
}
