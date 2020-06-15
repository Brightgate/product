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
	"archive/zip"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math"
	"math/big"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"bg/cl_common/registry"
	"bg/cloud_models/appliancedb"
	"bg/common/cfgapi"
	"bg/common/passwordgen"
	"bg/common/vpn"

	"cloud.google.com/go/storage"
	"github.com/gorilla/sessions"
	"github.com/labstack/echo"
	"github.com/lib/pq"
	"github.com/pkg/errors"
	"github.com/satori/uuid"
)

type accountHandler struct {
	db              appliancedb.DataStore
	sessionStore    sessions.Store
	avatarBucket    *storage.BucketHandle
	getConfigHandle registry.GetConfigHandleFunc
}

type accountSelfProvisionRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Verifier string `json:"verifier"`
}

type accountSelfProvisionResponse struct {
	Status    string    `json:"status"`
	Completed time.Time `json:"completed,omitempty"`
	Username  string    `json:"username"`
}

type accountRoles struct {
	TargetOrganization uuid.UUID `json:"targetOrganization"`
	Relationship       string    `json:"relationship"`
	Roles              []string  `json:"roles"`
	LimitRoles         []string  `json:"limitRoles"`
}

type accountRolesResponse []accountRoles

var pwRegime = passwordgen.HumanPasswordSpec.String()

// adminOrSelf is an access check which looks to see if the user is
// either an admin (in which case all accounts are permitted), or is asking a
// question about their own account.
func (a *accountHandler) adminOrSelf(c echo.Context) error {
	sessionAccountUUID, ok := c.Get("account_uuid").(uuid.UUID)
	if !ok || sessionAccountUUID == uuid.Nil {
		return newHTTPError(http.StatusUnauthorized)
	}
	accountUUID, err := uuid.FromString(c.Param("acct_uuid"))
	if err != nil {
		return newHTTPError(http.StatusBadRequest)
	}
	roles := c.Get("matched_roles").(matchedRoles)
	// Only admins may look at other account's information
	if !roles["admin"] && accountUUID != sessionAccountUUID {
		return newHTTPError(http.StatusUnauthorized)
	}
	return nil
}

// getAccountAvatar returns the user's profile picture, or avatar.
// This is unlike some of our other endpoints in that it manages
// Cache-Control so that browsers will recheck if the avatar has
// changed roughly once per hour.
func (a *accountHandler) getAccountAvatar(c echo.Context) error {
	ctx := c.Request().Context()

	targetUUIDParam := c.Param("acct_uuid")
	targetUUID, err := uuid.FromString(targetUUIDParam)
	if err != nil {
		return newHTTPError(http.StatusBadRequest)
	}

	account, err := a.db.AccountByUUID(ctx, targetUUID)
	if err != nil {
		return newHTTPError(http.StatusNotFound, err)
	}
	c.Logger().Debugf("account is %v", account)
	aHash := hex.EncodeToString(account.AvatarHash)
	if len(account.AvatarHash) == 0 {
		return newHTTPError(http.StatusNotFound)
	}
	c.Response().Header().Set("Cache-Control", "max-age=3600")
	c.Response().Header().Set("ETag", aHash)

	for _, ifNoneMatchVal := range c.Request().Header["If-None-Match"] {
		if ifNoneMatchVal == aHash {
			return newHTTPError(http.StatusNotModified)
		}
	}

	object := fmt.Sprintf("%s/%s", account.OrganizationUUID, account.UUID)
	obj := a.avatarBucket.Object(object)
	oa, err := obj.Attrs(ctx)
	if err != nil {
		if err == storage.ErrObjectNotExist {
			return newHTTPError(http.StatusNotFound)
		}
		return newHTTPError(http.StatusInternalServerError, err)
	}
	reader, err := obj.NewReader(ctx)
	if err != nil {
		if err == storage.ErrObjectNotExist {
			return newHTTPError(http.StatusNotFound)
		}
		return newHTTPError(http.StatusInternalServerError, err)
	}
	defer reader.Close()

	return c.Stream(http.StatusOK, oa.ContentType, reader)
}

// getAccountPasswordGen generates a password for the user, and sends it back
// to the user for inspection and, if desired, acceptance.  We desire to have
// the password in plaintext as little as possible, so we store (in the
// session) the crypted values, and send the user a "verifier" (sort of a
// nonce) code which it can send back to say "yes, ok, that one is fine."  The
// user agent may well send us the cleartext username and password as well, in
// order to get password managers to notice.  However we ignore those inputs.
//
// This endpoint mates up with postAccountSelfProvision().
func (a *accountHandler) getAccountPasswordGen(c echo.Context) error {
	ctx := c.Request().Context()
	sessionAccountUUID, ok := c.Get("account_uuid").(uuid.UUID)
	if !ok || sessionAccountUUID == uuid.Nil {
		return newHTTPError(http.StatusUnauthorized)
	}

	account, err := a.db.AccountByUUID(ctx, sessionAccountUUID)
	if err != nil {
		return newHTTPError(http.StatusInternalServerError, err)
	}

	pw, err := passwordgen.HumanPassword(passwordgen.HumanPasswordSpec)
	if err != nil {
		return newHTTPError(http.StatusInternalServerError, err)
	}
	// In the session, we store the bcrypt and mschapv2 versions of the
	// most recently generated password.  When we get the provisioning
	// request, we can see if the values match up.
	pwHash, err := cfgapi.HashUserPassword(pw)
	if err != nil {
		return newHTTPError(http.StatusInternalServerError, err)
	}
	mschapHash, err := cfgapi.HashMSCHAPv2Password(pw)
	if err != nil {
		return newHTTPError(http.StatusInternalServerError, err)
	}

	// The verifier is used to validate that the value saved in the
	// session and the value "confirmed" by the user are the same
	// password value.  The worst case is that the wrong password
	// is provisioned.
	verifier, err := rand.Int(rand.Reader, big.NewInt(math.MaxUint32))
	if err != nil {
		return newHTTPError(http.StatusInternalServerError, err)
	}

	session, err := a.sessionStore.Get(c.Request(), "bg_login")
	if err != nil {
		return newHTTPError(http.StatusUnauthorized)
	}
	session.Values["account-pw-user"] = pwHash
	session.Values["account-pw-mschapv2"] = mschapHash
	session.Values["account-pw-verifier"] = verifier.String()
	err = session.Save(c.Request(), c.Response())
	if err != nil {
		return newHTTPError(http.StatusInternalServerError, err)
	}
	c.Logger().Infof("saved crypted pw %s,%s", pwHash, mschapHash)
	return c.JSON(http.StatusOK, &accountSelfProvisionRequest{
		Username: account.Email,
		Password: pw,
		Verifier: verifier.String(),
	})
}

// postAccountSelfProvision takes the verifier code discussed
// above as input, as well as information from the session.
//
// After a bunch of pre-flight checks, it creates and saves
// the user to all of the customer sites.
func (a *accountHandler) postAccountSelfProvision(c echo.Context) error {
	ctx := c.Request().Context()
	sessionAccountUUID, ok := c.Get("account_uuid").(uuid.UUID)
	if !ok || sessionAccountUUID == uuid.Nil {
		return newHTTPError(http.StatusUnauthorized)
	}
	targetUUIDParam := c.Param("acct_uuid")
	targetUUID, err := uuid.FromString(targetUUIDParam)
	if err != nil {
		return newHTTPError(http.StatusBadRequest)
	}
	// Only ever allowed for yourself
	if sessionAccountUUID != targetUUID {
		return newHTTPError(http.StatusUnauthorized)
	}

	session, err := a.sessionStore.Get(c.Request(), "bg_login")
	if err != nil {
		return newHTTPError(http.StatusUnauthorized)
	}
	userpwSessionVal, ok := session.Values["account-pw-user"].(string)
	if !ok {
		c.Logger().Warnf("Didn't find account-pw-user in session")
		return newHTTPError(http.StatusBadRequest, err)
	}
	mschapSessionVal, ok := session.Values["account-pw-mschapv2"].(string)
	if !ok {
		c.Logger().Warnf("Didn't find account-pw-mschapv2 in session")
		return newHTTPError(http.StatusBadRequest, err)
	}
	verifierSessionVal, ok := session.Values["account-pw-verifier"].(string)
	if !ok {
		c.Logger().Warnf("Didn't find account-pw-verifier in session")
		return newHTTPError(http.StatusBadRequest, err)
	}

	var provReq accountSelfProvisionRequest
	if err := c.Bind(&provReq); err != nil {
		return newHTTPError(http.StatusBadRequest, err)
	}
	if provReq.Verifier == "" || provReq.Verifier != verifierSessionVal {
		c.Logger().Warnf("provInfo.Verifier %s != verifierSessionVal %s", provReq.Verifier, verifierSessionVal)
		return newHTTPError(http.StatusBadRequest, "stale request, verifier did not match")
	}

	// We've now got confirmation that the user wants to provision this
	// password; so stash it in the database.
	now := time.Now()
	accountSecrets := &appliancedb.AccountSecrets{
		AccountUUID:                 sessionAccountUUID,
		ApplianceUserBcrypt:         userpwSessionVal,
		ApplianceUserBcryptRegime:   pwRegime,
		ApplianceUserBcryptTs:       now,
		ApplianceUserMSCHAPv2:       mschapSessionVal,
		ApplianceUserMSCHAPv2Regime: pwRegime,
		ApplianceUserMSCHAPv2Ts:     now,
	}
	err = a.db.UpsertAccountSecrets(ctx, accountSecrets)
	if err != nil {
		return newHTTPError(http.StatusInternalServerError, err)
	}

	// Note that we pass 'false' so that this doesn't wait around.
	err = registry.SyncAccountSelfProv(ctx, a.db, a.getConfigHandle, sessionAccountUUID, nil, false)
	if err != nil {
		c.Logger().Errorf("registry.SyncAccountSelfProv failed: %v", err)
		return newHTTPError(http.StatusInternalServerError, err)
	}

	return c.Redirect(http.StatusFound, "/client-web")
}

// postAccountDeprovision removes self-provisioning for an account.
func (a *accountHandler) postAccountDeprovision(c echo.Context) error {
	var err error
	ctx := c.Request().Context()

	accountUUID, err := uuid.FromString(c.Param("acct_uuid"))
	if err != nil {
		return newHTTPError(http.StatusBadRequest)
	}

	err = registry.AccountDeprovision(ctx, a.db, a.getConfigHandle, accountUUID)
	if err != nil {
		c.Logger().Errorf("registry.AccountDeprovision failed: %v", err)
		return newHTTPError(http.StatusInternalServerError, err)
	}
	return nil
}

type matchedRoles map[string]bool

// getAccountSelfProvision returns self provisioning information for an
// account.
func (a *accountHandler) getAccountSelfProvision(c echo.Context) error {
	var err error
	ctx := c.Request().Context()
	err = a.adminOrSelf(c)
	if err != nil {
		return err
	}
	sessionAccountUUID, ok := c.Get("account_uuid").(uuid.UUID)
	if !ok || sessionAccountUUID == uuid.Nil {
		return newHTTPError(http.StatusUnauthorized, errors.Wrap(err, "account_uuid"))
	}

	accountUUID, err := uuid.FromString(c.Param("acct_uuid"))
	if err != nil {
		return newHTTPError(http.StatusBadRequest)
	}
	account, err := a.db.AccountByUUID(ctx, accountUUID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, err)
	}

	roles := c.Get("matched_roles").(matchedRoles)
	// Only admins may look at other account's information
	if !roles["admin"] && accountUUID != sessionAccountUUID {
		return newHTTPError(http.StatusUnauthorized, errors.Errorf("insufficient access; check roles"))
	}

	resp := &accountSelfProvisionResponse{
		Status: "unprovisioned",
	}
	secret, err := a.db.AccountSecretsByUUID(ctx, accountUUID)
	if err != nil {
		return c.JSON(http.StatusOK, resp)
	}
	if secret.ApplianceUserMSCHAPv2 == "" {
		return c.JSON(http.StatusOK, resp)
	}
	resp.Status = "provisioned"
	resp.Username = account.Email
	resp.Completed = secret.ApplianceUserMSCHAPv2Ts
	return c.JSON(http.StatusOK, resp)
}

// deleteAccount deletes the specified account
func (a *accountHandler) deleteAccount(c echo.Context) error {
	var err error
	ctx := c.Request().Context()
	accountUUID, err := uuid.FromString(c.Param("acct_uuid"))
	if err != nil {
		return newHTTPError(http.StatusBadRequest, err)
	}
	err = registry.DeleteAccountInformation(ctx, a.db, a.getConfigHandle, accountUUID)
	if err != nil {
		return newHTTPError(http.StatusInternalServerError, err)
	}
	return c.NoContent(http.StatusOK)
}

// getAccountRoles fetches roles for the specified account
func (a *accountHandler) getAccountRoles(c echo.Context) error {
	var err error
	ctx := c.Request().Context()
	err = a.adminOrSelf(c)
	if err != nil {
		return err
	}
	accountUUID, err := uuid.FromString(c.Param("acct_uuid"))
	if err != nil {
		return newHTTPError(http.StatusBadRequest)
	}
	aoRoles, err := a.db.AccountOrgRolesByAccount(ctx, accountUUID)
	if err != nil {
		c.Logger().Errorf("failed getting AccountOrgRolesByAccount: %v", err)
		return newHTTPError(http.StatusInternalServerError)
	}
	resp := make(accountRolesResponse, len(aoRoles))
	for i, aor := range aoRoles {
		resp[i] = accountRoles{
			TargetOrganization: aor.TargetOrganizationUUID,
			Relationship:       aor.Relationship,
			LimitRoles:         aor.LimitRoles,
			Roles:              aor.Roles,
		}
	}
	return c.JSON(http.StatusOK, resp)
}

// postAccountRoles modifies roles for an account
func (a *accountHandler) postAccountRoles(c echo.Context) error {
	var err error
	ctx := c.Request().Context()

	tgtAcctUUID, err := uuid.FromString(c.Param("acct_uuid"))
	if err != nil {
		return newHTTPError(http.StatusBadRequest)
	}
	tgtAcct, err := a.db.AccountByUUID(ctx, tgtAcctUUID)
	if err != nil {
		if _, ok := err.(appliancedb.NotFoundError); ok {
			return newHTTPError(http.StatusNotFound)
		}
		return newHTTPError(http.StatusInternalServerError)
	}
	tgtRole := c.Param("tgt_role")
	if !appliancedb.ValidRole(tgtRole) {
		return newHTTPError(http.StatusNotFound)
	}
	tgtOrgUUID, err := uuid.FromString(c.Param("tgt_org_uuid"))
	if err != nil {
		return newHTTPError(http.StatusNotFound)
	}

	type roleValue struct {
		Value bool `json:"value"`
	}
	var rv roleValue
	if err := c.Bind(&rv); err != nil {
		return newHTTPError(http.StatusBadRequest, err)
	}
	relationship := "self"
	// Until we have more than one kind of relationship, we can infer this
	if tgtAcct.OrganizationUUID != tgtOrgUUID {
		relationship = "msp"
	}
	aor := appliancedb.AccountOrgRole{
		AccountUUID:            tgtAcct.UUID,
		OrganizationUUID:       tgtAcct.OrganizationUUID,
		TargetOrganizationUUID: tgtOrgUUID,
		Role:                   tgtRole,
		Relationship:           relationship,
	}
	var cmd string
	if rv.Value {
		err = a.db.InsertAccountOrgRole(ctx, &aor)
		cmd = "insert"
	} else {
		err = a.db.DeleteAccountOrgRole(ctx, &aor)
		cmd = "delete"
	}
	if err != nil {
		pqe, ok := err.(*pq.Error)
		// Add details from PQE, as they can help us understand
		// what's going on here.
		if ok && pqe.Code.Name() == "foreign_key_violation" {
			c.Logger().Errorf("Couldn't %s role %v; the role or org/org relationship may not exist.\nPQ Message: %s\nPQ Detail: %s",
				cmd, aor, pqe.Message, pqe.Detail)
			return newHTTPError(http.StatusBadRequest, err)
		}
		return newHTTPError(http.StatusInternalServerError, err)
	}

	return nil
}

// mkAccountMiddleware manufactures a middleware which protects a route; only
// accounts with one or more of the allowedRoles can pass through the checks; the
// middleware adds "matched_roles" to the echo context, indicating which of the
// allowed_roles the account actually has.
func (a *accountHandler) mkAccountMiddleware(allowedRoles []string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			ctx := c.Request().Context()
			sessionAccountUUID, ok := c.Get("account_uuid").(uuid.UUID)
			if !ok || sessionAccountUUID == uuid.Nil {
				return newHTTPError(http.StatusUnauthorized)
			}

			targetUUIDParam := c.Param("acct_uuid")
			targetUUID, err := uuid.FromString(targetUUIDParam)
			if err != nil {
				return newHTTPError(http.StatusBadRequest)
			}

			tgtAcct, err := a.db.AccountByUUID(ctx, targetUUID)
			if err != nil {
				if _, ok := err.(appliancedb.NotFoundError); ok {
					return newHTTPError(http.StatusNotFound)
				}
				c.Logger().Errorf("failed to get account: %v", err)
				return newHTTPError(http.StatusInternalServerError)
			}
			// See what the session's account is allowed to do to the target's
			// org.
			aoRoles, err := a.db.AccountOrgRolesByAccountTarget(ctx,
				sessionAccountUUID, tgtAcct.OrganizationUUID)
			if err != nil {
				c.Logger().Errorf("failed to get account roles: %v", err)
				return newHTTPError(http.StatusInternalServerError, err)
			}
			matches := make(matchedRoles)
			for _, aor := range aoRoles {
				for _, r := range aor.Roles {
					for _, rr := range allowedRoles {
						if r == rr {
							matches[r] = true
						}
					}
				}
			}
			if len(matches) > 0 {
				c.Set("matched_roles", matches)
				return next(c)
			}
			c.Logger().Debugf("Unauthorized: %s acct=%v, acc=%v, ur=%v, ar=%v",
				c.Path(), sessionAccountUUID, targetUUID, aoRoles, allowedRoles)
			return newHTTPError(http.StatusUnauthorized)
		}
	}
}

type accountWGResponseConfig struct {
	// OrganizationUUID is not strictly needed; it accomodates a future in
	// which we might allow MSPs to get VPN access to their clients sites.
	// It simply describes the organization which owns the SiteUUID.
	OrganizationUUID uuid.UUID `json:"organizationUUID"`
	SiteUUID         uuid.UUID `json:"siteUUID"`
	Mac              string    `json:"mac"`
	Label            string    `json:"label"`
	PublicKey        string    `json:"publicKey"`
	AssignedIP       string    `json:"assignedIP"`
}

type accountWGResponse struct {
	EnabledSites []uuid.UUID               `json:"enabledSites"`
	Configs      []accountWGResponseConfig `json:"configs"`
}

// getAccountWG retrieves Wireguard VPN configurations for this account only
// non-sensitive materials are returned.
func (a *accountHandler) getAccountWG(c echo.Context) error {
	ctx := c.Request().Context()

	tgtAcctUUID, err := uuid.FromString(c.Param("acct_uuid"))
	if err != nil {
		return newHTTPError(http.StatusBadRequest, err)
	}

	account, err := a.db.AccountByUUID(ctx, tgtAcctUUID)
	if err != nil {
		return newHTTPError(http.StatusNotFound, err)
	}

	sites, err := a.db.CustomerSitesByAccount(c.Request().Context(), tgtAcctUUID)
	if err != nil {
		return newHTTPError(http.StatusNotFound, err)
	}

	resp := accountWGResponse{
		EnabledSites: make([]uuid.UUID, 0),
		Configs:      make([]accountWGResponseConfig, 0),
	}
	// Look through all of the sites, looking for VPN configs for this user.
	for _, site := range sites {
		// The DB query above will return all sites the user has rights
		// too, including other orgs.  We're not ready for that yet, so
		// filter those other orgs out.
		if site.OrganizationUUID != account.OrganizationUUID {
			continue
		}
		hdl, err := a.getConfigHandle(site.UUID.String())
		if err != nil && errors.Cause(err) == cfgapi.ErrNoConfig {
			c.Logger().Debugf("no config for site %s; continue", site.UUID)
			continue
		} else if err != nil {
			return newHTTPError(http.StatusInternalServerError, err)
		}

		v, err := vpn.NewVpn(hdl)
		if err == nil {
			if v.IsEnabled() {
				resp.EnabledSites = append(resp.EnabledSites, site.UUID)
			}
		}

		userInfo, err := hdl.GetUserByUUID(account.UUID)
		if err != nil {
			if errors.Cause(err) == cfgapi.ErrNoConfig {
				c.Logger().Debugf("didn't find user by UUID; no config for site; continue")
				continue
			}
			if _, nse := errors.Cause(err).(cfgapi.NoSuchUserError); nse {
				c.Logger().Debugf("didn't find user by UUID, continue")
				continue
			}
			return newHTTPError(http.StatusInternalServerError, err)
		}

		for _, wgConfig := range userInfo.WGConfig {
			r := accountWGResponseConfig{
				OrganizationUUID: site.OrganizationUUID,
				SiteUUID:         site.UUID,
				Label:            wgConfig.Label,
				Mac:              wgConfig.GetMac(),
				PublicKey:        wgConfig.WGPublicKey,
				AssignedIP:       wgConfig.WGAssignedIP,
			}
			resp.Configs = append(resp.Configs, r)
		}
	}
	return c.JSON(http.StatusOK, resp)
}

func confDataToZip(confFileName, clientTZ string, confData []byte) ([]byte, error) {
	var zipBuf bytes.Buffer

	// A zip's modified time is expressed as localtime wherever it was
	// made; so we ask the client to tell us what timezone it is in, and we
	// use that.  If we can't make sense of it, fall back to UTC.  A few
	// hours off is better than zero time, the MSDOS epoch 1/1/1980.
	loc, err := time.LoadLocation(clientTZ)
	if err != nil {
		loc, _ = time.LoadLocation("UTC")
	}
	ts := time.Now().In(loc)

	zipWriter := zip.NewWriter(&zipBuf)
	fileWriter, err := zipWriter.CreateHeader(&zip.FileHeader{
		Name:     confFileName,
		Modified: ts,
		// It feels like Deflate should be optional, but in
		// our experiments, Android barfed on a zip file
		// without Deflated content (see
		// https://stackoverflow.com/questions/47208)
		Method: zip.Deflate,
	})
	if err != nil {
		return nil, err
	}
	if _, err = fileWriter.Write(confData); err != nil {
		return nil, err
	}
	// This means "Finish writing the zip file" and the docs advise
	// that we must check the error.
	if err = zipWriter.Close(); err != nil {
		return nil, err
	}
	return zipBuf.Bytes(), nil
}

type wgNewConfigResponse struct {
	accountWGResponseConfig

	// Endpoint information
	ServerAddress string `json:"serverAddress"`
	ServerPort    int    `json:"serverPort"`

	// Name of the config inside of the zip; can be used as a
	// hint to the user.
	ConfName string `json:"confName"`
	// plain text representation of config file
	ConfData string `json:"confData"`
	// downloadable (zip file) archive containing config file, base64
	DownloadConfBody []byte `json:"downloadConfBody"`
	// downloadable file name
	DownloadConfName string `json:"downloadConfName"`
	// downloadable content type
	DownloadConfContentType string `json:"downloadConfContentType"`
}

type postAccountWGRequest struct {
	Label string `json:"label"`
	// Local timestamp from client, so that the zip file
	// has a sensible timestamp.
	TZ string `json:"tz,omitempty"`
}

// This RE is chosen from analyzing WireGuard clients on several platforms.  It
// seems to be the most universal subset.  The purpose is to strip invalid
// chars from e.g. a site name in order to give the user a tunnel name that
// makes sense to them.
var confNameReplacer = regexp.MustCompile(`[^a-zA-Z0-9_=+-.]`)

// Linux/Android are limited to just 15 characters in the conf name and will
// barf on longer names.
const wgConfNameMaxLen = 15

// wgConfName converts the input string to a string suitable for naming a
// WireGuard .conf file across known platforms.  Obscure reserved names on
// Windows (e.g. COM1) are not accounted for.
func wgConfName(name string) string {
	if len(name) > wgConfNameMaxLen {
		// Try dropping spaces to see if that helps
		name = strings.ReplaceAll(name, " ", "")
		// If not, then trim.
		if len(name) > wgConfNameMaxLen {
			name = name[:wgConfNameMaxLen]
		}
	}
	name = confNameReplacer.ReplaceAllLiteralString(name, "-")
	// Remove any trailing dashes
	name = strings.TrimRight(name, "-")
	if name == "" {
		// Just in case we trimmed away the whole thing
		name = "vpn"
	}
	return name
}

// postAccountWGNew creates a new Wireguard VPN configuration for the account,
// storing it into the config store.  The WG configuration is returned in
// several forms suitable for presentation or download.
func (a *accountHandler) postAccountWGNew(c echo.Context) error {
	ctx := c.Request().Context()

	tgtAcctUUID, err := uuid.FromString(c.Param("acct_uuid"))
	if err != nil {
		return newHTTPError(http.StatusBadRequest, err)
	}
	tgtSiteUUID, err := uuid.FromString(c.Param("site_uuid"))
	if err != nil {
		return newHTTPError(http.StatusBadRequest, err)
	}
	tgtSite, err := a.db.CustomerSiteByUUID(ctx, tgtSiteUUID)
	if err != nil {
		return newHTTPError(http.StatusBadRequest, err)
	}

	var req postAccountWGRequest
	if err := c.Bind(&req); err != nil {
		return err
	}
	if len(req.Label) > 64 {
		return newHTTPError(http.StatusBadRequest, errors.New("invalid label; too long"))
	}

	hdl, err := a.getConfigHandle(tgtSiteUUID.String())
	if err != nil && errors.Cause(err) == cfgapi.ErrNoConfig {
		// No config for this site; return Not found
		return newHTTPError(http.StatusNotFound, err)
	} else if err != nil {
		return newHTTPError(http.StatusInternalServerError, err)
	}

	userInfo, err := hdl.GetUserByUUID(tgtAcctUUID)
	if err != nil {
		if errors.Cause(err) == cfgapi.ErrNoConfig {
			c.Logger().Infof("didn't find user by UUID; no config for site")
			return newHTTPError(http.StatusNotFound, err)
		} else if _, ok := errors.Cause(err).(cfgapi.NoSuchUserError); ok {
			c.Logger().Warnf("didn't find user %s at site %s by UUID; calling registry.SyncAccountSelfProv: %v",
				tgtAcctUUID, tgtSiteUUID, err)
			err = registry.SyncAccountSelfProv(ctx, a.db,
				a.getConfigHandle, tgtAcctUUID,
				[]appliancedb.CustomerSite{*tgtSite}, true)
			if err != nil {
				return newHTTPError(http.StatusInternalServerError, err)
			}
			// Try again to get userInfo; we do expect this to work
			userInfo, err = hdl.GetUserByUUID(tgtAcctUUID)
			if err != nil {
				return newHTTPError(http.StatusInternalServerError, err)
			}
		} else {
			return newHTTPError(http.StatusInternalServerError, err)
		}
	}

	vpn, err := vpn.NewVpn(hdl)
	if err != nil {
		return newHTTPError(http.StatusInternalServerError, err)
	}

	addRes, err := vpn.AddKey(ctx, userInfo.UID, req.Label, "")
	if err != nil {
		if err == cfgapi.ErrQueued || err == cfgapi.ErrInProgress || err == cfgapi.ErrTimeout {
			return newHTTPError(http.StatusInternalServerError,
				"Site was not responsive to cloud commands")
		}
		return newHTTPError(http.StatusInternalServerError, err)
	}

	// On at least MacOS and Windows (but not on Android or some other
	// platforms) the conf file name informs the name of the tunnel in the
	// UI (although you can change it at any time).  Since the tunnel is a
	// tunnel from where you are (my laptop) TO someplace (Houston Office),
	// we choose to name the confFile after the site; we have to crunch
	// that down to an acceptable name.
	confName := wgConfName(tgtSite.Name)
	confFileName := confName
	// in case the user put ".conf" in their label name, don't double
	// suffix it
	if !strings.HasSuffix(confFileName, ".conf") {
		confFileName += ".conf"
	}
	zipFile, err := confDataToZip(confFileName, req.TZ, addRes.ConfData)
	if err != nil {
		return newHTTPError(http.StatusInternalServerError, err)
	}

	// Nuke out spaces and path separators to make the label name more palatable
	// in the filename.
	filenameLabel := strings.ReplaceAll(req.Label, " ", "-")
	filenameLabel = strings.ReplaceAll(filenameLabel, "/", "-")
	filenameLabel = strings.ReplaceAll(filenameLabel, "\\", "-")
	filenameLabel = strings.ReplaceAll(filenameLabel, ":", "-")

	// So if we started with:
	//   req.Label='My Laptop' tgtSite.Name='Minn/St. Paul Office'
	// We would get:
	//   filenameLabel='My-Laptop' confName='Minn-St.PaulOff'
	// And then merge those into:
	//   zipFileName='My-Laptop-Minn-St.PaulOff-Brightgate-WireGuard.zip'
	// Which is long but also clear.
	zipFileName := fmt.Sprintf("%s-%s-Brightgate-WireGuard.zip", filenameLabel, confName)

	resp := wgNewConfigResponse{
		accountWGResponseConfig: accountWGResponseConfig{
			OrganizationUUID: tgtSite.OrganizationUUID,
			SiteUUID:         tgtSite.UUID,
			PublicKey:        addRes.Publickey,
			AssignedIP:       addRes.AssignedIP,
			Label:            addRes.Label,
			Mac:              addRes.Mac,
		},
		ServerAddress:           addRes.ServerAddress,
		ServerPort:              addRes.ServerPort,
		ConfName:                confName,
		ConfData:                string(addRes.ConfData),
		DownloadConfBody:        zipFile,
		DownloadConfName:        zipFileName,
		DownloadConfContentType: "application/octet-stream",
	}

	return c.JSON(http.StatusCreated, resp)
}

// postAccountWGSiteMacRekey regenerates the private key for a vpn
// config, leaving everything else the same.
func (a *accountHandler) postAccountWGSiteMacRekey(c echo.Context) error {
	return c.NoContent(http.StatusNotImplemented)
}

// deleteAccountWGSiteMac removes a Wireguard VPN configuration
func (a *accountHandler) deleteAccountWGSiteMac(c echo.Context) error {
	ctx := c.Request().Context()

	tgtAcctUUID, err := uuid.FromString(c.Param("acct_uuid"))
	if err != nil {
		return newHTTPError(http.StatusBadRequest, errors.Wrap(err, "acct_uuid"))
	}
	tgtSiteUUID, err := uuid.FromString(c.Param("site_uuid"))
	if err != nil {
		return newHTTPError(http.StatusBadRequest, errors.Wrap(err, "site_uuid"))
	}
	tgtMac, err := url.PathUnescape(c.Param("mac"))
	if err != nil {
		return newHTTPError(http.StatusBadRequest, errors.Wrap(err, "mac"))
	}
	pubKey, err := url.PathUnescape(c.Param("pubkey"))
	if err != nil {
		return newHTTPError(http.StatusBadRequest, errors.Wrap(err, "pubkey"))
	}

	hdl, err := a.getConfigHandle(tgtSiteUUID.String())
	if err != nil && errors.Cause(err) == cfgapi.ErrNoConfig {
		// No config for this site; return Not found
		return newHTTPError(http.StatusNotFound, err)
	} else if err != nil {
		return newHTTPError(http.StatusInternalServerError, err)
	}

	userInfo, err := hdl.GetUserByUUID(tgtAcctUUID)
	if err != nil {
		if errors.Cause(err) == cfgapi.ErrNoConfig {
			c.Logger().Infof("didn't find user by UUID; no config for site; continue")
			return newHTTPError(http.StatusNotFound, err)
		}
		if _, ok := errors.Cause(err).(cfgapi.NoSuchUserError); ok {
			c.Logger().Infof("didn't find user by UUID, continue")
			return newHTTPError(http.StatusNotFound, err)
		}
		return newHTTPError(http.StatusInternalServerError, err)
	}

	vpn, err := vpn.NewVpn(hdl)
	if err != nil {
		return newHTTPError(http.StatusInternalServerError, err)
	}
	err = vpn.RemoveKey(ctx, userInfo.UID, tgtMac, pubKey)
	if err != nil {
		return newHTTPError(http.StatusInternalServerError, err)
	}

	return nil
}

// newAccountAPIHandler creates an accountHandler for the given DataStore and session
// Store, and routes the handler into the echo instance.
func newAccountHandler(r *echo.Echo, db appliancedb.DataStore,
	middlewares []echo.MiddlewareFunc,
	sessionStore sessions.Store,
	avatarBucket *storage.BucketHandle,
	getConfigHandle registry.GetConfigHandleFunc) *accountHandler {

	h := &accountHandler{db, sessionStore, avatarBucket, getConfigHandle}
	acct := r.Group("/api/account")
	acct.Use(middlewares...)

	admin := h.mkAccountMiddleware([]string{"admin"})
	user := h.mkAccountMiddleware([]string{"admin", "user"})

	acct.GET("/passwordgen", h.getAccountPasswordGen)
	acct.DELETE("/:acct_uuid", h.deleteAccount, admin)
	acct.GET("/:acct_uuid/avatar", h.getAccountAvatar, user)
	acct.GET("/:acct_uuid/selfprovision", h.getAccountSelfProvision, user)
	acct.POST("/:acct_uuid/selfprovision", h.postAccountSelfProvision, user)
	acct.POST("/:acct_uuid/deprovision", h.postAccountDeprovision, admin)
	acct.GET("/:acct_uuid/roles", h.getAccountRoles, user)
	acct.POST("/:acct_uuid/roles/:tgt_org_uuid/:tgt_role", h.postAccountRoles, admin)
	acct.GET("/:acct_uuid/wg", h.getAccountWG, user)
	acct.POST("/:acct_uuid/wg/:site_uuid/new", h.postAccountWGNew, user)
	acct.POST("/:acct_uuid/wg/:site_uuid/:mac/rekey", h.postAccountWGSiteMacRekey, user)
	acct.DELETE("/:acct_uuid/wg/:site_uuid/:mac/:pubkey", h.deleteAccountWGSiteMac, user)
	return h
}
