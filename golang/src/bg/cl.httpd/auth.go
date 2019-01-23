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
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"bg/cloud_models/appliancedb"

	"github.com/gorilla/sessions"
	"github.com/labstack/echo"
	"github.com/satori/uuid"

	"github.com/markbates/goth"
	"github.com/markbates/goth/gothic"
	"github.com/markbates/goth/providers/auth0"
	"github.com/markbates/goth/providers/azureadv2"
	"github.com/markbates/goth/providers/google"
	"github.com/markbates/goth/providers/openidConnect"
)

type providerContextKey struct{}

func init() {
	// Replace Gothic GetProviderName routine
	gothic.GetProviderName = getProviderName
}

type authHandler struct {
	sessionStore sessions.Store
	applianceDB  appliancedb.DataStore
}

// getProviderName fetches the name of the auth provider.  This function
// is used as a drop-in replacement for gothic.GetProviderName, which is
// overrideable.
func getProviderName(req *http.Request) (string, error) {
	s, ok := req.Context().Value(providerContextKey{}).(string)
	if !ok {
		return "", fmt.Errorf("couldn't get Provider name")
	}
	return s, nil
}

// Transfer the URL "provider" parameter to the Go Context, so that getProviderName can
// fetch it.  See also https://github.com/markbates/goth/issues/238.
func providerToGoContext(c echo.Context) {
	// Put the provider name into the go context so gothic can find it
	newCtx := context.WithValue(c.Request().Context(), providerContextKey{}, c.Param("provider"))
	nr := c.Request().WithContext(newCtx)
	c.SetRequest(nr)
}

func callback(provider string) string {
	var callback string
	if environ.Developer {
		hoststr, portstr, err := net.SplitHostPort(environ.HTTPSListen)
		if err != nil {
			log.Fatalf("bad HTTPSListen address")
		}
		if hoststr == "" {
			hoststr, err = os.Hostname()
			if err != nil {
				log.Fatalf("could not get hostname")
			}
		}
		port, err := net.LookupPort("tcp", portstr)
		if err != nil {
			log.Fatalf("could not parse port %s", portstr)
		}
		scheme := "https"
		if environ.DisableTLS {
			scheme = "http"
		}
		callback = fmt.Sprintf("%s://%s.b10e.net:%d/auth/%s/callback",
			scheme, hoststr, port, provider)
	} else {
		callback = fmt.Sprintf("https://%s/auth/%s/callback",
			environ.CertHostname, provider)
	}
	return callback
}

func googleProvider() {
	if environ.GoogleKey == "" && environ.GoogleSecret == "" {
		log.Printf("not enabling google authentication: missing B10E_CLHTTPD_GOOGLE_KEY or B10E_CLHTTPD_GOOGLE_SECRET")
		return
	}

	googleProvider := google.New(environ.GoogleKey, environ.GoogleSecret,
		callback("google"), "openid", "profile", "email", "phone")
	goth.UseProviders(googleProvider)
}

func openidConnectProvider() {
	if environ.OpenIDConnectKey == "" || environ.OpenIDConnectSecret == "" || environ.OpenIDConnectDiscoveryURL == "" {
		log.Printf("not enabling openid authentication: missing B10E_CLHTTPD_OPENID_CONNECT_KEY, B10E_CLHTTPD_OPENID_CONNECT_SECRET or B10E_CLHTTPD_OPENID_CONNECT_DISCOVERY_URL")
		return
	}
	log.Printf("enabling openid connect authentication via %s", environ.OpenIDConnectDiscoveryURL)
	openidConnect, err := openidConnect.New(
		environ.OpenIDConnectKey,
		environ.OpenIDConnectSecret,
		callback("openid-connect"),
		environ.OpenIDConnectDiscoveryURL,
		"openid", "profile", "email", "phone")
	if err != nil || openidConnect == nil {
		log.Fatalf("failed to initialized openid-connect")
	}
	goth.UseProviders(openidConnect)
}

func auth0Provider() {
	if environ.Auth0Key == "" || environ.Auth0Secret == "" || environ.Auth0Domain == "" {
		log.Printf("not enabling Auth0 authentication: missing B10E_CLHTTPD_AUTH0_KEY, B10E_CLHTTPD_AUTH0_SECRET or B10E_CLHTTPD_AUTH0_DOMAIN")
		return
	}

	log.Printf("enabling Auth0 authentication")
	auth0Provider := auth0.New(environ.Auth0Key, environ.Auth0Secret, callback("auth0"),
		environ.Auth0Domain, "openid", "profile", "email")
	goth.UseProviders(auth0Provider)
}

func azureadv2Provider() {
	if environ.AzureADV2Key == "" || environ.AzureADV2Secret == "" {
		log.Printf("not enabling AzureADV2 authentication: missing B10E_CLHTTPD_AZUREADV2_KEY, B10E_CLHTTPD_AZUREADV2_SECRET")
		return
	}

	log.Printf("enabling AzureADV2 authentication")
	azureADV2Provider := azureadv2.New(environ.AzureADV2Key, environ.AzureADV2Secret, callback("azureadv2"), azureadv2.ProviderOptions{})
	goth.UseProviders(azureADV2Provider)
}

// getProvider implements /auth/:provider, which starts the oauth flow.
func (a *authHandler) getProvider(c echo.Context) error {
	// Transfer the provider name to the Go context.  Done so that
	// gothic can determine the provider name later.  This is done
	// by our override of gothic.GetProviderName, getProviderName.
	// Yes, this is annoying and complicated.
	providerToGoContext(c)
	// try to get the user without re-authenticating
	user, err := gothic.CompleteUserAuth(c.Response(), c.Request())
	if err != nil {
		gothic.BeginAuthHandler(c.Response(), c.Request())
		return nil
	}
	return c.JSON(http.StatusOK, user)
}

// findOrganization does three tests against the incoming user, looking
// at the OAuth2Organization Rules.
//
// 1. If we can extract a tenant ID for the user, see if there is an matching
// provider/tenant in the rules table.  This is by far the best and most secure
// method, and what we expect to use in production most of the time.
// 2. If email address is present, extract the domain name and see if there is
// a matching provider/domain in the rules table.
// 3. If email address is present, see if there is a matching email address
// in the rules table.
func (a *authHandler) findOrganization(ctx context.Context, c echo.Context,
	user goth.User) (uuid.UUID, error) {

	var tenant string
	// First see if we can look the user up by tenant ID
	if user.Provider == "google" {
		// hd (hosted domain) is basically google's tenant ID
		hdParam, ok := user.RawData["hd"].(string)
		if ok && hdParam != "" {
			tenant = hdParam
		}
	}

	if tenant != "" {
		rule, err := a.applianceDB.OAuth2OrganizationRuleTest(ctx,
			user.Provider, appliancedb.RuleTypeTenant, tenant)
		if err == nil {
			return rule.OrganizationUUID, nil
		}
	}

	// Next see if there is a domain level rule for the user
	_, domainPart, err := splitEmail(user.Email)
	if err == nil && domainPart != "" {
		rule, err := a.applianceDB.OAuth2OrganizationRuleTest(ctx,
			user.Provider, appliancedb.RuleTypeDomain, domainPart)
		if err == nil {
			return rule.OrganizationUUID, nil
		}
	}

	// Finally, see if there is an email address level rule for the user
	if user.Email != "" {
		rule, err := a.applianceDB.OAuth2OrganizationRuleTest(ctx,
			user.Provider, appliancedb.RuleTypeEmail, user.Email)
		if err == nil {
			return rule.OrganizationUUID, nil
		}
	}

	c.Logger().Warnf("findOrganization: No rules matched tenant=%q domain=%q email=%q",
		tenant, domainPart, user.Email)
	return uuid.Nil, fmt.Errorf("no rule matched user's tenant, domain or email")
}

func (a *authHandler) mkNewUser(c echo.Context, user goth.User,
	organization uuid.UUID) (*appliancedb.LoginInfo, error) {

	person := &appliancedb.Person{
		UUID:         uuid.NewV4(),
		Name:         user.Name,
		PrimaryEmail: user.Email,
	}

	account := &appliancedb.Account{
		UUID:             uuid.NewV4(),
		Email:            user.Email,
		PhoneNumber:      "", // XXX
		PersonUUID:       person.UUID,
		OrganizationUUID: organization,
	}

	oauth2ID := &appliancedb.OAuth2Identity{
		Subject:     user.UserID,
		Provider:    user.Provider,
		AccountUUID: account.UUID,
	}
	c.Logger().Infof("Creating new user: person=%#v account=%#v oauth2ID=%#v", person, account, oauth2ID)

	ctx := c.Request().Context()
	tx, err := a.applianceDB.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err != nil {
		return nil, err
	}
	err = a.applianceDB.InsertPersonTx(ctx, tx, person)
	if err != nil {
		return nil, err
	}
	err = a.applianceDB.InsertAccountTx(ctx, tx, account)
	if err != nil {
		return nil, err
	}
	err = a.applianceDB.InsertOAuth2IdentityTx(ctx, tx, oauth2ID)
	if err != nil {
		return nil, err
	}
	err = tx.Commit()
	if err != nil {
		return nil, err
	}
	return a.applianceDB.LoginInfoByProviderAndSubject(
		context.TODO(), user.Provider, user.UserID)
}

func splitEmail(email string) (string, string, error) {
	split := strings.SplitN(email, "@", 2)
	if len(split) != 2 {
		return "", "", fmt.Errorf("bad email address")
	}
	return split[0], split[1], nil
}

// getProviderCallback implements /auth/:provider/callback, which completes
// the oauth flow.
func (a *authHandler) getProviderCallback(c echo.Context) error {
	// Put the provider name into the go context so gothic can find it
	// See comment in getProvider().
	providerToGoContext(c)
	ctx := c.Request().Context()

	user, err := gothic.CompleteUserAuth(c.Response(), c.Request())
	c.Logger().Infof("user is %#v", user)

	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err)
	}

	c.Logger().Infof("Lookup loginInfo for (%v,%v)", user.Provider, user.UserID)
	loginInfo, err := a.applianceDB.LoginInfoByProviderAndSubject(
		ctx, user.Provider, user.UserID)
	if err != nil {
		if _, ok := err.(appliancedb.NotFoundError); ok {
			c.Logger().Infof(
				"Didn't find user (%v,%v).  Try to create one",
				user.Provider, user.UserID)
			// See if we can find an organization for this user.
			var orgUU uuid.UUID
			orgUU, err = a.findOrganization(ctx, c, user)
			if err != nil {
				s := fmt.Sprintf("Your identity '%s' (via %s) is not affiliated with a recognized customer; or %s is not registered as a login provider for your organization.",
					user.Email, user.Provider, user.Provider)
				return echo.NewHTTPError(http.StatusUnauthorized, s)
			}
			loginInfo, err = a.mkNewUser(c, user, orgUU)
		}
		if err != nil {
			c.Logger().Errorf("Error: %s", err)
			return echo.NewHTTPError(http.StatusUnauthorized,
				"Login Failed. Please contact support.") // XXX
		}
	}
	c.Logger().Infof("loginInfo is %#v", loginInfo)

	// Try to save the refresh token
	//
	// XXX this is somewhat dead because there is presently no way to get
	// the goth/google provider to specify oauth2.AccessTypeOffline (see
	// golang.org/x/oauth2 docs)
	if user.RefreshToken != "" {
		tok := appliancedb.OAuth2RefreshToken{
			OAuth2IdentityID: loginInfo.OAuth2IdentityID,
			Token:            user.RefreshToken,
		}
		err = a.applianceDB.UpsertOAuth2RefreshToken(ctx, &tok)
		if err != nil {
			c.Logger().Warnf("failed to store refresh token: %v", err)
		}
	}

	// XXX in the future, we may also want to stash the access token, and
	// the database has schema for that.  But we have no use for this
	// material now, and the tokens are short-lived, so there is no reason
	// to keep them yet.

	sessionUserID := c.Param("provider") + "|" + user.UserID

	// As per
	// http://www.gorillatoolkit.org/pkg/sessions#CookieStore.Get,
	// Get() 'returns a new session and an error if the session
	// exists but could not be decoded.'  For our purposes, we just
	// want to blow over top of an invalid session, so drive on in
	// that case.
	session, err := a.sessionStore.Get(c.Request(), "bg_login")
	if err != nil && session == nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err)
	}
	session.Values["email"] = user.Email
	session.Values["userid"] = sessionUserID
	session.Values["auth_time"] = time.Now().Format(time.RFC3339)
	session.Values["account_uuid"] = loginInfo.Account.UUID.String()
	session.Values["organization_uuid"] = loginInfo.Account.OrganizationUUID.String()

	if err = session.Save(c.Request(), c.Response()); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err)
	}
	return c.Redirect(http.StatusTemporaryRedirect, "/client-web/")
}

// getLogout implements /auth/logout
func (a *authHandler) getLogout(c echo.Context) error {
	gothic.Logout(c.Response(), c.Request())
	session, _ := a.sessionStore.Get(c.Request(), "bg_login")
	if session != nil {
		session.Options.MaxAge = -1
		session.Values = make(map[interface{}]interface{})
		if err := session.Save(c.Request(), c.Response()); err != nil {
			c.Logger().Warnf("logout: Failed to save session: %v", err)
		}
	}
	return c.Redirect(http.StatusTemporaryRedirect, "/")
}

// getUserID implements /auth/userid; for now this is for development purposes.
func (a *authHandler) getUserID(c echo.Context) error {
	session, err := a.sessionStore.Get(c.Request(), "bg_login")
	if err != nil || session.Values["userid"] == nil {
		return echo.NewHTTPError(http.StatusUnauthorized)
	}
	return c.JSON(http.StatusOK, session.Values["userid"].(string))
}

// newAuthHandler creates an authHandler to handle authentication endpoints
// and routes the handler into the echo instance.  Note that it manipulates
// global gothic state.
func newAuthHandler(r *echo.Echo, sessionStore sessions.Store, applianceDB appliancedb.DataStore) *authHandler {
	gothic.Store = sessionStore
	auth0Provider()
	googleProvider()
	openidConnectProvider()
	azureadv2Provider()
	h := &authHandler{sessionStore, applianceDB}

	r.GET("/auth/:provider", h.getProvider)
	r.GET("/auth/:provider/callback", h.getProviderCallback)
	r.GET("/auth/logout", h.getLogout)
	r.GET("/auth/userid", h.getUserID)
	return h
}
