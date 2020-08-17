/*
 * Copyright 2019 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package m2mauth

import (
	"context"
	"crypto/rsa"
	"io/ioutil"
	"os"
	"testing"
	"time"

	"bg/base_def"
	"bg/cloud_models/appliancedb"
	"bg/cloud_models/appliancedb/mocks"

	"go.uber.org/zap"
	"go.uber.org/zap/zapgrpc"
	"go.uber.org/zap/zaptest"

	"github.com/dgrijalva/jwt-go"
	"github.com/grpc-ecosystem/go-grpc-middleware/util/metautils"
	"github.com/satori/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/grpclog"
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
				ApplianceUUID: uuid.Must(uuid.FromString("b3798a8e-41e0-4939-a038-e7675af864d5")),
				SiteUUID:      uuid.Must(uuid.FromString("3ca1ba0c-629d-44a2-8945-fc22d0c4bf0c")),
			},
			ClientID: "projects/foo/locations/bar/registries/baz/appliances/mock0",
			Prefix:   "mock0",
		},
		{
			ApplianceID: appliancedb.ApplianceID{
				ApplianceUUID: uuid.Must(uuid.FromString("099239f6-d8cd-4e57-a696-ef84a3bf39d0")),
				SiteUUID:      uuid.Must(uuid.FromString("9b941221-e7ab-4760-a1c8-9757e96bcfd8")),
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

	// Redirect grpc internal log messages to zap, at DEBUG
	glogger := logger.WithOptions(
		// zapgrpc adds extra frames, which need to be skipped
		zap.AddCallerSkip(3),
	)
	grpclog.SetLogger(zapgrpc.NewLogger(glogger, zapgrpc.WithDebug()))
	return logger, slogger
}

func assertErrAndCode(t *testing.T, err error, code codes.Code) {
	assert := require.New(t)
	assert.Error(err, "Expected error")

	s, ok := status.FromError(err)
	assert.True(ok, "Could not get GRPC status from Error!")
	assert.Equal(code.String(), s.Code().String())

	t.Logf("Saw expected err (code=%s)", code.String())
}

func mbCommon(ma *MockAppliance, token *jwt.Token) string {
	tokenString, err := token.SignedString(ma.PrivateKey)
	if err != nil {
		panic(err)
	}
	return "bearer " + tokenString
}

func makeBearer(ma *MockAppliance) string {
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"exp": int32(time.Now().Unix()) + base_def.BEARER_JWT_EXPIRY_SECS,
	})
	return mbCommon(ma, token)
}

func makeBearerOffset(ma *MockAppliance, offset int32) string {
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"exp": int32(time.Now().Unix()) + offset,
	})
	return mbCommon(ma, token)
}

func makeBearerNoClaims(ma *MockAppliance) string {
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{})
	return mbCommon(ma, token)
}

func makeBearerUnsigned(ma *MockAppliance) string {
	token := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.MapClaims{
		"exp": int32(time.Now().Unix()) + base_def.BEARER_JWT_EXPIRY_SECS,
	})
	tokenString, err := token.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		panic(err)
	}
	return "bearer " + tokenString
}

func TestBasic(t *testing.T) {
	_, _ = setupLogging(t)
	m := mockAppliances[0]
	assert := require.New(t)

	dMock := &mocks.DataStore{}
	dMock.On("ApplianceIDByClientID", mock.Anything, m.ClientID).Return(&m.ApplianceID, nil)
	dMock.On("KeysByUUID", mock.Anything, mock.Anything).Return(m.Keys, nil)
	defer dMock.AssertExpectations(t)

	mw := New(dMock)
	ctx := metautils.ExtractIncoming(context.Background()).
		Add("authorization", makeBearer(m)).
		Add("clientid", m.ClientID).
		ToIncoming(context.Background())
	uu := metautils.ExtractIncoming(ctx).Get("appliance_uuid")
	assert.Equal("", uu, "saw unexpected appliance_uuid in ctx")
	uu = metautils.ExtractIncoming(ctx).Get("site_uuid")
	assert.Equal("", uu, "saw unexpected site_uuid in ctx")

	resultctx, err := mw.authFunc(ctx)
	assert.NoError(err)

	// check resultant context looks good
	assert.NotNil(resultctx)
	uu = metautils.ExtractIncoming(resultctx).Get("appliance_uuid")
	assert.Equal(m.ApplianceID.ApplianceUUID.String(), uu,
		"appliance_uuid was not set as expected in context")
	uu = metautils.ExtractIncoming(resultctx).Get("site_uuid")
	assert.Equal(m.ApplianceID.SiteUUID.String(), uu,
		"site_uuid was not set as expected in context")
	assert.Equal(1, mw.authCache.Len(false), "authCache has unexpected size")

	// try again; we expect this to be served from cache
	resultctx, err = mw.authFunc(ctx)
	assert.NoError(err)

	// check resultant context, generated from cache, looks good
	assert.NotNil(resultctx)
	uu = metautils.ExtractIncoming(resultctx).Get("appliance_uuid")
	assert.Equal(m.ApplianceID.ApplianceUUID.String(), uu,
		"appliance_uuid was not set as expected in context")
	uu = metautils.ExtractIncoming(resultctx).Get("site_uuid")
	assert.Equal(m.ApplianceID.SiteUUID.String(), uu,
		"site_uuid was not set as expected in context")
	assert.Equal(1, mw.authCache.Len(false), "authCache has unexpected size")
}

// TestExpLeeway tests the case where the client is initiating a connection, but
// its clock is slightly ahead of the server, resulting in the Expiry being
// slightly more than BEARER_JWT_EXPIRY_SECS in the "future" (as seen by the
// server).  The code allows a leeway period for this purpose.
func TestExpLeeway(t *testing.T) {
	_, _ = setupLogging(t)
	m := mockAppliances[0]
	assert := require.New(t)

	dMock := &mocks.DataStore{}
	dMock.On("ApplianceIDByClientID", mock.Anything, m.ClientID).Return(&m.ApplianceID, nil)
	dMock.On("KeysByUUID", mock.Anything, mock.Anything).Return(m.Keys, nil)
	defer dMock.AssertExpectations(t)

	mw := New(dMock)

	ctx := metautils.ExtractIncoming(context.Background()).
		Add("authorization", makeBearerOffset(m, base_def.BEARER_JWT_EXPIRY_SECS+10)).
		Add("clientid", m.ClientID).
		ToIncoming(context.Background())
	resultctx, err := mw.authFunc(ctx)
	assert.NoError(err)
	assert.NotNil(resultctx)
}

// TestBadBearer tests a series of cases where the setup/teardown
// is all exactly the same.
func TestBadBearer(t *testing.T) {
	_, _ = setupLogging(t)
	m := mockAppliances[0]
	assert := require.New(t)

	testCases := []struct {
		desc   string
		bearer string
	}{
		{"BogusBearer", "bearer bogus"},
		// We are mostly protected by our JWT library which doesn't
		// allow unsigned JWTs, but because it is such a substantial
		// threat, we test it anyway.
		// https://auth0.com/blog/critical-vulnerabilities-in-json-web-token-libraries/
		{"UnsignedBearer", makeBearerUnsigned(m)},
		{"ExpClaimExcessive", makeBearerOffset(m, base_def.BEARER_JWT_EXPIRY_SECS*2)},
		{"ExpClaimMissing", makeBearerNoClaims(m)},
		{"ExpClaimExpired", makeBearerOffset(m, -1*base_def.BEARER_JWT_EXPIRY_SECS)},
	}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			dMock := &mocks.DataStore{}
			dMock.On("ApplianceIDByClientID", mock.Anything, m.ClientID).Return(&m.ApplianceID, nil)
			dMock.On("KeysByUUID", mock.Anything, mock.Anything).Return(m.Keys, nil)
			defer dMock.AssertExpectations(t)
			mw := New(dMock)
			ctx := metautils.ExtractIncoming(context.Background()).
				Add("authorization", tc.bearer).
				Add("clientid", m.ClientID).
				ToIncoming(context.Background())
			_, err := mw.authFunc(ctx)
			assertErrAndCode(t, err, codes.Unauthenticated)
			assert.Equal(0, mw.authCache.Len(false), "authCache has unexpected size")
		})
	}
}

func TestExpiredBearerCached(t *testing.T) {
	_, slog := setupLogging(t)
	m := mockAppliances[0]
	assert := require.New(t)

	dMock := &mocks.DataStore{}
	defer dMock.AssertExpectations(t)

	mw := New(dMock)

	// Manufacture an expired token, make a bearer for it, then parse the
	// token with claims validation disabled, and stuff the cache with it.
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"exp": int32(time.Now().Unix()) - base_def.BEARER_JWT_EXPIRY_SECS,
	})
	slog.Infof("token is %#v", token)

	assert.NotEmpty(m.PrivateKey)
	tokenString, err := token.SignedString(m.PrivateKey)
	assert.NoError(err)
	bearer := "bearer " + tokenString

	parser := &jwt.Parser{SkipClaimsValidation: true}
	parsedToken, err := parser.Parse(tokenString, func(*jwt.Token) (interface{}, error) {
		pub, err := jwt.ParseRSAPublicKeyFromPEM(m.PublicKeyPEM)
		if err != nil {
			panic(err)
		}
		return pub, nil
	})
	assert.NoError(err)
	assert.NotEmpty(parsedToken)
	assert.Error(parsedToken.Claims.Valid())
	slog.Infof("parsedToken is %#v", parsedToken)
	_ = mw.authCache.Set(tokenString, &authCacheEntry{
		ClientID:      m.ClientID,
		Token:         parsedToken,
		ApplianceUUID: m.ApplianceUUID,
		SiteUUID:      m.SiteUUID,
	})
	assert.Equalf(1, mw.authCache.Len(false), "authCache has unexpected size")

	ctx := metautils.ExtractIncoming(context.Background()).
		Add("authorization", bearer).
		Add("clientid", m.ClientID).
		ToIncoming(context.Background())
	_, err = mw.authFunc(ctx)
	assertErrAndCode(t, err, codes.Unauthenticated)
	assert.Equalf(0, mw.authCache.Len(false), "authCache has unexpected size")
}

func TestBadClientID(t *testing.T) {
	_, _ = setupLogging(t)
	m := mockAppliances[0]

	dMock := &mocks.DataStore{}
	dMock.On("ApplianceIDByClientID", mock.Anything, mock.Anything).Return(nil, appliancedb.NotFoundError{})
	defer dMock.AssertExpectations(t)

	mw := New(dMock)

	ctx := metautils.ExtractIncoming(context.Background()).
		Add("authorization", makeBearer(m)).
		Add("clientid", m.ClientID+"bad").
		ToIncoming(context.Background())
	_, err := mw.authFunc(ctx)
	assertErrAndCode(t, err, codes.Unauthenticated)
}

func TestCertMismatch(t *testing.T) {
	_, _ = setupLogging(t)
	m := mockAppliances[0]
	m1 := mockAppliances[1]

	dMock := &mocks.DataStore{}
	dMock.On("ApplianceIDByClientID", mock.Anything, m.ClientID).Return(&m.ApplianceID, nil)
	dMock.On("KeysByUUID", mock.Anything, m.ApplianceUUID).Return(m1.Keys, nil)
	defer dMock.AssertExpectations(t)

	mw := New(dMock)

	ctx := metautils.ExtractIncoming(context.Background()).
		Add("authorization", makeBearer(m)).
		Add("clientid", m.ClientID).
		ToIncoming(context.Background())
	_, err := mw.authFunc(ctx)
	assertErrAndCode(t, err, codes.Unauthenticated)
}

func TestNoKeys(t *testing.T) {
	_, _ = setupLogging(t)
	m := mockAppliances[0]

	dMock := &mocks.DataStore{}
	dMock.On("ApplianceIDByClientID", mock.Anything, m.ClientID).Return(&m.ApplianceID, nil)
	// Return empty keys
	dMock.On("KeysByUUID", mock.Anything, m.ApplianceUUID).Return([]appliancedb.AppliancePubKey{}, nil)
	defer dMock.AssertExpectations(t)

	mw := New(dMock)

	ctx := metautils.ExtractIncoming(context.Background()).
		Add("authorization", makeBearer(m)).
		Add("clientid", m.ClientID).
		ToIncoming(context.Background())
	_, err := mw.authFunc(ctx)
	assertErrAndCode(t, err, codes.Unauthenticated)
}

func TestEmptyContext(t *testing.T) {
	_, _ = setupLogging(t)

	dMock := &mocks.DataStore{}
	dMock.AssertExpectations(t)

	mw := New(dMock)

	_, err := mw.authFunc(context.Background())
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

