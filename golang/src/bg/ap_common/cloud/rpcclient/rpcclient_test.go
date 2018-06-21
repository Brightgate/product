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
	"fmt"
	"regexp"
	"strings"
	"testing"

	"google.golang.org/grpc/metadata"

	jwt "github.com/dgrijalva/jwt-go"
	"github.com/fgrosse/zaptest"
	"go.uber.org/zap"
)

const testProject = "peppy-breaker-161717"
const testRegion = "us-central1"
const testRegistry = "unit-testing"
const testApplianceID = "unit-testing-fake-appliance"

// This key doesn't grant access to any real resource.
const testPEM = `
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

func setupLogging(t *testing.T) (*zap.Logger, *zap.SugaredLogger) {
	logger := zaptest.Logger(t)
	slogger := logger.Sugar()
	return logger, slogger
}

func mkCred(t *testing.T) *Credential {
	pk, err := jwt.ParseRSAPrivateKeyFromPEM([]byte(testPEM))
	if err != nil {
		t.Errorf("Failed to make private key")
		return nil
	}

	cred := NewCredential(testProject, testRegion, testRegistry, testApplianceID, pk)
	if err != nil {
		t.Errorf("Failed to make cred from JSON")
		return nil
	}
	return cred
}

func TestNewCredential(t *testing.T) {
	setupLogging(t)
	cred := mkCred(t)

	// Exercise String() method of cred
	t.Logf("created cred %s", cred)
}

func TestContext(t *testing.T) {
	setupLogging(t)
	cred := mkCred(t)

	testContext, err := cred.MakeGRPCContext(context.Background())
	if err != nil {
		t.Errorf("Failed to make grpc context")
		return
	}
	md, _ := metadata.FromOutgoingContext(testContext)
	if md.Get("clientid")[0] != cred.ClientID() {
		t.Errorf("Unexpected clientid header")
		return
	}
	re := regexp.MustCompile("bearer [-_.a-zA-Z0-9]+")
	if !re.Match([]byte(md.Get("authorization")[0])) {
		t.Errorf("Unexpected authorization header")
		return
	}
}

func TestNewJSON(t *testing.T) {
	setupLogging(t)
	pemstr := strings.Replace(testPEM, "\n", "\\n", -1)
	credJSON := fmt.Sprintf(`
		{
		"project": "%s",
		"region": "%s",
		"registry": "%s",
		"appliance_id": "%s",
		"private_key": "%s"
		}`, testProject, testRegion, testRegistry, testApplianceID, pemstr)

	cred, err := NewCredentialFromJSON([]byte(credJSON))
	if err != nil {
		t.Errorf("Failed to make cred from JSON")
	}

	// Also exercise String() method of cred
	t.Logf("created cred %s", cred)
}

func TestBadJSON(t *testing.T) {
	setupLogging(t)
	// Mostly valid with a bad PEM
	badPEM := fmt.Sprintf(`{
		"project": "%s",
		"region": "%s",
		"registry": "%s",
		"appliance_id": "%s",
		"private_key": "nonsense"
		}`, testProject, testRegion, testRegistry, testApplianceID)

	for _, badJSON := range []string{"{}", "", "badjson", badPEM} {
		_, err := NewCredentialFromJSON([]byte(badJSON))
		if err == nil {
			t.Errorf("Unexpected success making cred from bad JSON")
			return
		}
		t.Logf("Saw expected error: %s", err)
	}
}
