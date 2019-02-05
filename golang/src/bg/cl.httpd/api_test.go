//
// COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
//
// This copyright notice is Copyright Management Information under 17 USC 1202
// and is included to protect this work and deter copyright infringement.
// Removal or alteration of this Copyright Management Information without the
// express written permission of Brightgate Inc is prohibited, and any
// such unauthorized removal or alteration will be a violation of federal law.
//

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"bg/common/cfgapi"

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
	mockSites = []appliancedb.CustomerSite{
		appliancedb.CustomerSite{
			UUID: uuid.Must(uuid.FromString("b3798a8e-41e0-4939-a038-e7675af864d5")),
			Name: "mock-site-0",
		},
		appliancedb.CustomerSite{
			UUID: uuid.Must(uuid.FromString("099239f6-d8cd-4e57-a696-ef84a3bf39d0")),
			Name: "mock-site-1",
		},
	}
	orgUUID     = uuid.Must(uuid.FromString("10000000-0000-0000-0000-000000000000"))
	accountUUID = uuid.Must(uuid.FromString("20000000-0000-0000-0000-000000000000"))
	personUUID  = uuid.Must(uuid.FromString("30000000-0000-0000-0000-000000000000"))

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
	}
)

// TestCmdHdl Implements a mocked CmdHdl; this handle always returns
// cfgapi.ErrNoConfig.
type TestCmdHdl struct{}

func (h *TestCmdHdl) Status(ctx context.Context) (string, error) {
	return "", cfgapi.ErrNoConfig
}

func (h *TestCmdHdl) Wait(ctx context.Context) (string, error) {
	return "", cfgapi.ErrNoConfig
}

// TestConfigExec Implements ConfigExec; it does nothing except return
// TestCmdHdl, which just returns cfgapi.ErrNoConfig.
type TestConfigExec struct{}

func (t *TestConfigExec) Ping(ctx context.Context) error {
	return nil
}

func (t *TestConfigExec) Execute(ctx context.Context, ops []cfgapi.PropertyOp) cfgapi.CmdHdl {
	return &TestCmdHdl{}
}

func (t *TestConfigExec) HandleChange(path string, handler func([]string, string, *time.Time)) error {
	return nil
}

func (t *TestConfigExec) HandleDelete(path string, handler func([]string)) error {
	return nil
}

func (t *TestConfigExec) HandleExpire(path string, handler func([]string)) error {
	return nil
}

func (t *TestConfigExec) Close() {
}

// Return the a TestConfigExec backed handle.  This will always reply with
// cfgapi.ErrNoConfig.  In the future we'd like a fully flexible config mock
// handle.
func getMockClientHandle(uuid string) (*cfgapi.Handle, error) {
	return cfgapi.NewHandle(&TestConfigExec{}), nil
}

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
	sess.Values["account_uuid"] = accountUUID.String()
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
	req := httptest.NewRequest(method, target, body)
	addValidSession(req, ss)
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
	e := echo.New()
	_ = newAPIHandler(e, dMock, ss, getMockClientHandle, securecookie.GenerateRandomKey(32))

	// Setup request
	req, rec := setupReqRec(echo.GET, "/api/sites", nil, ss)

	// Test
	e.ServeHTTP(rec, req)
	assert.Equal(http.StatusOK, rec.Code)
	exp := fmt.Sprintf(`[
	{
		"uuid": "%s",
		"name": "%s"
	},{
		"uuid": "%s",
		"name": "%s"
	}]`, m0.UUID, m0.Name, m1.UUID, m1.Name)
	t.Logf("return body: %s", rec.Body.String())
	assert.JSONEq(exp, rec.Body.String())
}

func TestSitesUUID(t *testing.T) {
	assert := require.New(t)
	// Mock DB
	m0 := mockSites[0]
	dMock := &mocks.DataStore{}
	dMock.On("CustomerSitesByAccount", mock.Anything, mock.Anything).Return(mockSites, nil)
	dMock.On("CustomerSiteByUUID", mock.Anything, m0.UUID).Return(&m0, nil)
	dMock.On("CustomerSiteByUUID", mock.Anything, mock.Anything).Return(nil, appliancedb.NotFoundError{})
	defer dMock.AssertExpectations(t)

	// Setup Echo
	ss := sessions.NewCookieStore(securecookie.GenerateRandomKey(32))
	e := echo.New()
	_ = newAPIHandler(e, dMock, ss, getMockClientHandle, securecookie.GenerateRandomKey(32))

	// Setup request
	req, rec := setupReqRec(echo.GET,
		fmt.Sprintf("/api/sites/%s", m0.UUID), nil, ss)

	// Test
	e.ServeHTTP(rec, req)
	assert.Equal(http.StatusOK, rec.Code)
	exp, err := json.Marshal(m0)
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
		req, rec := setupReqRec(echo.GET, url, nil, ss)
		e.ServeHTTP(rec, req)
		assert.Equal(ret, rec.Code)
	}
}

func TestAccountsGenAndProvision(t *testing.T) {
	var err error
	assert := require.New(t)
	// Mock DB
	dMock := &mocks.DataStore{}
	dMock.On("AccountByUUID", mock.Anything, mock.Anything).Return(&mockAccount, nil)
	dMock.On("CustomerSitesByAccount", mock.Anything, mock.Anything).Return(mockSites, nil)
	dMock.On("PersonByUUID", mock.Anything, mock.Anything).Return(&mockPerson, nil)
	dMock.On("UpsertAccountSecrets", mock.Anything, mock.Anything).Return(nil)
	defer dMock.AssertExpectations(t)

	// Setup Echo
	ss := sessions.NewCookieStore(securecookie.GenerateRandomKey(32))
	e := echo.New()
	_ = newAPIHandler(e, dMock, ss, getMockClientHandle, securecookie.GenerateRandomKey(32))

	// Setup request for password generation
	req, rec := setupReqRec(echo.GET,
		fmt.Sprintf("/api/account/0/passwordgen"), nil, ss)

	// Test
	e.ServeHTTP(rec, req)
	assert.Equal(http.StatusOK, rec.Code)
	t.Logf("return body:Svc %s", rec.Body.String())

	var ret apiSelfProvisionInfo
	err = json.Unmarshal(rec.Body.Bytes(), &ret)
	assert.NoError(err)
	assert.NotEmpty(ret.Password)
	assert.Equal(mockAccount.Email, ret.Username)

	// Go around again, see that it's different
	req, rec = setupReqRec(echo.GET,
		fmt.Sprintf("/api/account/0/passwordgen"), nil, ss)

	// Test
	e.ServeHTTP(rec, req)
	assert.Equal(http.StatusOK, rec.Code)
	t.Logf("return body:Svc %s", rec.Body.String())
	var ret2 apiSelfProvisionInfo
	cookies := rec.Result().Cookies()
	err = json.Unmarshal(rec.Body.Bytes(), &ret2)

	assert.Equal(ret.Username, ret2.Username)
	assert.NotEqual(ret.Password, ret2.Password)

	body := apiSelfProvisionInfo{
		Username: ret2.Username,
		Password: "anything",
		Verifier: ret2.Verifier,
	}
	bodyBytes, err := json.Marshal(body)
	assert.NoError(err)

	// Now accept the generated password
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(echo.POST, "/api/account/0/selfprovision", bytes.NewReader(bodyBytes))
	req.AddCookie(cookies[0])
	req.Header.Add("Content-Type", "application/json")
	// Test
	e.ServeHTTP(rec, req)
	t.Logf("return body:Svc %s", rec.Body.String())
	assert.Equal(http.StatusFound, rec.Code)
}

func TestUnauthorized(t *testing.T) {
	assert := require.New(t)
	ss := sessions.NewCookieStore(securecookie.GenerateRandomKey(32))
	e := echo.New()
	dMock := &mocks.DataStore{}
	h := newAPIHandler(e, dMock, ss, getMockClientHandle, securecookie.GenerateRandomKey(32))

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
