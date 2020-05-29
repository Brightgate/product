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
	"bytes"
	"context"
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
	"github.com/labstack/echo/middleware"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"bg/cloud_models/appliancedb"
	"bg/cloud_models/appliancedb/mocks"
	"bg/common/cfgapi"
	"bg/common/mockcfg"
	"bg/common/vpn"

	"cloud.google.com/go/storage"
	"github.com/fsouza/fake-gcs-server/fakestorage"
	"github.com/google/go-cmp/cmp"
)

const mockBucketName = "mock-avatar-bucket"

var mockAccountSecrets = appliancedb.AccountSecrets{
	AccountUUID:                 accountUUID,
	ApplianceUserBcrypt:         "foobar",
	ApplianceUserBcryptRegime:   "1",
	ApplianceUserBcryptTs:       time.Now(),
	ApplianceUserMSCHAPv2:       "foobaz",
	ApplianceUserMSCHAPv2Regime: "1",
	ApplianceUserMSCHAPv2Ts:     time.Now(),
}

func setupFakeCS(t *testing.T) (*storage.Client, *fakestorage.Server) {
	fakeCS := fakestorage.NewServer([]fakestorage.Object{})
	fakeCS.CreateBucket(mockBucketName)
	return fakeCS.Client(), fakeCS
}

func TestAccountsGenAndProvision(t *testing.T) {
	var err error
	assert := require.New(t)
	// Mock DB

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
	me0 := mockcfg.NewMockExecFromDefaults()
	me0.Logf = t.Logf
	me1 := mockcfg.NewMockExecFromDefaults()
	me1.Logf = t.Logf
	getClientHandle := func(uuid string) (*cfgapi.Handle, error) {
		if uuid == mockSites[0].UUID.String() {
			return cfgapi.NewHandle(me0), nil
		}
		if uuid == mockSites[1].UUID.String() {
			return cfgapi.NewHandle(me1), nil
		}
		return nil, cfgapi.ErrNoConfig
	}
	_ = newAccountHandler(e, dMock, mw, ss, mockBucket, getClientHandle)

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

	// Try with mismatched Verifier
	badBody := body
	badBody.Verifier = body.Verifier + "bad"
	badBodyBytes, err := json.Marshal(badBody)
	url = fmt.Sprintf("/api/account/%s/selfprovision", mockAccount.UUID.String())
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(echo.POST, url, bytes.NewReader(badBodyBytes))
	req.AddCookie(cookies[0])
	req.Header.Add("Content-Type", "application/json")
	// Test
	e.ServeHTTP(rec, req)
	t.Logf("return body:Svc %s", rec.Body.String())
	assert.Equal(http.StatusBadRequest, rec.Code)

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
	// Since mockcfg is always synchronous, we know this is done, even
	// though the server allows things to proceed async.
	assert.NoError(me0.PropExists(fmt.Sprintf("@/users/%s/user_md4_password", mockAccount.Email)))
	assert.NoError(me1.PropExists(fmt.Sprintf("@/users/%s/user_md4_password", mockAccount.Email)))
	assert.NoError(me0.PropExists(fmt.Sprintf("@/users/%s/user_password", mockAccount.Email)))
	assert.NoError(me1.PropExists(fmt.Sprintf("@/users/%s/user_password", mockAccount.Email)))

	url = fmt.Sprintf("/api/account/%s/deprovision", mockAccount.UUID.String())
	req, rec = setupReqRec(&mockAccount, echo.POST, url, nil, ss)
	e.ServeHTTP(rec, req)
	t.Logf("return body:Svc %s", rec.Body.String())
	assert.Equal(http.StatusOK, rec.Code)
	// See that account still present ...
	assert.NoError(me0.PropExists(fmt.Sprintf("@/users/%s", mockAccount.Email)))
	assert.NoError(me1.PropExists(fmt.Sprintf("@/users/%s", mockAccount.Email)))
	// ... but passwords are gone
	assert.NoError(me0.PropAbsent(fmt.Sprintf("@/users/%s/user_md4_password", mockAccount.Email)))
	assert.NoError(me1.PropAbsent(fmt.Sprintf("@/users/%s/user_md4_password", mockAccount.Email)))
	assert.NoError(me0.PropAbsent(fmt.Sprintf("@/users/%s/user_password", mockAccount.Email)))
	assert.NoError(me1.PropAbsent(fmt.Sprintf("@/users/%s/user_password", mockAccount.Email)))

	type basicTest struct {
		name   string
		url    string
		method string
		caller *appliancedb.Account
		status int
	}
	tests := []basicTest{
		{
			name:   "Non-admins cannot deprovision",
			url:    fmt.Sprintf("/api/account/%s/deprovision", mockUserAccount.UUID),
			method: echo.POST,
			caller: &mockUserAccount,
			status: http.StatusUnauthorized,
		},
		{
			name:   "Admins can Deprovision",
			url:    fmt.Sprintf("/api/account/%s/deprovision", mockAccount.UUID),
			method: echo.POST,
			caller: &mockAccount,
			status: http.StatusOK,
		},
		{
			name:   "Non-admins cannot delete accounts",
			url:    fmt.Sprintf("/api/account/%s", mockUserAccount.UUID),
			method: echo.DELETE,
			caller: &mockUserAccount,
			status: http.StatusUnauthorized,
		},
		{
			name:   "Admins can delete accounts",
			url:    fmt.Sprintf("/api/account/%s", mockUserAccount.UUID),
			method: echo.DELETE,
			caller: &mockAccount,
			status: http.StatusOK,
		},
		{
			name:   "Non-admins can fetch self-prov info for their own accounts",
			url:    fmt.Sprintf("/api/account/%s/selfprovision", mockUserAccount.UUID),
			method: echo.GET,
			caller: &mockUserAccount,
			status: http.StatusOK,
		},
		{
			name:   "Non-admins cannot fetch self-prov info for other accounts",
			url:    fmt.Sprintf("/api/account/%s/selfprovision", mockAccount.UUID),
			method: echo.GET,
			caller: &mockUserAccount,
			status: http.StatusUnauthorized,
		},
		{
			name:   "Admins can fetch self-prov info for other accounts",
			url:    fmt.Sprintf("/api/account/%s/selfprovision", mockUserAccount.UUID),
			method: echo.GET,
			caller: &mockAccount,
			status: http.StatusOK,
		},
	}

	for _, test := range tests {
		t.Logf("--- %s", test.name)
		req, rec = setupReqRec(test.caller, test.method, test.url, nil, ss)
		e.ServeHTTP(rec, req)
		t.Logf("return body:Svc %s", rec.Body.String())
		assert.Equal(test.status, rec.Code)
	}
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

type writeLogger struct {
	prefix string
	t      *testing.T
}

func (l *writeLogger) Write(p []byte) (n int, err error) {
	l.t.Logf("%s: %s", l.prefix, string(p))
	return len(p), nil
}

// NewWriteLogger returns a writer that behaves like w except
// that it logs (using log.Printf) each write to standard error,
// printing the prefix data written; adapted from testing/iotest.
func newWriteLogger(prefix string, t *testing.T) *writeLogger {
	return &writeLogger{prefix, t}
}

func TestAccountsVPN(t *testing.T) {
	assert := require.New(t)
	ctx := context.Background()
	// Mock DB

	dMock := &mocks.DataStore{}
	dMock.On("AccountByUUID", mock.Anything, mockAccount.UUID).Return(&mockAccount, nil)
	dMock.On("AccountOrgRolesByAccountTarget", mock.Anything, mockAccount.UUID, mock.Anything).Return(mockAccountOrgRoles, nil)
	dMock.On("AccountSecretsByUUID", mock.Anything, mock.Anything).Return(&mockAccountSecrets, nil)
	dMock.On("CustomerSitesByAccount", mock.Anything, mock.Anything).Return(mockSites, nil)
	dMock.On("CustomerSiteByUUID", mock.Anything, mockSites[0].UUID).Return(&mockSites[0], nil)
	dMock.On("PersonByUUID", mock.Anything, mock.Anything).Return(&mockPerson, nil)

	defer dMock.AssertExpectations(t)

	csclient, csserver := setupFakeCS(t)
	defer csserver.Stop()
	mockBucket := csclient.Bucket(mockBucketName)

	// Setup Echo
	ss := sessions.NewCookieStore(securecookie.GenerateRandomKey(32))
	mw := []echo.MiddlewareFunc{
		newSessionMiddleware(ss).Process,
	}
	e := echo.New()
	e.Use(middleware.Logger())

	me := mockcfg.NewMockExecFromDefaults()
	me.Logf = t.Logf
	mehdl := cfgapi.NewHandle(me)

	var err error
	props := map[string]string{
		vpn.PublicProp:  "Jtl3E4nr8KIqyi5ukyzXX1KKz1fkPVKCqLX5HfCAT1A=",
		vpn.AddressProp: "192.168.5.1",
		vpn.PortProp:    "51281",
		vpn.LastMacProp: "00:40:54:00:00:00",
	}
	err = mehdl.CreateProps(props, nil)
	assert.NoError(err)
	me.PTree.Dump(newWriteLogger("ptree setup", t))

	_ = newAccountHandler(e, dMock, mw, ss, mockBucket,
		func(uuid string) (*cfgapi.Handle, error) {
			if uuid == mockSites[0].UUID.String() {
				return cfgapi.NewHandle(me), nil
			}
			return nil, cfgapi.ErrNoConfig
		})

	// Request vpn config, there should be none
	url := fmt.Sprintf("/api/account/%s/wg", mockAccount.UUID.String())
	req, rec := setupReqRec(&mockAccount, echo.GET, url, nil, ss)
	e.ServeHTTP(rec, req)
	t.Logf("return body %s", rec.Body.String())
	assert.Equal(http.StatusOK, rec.Code)
	assert.Equal("[]\n", rec.Body.String())

	ui, err := mehdl.NewSelfProvisionUserInfo(mockAccount.Email, mockAccount.UUID)
	assert.NoError(err)
	ui.Email = mockAccount.Email
	ui.TelephoneNumber = mockAccount.PhoneNumber
	// Make sure password props are set
	ui.SetPassword("foobar")
	hdl, err := ui.Update(ctx)
	assert.NoError(err)
	_, err = hdl.Wait(ctx)
	assert.NoError(err)
	propStem := fmt.Sprintf("@/users/%s", ui.UID)
	// Check that user was setup as we expect
	assert.NoError(me.PropEq(propStem+"/self_provisioning", "true"))
	assert.NoError(me.PropExists(propStem + "/user_password"))

	// Request vpn config, there should be none
	url = fmt.Sprintf("/api/account/%s/wg", mockAccount.UUID.String())
	req, rec = setupReqRec(&mockAccount, echo.GET, url, nil, ss)
	e.ServeHTTP(rec, req)
	assert.Equal(http.StatusOK, rec.Code)
	t.Logf("return body %s", rec.Body.String())
	assert.Equal("[]\n", rec.Body.String())

	// Allocate new vpn config
	url = fmt.Sprintf("/api/account/%s/wg/%s/new", mockAccount.UUID.String(), mockSites[0].UUID.String())
	bodyRdr := strings.NewReader(`{"label": "abcd", "tz": "America/Los_Angeles"}`)
	req, rec = setupReqRec(&mockAccount, echo.POST, url, bodyRdr, ss)
	req.Header.Add("Content-Type", "application/json")
	e.ServeHTTP(rec, req)
	assert.Equal(http.StatusCreated, rec.Code)
	t.Logf("return body %s", rec.Body.String())
	// See that it landed in config tree
	assert.NoError(me.PropEq(propStem+"/vpn/00:40:54:00:00:01/label", "abcd"))
	// See that password is still present
	assert.NoError(me.PropExists(propStem + "/user_password"))
	// Check the REST result; borrow this from account to make json decode
	// easier
	var newVpnResult wgNewConfigResponse
	err = json.Unmarshal(rec.Body.Bytes(), &newVpnResult)
	assert.NoError(err)
	assert.Equal(mockSites[0].OrganizationUUID, newVpnResult.OrganizationUUID)
	assert.Equal("abcd", newVpnResult.Label)
	assert.Equal("192.168.5.1", newVpnResult.ServerAddress)

	// Request vpn config, should see new one
	url = fmt.Sprintf("/api/account/%s/wg", mockAccount.UUID.String())
	req, rec = setupReqRec(&mockAccount, echo.GET, url, nil, ss)
	e.ServeHTTP(rec, req)
	t.Logf("return body %s", rec.Body.String())
	assert.Equal(http.StatusOK, rec.Code)
	// Check response body
	var acctWG accountWGResponse
	err = json.Unmarshal(rec.Body.Bytes(), &acctWG)
	assert.NoError(err)
	assert.Equal(newVpnResult.PublicKey, acctWG[0].PublicKey)
	assert.Equal(newVpnResult.Label, "abcd")

	me.PTree.Dump(newWriteLogger("ptree before delete", t))

	// Delete VPN config
	url = fmt.Sprintf("/api/account/%s/wg/%s/%s/%s",
		mockAccount.UUID.String(),
		newVpnResult.SiteUUID.String(),
		neturl.PathEscape(newVpnResult.accountWGResponseItem.Mac),
		neturl.PathEscape(newVpnResult.accountWGResponseItem.PublicKey))
	t.Logf("url is %s", url)
	req, rec = setupReqRec(&mockAccount, echo.DELETE, url, nil, ss)
	e.ServeHTTP(rec, req)
	assert.Equal(http.StatusOK, rec.Code)
	me.PTree.Dump(newWriteLogger("ptree after delete", t))
	// Check deletion went through, but passwd unchanged
	assert.NoError(me.PropAbsent(propStem + "/vpn/00:40:54:00:00:01"))
	assert.NoError(me.PropExists(propStem + "/user_password"))

	// Request vpn config, there should be none
	url = fmt.Sprintf("/api/account/%s/wg", mockAccount.UUID.String())
	req, rec = setupReqRec(&mockAccount, echo.GET, url, nil, ss)
	e.ServeHTTP(rec, req)
	assert.Equal(http.StatusOK, rec.Code)
	t.Logf("return body %s", rec.Body.String())
	assert.Equal("[]\n", rec.Body.String())

	// Delete User, take another lap (using req we setup before),
	// allocating new VPN config.  Should see user automatically
	// re-created in cfg tree.
	me.PTree.ChangesetInit()
	_, err = me.PTree.Delete(propStem)
	me.PTree.ChangesetCommit()
	assert.NoError(err)
	url = fmt.Sprintf("/api/account/%s/wg/%s/new", mockAccount.UUID.String(), mockSites[0].UUID.String())
	bodyRdr = strings.NewReader(`{"label": "abcd", "tz": "America/Los_Angeles"}`)
	req, rec = setupReqRec(&mockAccount, echo.POST, url, bodyRdr, ss)
	req.Header.Add("Content-Type", "application/json")
	e.ServeHTTP(rec, req)
	assert.Equal(http.StatusCreated, rec.Code)
	assert.NoError(me.PropExists(propStem))
	// n.b. increment of mac to ...:02
	assert.NoError(me.PropEq(propStem+"/vpn/00:40:54:00:00:02/label", "abcd"))
	assert.NoError(me.PropEq(propStem+"/self_provisioning", "true"))
}
