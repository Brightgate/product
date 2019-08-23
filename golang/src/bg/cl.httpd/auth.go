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
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"bg/cloud_models/appliancedb"

	"github.com/dgrijalva/jwt-go"
	"github.com/gorilla/sessions"
	"github.com/labstack/echo"
	"github.com/pkg/errors"
	"github.com/satori/uuid"
	"golang.org/x/oauth2"

	"github.com/markbates/goth"
	"github.com/markbates/goth/gothic"
	"github.com/markbates/goth/providers/auth0"
	"github.com/markbates/goth/providers/azureadv2"
	"github.com/markbates/goth/providers/google"
	"github.com/markbates/goth/providers/openidConnect"

	"google.golang.org/api/people/v1"
)

type providerContextKey struct{}

func init() {
	// Replace Gothic GetProviderName routine
	gothic.GetProviderName = getProviderName
}

type authHandler struct {
	sessionStore sessions.Store
	db           appliancedb.DataStore
	providers    []goth.Provider
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

func callbackURI(provider string) (string, error) {
	if !environ.Developer {
		return fmt.Sprintf("https://%s/auth/%s/callback",
			environ.CertHostname, provider), nil
	}
	// Developer Mode
	hoststr, portstr, err := net.SplitHostPort(environ.HTTPSListen)
	if err != nil {
		return "", errors.Wrap(err, "bad HTTPSListen address")
	}
	if hoststr == "" {
		hoststr, err = os.Hostname()
		if err != nil {
			return "", errors.New("could not get hostname")
		}
	}
	port, err := net.LookupPort("tcp", portstr)
	if err != nil {
		return "", errors.Errorf("could not parse port %s", portstr)
	}
	scheme := "https"
	if environ.DisableTLS {
		scheme = "http"
	}
	return fmt.Sprintf("%s://%s.b10e.net:%d/auth/%s/callback",
		scheme, hoststr, port, provider), nil
}

type providerFunc func(*echo.Echo) goth.Provider

func googleProvider(r *echo.Echo) goth.Provider {
	if environ.GoogleKey == "" && environ.GoogleSecret == "" {
		r.Logger.Warnf("not enabling google authentication: missing B10E_CLHTTPD_GOOGLE_KEY or B10E_CLHTTPD_GOOGLE_SECRET")
		return nil
	}

	callback, err := callbackURI("google")
	if err != nil {
		r.Logger.Fatalf("Failed to enable google auth: %v", err)
	}
	r.Logger.Infof("enabling google authentication")
	return google.New(environ.GoogleKey, environ.GoogleSecret,
		callback, "profile", "email",
		people.UserPhonenumbersReadScope)
}

func openidConnectProvider(r *echo.Echo) goth.Provider {
	if environ.OpenIDConnectKey == "" || environ.OpenIDConnectSecret == "" || environ.OpenIDConnectDiscoveryURL == "" {
		r.Logger.Warnf("not enabling openid authentication: missing B10E_CLHTTPD_OPENID_CONNECT_KEY, B10E_CLHTTPD_OPENID_CONNECT_SECRET or B10E_CLHTTPD_OPENID_CONNECT_DISCOVERY_URL")
		return nil
	}
	callback, err := callbackURI("openid-connect")
	if err != nil {
		r.Logger.Fatalf("Failed to enable openid-connect auth: %v", err)
	}
	r.Logger.Infof("enabling openid connect authentication via %s", environ.OpenIDConnectDiscoveryURL)
	openidConnect, err := openidConnect.New(
		environ.OpenIDConnectKey,
		environ.OpenIDConnectSecret,
		callback,
		environ.OpenIDConnectDiscoveryURL,
		"openid", "profile", "email", "phone")
	if err != nil || openidConnect == nil {
		r.Logger.Fatalf("failed to initialized openid-connect")
	}
	return openidConnect
}

func auth0Provider(r *echo.Echo) goth.Provider {
	if environ.Auth0Key == "" || environ.Auth0Secret == "" || environ.Auth0Domain == "" {
		r.Logger.Warnf("not enabling Auth0 authentication: missing B10E_CLHTTPD_AUTH0_KEY, B10E_CLHTTPD_AUTH0_SECRET or B10E_CLHTTPD_AUTH0_DOMAIN")
		return nil
	}

	callback, err := callbackURI("auth0")
	if err != nil {
		r.Logger.Fatalf("Failed to enable auth0 auth: %v", err)
	}
	r.Logger.Infof("enabling Auth0 authentication")
	return auth0.New(environ.Auth0Key, environ.Auth0Secret, callback,
		environ.Auth0Domain, "openid", "profile", "email", "zug.zug")
}

func azureadv2Provider(r *echo.Echo) goth.Provider {
	if environ.AzureADV2Key == "" || environ.AzureADV2Secret == "" {
		r.Logger.Warnf("not enabling AzureADV2 authentication: missing B10E_CLHTTPD_AZUREADV2_KEY, B10E_CLHTTPD_AZUREADV2_SECRET")
		return nil
	}

	callback, err := callbackURI("azureadv2")
	if err != nil {
		r.Logger.Fatalf("Failed to enable azureadv2 auth: %v", err)
	}
	r.Logger.Infof("enabling AzureADV2 authentication")
	opts := azureadv2.ProviderOptions{}
	return azureadv2.New(environ.AzureADV2Key, environ.AzureADV2Secret, callback, opts)
}

// getProviders implements /auth/providers, which indicates which oauth
// providers are available.
func (a *authHandler) getProviders(c echo.Context) error {
	provNames := make([]string, 0)
	for _, prov := range a.providers {
		provNames = append(provNames, prov.Name())
	}
	providers := struct {
		Mode      string   `json:"mode"`
		Providers []string `json:"providers"`
	}{
		Mode: "cloud",
		// XXX make more dynamic as providers register
		Providers: provNames,
	}
	return c.JSON(http.StatusOK, providers)
}

// getAuth implements /auth and gives (developer-focused)
// auth choices and controls.
func (a *authHandler) getAuth(c echo.Context) error {
	html := `<!doctype html>
		<head>
		<meta charset=utf-8>
		<title>Auth</title>
		</head>
		<body>`

	for _, prov := range a.providers {
		html += fmt.Sprintf("<p><a href=\"/auth/%s\">Login with %s</a></p>\n", prov.Name(), prov.Name())
	}
	html += "<p><a href=\"/auth/logout\">Logout</a></p>\n"

	sess, err := a.sessionStore.Get(c.Request(), "bg_login")
	if err == nil {
		var email string
		email, ok := sess.Values["email"].(string)
		if ok {
			html += fmt.Sprintf("<p>Hello there; I think you are: '%v'</p>\n", email)
		} else {
			html += fmt.Sprintf("<p>Hello there; Log in so I know who you are.</p>\n")
		}
	} else {
		html += fmt.Sprintf("<p>Error was: %v</p>\n", err)
	}
	html += `</body></html>`
	return c.HTML(http.StatusOK, html)
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

func splitEmail(email string) (string, string, error) {
	split := strings.SplitN(email, "@", 2)
	if len(split) != 2 {
		return "", "", fmt.Errorf("bad email address")
	}
	return split[0], split[1], nil
}

func getUserGoogleHD(user *goth.User) string {
	// hd (hosted domain) is basically google's tenant ID
	hdParam, ok := user.RawData["hd"].(string)
	if ok && hdParam != "" {
		return hdParam
	}
	return ""
}

func getUserAzureTenant(user *goth.User) (string, error) {
	// ParseUnverified is not to be used lightly-- however by the time we
	// have reached this code, we have completed the oauth2 exchange, and we
	// trust the JWT.
	tok, _, err := new(jwt.Parser).ParseUnverified(user.AccessToken, jwt.MapClaims{})
	if err != nil {
		return "", fmt.Errorf("getUserAzureTenant: failed to parse user.AccessToken: %v", err)
	}
	// We don't check this cast because the type is set above
	claims := tok.Claims.(jwt.MapClaims)
	tid, ok := claims["tid"].(string)
	if !ok {
		return "", fmt.Errorf("getUserAzureTenant: failed to find 'tid' in claims: %v", claims)
	}
	return tid, nil
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
		tenant = getUserGoogleHD(&user)
	}

	if user.Provider == "azureadv2" {
		var err error
		tenant, err = getUserAzureTenant(&user)
		if err != nil {
			c.Logger().Warnf("Failed to get tenant for azure user: %v", err)
		}
	}

	if tenant != "" {
		rule, err := a.db.OAuth2OrganizationRuleTest(ctx,
			user.Provider, appliancedb.RuleTypeTenant, tenant)
		if err == nil {
			return rule.OrganizationUUID, nil
		}
	}

	// Next see if there is a domain level rule for the user
	_, domainPart, err := splitEmail(user.Email)
	if err == nil && domainPart != "" {
		rule, err := a.db.OAuth2OrganizationRuleTest(ctx,
			user.Provider, appliancedb.RuleTypeDomain, domainPart)
		if err == nil {
			return rule.OrganizationUUID, nil
		}
	}

	// Finally, see if there is an email address level rule for the user
	if user.Email != "" {
		rule, err := a.db.OAuth2OrganizationRuleTest(ctx,
			user.Provider, appliancedb.RuleTypeEmail, user.Email)
		if err == nil {
			return rule.OrganizationUUID, nil
		}
	}

	c.Logger().Warnf("findOrganization: No rules matched tenant=%q domain=%q email=%q",
		tenant, domainPart, user.Email)
	return uuid.Nil, fmt.Errorf("no rule matched user's tenant, domain or email")
}

func getAzureUserPhone(logger echo.Logger, user goth.User) (string, error) {
	var phoneNumber string
	logger.Debugf("Trying to get phone number for %s, RawData %#v", user.UserID, user.RawData)
	phoneNumber, _ = user.RawData["mobilePhone"].(string)
	if phoneNumber != "" {
		return phoneNumber, nil
	}

	bizPhones, ok := user.RawData["businessPhones"].([]interface{})
	if ok && len(bizPhones) > 0 {
		phoneNumber, _ = bizPhones[0].(string)
	}
	if phoneNumber != "" {
		return phoneNumber, nil
	}
	return "", errors.Errorf("Failed to find any phone Numbers for %#v", user)
}

func getGoogleUserPhone(logger echo.Logger, user goth.User) (string, error) {
	logger.Debugf("Trying to get a googlePerson and PhoneNumber for %s", user.UserID)
	tokenSource := oauth2.StaticTokenSource(&oauth2.Token{
		AccessToken: user.AccessToken,
	})
	client := &http.Client{
		Transport: &oauth2.Transport{
			Source: tokenSource,
		},
	}
	svc, err := people.New(client)
	if err != nil {
		return "", errors.Wrap(err, "Failed to make people client")
	}
	peopleService := people.NewPeopleService(svc)
	googlePerson, err := peopleService.Get("people/me").PersonFields("phoneNumbers").Do()
	if err != nil {
		return "", errors.Wrap(err, "Failed to get googlePerson")
	}
	logger.Infof("Fetched googlePerson: %#v", googlePerson)
	logger.Debugf("PhoneNumbers: %#v", googlePerson.PhoneNumbers)
	if len(googlePerson.PhoneNumbers) == 0 {
		logger.Warnf("No phone number in response %#v; account may not have a phone number", googlePerson)
		return "", nil
	}
	// If none are marked "primary", this will cause us to take the first
	bestIndex := 0
	for i, num := range googlePerson.PhoneNumbers {
		logger.Infof("phone #%d, %#v", i, num)
		if num.Metadata.Primary {
			bestIndex = i
			break
		}
	}

	if googlePerson.PhoneNumbers[bestIndex].CanonicalForm != "" {
		return googlePerson.PhoneNumbers[bestIndex].CanonicalForm, nil
	} else if googlePerson.PhoneNumbers[bestIndex].Value != "" {
		return googlePerson.PhoneNumbers[bestIndex].Value, nil
	}
	return "", fmt.Errorf("Couldn't understand phonenumber record %#v", googlePerson.PhoneNumbers[bestIndex])
}

func (a *authHandler) mkNewUser(c echo.Context, user goth.User) (*appliancedb.LoginInfo, error) {
	// See if we can find an organization for this user.
	var err error
	var phoneNumber string
	ctx := c.Request().Context()

	orgUUID, err := a.findOrganization(ctx, c, user)
	if err != nil {
		c.Logger().Warnf("identity -> organization mapping failed: Provider=%s Email=%s HD=%s", user.Provider, user.Email, getUserGoogleHD(&user))
		return nil, fmt.Errorf("identity '%s' (via %s) is not affiliated with a recognized customer; or %s is not registered as a login provider for your organization",
			user.Email, user.Provider, user.Provider)
	}
	organization, err := a.db.OrganizationByUUID(ctx, orgUUID)

	c.Logger().Infof("Creating new account for '%s' from '%s' <%s> (%s|%s)",
		user.Name, organization.Name, user.Email, user.Provider,
		user.UserID)

	if user.Provider == "google" {
		var err error
		phoneNumber, err = getGoogleUserPhone(c.Logger(), user)
		if err != nil {
			c.Logger().Warnf("Couldn't get google user phone: %s", err)
		}
	} else if user.Provider == "azureadv2" {
		var err error
		phoneNumber, err = getAzureUserPhone(c.Logger(), user)
		if err != nil {
			c.Logger().Warnf("Couldn't get azure user phone: %s", err)
		}
	}

	person := &appliancedb.Person{
		UUID:         uuid.NewV4(),
		Name:         user.Name,
		PrimaryEmail: user.Email,
	}

	account := &appliancedb.Account{
		UUID:             uuid.NewV4(),
		Email:            user.Email,
		PhoneNumber:      phoneNumber,
		PersonUUID:       person.UUID,
		OrganizationUUID: organization.UUID,
	}

	oauth2ID := &appliancedb.OAuth2Identity{
		Subject:     user.UserID,
		Provider:    user.Provider,
		AccountUUID: account.UUID,
	}
	c.Logger().Debugf("Add new user: person=%v, account=%v, oauth2ID=%v",
		person, account, oauth2ID)

	tx, err := a.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	err = a.db.InsertPersonTx(ctx, tx, person)
	if err != nil {
		return nil, err
	}
	err = a.db.InsertAccountTx(ctx, tx, account)
	if err != nil {
		return nil, err
	}
	orgRole := &appliancedb.AccountOrgRole{
		AccountUUID:            account.UUID,
		OrganizationUUID:       account.OrganizationUUID,
		TargetOrganizationUUID: account.OrganizationUUID,
		Relationship:           "self",
		Role:                   "user",
	}
	err = a.db.InsertAccountOrgRoleTx(ctx, tx, orgRole)
	if err != nil {
		return nil, err
	}
	adminRoles, err := a.db.AccountOrgRolesByOrgTx(ctx, tx,
		organization.UUID, "admin")
	if err != nil {
		return nil, err
	}
	if len(adminRoles) == 0 {
		c.Logger().Infof("No admins for this organization; also giving admin role: %v", account)
		orgRole.Role = "admin"
		err = a.db.InsertAccountOrgRoleTx(ctx, tx, orgRole)
		if err != nil {
			return nil, err
		}
	}
	err = a.db.InsertOAuth2IdentityTx(ctx, tx, oauth2ID)
	if err != nil {
		return nil, err
	}
	err = tx.Commit()
	if err != nil {
		return nil, err
	}
	return a.db.LoginInfoByProviderAndSubject(
		context.TODO(), user.Provider, user.UserID)
}

func (a *authHandler) getUser(c echo.Context, user goth.User) (*appliancedb.LoginInfo, error) {
	// See if we can find an organization for this user.
	var err error
	ctx := c.Request().Context()

	c.Logger().Debugf("getUser for %v|%v", user.Provider, user.UserID)

	loginInfo, err := a.db.LoginInfoByProviderAndSubject(
		ctx, user.Provider, user.UserID)
	if _, ok := err.(appliancedb.NotFoundError); ok {
		return a.mkNewUser(c, user)
	}

	// XXX in the future, this is a place to do post-login checks on the
	// account, and possibly go back to the oauth provider for up-to-date
	// info about the user.

	return loginInfo, err
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

	loginInfo, err := a.getUser(c, user)
	if err != nil {
		c.Logger().Errorf("Error: %s", err)
		return echo.NewHTTPError(http.StatusUnauthorized, err.Error())
	}
	c.Logger().Infof("loginInfo is %#v", loginInfo)

	if len(loginInfo.PrimaryOrgRoles) == 0 {
		return echo.NewHTTPError(http.StatusUnauthorized,
			"login is disabled for this account; the account has no roles")
	}

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
		err = a.db.UpsertOAuth2RefreshToken(ctx, &tok)
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
	session.Values["primary_org_roles"] = loginInfo.PrimaryOrgRoles

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

type userIDResponse struct {
	Username        string    `json:"username"`
	Email           string    `json:"email"`
	PhoneNumber     string    `json:"phoneNumber"`
	Name            string    `json:"name"`
	Organization    string    `json:"organization"`
	SelfProvisioned bool      `json:"selfProvisioned"`
	AccountUUID     uuid.UUID `json:"accountUUID"`
}

// getUserID implements /auth/userid, which returns information about the
// logged-in user.
func (a *authHandler) getUserID(c echo.Context) error {
	ctx := c.Request().Context()
	session, err := a.sessionStore.Get(c.Request(), "bg_login")
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized)
	}
	au, ok := session.Values["account_uuid"].(string)
	if !ok {
		return echo.NewHTTPError(http.StatusUnauthorized)
	}
	accountUUID, err := uuid.FromString(au)
	if err != nil || accountUUID == uuid.Nil {
		return echo.NewHTTPError(http.StatusUnauthorized)
	}

	account, err := a.db.AccountByUUID(ctx, accountUUID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err)
	}
	person, err := a.db.PersonByUUID(ctx, account.PersonUUID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err)
	}
	resp := userIDResponse{
		Username:    account.Email,
		Email:       account.Email,
		PhoneNumber: account.PhoneNumber,
		Name:        person.Name,
		AccountUUID: accountUUID,
	}
	if account.OrganizationUUID != appliancedb.NullOrganizationUUID {
		organization, err := a.db.OrganizationByUUID(ctx, account.OrganizationUUID)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err)
		}
		resp.Organization = organization.Name
	}

	_, err = a.db.AccountSecretsByUUID(ctx, accountUUID)
	if err == nil {
		resp.SelfProvisioned = true
	}

	return c.JSON(http.StatusOK, &resp)
}

// newAuthHandler creates an authHandler to handle authentication endpoints
// and routes the handler into the echo instance.  Note that it manipulates
// global gothic state.
func newAuthHandler(r *echo.Echo, sessionStore sessions.Store, applianceDB appliancedb.DataStore) *authHandler {
	h := &authHandler{
		sessionStore: sessionStore,
		providers:    make([]goth.Provider, 0),
		db:           applianceDB,
	}
	gothic.Store = sessionStore
	providers := []providerFunc{googleProvider, azureadv2Provider, openidConnectProvider, auth0Provider}
	for _, provFunc := range providers {
		p := provFunc(r)
		if p != nil {
			h.providers = append(h.providers, p)
		}
	}
	if len(h.providers) == 0 {
		r.Logger.Warnf("No auth providers configured!  No one can log in.")
	}
	goth.UseProviders(h.providers...)

	r.GET("/auth", h.getAuth)
	r.GET("/auth/providers", h.getProviders)
	r.GET("/auth/:provider", h.getProvider)
	r.GET("/auth/:provider/callback", h.getProviderCallback)
	r.GET("/auth/logout", h.getLogout)
	r.GET("/auth/userid", h.getUserID)
	return h
}

type sessionMiddleware struct {
	sessionStore sessions.Store
}

func newSessionMiddleware(sessionStore sessions.Store) *sessionMiddleware {
	return &sessionMiddleware{
		sessionStore,
	}
}

// Process checks that the user has a valid login session, and places the
// account_uuid into the echo context for use in subsequent handlers.
func (sm *sessionMiddleware) Process(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		session, err := sm.sessionStore.Get(c.Request(), "bg_login")
		if err != nil {
			return echo.NewHTTPError(http.StatusUnauthorized)
		}
		au, ok := session.Values["account_uuid"].(string)
		if !ok {
			return echo.NewHTTPError(http.StatusUnauthorized)
		}
		accountUUID, err := uuid.FromString(au)
		if err != nil {
			return echo.NewHTTPError(http.StatusUnauthorized)
		}
		c.Set("account_uuid", accountUUID)
		return next(c)
	}
}
