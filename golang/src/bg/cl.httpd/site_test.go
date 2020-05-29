//
// COPYRIGHT 2020 Brightgate Inc.  All rights reserved.
//
// This copyright notice is Copyright Management Information under 17 USC 1202
// and is included to protect this work and deter copyright infringement.
// Removal or alteration of this Copyright Management Information without the
// express written permission of Brightgate Inc is prohibited, and any
// such unauthorized removal or alteration will be a violation of federal law.
//

package main

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"bg/common/cfgapi"
	"bg/common/mockcfg"

	"github.com/gorilla/securecookie"
	"github.com/gorilla/sessions"
	"github.com/labstack/echo"
	"github.com/satori/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"bg/cloud_models/appliancedb"
	"bg/cloud_models/appliancedb/mocks"
)

var (
	orgUUID         = uuid.Must(uuid.FromString("10000000-0000-0000-0000-000000000000"))
	accountUUID     = uuid.Must(uuid.FromString("20000000-0000-0000-0000-000000000000"))
	userAccountUUID = uuid.Must(uuid.FromString("20000000-0000-0000-0000-000000000001"))
	personUUID      = uuid.Must(uuid.FromString("30000000-0000-0000-0000-000000000000"))

	mockOrg = appliancedb.Organization{
		UUID: orgUUID,
		Name: "TestOrg",
	}

	mockSites = []appliancedb.CustomerSite{
		appliancedb.CustomerSite{
			UUID:             uuid.Must(uuid.FromString("b3798a8e-41e0-4939-a038-e7675af864d5")),
			Name:             "mock-site-0",
			OrganizationUUID: orgUUID,
		},
		appliancedb.CustomerSite{
			UUID:             uuid.Must(uuid.FromString("099239f6-d8cd-4e57-a696-ef84a3bf39d0")),
			Name:             "mock-site-1",
			OrganizationUUID: orgUUID,
		},
	}
	mockPerson = appliancedb.Person{
		UUID:         personUUID,
		Name:         "Foo Bar",
		PrimaryEmail: "foo@example.com",
	}

	mockAccount = appliancedb.Account{
		UUID:             accountUUID,
		Email:            "foo@example.com",
		OrganizationUUID: orgUUID,
		PhoneNumber:      "650-555-1212",
		PersonUUID:       personUUID,
		AvatarHash:       []byte{},
	}

	// "I LIKE COCONUTS"
	avHash, _ = hex.DecodeString("dc2bb1308bcee0f5ac68bb2b117b358d6520921df2fbb87d24fa516021cce377")

	mockUserAccount = appliancedb.Account{
		UUID:             userAccountUUID,
		Email:            "bar@example.com",
		OrganizationUUID: orgUUID,
		PhoneNumber:      "650-555-1212",
		PersonUUID:       personUUID,
		AvatarHash:       avHash,
	}

	mockAccountOrgRoles = []appliancedb.AccountOrgRoles{
		{
			AccountUUID:            mockAccount.UUID,
			OrganizationUUID:       mockAccount.OrganizationUUID,
			TargetOrganizationUUID: mockAccount.OrganizationUUID,
			Relationship:           "self",
			LimitRoles:             []string{"admin", "user"},
			Roles:                  []string{"admin"},
		},
	}

	mockUserAccountOrgRoles = []appliancedb.AccountOrgRoles{
		{
			AccountUUID:            mockUserAccount.UUID,
			OrganizationUUID:       mockUserAccount.OrganizationUUID,
			TargetOrganizationUUID: mockUserAccount.OrganizationUUID,
			Relationship:           "self",
			LimitRoles:             []string{"admin", "user"},
			Roles:                  []string{"user"},
		},
	}
)

// Return a MockExec backed handle.  We're being super lazy here and
// always building a new MockExec-- which means that every time this is
// called it's acting on a fresh tree-- there is room for improvement
// here.
func getMockClientHandle(uuid string) (*cfgapi.Handle, error) {
	return cfgapi.NewHandle(mockcfg.NewMockExecFromDefaults()), nil
}

// addValidSession does a handstand to setup a valid session cookie on the
// request.  We make a new httptest.ResponseRecorder, save a session into it,
// then extract the session cookie from that, and stick it into the req, tossing
// out the recorder.  This is cribbed and refined from
// https://gist.github.com/jonnyreeves/17f91155a0d4a5d296d6
func addValidSession(req *http.Request, ss sessions.Store, acct *appliancedb.Account) {
	rec := httptest.NewRecorder()
	sess, err := ss.New(req, "bg_login")
	if err != nil {
		panic("Failed session create")
	}
	sess.Values["userid"] = "test"
	sess.Values["email"] = acct.Email
	sess.Values["auth_time"] = time.Now().Format(time.RFC3339)
	sess.Values["account_uuid"] = acct.UUID.String()
	err = sess.Save(req, rec)
	if err != nil {
		panic("Failed session save")
	}
	req.Header.Add("Cookie", rec.HeaderMap["Set-Cookie"][0])
}

// setupReqRec is basically a wrapper around httptest.NewRequest which adds a
// valid session to the request; it also allocates an httptest recorder.
func setupReqRec(acct *appliancedb.Account, method string, target string, body io.Reader, ss sessions.Store) (*http.Request, *httptest.ResponseRecorder) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, target, body)
	addValidSession(req, ss, acct)
	return req, rec
}

func TestSites(t *testing.T) {
	assert := require.New(t)
	// Mock DB
	m0 := mockSites[0]
	m1 := mockSites[1]
	dMock := &mocks.DataStore{}
	dMock.On("CustomerSitesByAccount", mock.Anything, mock.Anything).Return(mockSites, nil)
	defer dMock.AssertExpectations(t)

	// Setup Echo
	ss := sessions.NewCookieStore(securecookie.GenerateRandomKey(32),
		securecookie.GenerateRandomKey(32))
	mw := []echo.MiddlewareFunc{
		newSessionMiddleware(ss).Process,
	}
	e := echo.New()
	_ = newSiteHandler(e, dMock, mw, getMockClientHandle, nil)

	// Setup request
	req, rec := setupReqRec(&mockAccount, echo.GET, "/api/sites", nil, ss)

	// Test
	e.ServeHTTP(rec, req)
	assert.Equal(http.StatusOK, rec.Code)
	exp := fmt.Sprintf(`[
	{
		"UUID": "%s",
		"name": "%s",
		"organizationUUID": "%s"
	},{
		"UUID": "%s",
		"name": "%s",
		"organizationUUID": "%s"
	}]`, m0.UUID, m0.Name, mockOrg.UUID.String(),
		m1.UUID, m1.Name, mockOrg.UUID.String())
	t.Logf("return body: %s", rec.Body.String())
	assert.JSONEq(exp, rec.Body.String())
}

func TestSitesUUID(t *testing.T) {
	assert := require.New(t)
	// Mock DB
	m0 := mockSites[0]
	dMock := &mocks.DataStore{}
	dMock.On("AccountOrgRolesByAccountTarget", mock.Anything, accountUUID, orgUUID).Return(
		[]appliancedb.AccountOrgRoles{
			{
				AccountUUID:            accountUUID,
				OrganizationUUID:       orgUUID,
				TargetOrganizationUUID: orgUUID,
				Relationship:           "self",
				LimitRoles:             []string{"admin", "user"},
				Roles:                  []string{"admin"},
			},
		}, nil)
	dMock.On("CustomerSiteByUUID", mock.Anything, m0.UUID).Return(&m0, nil)
	dMock.On("CustomerSiteByUUID", mock.Anything, mock.Anything).Return(nil, appliancedb.NotFoundError{})
	defer dMock.AssertExpectations(t)

	// Setup Echo
	ss := sessions.NewCookieStore(securecookie.GenerateRandomKey(32))
	mw := []echo.MiddlewareFunc{
		newSessionMiddleware(ss).Process,
	}
	e := echo.New()
	_ = newSiteHandler(e, dMock, mw, getMockClientHandle, nil)

	// Setup request
	req, rec := setupReqRec(&mockAccount, echo.GET,
		fmt.Sprintf("/api/sites/%s", m0.UUID), nil, ss)

	// Test
	e.ServeHTTP(rec, req)
	assert.Equal(http.StatusOK, rec.Code)
	expStruct := &siteResponse{
		UUID:             m0.UUID,
		Name:             m0.Name,
		OrganizationUUID: mockOrg.UUID,
	}
	exp, err := json.Marshal(expStruct)
	assert.NoError(err)
	t.Logf("return body: %s", rec.Body.String())
	assert.JSONEq(string(exp), rec.Body.String())

	// Test various error cases
	req4xx := map[string]int{
		"/api/sites/invalid":                              400,
		"/api/sites/61df362c-338d-4f53-b1d9-c77c0522bb03": 404,
	}

	for url, ret := range req4xx {
		t.Logf("testing %s for %d", url, ret)
		req, rec := setupReqRec(&mockAccount, echo.GET, url, nil, ss)
		e.ServeHTTP(rec, req)
		assert.Equal(ret, rec.Code)
	}
}

func TestSiteUnauthorized(t *testing.T) {
	assert := require.New(t)
	ss := sessions.NewCookieStore(securecookie.GenerateRandomKey(32))
	mw := []echo.MiddlewareFunc{
		newSessionMiddleware(ss).Process,
	}
	e := echo.New()
	dMock := &mocks.DataStore{}
	h := newSiteHandler(e, dMock, mw, getMockClientHandle, nil)

	testCases := []struct {
		path    string
		handler echo.HandlerFunc
	}{
		{"/api/sites", h.getSites},
		{"/api/sites/" + uuid.Nil.String(), h.getSitesUUID},
	}

	for _, tc := range testCases {
		req := httptest.NewRequest(echo.GET, tc.path, nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		assert.Equal(http.StatusUnauthorized, rec.Code)
	}
}
