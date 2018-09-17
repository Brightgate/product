//
// COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
//
// This copyright notice is Copyright Management Information under 17 USC 1202
// and is included to protect this work and deter copyright infringement.
// Removal or alteration of this Copyright Management Information without the
// express written permission of Brightgate Inc is prohibited, and any
// such unauthorized removal or alteration will be a violation of federal law.
//

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/securecookie"
	"github.com/gorilla/sessions"
	"github.com/labstack/echo"
	"github.com/satori/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"bg/cloud_models/appliancedb"
	"bg/cloud_models/appliancedb/mocks"
)

type MockAppliance struct {
	appliancedb.ApplianceID
}

func init() {
	zeroUUIDStr = uuid.UUID{}.String()
}

var (
	zeroUUIDStr    string
	mockAppliances = []*MockAppliance{
		{
			ApplianceID: appliancedb.ApplianceID{
				CloudUUID:      uuid.Must(uuid.FromString("b3798a8e-41e0-4939-a038-e7675af864d5")),
				GCPProject:     "foo",
				GCPRegion:      "bar",
				ApplianceReg:   "baz",
				ApplianceRegID: "mock0",
			},
		},
		{
			ApplianceID: appliancedb.ApplianceID{
				CloudUUID:      uuid.Must(uuid.FromString("099239f6-d8cd-4e57-a696-ef84a3bf39d0")),
				GCPProject:     "foo",
				GCPRegion:      "bar",
				ApplianceReg:   "baz",
				ApplianceRegID: "mock1",
			},
		},
	}
)

// addValidSession does a handstand to setup a valid session cookie on the
// request.  We make a new httptest.ResponseRecorder, save a session into it,
// then extract the session cookie from that, and stick it into the req, tossing
// out the recorder.  This is cribbed and refined from
// https://gist.github.com/jonnyreeves/17f91155a0d4a5d296d6
func addValidSession(req *http.Request, ss sessions.Store) {
	rec := httptest.NewRecorder()
	sess, err := ss.New(req, "bg_login")
	if err != nil {
		panic("Failed session create")
	}
	sess.Values["userid"] = "test"
	sess.Values["email"] = "test@brightgate.com"
	sess.Values["auth_time"] = time.Now().Format(time.RFC3339)
	err = sess.Save(req, rec)
	if err != nil {
		panic("Failed session save")
	}
	req.Header.Add("Cookie", rec.HeaderMap["Set-Cookie"][0])
}

// setupReqRec is basically a wrapper around httptest.NewRequest which adds a
// valid session to the request; it also allocates an httptest recorder.
func setupReqRec(method string, target string, body io.Reader, ss sessions.Store) (*http.Request, *httptest.ResponseRecorder) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(echo.GET, target, body)
	addValidSession(req, ss)
	return req, rec
}

func TestAppliances(t *testing.T) {
	assert := require.New(t)
	// Mock DB
	m0 := mockAppliances[0]
	m1 := mockAppliances[1]
	dMock := &mocks.DataStore{}
	dMock.On("AllApplianceIDs", mock.Anything).Return(
		[]appliancedb.ApplianceID{m0.ApplianceID, m1.ApplianceID}, nil)
	defer dMock.AssertExpectations(t)

	// Setup Echo
	ss := sessions.NewCookieStore(securecookie.GenerateRandomKey(32))
	e := echo.New()
	_ = newAPIHandler(e, dMock, ss)

	// Setup request
	req, rec := setupReqRec(echo.GET, "/api/appliances", nil, ss)

	// Test
	e.ServeHTTP(rec, req)
	assert.Equal(http.StatusOK, rec.Code)
	exp := fmt.Sprintf(`["%s", "%s"]`, m0.CloudUUID, m1.CloudUUID)
	t.Logf("return body: %s", rec.Body.String())
	assert.JSONEq(exp, rec.Body.String())
}

func TestApplianceUUID(t *testing.T) {
	assert := require.New(t)
	// Mock DB
	m0 := mockAppliances[0].ApplianceID
	dMock := &mocks.DataStore{}
	dMock.On("ApplianceIDByUUID", mock.Anything, m0.CloudUUID).Return(&m0, nil)
	dMock.On("ApplianceIDByUUID", mock.Anything, mock.Anything).Return(nil, appliancedb.NotFoundError{})
	defer dMock.AssertExpectations(t)

	// Setup Echo
	ss := sessions.NewCookieStore(securecookie.GenerateRandomKey(32))
	e := echo.New()
	_ = newAPIHandler(e, dMock, ss)

	// Setup request
	req, rec := setupReqRec(echo.GET,
		fmt.Sprintf("/api/appliances/%s", m0.CloudUUID), nil, ss)

	// Test
	e.ServeHTTP(rec, req)
	assert.Equal(http.StatusOK, rec.Code)
	exp, err := json.Marshal(m0)
	assert.NoError(err)
	t.Logf("return body: %s", rec.Body.String())
	assert.JSONEq(string(exp), rec.Body.String())

	// Test various error cases
	req4xx := map[string]int{
		"/api/appliances/invalid":                              400,
		"/api/appliances/61df362c-338d-4f53-b1d9-c77c0522bb03": 404,
	}

	for url, ret := range req4xx {
		t.Logf("testing %s for %d", url, ret)
		req, rec := setupReqRec(echo.GET, url, nil, ss)
		e.ServeHTTP(rec, req)
		assert.Equal(ret, rec.Code)
	}
}

func TestUnauthorized(t *testing.T) {
	assert := require.New(t)
	ss := sessions.NewCookieStore(securecookie.GenerateRandomKey(32))
	e := echo.New()
	dMock := &mocks.DataStore{}
	h := newAPIHandler(e, dMock, ss)

	testCases := []struct {
		path    string
		handler echo.HandlerFunc
	}{
		{"/api/appliances", h.getAppliances},
		{"/api/appliances/" + zeroUUIDStr, h.getAppliancesUUID},
	}

	for _, tc := range testCases {
		req := httptest.NewRequest(echo.GET, tc.path, nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		assert.Equal(http.StatusUnauthorized, rec.Code)
	}
}
