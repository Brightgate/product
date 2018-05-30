/*
 * COPYRIGHT 2018 Brightgate Inc. All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package cloudiotsvc

import (
	"context"
	"fmt"
	"testing"

	"cloud.google.com/go/pubsub"
	"github.com/fgrosse/zaptest"
	"go.uber.org/zap"
	"golang.org/x/oauth2/google"
	cloudiot "google.golang.org/api/cloudiot/v1"
	apioption "google.golang.org/api/option"
)

const testProject = "b10e-unit-testing"
const testRegion = "us-central1"
const testRegistry = "iot-unit-testing"
const testDevice = "unit-testing-appliance1"

// XXX This credential contains a GCP private key for the iot-unit-testing
// service account.  However, the key's scope is limited to the
// "b10e-unit-testing" project, so this is not much of a security problem; when
// we have a key management solution, this credential should be regenerated and
// saved there.
const testCredential = `
{
  "type": "service_account",
  "project_id": "b10e-unit-testing",
  "private_key_id": "89968ea11f49571542aafe00d371c149e1a45b61",
  "private_key": "-----BEGIN PRIVATE KEY-----\nMIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcwggSjAgEAAoIBAQDdHMgdIxGZpL2Q\n3x7kmcfw7uagpK01sTMWDAIk1xHV/aRLmohfnshbEYL+UdFSD7VdOD/zUu952Qr/\n+hC2Oop5EaoGzFUqYHtG7LICSGvbtkpwYi95TPSbdwbGk7qMIyf/MONc6mgh0Qay\nrj8XRfwwVtI/2GGYuqREax9jDBD00ia2/mjyL1mZ3Br8UsN888ymgxK/uX5eUVLK\nNjLEzw9TESvellz09PiXSOH94w/38wCgwNmIb4eMiwBv0kbnh/n0NSWaxsdM3VyB\nk7br8kHnqSsjhdOHfkG3QAK2lMVSnWzIrXmt4Q9t2+bE72jcFcDMNzg0yle41XuY\ndQSTkhxnAgMBAAECggEADwHFEzUuHJ9xvkNmdV16lH+iZ4TFvL8qGHT4MEfojf2J\nCRiT6Ol977Bgk6I58rfeN1V6Aam/VyXD+Vufhr6yZ0UrpQp5PUcPFuE5s632pBLb\nOoVvc2wlreeGLjQYlSpNrKREyimep6zoJ3hsD8hQNXevDWZCOXtxarNajf5jqDn6\nyCZXEGrdmMPUfOJh3DvXlIRBTelYx3ZoIdrNC1LIm1ShBvJOErlCXcRcjXgnktMX\nOb5xvqwwoJwZIvhgY7pJ6yeFO+YsNyXFlm+9r5uZwgoK1GiVEjkyKtQNWfpcBwx5\nvyN4tVGiSRm6csh05CKPV3CDNfaFDvNOl2UFNXrtAQKBgQD+j1Ej6smrHXG3gTO+\nnbyw4Nwkm2NIzXeKVsM5Agkb7lE6hSesK6Jy98wr0lnT4Ef5uT0tNiEPNqochBft\nHAP1K0St2aEC+UpPWZL2q12kv9De1tTadN1Bf8EFGpGUEAhQXbaCNsKR/cY9iqHL\ni0v4UX5NbqAjHasb952EcymNMwKBgQDeXQW3v3VzV3+6AEA+C4yHfhB+l8Ck8ZaV\nU/iQbhBQgmBh8wZYGBg28fpGo+XzohrLOvyQ3I4Mx1VHbXuJ53KoYqQDh2XCo5M9\nQma6w7rASMb7Wk7Q2o5UtGAhou+CNwrvjEahUbB4NzhzGPwjCXPxV7zoj0YeMMjn\nTNXasA4r/QKBgQDv3af9ii2BmfsfiRVzFjtJCHkn3WvOnB16M4s9Wpeuw/+yfuoF\nKBCo+Kpg2JNgPMRVoaDty0WXilD9EdNhz7ZC/QR4NMute63z21nKKWvR5BUzBYgI\nWXprT7BX2NM4i2rqH4PsayEoY9K7BriyjY2GbXPwDr/ClyA2+DprJgEPVQKBgEs9\n7tFeV7/Pu8iUjShxf/vZDHvJncYyeWHOKC23EI4tj6+VLHBits7g0m9UxlrKX4al\nTxE1kFuCl7izsznWt1WDCzymdCiIcSopbdmEoYyvE6W5yTGiwsamwmCfYawONAUa\n0kuD+NK03MUVjzvL1w+zQJjw4ikVGOYrebGmISWBAoGAajYZ8Pr94aFNeHQcrs8M\nvbU8pWJVN+jpeT5eKEnhq/faudl8RPn0TzTsxZUmdqxXyF04koUJ2MAWUVWM7beV\nH6DlMK4Hkvj+JSAznaSeRi3w+alLNOEIFihuqFgUiefyYEK0njnbAXdEM98K2LSd\n0UFeOG/XKArsVD99Q2Lp2ZI=\n-----END PRIVATE KEY-----\n",
  "client_email": "iot-unit-testing@b10e-unit-testing.iam.gserviceaccount.com",
  "client_id": "111449156493841715421",
  "auth_uri": "https://accounts.google.com/o/oauth2/auth",
  "token_uri": "https://accounts.google.com/o/oauth2/token",
  "auth_provider_x509_cert_url": "https://www.googleapis.com/oauth2/v1/certs",
  "client_x509_cert_url": "https://www.googleapis.com/robot/v1/metadata/x509/iot-unit-testing%40b10e-unit-testing.iam.gserviceaccount.com"
}
`

// Utility function to remove subscriptions
func (c *serviceImpl) deleteSubs(ctx context.Context) {
	for _, tName := range []string{"events", "state"} {
		subName := fmt.Sprintf("iot-%s-%s-cl-eventd", c.registry, tName)
		sub := c.pubsubClient.Subscription(subName)
		exists, _ := sub.Exists(ctx)
		if exists {
			err := sub.Delete(ctx)
			if err != nil {
				panic(err)
			}
		}
		exists, _ = sub.Exists(ctx)
		if exists {
			panic("sub still exists!")
		}
	}
}

func setupLogging(t *testing.T) (*zap.Logger, *zap.SugaredLogger) {
	logger := zaptest.Logger(t)
	slogger := logger.Sugar()
	return logger, slogger
}

func newTestClient(ctx context.Context) *serviceImpl {
	// Build an Oauth2 'Config' from the JSON above, requesting access
	// using the listed scopes.  Then turn that into an http.Client
	config, err := google.JWTConfigFromJSON([]byte(testCredential),
		cloudiot.CloudiotScope, pubsub.ScopePubSub)
	httpclient := config.Client(ctx)

	pso := apioption.WithTokenSource(config.TokenSource(ctx))
	pubsub, err := pubsub.NewClient(ctx, testProject, pso)
	if err != nil {
		panic(err)
	}

	svcImpl, err := newServiceImpl(ctx, httpclient, pubsub, testProject,
		testRegion, testRegistry)
	if err != nil {
		panic(err)
	}
	return svcImpl
}

func TestService(t *testing.T) {
	var err error
	_, slog := setupLogging(t)
	ctx := context.Background()
	client := newTestClient(ctx)

	t.Run("GetRegistry", func(t *testing.T) {
		reg, err := client.GetRegistry()
		if err != nil {
			t.Errorf("Failed to GetRegistry: %s", err)
		}
		slog.Debugw("Got registry", "reg", reg)
		if reg.Id != testRegistry {
			t.Errorf("Registry had unexpected ID")
		}
	})

	t.Run("GetDevice", func(t *testing.T) {
		dev, err := client.GetDevice(testDevice)
		if err != nil {
			t.Errorf("Failed to GetDevice: %s", err)
		}
		slog.Debugw("Got device", "dev", dev)
		if dev.Id != testDevice {
			t.Errorf("Device had unexpected ID")
		}
	})

	t.Run("DeleteSub, Subscribe", func(t *testing.T) {
		client.deleteSubs(ctx)
		_, err = client.SubscribeState(context.Background())
		if err != nil {
			t.Errorf("Failed to GetDevice: %s", err)
		}
		_, err = client.SubscribeEvents(context.Background())
		if err != nil {
			t.Errorf("Failed to GetDevice: %s", err)
		}
	})

	t.Run("Subscribe [subs exist already]", func(t *testing.T) {
		_, err = client.SubscribeState(context.Background())
		if err != nil {
			t.Errorf("Failed to GetDevice: %s", err)
		}
		_, err = client.SubscribeEvents(context.Background())
		if err != nil {
			t.Errorf("Failed to GetDevice: %s", err)
		}
	})
	client.deleteSubs(ctx)
}
