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
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/securecookie"
	"github.com/gorilla/sessions"
	"github.com/labstack/echo"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"bg/cloud_models/appliancedb/mocks"
)

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
	mw := []echo.MiddlewareFunc{
		newSessionMiddleware(ss).Process,
	}
	e := echo.New()
	_ = newAccountHandler(e, dMock, mw, ss, getMockClientHandle, securecookie.GenerateRandomKey(32))

	// Setup request for password generation
	req, rec := setupReqRec(echo.GET,
		fmt.Sprintf("/api/account/0/passwordgen"), nil, ss)

	// Test
	e.ServeHTTP(rec, req)
	assert.Equal(http.StatusOK, rec.Code)
	t.Logf("return body:Svc %s", rec.Body.String())

	var ret accountSelfProvisionRequest
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
	var ret2 accountSelfProvisionRequest
	cookies := rec.Result().Cookies()
	err = json.Unmarshal(rec.Body.Bytes(), &ret2)

	assert.Equal(ret.Username, ret2.Username)
	assert.NotEqual(ret.Password, ret2.Password)

	body := accountSelfProvisionRequest{
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
