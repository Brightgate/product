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
	"crypto/sha256"
	"encoding/gob"
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/mail"
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

	"cloud.google.com/go/storage"
	"google.golang.org/api/option"
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
	avatarBucket *storage.BucketHandle
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
	if secrets.GoogleKey == "" && secrets.GoogleSecret == "" {
		r.Logger.Warnf("not enabling google authentication: missing B10E_CLHTTPD_GOOGLE_KEY or B10E_CLHTTPD_GOOGLE_SECRET")
		return nil
	}

	callback, err := callbackURI("google")
	if err != nil {
		r.Logger.Fatalf("Failed to enable google auth: %v", err)
	}
	r.Logger.Infof("enabling google authentication")
	return google.New(secrets.GoogleKey, secrets.GoogleSecret,
		callback, "profile", "email",
		people.UserPhonenumbersReadScope)
}

func openidConnectProvider(r *echo.Echo) goth.Provider {
	if secrets.OpenIDConnectKey == "" || secrets.OpenIDConnectSecret == "" || environ.OpenIDConnectDiscoveryURL == "" {
		r.Logger.Warnf("not enabling openid authentication: missing B10E_CLHTTPD_OPENID_CONNECT_KEY, B10E_CLHTTPD_OPENID_CONNECT_SECRET or B10E_CLHTTPD_OPENID_CONNECT_DISCOVERY_URL")
		return nil
	}
	callback, err := callbackURI("openid-connect")
	if err != nil {
		r.Logger.Fatalf("Failed to enable openid-connect auth: %v", err)
	}
	r.Logger.Infof("enabling openid connect authentication via %s", environ.OpenIDConnectDiscoveryURL)
	openidConnect, err := openidConnect.New(
		secrets.OpenIDConnectKey,
		secrets.OpenIDConnectSecret,
		callback,
		environ.OpenIDConnectDiscoveryURL,
		"openid", "profile", "email", "phone")
	if err != nil || openidConnect == nil {
		r.Logger.Fatalf("failed to initialize openid-connect: %v", err)
	}
	return openidConnect
}

func auth0Provider(r *echo.Echo) goth.Provider {
	if secrets.Auth0Key == "" || secrets.Auth0Secret == "" || environ.Auth0Domain == "" {
		r.Logger.Warnf("not enabling Auth0 authentication: missing B10E_CLHTTPD_AUTH0_KEY, B10E_CLHTTPD_AUTH0_SECRET or B10E_CLHTTPD_AUTH0_DOMAIN")
		return nil
	}

	callback, err := callbackURI("auth0")
	if err != nil {
		r.Logger.Fatalf("Failed to enable auth0 auth: %v", err)
	}
	r.Logger.Infof("enabling Auth0 authentication")
	return auth0.New(secrets.Auth0Key, secrets.Auth0Secret, callback,
		environ.Auth0Domain, "openid", "profile", "email", "zug.zug")
}

func azureadv2Provider(r *echo.Echo) goth.Provider {
	if secrets.AzureADV2Key == "" || secrets.AzureADV2Secret == "" {
		r.Logger.Warnf("not enabling AzureADV2 authentication: missing B10E_CLHTTPD_AZUREADV2_KEY, B10E_CLHTTPD_AZUREADV2_SECRET")
		return nil
	}

	callback, err := callbackURI("azureadv2")
	if err != nil {
		r.Logger.Fatalf("Failed to enable azureadv2 auth: %v", err)
	}
	r.Logger.Infof("enabling AzureADV2 authentication")
	opts := azureadv2.ProviderOptions{}
	return azureadv2.New(secrets.AzureADV2Key, secrets.AzureADV2Secret, callback, opts)
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

const reasonServerError = 1
const reasonNoOauthRuleMatch = 2
const reasonNoRoles = 3
const reasonNoSession = 4

type useridError struct {
	Reason       int    `json:"reason"`
	Email        string `json:"email"`
	Provider     string `json:"provider"`
	Tenant       string `json:"tenant"`
	WrappedError error  `json:"-"`
}

func (e useridError) Error() string {
	switch e.Reason {
	case reasonServerError:
		return fmt.Sprintf("internal error: %s", e.WrappedError)
	case reasonNoOauthRuleMatch:
		return fmt.Sprintf("identity '%s' (%s) not affiliated with a "+
			"recognized customer; or %s is a login provider for this org.",
			e.Email, e.Provider, e.Provider)
	case reasonNoRoles:
		return fmt.Sprintf("no roles for %s (%s)", e.Email, e.Provider)
	}
	panic(fmt.Sprintf("invalid useridError reason %d", e.Reason))
}

func (e useridError) Unwrap() error {
	return e.WrappedError
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

	c.Logger().Warnf("findOrganization: No rules matched provider=%q tenant=%q domain=%q email=%q",
		user.Provider, tenant, domainPart, user.Email)
	return uuid.Nil, useridError{reasonNoOauthRuleMatch, user.Email, user.Provider, tenant, nil}
}

func getAzurePhone(c echo.Context, user goth.User) (string, error) {
	var phoneNumber string
	c.Logger().Debugf("Trying to get phone number for %s, RawData %#v", user.UserID, user.RawData)
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

func getGooglePersonPhone(c echo.Context, user goth.User) (string, error) {
	c.Logger().Debugf("Trying to get a googlePerson and PhoneNumber for %s", user.UserID)
	tokenSource := oauth2.StaticTokenSource(&oauth2.Token{
		AccessToken: user.AccessToken,
	})
	svc, err := people.NewService(c.Request().Context(), option.WithTokenSource(tokenSource))
	if err != nil {
		return "", errors.Wrap(err, "Failed to make people client")
	}
	peopleService := people.NewPeopleService(svc)
	googlePerson, err := peopleService.Get("people/me").PersonFields("phoneNumbers").Do()
	if err != nil {
		return "", errors.Wrap(err, "Failed to get googlePerson")
	}
	c.Logger().Infof("Fetched googlePerson: %#v", googlePerson)
	c.Logger().Debugf("PhoneNumbers: %#v", googlePerson.PhoneNumbers)
	if len(googlePerson.PhoneNumbers) == 0 {
		c.Logger().Warnf("No phone number in response %#v; account may not have a phone number", googlePerson)
		return "", nil
	}
	// If none are marked "primary", this will cause us to take the first
	bestIndex := 0
	for i, num := range googlePerson.PhoneNumbers {
		c.Logger().Infof("phone #%d, %#v", i, num)
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

func getGooglePersonAvatarURL(c echo.Context, user goth.User) (string, error) {
	c.Logger().Debugf("Trying to get a googlePerson and Avatar for %s", user.UserID)
	tokenSource := oauth2.StaticTokenSource(&oauth2.Token{
		AccessToken: user.AccessToken,
	})
	svc, err := people.NewService(c.Request().Context(), option.WithTokenSource(tokenSource))
	if err != nil {
		return "", errors.Wrap(err, "Failed to make people client")
	}
	peopleService := people.NewPeopleService(svc)
	googlePerson, err := peopleService.Get("people/me").PersonFields("photos").Do()
	if err != nil {
		return "", errors.Wrap(err, "Failed to get googlePerson")
	}
	c.Logger().Infof("Fetched googlePerson: %#v", googlePerson)
	c.Logger().Debugf("Photos: %#v", googlePerson.Photos)
	if len(googlePerson.Photos) == 0 {
		c.Logger().Warnf("No photo in response %#v", googlePerson)
		return "", nil
	}
	// If none are marked "primary", this will cause us to take the first
	bestIndex := 0
	for i, photo := range googlePerson.Photos {
		if photo.Metadata.Primary {
			bestIndex = i
			break
		}
	}
	bestPhoto := googlePerson.Photos[bestIndex]

	// See if it's the google default image
	if bestPhoto.Default {
		return "", nil
	} else if bestPhoto.Url != "" {
		return bestPhoto.Url, nil
	}
	return "", fmt.Errorf("Couldn't understand photo record %#v", bestPhoto)
}

func (a *authHandler) getPhone(c echo.Context, user goth.User) (string, error) {
	var phoneNumber string
	var err error
	if user.Provider == "google" {
		phoneNumber, err = getGooglePersonPhone(c, user)
		if err != nil {
			c.Logger().Warnf("Couldn't get google user phone: %s", err)
		}
	} else if user.Provider == "azureadv2" {
		phoneNumber, err = getAzurePhone(c, user)
		if err != nil {
			c.Logger().Warnf("Couldn't get azure user phone: %s", err)
		}
	}
	return phoneNumber, err
}

func (a *authHandler) storeAvatar(c echo.Context, avatar *avatar, account *appliancedb.Account) error {
	var err error
	c.Logger().Infof("storing Avatar for %s", account.UUID)
	ctx := c.Request().Context()
	object := fmt.Sprintf("%s/%s", account.OrganizationUUID, account.UUID)
	obj := a.avatarBucket.Object(object)
	wc := obj.NewWriter(ctx)
	// Set initial attributes
	wc.ObjectAttrs.ContentType = avatar.ContentType
	if wc.ObjectAttrs.Metadata == nil {
		wc.ObjectAttrs.Metadata = make(map[string]string)
	}
	wc.ObjectAttrs.Metadata["sha256"] = hex.EncodeToString(avatar.Hash[:])
	if _, err := wc.Write(avatar.Data); err != nil {
		return err
	}
	if err = wc.Close(); err != nil {
		return err
	}
	c.Logger().Infof("stored Avatar for %s", account.UUID)
	account.AvatarHash = avatar.Hash[:]
	err = a.db.UpdateAccount(ctx, account)
	if err != nil {
		return err
	}
	return nil
}

// avatar encloses the data we need to track for a profile photo.
type avatar struct {
	Data        []byte
	ContentType string
	Hash        [sha256.Size]byte
}

func (a *authHandler) getCloudProviderAvatar(c echo.Context, user goth.User) (*avatar, error) {
	var err error
	var resp *http.Response
	var req *http.Request

	if user.AvatarURL == "" {
		c.Logger().Debugf("No avatar for %s|%s", user.Provider, user.UserID)
		return nil, nil
	}
	c.Logger().Debugf("Trying to get avatar for %s|%s", user.Provider, user.UserID)

	if user.Provider == "google" {
		url, err := getGooglePersonAvatarURL(c, user)
		if err != nil {
			return nil, errors.Wrap(err, "Failed to get photo url")
		}
		if url == "" {
			return nil, nil
		}
		req, err = http.NewRequest("GET", user.AvatarURL, nil)
		if err != nil {
			return nil, err
		}
	} else if user.Provider == "azureadv2" {
		bearer := fmt.Sprintf("Bearer %s", user.AccessToken)
		req, err = http.NewRequest("GET", "https://graph.microsoft.com/v1.0/me/photo/$value", nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", bearer)
	}
	client := http.Client{
		Timeout: time.Second * 10,
	}
	resp, err = client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Avatar GET %s failed with %s: %s", user.AvatarURL, resp.Status, data)
	}
	av := &avatar{
		Data:        data,
		Hash:        sha256.Sum256(data),
		ContentType: resp.Header.Get("Content-Type"),
	}
	if av.ContentType == "" {
		return nil, errors.New("Can't get Content-Type of avatar")
	}
	return av, nil
}

// Note: May modify appliancedb.LoginInfo
func (a *authHandler) updateAccountPhone(c echo.Context, user goth.User, li *appliancedb.LoginInfo) error {
	ctx := c.Request().Context()

	phoneNumber, err := a.getPhone(c, user)
	if err != nil {
		return errors.Wrapf(err, "Failed to getPhone for %s|%s", user.Provider, user.UserID)
	}
	if phoneNumber == li.Account.PhoneNumber {
		return nil
	}
	li.Account.PhoneNumber = phoneNumber
	err = a.db.UpdateAccount(ctx, &li.Account)
	if err != nil {
		return errors.Wrapf(err, "Failed to update account for %s|%s", user.Provider, user.UserID)
	}
	return nil
}

// Note: May modify appliancedb.LoginInfo
func (a *authHandler) updateAccountAvatar(c echo.Context, user goth.User, li *appliancedb.LoginInfo) error {
	newAvatar, err := a.getCloudProviderAvatar(c, user)
	if err != nil {
		return errors.Wrapf(err, "Failed to getAvatarfor %s|%s", user.Provider, user.UserID)
	}
	if newAvatar == nil {
		c.Logger().Debugf("no avatar for %s|%s", user.Provider, user.UserID)
		return nil
	}

	c.Logger().Debugf("avatar hash compare: %x == %x", li.Account.AvatarHash, newAvatar.Hash)
	if bytes.Equal(newAvatar.Hash[:], li.Account.AvatarHash) {
		return nil
	}

	err = a.storeAvatar(c, newAvatar, &li.Account)
	if err != nil {
		return err
	}
	return nil
}

func (a *authHandler) mkNewAccount(c echo.Context, user goth.User) (*appliancedb.LoginInfo, error) {
	// See if we can find an organization for this user.
	var err error
	ctx := c.Request().Context()

	orgUUID, err := a.findOrganization(ctx, c, user)
	if err != nil {
		c.Logger().Warnf("findOrganization failed: %s", err)
		return nil, err
	}
	organization, err := a.db.OrganizationByUUID(ctx, orgUUID)
	if err != nil {
		return nil, err
	}

	c.Logger().Infof("Creating new account for '%s' from '%s' <%s> (%s|%s)",
		user.Name, organization.Name, user.Email, user.Provider,
		user.UserID)

	person := &appliancedb.Person{
		UUID:         uuid.NewV4(),
		Name:         user.Name,
		PrimaryEmail: user.Email,
	}

	account := &appliancedb.Account{
		UUID:             uuid.NewV4(),
		Email:            user.Email,
		PhoneNumber:      "",       // loaded in post-login phase
		AvatarHash:       []byte{}, // loaded in post-login phase
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

func (a *authHandler) getLoginInfo(c echo.Context, user goth.User) (*appliancedb.LoginInfo, error) {
	// See if we can find an organization for this user.
	var err error
	ctx := c.Request().Context()

	c.Logger().Debugf("getUser for %v|%v", user.Provider, user.UserID)

	// Try to handle things if user.Email is empty
	if user.Provider == "azureadv2" && user.Email == "" {
		upn, ok := user.RawData["userPrincipalName"].(string)
		if ok && upn != "" {
			if addr, err := mail.ParseAddress(upn); err == nil {
				c.Logger().Debugf("using userPrincipalName %s for %v|%v",
					addr.Address, user.Provider, user.UserID)
				user.Email = addr.Address
			}
		}
	}

	loginInfo, err := a.db.LoginInfoByProviderAndSubject(
		ctx, user.Provider, user.UserID)
	if _, ok := err.(appliancedb.NotFoundError); ok {
		loginInfo, err = a.mkNewAccount(c, user)
	}
	if err != nil {
		return loginInfo, err
	}

	// Perform post-login checks on the account; as needed, go back to the
	// oauth provider for up-to-date info about the user.
	// XXX We may want to consider moving these to a goroutine to allow
	// the login processing to complete.
	if werr := a.updateAccountPhone(c, user, loginInfo); werr != nil {
		c.Logger().Warnf("error updating phone %v|%v: %s", user.Provider, user.UserID, werr)
	}
	if werr := a.updateAccountAvatar(c, user, loginInfo); werr != nil {
		c.Logger().Warnf("error updating avatar %v|%v: %s", user.Provider, user.UserID, werr)
	}

	return loginInfo, nil
}

func (a *authHandler) setUserIDError(c echo.Context, err error) {
	lerr, ok := err.(useridError)
	if !ok {
		lerr = useridError{
			Reason:       reasonServerError,
			WrappedError: err,
		}
	}
	session, err := a.sessionStore.Get(c.Request(), "bg_login")
	if err != nil && session == nil {
		c.Logger().Warnf("Couldn't get bg_login: %s", err)
		return
	}

	c.Logger().Debugf("adding flash %v", lerr)
	session.AddFlash(lerr, "useridError")
	if err = session.Save(c.Request(), c.Response()); err != nil {
		c.Logger().Warnf("Couldn't save bg_login: %s", err)
		return
	}
}

// getProviderCallback implements /auth/:provider/callback, which completes
// the oauth flow.
func (a *authHandler) getProviderCallback(c echo.Context) error {
	// Put the provider name into the go context so gothic can find it
	// See comment in getProvider().
	providerToGoContext(c)
	ctx := c.Request().Context()

	user, err := gothic.CompleteUserAuth(c.Response(), c.Request())
	if err != nil {
		c.Logger().Errorf("CompleteUserAuth failed: %#v", err)
		a.setUserIDError(c, err)
		return c.Redirect(http.StatusTemporaryRedirect, "/client-web/")
	}
	c.Logger().Infof("user is %#v", user)

	loginInfo, err := a.getLoginInfo(c, user)
	if err != nil {
		c.Logger().Errorf("Error: %s", err)
		a.setUserIDError(c, err)
		return c.Redirect(http.StatusTemporaryRedirect, "/client-web/")
	}
	c.Logger().Infof("loginInfo is %#v", loginInfo)

	if len(loginInfo.PrimaryOrgRoles) == 0 {
		c.Logger().Warn("login is disabled for this account; the account has no roles")
		a.setUserIDError(c, useridError{
			Email:    user.Email,
			Provider: user.Provider,
			Reason:   reasonNoRoles,
		})
		return c.Redirect(http.StatusTemporaryRedirect, "/client-web/")
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
	session.Values["email"] = loginInfo.Account.Email
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

	flashes := session.Flashes("useridError")
	if len(flashes) != 0 {
		// Need to save to delete flashes
		_ = session.Save(c.Request(), c.Response())
		lerr, ok := flashes[0].(useridError)
		// if !ok, this will likely fall through to
		// one of the error handlers below
		if ok {
			c.Logger().Debugf("returning login error %v", lerr)
			return c.JSON(http.StatusUnauthorized, lerr)
		}
	}

	au, ok := session.Values["account_uuid"].(string)
	if !ok {
		return c.JSON(http.StatusUnauthorized, useridError{
			Reason: reasonNoSession,
		})
	}
	accountUUID, err := uuid.FromString(au)
	if err != nil || accountUUID == uuid.Nil {
		return c.JSON(http.StatusUnauthorized, useridError{
			Reason: reasonServerError,
		})
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
func newAuthHandler(r *echo.Echo, sessionStore sessions.Store, applianceDB appliancedb.DataStore, avatarBucket *storage.BucketHandle) *authHandler {

	h := &authHandler{
		sessionStore: sessionStore,
		providers:    make([]goth.Provider, 0),
		db:           applianceDB,
		avatarBucket: avatarBucket,
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

func init() {
	gob.Register(useridError{})
}
