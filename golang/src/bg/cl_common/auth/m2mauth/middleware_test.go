/*
 * COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 *
 */

package m2mauth

import (
	"context"
	"crypto/rsa"
	"io/ioutil"
	"os"
	"testing"
	"time"

	"bg/cloud_models/appliancedb"
	"bg/cloud_models/appliancedb/mocks"

	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"

	"github.com/dgrijalva/jwt-go"
	"github.com/grpc-ecosystem/go-grpc-middleware/util/metautils"
	"github.com/satori/uuid"
	"github.com/stretchr/testify/mock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type MockAppliance struct {
	appliancedb.ApplianceID
	ClientID      string
	Prefix        string
	PrivateKeyPEM []byte
	PrivateKey    *rsa.PrivateKey
	PublicKeyPEM  []byte
	Keys          []appliancedb.AppliancePubKey
}

var (
	mockAppliances = []*MockAppliance{
		{
			ApplianceID: appliancedb.ApplianceID{
				CloudUUID: uuid.Must(uuid.FromString("b3798a8e-41e0-4939-a038-e7675af864d5")),
			},
			ClientID: "projects/foo/locations/bar/registries/baz/appliances/mock0",
			Prefix:   "mock0",
		},
		{
			ApplianceID: appliancedb.ApplianceID{
				CloudUUID: uuid.Must(uuid.FromString("099239f6-d8cd-4e57-a696-ef84a3bf39d0")),
			},
			ClientID: "projects/foo/locations/bar/registries/baz/appliances/mock1",
			Prefix:   "mock1",
		},
	}
)

func setupLogging(t *testing.T) (*zap.Logger, *zap.SugaredLogger) {
	// Assign globals
	logger := zaptest.NewLogger(t)
	slogger := logger.Sugar()
	grpc_zap.ReplaceGrpcLogger(logger)
	return logger, slogger
}

func assertErrAndCode(t *testing.T, err error, code codes.Code) {
	if err == nil {
		t.Fatalf("Expected error, but got nil")
	}
	s, ok := status.FromError(err)
	if !ok {
		t.Fatalf("Could not get GRPC status from Error!")
	}
	if s.Code() != code {
		t.Fatalf("Expected code %v, but got %v", code.String(), s.Code().String())
	}
	t.Logf("Saw expected err (code=%s)", code.String())
}

func makeBearer(ma *MockAppliance, offset int32) string {
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"exp": int32(time.Now().Unix()) + offset,
	})

	tokenString, err := token.SignedString(ma.PrivateKey)
	if err != nil {
		panic(err)
	}
	return "bearer " + tokenString
}

func TestBasic(t *testing.T) {
	_, _ = setupLogging(t)
	reinitAuthCache()
	m := mockAppliances[0]

	dMock := &mocks.DataStore{}
	dMock.On("ApplianceIDByClientID", mock.Anything, m.ClientID).Return(&m.ApplianceID, nil)
	dMock.On("KeysByUUID", mock.Anything, mock.Anything).Return(m.Keys, nil)
	defer dMock.AssertExpectations(t)

	ctx := metautils.ExtractIncoming(context.Background()).
		Add("authorization", makeBearer(m, 600)).
		Add("clientid", m.ClientID).
		ToIncoming(context.Background())
	resultctx, err := authFunc(ctx, dMock)
	if err != nil {
		t.Fatalf("saw unexpected error: %+v", err)
	}
	if resultctx == nil {
		t.Fatalf("resultctx is nil")
	}
	if len(authCache) != 1 {
		t.Fatalf("authCache has unexpected size")
	}
	// try again; we expect this to be served from cache
	resultctx, err = authFunc(ctx, dMock)
	if err != nil {
		t.Fatalf("saw unexpected error: %+v", err)
	}
	if resultctx == nil {
		t.Fatalf("resultctx is nil")
	}
}

func TestBadBearer(t *testing.T) {
	_, _ = setupLogging(t)
	reinitAuthCache()
	m := mockAppliances[0]

	dMock := &mocks.DataStore{}
	dMock.On("ApplianceIDByClientID", mock.Anything, m.ClientID).Return(&m.ApplianceID, nil)
	dMock.On("KeysByUUID", mock.Anything, mock.Anything).Return(m.Keys, nil)
	defer dMock.AssertExpectations(t)

	ctx := metautils.ExtractIncoming(context.Background()).
		Add("authorization", "bearer bogus").
		Add("clientid", m.ClientID).
		ToIncoming(context.Background())
	_, err := authFunc(ctx, dMock)
	assertErrAndCode(t, err, codes.Unauthenticated)
}

func TestExpiredBearer(t *testing.T) {
	_, _ = setupLogging(t)
	reinitAuthCache()
	m := mockAppliances[0]

	dMock := &mocks.DataStore{}
	dMock.On("ApplianceIDByClientID", mock.Anything, m.ClientID).Return(&m.ApplianceID, nil)
	dMock.On("KeysByUUID", mock.Anything, m.CloudUUID).Return(m.Keys, nil)
	defer dMock.AssertExpectations(t)

	ctx := metautils.ExtractIncoming(context.Background()).
		Add("authorization", makeBearer(m, -600)).
		Add("clientid", m.ClientID).
		ToIncoming(context.Background())
	_, err := authFunc(ctx, dMock)
	assertErrAndCode(t, err, codes.Unauthenticated)
}

func TestExpiredBearerCached(t *testing.T) {
	_, _ = setupLogging(t)
	reinitAuthCache()
	m := mockAppliances[0]

	dMock := &mocks.DataStore{}
	defer dMock.AssertExpectations(t)

	// Manufacture an expired token, make a bearer for it, then parse the
	// token with claims validation disabled, and stuff the cache it.
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"exp": int32(time.Now().Unix()) - 1000,
	})

	tokenString, err := token.SignedString(m.PrivateKey)
	if err != nil {
		panic(err)
	}
	bearer := "bearer " + tokenString

	parser := &jwt.Parser{SkipClaimsValidation: true}
	parsedToken, err := parser.Parse(tokenString, func(*jwt.Token) (interface{}, error) {
		return m.PrivateKey, nil
	})
	authCache[m.ClientID] = &authCacheEntry{
		Token:     parsedToken,
		CloudUUID: m.CloudUUID,
	}
	if len(authCache) != 1 {
		t.Fatalf("authCache has unexpected size != 1: %v", len(authCache))
	}

	ctx := metautils.ExtractIncoming(context.Background()).
		Add("authorization", bearer).
		Add("clientid", m.ClientID).
		ToIncoming(context.Background())
	_, err = authFunc(ctx, dMock)
	assertErrAndCode(t, err, codes.Unauthenticated)
	if len(authCache) != 0 {
		t.Fatalf("authCache has unexpected size > 0: %v", len(authCache))
	}
}

func TestBadClientID(t *testing.T) {
	_, _ = setupLogging(t)
	reinitAuthCache()
	m := mockAppliances[0]

	dMock := &mocks.DataStore{}
	dMock.On("ApplianceIDByClientID", mock.Anything, mock.Anything).Return(nil, appliancedb.NotFoundError{})
	defer dMock.AssertExpectations(t)

	ctx := metautils.ExtractIncoming(context.Background()).
		Add("authorization", makeBearer(m, 600)).
		Add("clientid", m.ClientID+"bad").
		ToIncoming(context.Background())
	_, err := authFunc(ctx, dMock)
	assertErrAndCode(t, err, codes.Unauthenticated)
}

func TestCertMismatch(t *testing.T) {
	_, _ = setupLogging(t)
	reinitAuthCache()
	m := mockAppliances[0]
	m1 := mockAppliances[1]

	dMock := &mocks.DataStore{}
	dMock.On("ApplianceIDByClientID", mock.Anything, m.ClientID).Return(&m.ApplianceID, nil)
	dMock.On("KeysByUUID", mock.Anything, m.CloudUUID).Return(m1.Keys, nil)
	defer dMock.AssertExpectations(t)

	ctx := metautils.ExtractIncoming(context.Background()).
		Add("authorization", makeBearer(m, 600)).
		Add("clientid", m.ClientID).
		ToIncoming(context.Background())
	_, err := authFunc(ctx, dMock)
	assertErrAndCode(t, err, codes.Unauthenticated)
}

// We are mostly protected by our JWT library which doesn't allow unsigned
// JWTs, but because it is such a substantial threat, we test it anyway.
// https://auth0.com/blog/critical-vulnerabilities-in-json-web-token-libraries/
func TestUnsignedBearer(t *testing.T) {
	_, _ = setupLogging(t)
	reinitAuthCache()
	m := mockAppliances[0]

	dMock := &mocks.DataStore{}
	dMock.On("ApplianceIDByClientID", mock.Anything, m.ClientID).Return(&m.ApplianceID, nil)
	dMock.On("KeysByUUID", mock.Anything, mock.Anything).Return(m.Keys, nil)
	defer dMock.AssertExpectations(t)

	token := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.MapClaims{
		"exp": int32(time.Now().Unix()) + 10000,
	})
	tokenString, err := token.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		panic(err)
	}
	bearerToken := "bearer " + tokenString

	ctx := metautils.ExtractIncoming(context.Background()).
		Add("authorization", bearerToken).
		Add("clientid", m.ClientID).
		ToIncoming(context.Background())
	_, err = authFunc(ctx, dMock)
	assertErrAndCode(t, err, codes.Unauthenticated)
}

func TestNoKeys(t *testing.T) {
	_, _ = setupLogging(t)
	reinitAuthCache()
	m := mockAppliances[0]

	dMock := &mocks.DataStore{}
	dMock.On("ApplianceIDByClientID", mock.Anything, m.ClientID).Return(&m.ApplianceID, nil)
	dMock.On("KeysByUUID", mock.Anything, m.CloudUUID).Return([]appliancedb.AppliancePubKey{}, nil)
	defer dMock.AssertExpectations(t)

	ctx := metautils.ExtractIncoming(context.Background()).
		Add("authorization", makeBearer(m, 600)).
		Add("clientid", m.ClientID).
		ToIncoming(context.Background())
	_, err := authFunc(ctx, dMock)
	assertErrAndCode(t, err, codes.Unauthenticated)
}

func TestEmptyContext(t *testing.T) {
	_, _ = setupLogging(t)

	dMock := &mocks.DataStore{}
	dMock.AssertExpectations(t)

	_, err := authFunc(context.Background(), dMock)
	assertErrAndCode(t, err, codes.Unauthenticated)
}

func loadMock(mock *MockAppliance) {
	var err error
	mock.PrivateKeyPEM, err = ioutil.ReadFile(mock.Prefix + ".rsa_private.pem")
	if err != nil {
		panic(err)
	}
	mock.PrivateKey, err = jwt.ParseRSAPrivateKeyFromPEM(mock.PrivateKeyPEM)
	if err != nil {
		panic(err)
	}
	mock.PublicKeyPEM, err = ioutil.ReadFile(mock.Prefix + ".rsa_cert.pem")
	if err != nil {
		panic(err)
	}
	mock.Keys = []appliancedb.AppliancePubKey{
		{
			ID:     0,
			Format: "RS256_X509",
			Key:    string(mock.PublicKeyPEM),
		},
	}
}

func TestMain(m *testing.M) {
	for _, m := range mockAppliances {
		loadMock(m)
	}
	os.Exit(m.Run())
}
