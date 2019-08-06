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
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/securecookie"
	"github.com/gorilla/sessions"
	"github.com/labstack/echo"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"bg/cloud_models/appliancedb"
	"bg/cloud_models/appliancedb/mocks"
)

func TestAccountsGenAndProvision(t *testing.T) {
	var err error
	assert := require.New(t)
	// Mock DB

	mockAccountSecrets := appliancedb.AccountSecrets{
		AccountUUID:                 accountUUID,
		ApplianceUserBcrypt:         "foobar",
		ApplianceUserBcryptRegime:   "1",
		ApplianceUserBcryptTs:       time.Now(),
		ApplianceUserMSCHAPv2:       "foobaz",
		ApplianceUserMSCHAPv2Regime: "1",
		ApplianceUserMSCHAPv2Ts:     time.Now(),
	}

	dMock := &mocks.DataStore{}
	dMock.On("AccountByUUID", mock.Anything, mockAccount.UUID).Return(&mockAccount, nil)
	dMock.On("AccountByUUID", mock.Anything, mockUserAccount.UUID).Return(&mockUserAccount, nil)
	dMock.On("AccountSecretsByUUID", mock.Anything, mock.Anything).Return(&mockAccountSecrets, nil)
	dMock.On("AccountOrgRolesByAccountTarget", mock.Anything, mockAccount.UUID, mock.Anything).Return(mockAccountOrgRoles, nil)
	dMock.On("AccountOrgRolesByAccountTarget", mock.Anything, mockUserAccount.UUID, mock.Anything).Return(mockUserAccountOrgRoles, nil)
	dMock.On("CustomerSitesByOrganization", mock.Anything, mock.Anything).Return(mockSites, nil)
	dMock.On("PersonByUUID", mock.Anything, mock.Anything).Return(&mockPerson, nil)
	dMock.On("UpsertAccountSecrets", mock.Anything, mock.Anything).Return(nil)
	dMock.On("DeleteAccountSecrets", mock.Anything, mock.Anything).Return(nil)
	dMock.On("DeleteAccount", mock.Anything, mock.Anything).Return(nil)
	defer dMock.AssertExpectations(t)

	// Setup Echo
	ss := sessions.NewCookieStore(securecookie.GenerateRandomKey(32))
	mw := []echo.MiddlewareFunc{
		newSessionMiddleware(ss).Process,
	}
	e := echo.New()
	_ = newAccountHandler(e, dMock, mw, ss, getMockClientHandle)

	// Setup request for password generation
	req, rec := setupReqRec(&mockAccount, echo.GET, "/api/account/passwordgen", nil, ss)

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
	req, rec = setupReqRec(&mockAccount, echo.GET, "/api/account/passwordgen", nil, ss)

	// Test
	e.ServeHTTP(rec, req)
	assert.Equal(http.StatusOK, rec.Code)
	t.Logf("return body:Svc %s", rec.Body.String())
	var ret2 accountSelfProvisionRequest
	resp := rec.Result()
	bodyBytes, _ := ioutil.ReadAll(resp.Body)
	cookies := resp.Cookies()
	resp.Body.Close()
	err = json.Unmarshal(bodyBytes, &ret2)

	assert.NoError(err)
	assert.Equal(ret.Username, ret2.Username)
	assert.NotEqual(ret.Password, ret2.Password)

	body := accountSelfProvisionRequest{
		Username: ret2.Username,
		Password: "anything",
		Verifier: ret2.Verifier,
	}
	bodyBytes, err = json.Marshal(body)
	assert.NoError(err)

	// Can't accept a generated password to a different account
	url := fmt.Sprintf("/api/account/%s/selfprovision", mockUserAccount.UUID.String())
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(echo.POST, url, bytes.NewReader(bodyBytes))
	req.AddCookie(cookies[0])
	req.Header.Add("Content-Type", "application/json")
	// Test
	e.ServeHTTP(rec, req)
	t.Logf("return body:Svc %s", rec.Body.String())
	assert.Equal(http.StatusUnauthorized, rec.Code)

	// Now accept the generated password
	url = fmt.Sprintf("/api/account/%s/selfprovision", mockAccount.UUID.String())
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(echo.POST, url, bytes.NewReader(bodyBytes))
	req.AddCookie(cookies[0])
	req.Header.Add("Content-Type", "application/json")
	// Test
	e.ServeHTTP(rec, req)
	t.Logf("return body:Svc %s", rec.Body.String())
	assert.Equal(http.StatusFound, rec.Code)

	// Non-admins cannot deprovision
	url = fmt.Sprintf("/api/account/%s/deprovision", mockUserAccount.UUID.String())
	req, rec = setupReqRec(&mockUserAccount, echo.POST, url, bytes.NewReader([]byte{}), ss)
	// Test
	e.ServeHTTP(rec, req)
	t.Logf("return body:Svc %s", rec.Body.String())
	assert.Equal(http.StatusUnauthorized, rec.Code)

	// Admins can Deprovision
	url = fmt.Sprintf("/api/account/%s/deprovision", mockAccount.UUID.String())
	req, rec = setupReqRec(&mockAccount, echo.POST, url, bytes.NewReader([]byte{}), ss)
	// Test
	e.ServeHTTP(rec, req)
	t.Logf("return body:Svc %s", rec.Body.String())
	assert.Equal(http.StatusOK, rec.Code)

	// Non-admins cannot delete accounts
	url = fmt.Sprintf("/api/account/%s", mockUserAccount.UUID.String())
	req, rec = setupReqRec(&mockUserAccount, echo.DELETE, url, nil, ss)
	// Test
	e.ServeHTTP(rec, req)
	t.Logf("return body:Svc %s", rec.Body.String())
	assert.Equal(http.StatusUnauthorized, rec.Code)

	// Admins can delete accounts
	url = fmt.Sprintf("/api/account/%s", mockAccount.UUID.String())
	req, rec = setupReqRec(&mockAccount, echo.DELETE, url, nil, ss)
	// Test
	e.ServeHTTP(rec, req)
	t.Logf("return body:Svc %s", rec.Body.String())
	assert.Equal(http.StatusOK, rec.Code)
}
