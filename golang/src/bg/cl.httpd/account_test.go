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
	neturl "net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/securecookie"
	"github.com/gorilla/sessions"
	"github.com/labstack/echo"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"bg/cloud_models/appliancedb"
	"bg/cloud_models/appliancedb/mocks"

	"cloud.google.com/go/storage"
	"github.com/fsouza/fake-gcs-server/fakestorage"
	"github.com/google/go-cmp/cmp"
)

const mockBucketName = "mock-avatar-bucket"

func setupFakeCS(t *testing.T) (*storage.Client, *fakestorage.Server) {
	fakeCS := fakestorage.NewServer([]fakestorage.Object{})
	fakeCS.CreateBucket(mockBucketName)
	return fakeCS.Client(), fakeCS
}

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

	csclient, csserver := setupFakeCS(t)
	defer csserver.Stop()
	mockBucket := csclient.Bucket(mockBucketName)
	_ = newAccountHandler(e, dMock, mw, ss, mockBucket, getMockClientHandle)

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

func TestAccountsRoles(t *testing.T) {
	assert := require.New(t)
	// Mock DB

	dMock := &mocks.DataStore{}
	dMock.On("AccountByUUID", mock.Anything, mockAccount.UUID).Return(&mockAccount, nil)
	dMock.On("AccountByUUID", mock.Anything, mockUserAccount.UUID).Return(&mockUserAccount, nil)
	dMock.On("AccountOrgRolesByAccount", mock.Anything, mockAccount.UUID).Return(mockAccountOrgRoles, nil)
	dMock.On("AccountOrgRolesByAccount", mock.Anything, mockUserAccount.UUID).Return(mockUserAccountOrgRoles, nil)
	dMock.On("AccountOrgRolesByAccountTarget", mock.Anything, mockAccount.UUID, mock.Anything).Return(mockAccountOrgRoles, nil)
	dMock.On("AccountOrgRolesByAccountTarget", mock.Anything, mockUserAccount.UUID, mock.Anything).Return(mockUserAccountOrgRoles, nil)
	dMock.On("InsertAccountOrgRole", mock.Anything, mock.Anything).Return(nil)
	dMock.On("DeleteAccountOrgRole", mock.Anything, mock.Anything).Return(nil)
	defer dMock.AssertExpectations(t)

	// Setup Echo
	ss := sessions.NewCookieStore(securecookie.GenerateRandomKey(32))
	mw := []echo.MiddlewareFunc{
		newSessionMiddleware(ss).Process,
	}
	e := echo.New()

	csclient, csserver := setupFakeCS(t)
	defer csserver.Stop()
	mockBucket := csclient.Bucket(mockBucketName)

	_ = newAccountHandler(e, dMock, mw, ss, mockBucket, getMockClientHandle)

	accts := []appliancedb.Account{mockAccount, mockUserAccount}

	// Non-admins can get their own account roles
	// Non-admins cannot get other account roles
	// Admins can get anyone's account roles
	for _, srcAcct := range accts {
		for _, tgtAcct := range accts {
			t.Logf("test %s getting roles for %s",
				srcAcct.UUID.String(), tgtAcct.UUID.String())
			url := fmt.Sprintf("/api/account/%s/roles", tgtAcct.UUID.String())
			req, rec := setupReqRec(&srcAcct, echo.GET, url, bytes.NewReader([]byte{}), ss)
			// Test
			e.ServeHTTP(rec, req)
			t.Logf("return body:Svc %s", rec.Body.String())
			if srcAcct.UUID == mockUserAccount.UUID &&
				tgtAcct.UUID == mockAccount.UUID {
				assert.Equal(http.StatusUnauthorized, rec.Code)
			} else {
				assert.Equal(http.StatusOK, rec.Code)
			}
		}
	}

	// Admins can change their own and others account roles; regular users cannot
	for _, srcAcct := range accts {
		for _, tgtAcct := range accts {
			for _, role := range []string{"admin", "user", "badrole"} {
				for _, value := range []string{"false", "true"} {
					t.Logf("test %s setting %s=%s on %s",
						srcAcct.UUID.String(),
						role, value,
						tgtAcct.UUID.String())
					data := neturl.Values{}
					data.Set("value", value)
					url := fmt.Sprintf("/api/account/%s/roles/%s/%s",
						tgtAcct.UUID.String(),
						tgtAcct.OrganizationUUID.String(), role)
					req, rec := setupReqRec(&srcAcct, echo.POST, url,
						strings.NewReader(data.Encode()), ss)
					req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
					req.Header.Add("Content-Length", strconv.Itoa(len(data.Encode())))
					// Test
					e.ServeHTTP(rec, req)
					t.Logf("return body:Svc %s", rec.Body.String())
					if cmp.Equal(srcAcct, mockAccount) {
						if role == "badrole" {
							assert.Equal(http.StatusNotFound, rec.Code)
						} else {
							assert.Equal(http.StatusOK, rec.Code)
						}
					} else {
						assert.Equal(http.StatusUnauthorized, rec.Code)
					}
				}
			}
		}
	}
}

func TestAccountsAvatar(t *testing.T) {
	assert := require.New(t)
	// Mock DB

	dMock := &mocks.DataStore{}
	dMock.On("AccountByUUID", mock.Anything, mockAccount.UUID).Return(&mockAccount, nil)
	dMock.On("AccountByUUID", mock.Anything, mockUserAccount.UUID).Return(&mockUserAccount, nil)
	dMock.On("AccountOrgRolesByAccountTarget", mock.Anything, mockAccount.UUID, mock.Anything).Return(mockAccountOrgRoles, nil)
	dMock.On("AccountOrgRolesByAccountTarget", mock.Anything, mockUserAccount.UUID, mock.Anything).Return(mockUserAccountOrgRoles, nil)
	defer dMock.AssertExpectations(t)

	// Setup Echo
	ss := sessions.NewCookieStore(securecookie.GenerateRandomKey(32))
	mw := []echo.MiddlewareFunc{
		newSessionMiddleware(ss).Process,
	}
	e := echo.New()

	csclient, csserver := setupFakeCS(t)
	defer csserver.Stop()
	mockBucket := csclient.Bucket(mockBucketName)

	_ = newAccountHandler(e, dMock, mw, ss, mockBucket, getMockClientHandle)

	// Request avatar, there should be none
	url := fmt.Sprintf("/api/account/%s/avatar", mockAccount.UUID.String())
	req, rec := setupReqRec(&mockAccount, echo.GET, url, nil, ss)
	e.ServeHTTP(rec, req)
	t.Logf("return body %s", rec.Body.String())
	assert.Equal(http.StatusNotFound, rec.Code)

	// Request avatar, should get 404 because the backend has no avatar, despite
	// the hash value in the db.
	url = fmt.Sprintf("/api/account/%s/avatar", mockUserAccount.UUID.String())
	req, rec = setupReqRec(&mockUserAccount, echo.GET, url, nil, ss)
	e.ServeHTTP(rec, req)
	assert.Equal(http.StatusNotFound, rec.Code)

	csserver.CreateObject(fakestorage.Object{
		BucketName:  mockBucketName,
		Name:        fmt.Sprintf("%s/%s", mockUserAccount.OrganizationUUID, mockUserAccount.UUID),
		ContentType: "application/octet-stream",
		Content:     []byte("I LIKE COCONUTS"),
	})

	// Now that the CS server has something, request avatar, should get something
	url = fmt.Sprintf("/api/account/%s/avatar", mockUserAccount.UUID.String())
	req, rec = setupReqRec(&mockUserAccount, echo.GET, url, nil, ss)
	e.ServeHTTP(rec, req)
	t.Logf("return body %s", rec.Body.String())
	assert.Equal(http.StatusOK, rec.Code)
}
