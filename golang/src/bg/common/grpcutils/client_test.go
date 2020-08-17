/*
 * Copyright 2018 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package grpcutils

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/metadata"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

const testProject = "testproj"
const testRegion = "testregion"
const testRegistry = "testreg"
const testApplianceID = "testappliance"

func setupLogging(t *testing.T) (*zap.Logger, *zap.SugaredLogger) {
	logger := zaptest.NewLogger(t)
	slogger := logger.Sugar()
	return logger, slogger
}

func TestNewCredential(t *testing.T) {
	setupLogging(t)
	cred := NewTestCredential()

	// Exercise String() method of cred
	t.Logf("created cred %s", cred)
}

func TestContext(t *testing.T) {
	setupLogging(t)
	cred := NewTestCredential()

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

func mustRefreshJWT(t *testing.T, cred *Credential) string {
	signed, err := cred.refreshJWT()
	if err != nil {
		t.Fatalf("failed to refresh: %v", err)
	}
	return signed
}

func TestCredRefresh(t *testing.T) {
	setupLogging(t)

	// In these tests we manually control time.
	origTime := time.Now()
	testTime := origTime
	oldTimeNowFunc := timeNowFunc
	timeNowFunc = func() time.Time { return testTime }
	defer func() { timeNowFunc = oldTimeNowFunc }()

	testCases := []struct {
		desc         string
		deltaTime    time.Duration
		expectChange bool
	}{
		{"no time change, expect match", time.Duration(0), false},
		{"small time change, expect match", time.Duration(1 * time.Second), false},
		{"past 3/4 point, expect refresh", cJWTExpiry * 7 / 8, true},
		{"expired, expect refresh", cJWTExpiry * 2, true},
	}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			cred := NewTestCredential()
			testTime = origTime
			signed0 := mustRefreshJWT(t, cred)
			testTime = origTime.Add(tc.deltaTime)
			signed1 := mustRefreshJWT(t, cred)
			// See if the JWT changed, or didn't based on our expectation
			changed := signed0 != signed1
			if changed != tc.expectChange {
				t.Fatalf("unexpected: signed1=%v signed0=%v", signed1, signed0)
			}
		})
	}
}

func TestNewJSON(t *testing.T) {
	setupLogging(t)
	pemstr := strings.Replace(TestCredentialPEM, "\n", "\\n", -1)
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

