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
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math"
	"math/big"
	"net/http"
	"time"

	"bg/cl_common/registry"
	"bg/cloud_models/appliancedb"
	"bg/common/cfgapi"
	"bg/common/passwordgen"

	"cloud.google.com/go/storage"
	"github.com/gorilla/sessions"
	"github.com/labstack/echo"
	"github.com/lib/pq"
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
		return echo.NewHTTPError(http.StatusUnauthorized)
	}
	accountUUID, err := uuid.FromString(c.Param("acct_uuid"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest)
	}
	roles := c.Get("matched_roles").(matchedRoles)
	// Only admins may look at other account's information
	if !roles["admin"] && accountUUID != sessionAccountUUID {
		return echo.NewHTTPError(http.StatusUnauthorized)
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
		return echo.NewHTTPError(http.StatusBadRequest)
	}

	account, err := a.db.AccountByUUID(ctx, targetUUID)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, err)
	}
	c.Logger().Debugf("account is %v", account)
	aHash := hex.EncodeToString(account.AvatarHash)
	if len(account.AvatarHash) == 0 {
		return echo.NewHTTPError(http.StatusNotFound)
	}
	c.Response().Header().Set("Cache-Control", "max-age=3600")
	c.Response().Header().Set("ETag", aHash)

	for _, ifNoneMatchVal := range c.Request().Header["If-None-Match"] {
		if ifNoneMatchVal == aHash {
			return echo.NewHTTPError(http.StatusNotModified)
		}
	}

	object := fmt.Sprintf("%s/%s", account.OrganizationUUID, account.UUID)
	obj := a.avatarBucket.Object(object)
	oa, err := obj.Attrs(ctx)
	if err != nil {
		if err == storage.ErrObjectNotExist {
			return echo.NewHTTPError(http.StatusNotFound)
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err)
	}
	reader, err := obj.NewReader(ctx)
	if err != nil {
		if err == storage.ErrObjectNotExist {
			return echo.NewHTTPError(http.StatusNotFound)
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err)
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
		return echo.NewHTTPError(http.StatusUnauthorized)
	}

	account, err := a.db.AccountByUUID(ctx, sessionAccountUUID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err)
	}

	pw, err := passwordgen.HumanPassword(passwordgen.HumanPasswordSpec)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err)
	}
	// In the session, we store the bcrypt and mschapv2 versions of the
	// most recently generated password.  When we get the provisioning
	// request, we can see if the values match up.
	pwHash, err := cfgapi.HashUserPassword(pw)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err)
	}
	mschapHash, err := cfgapi.HashMSCHAPv2Password(pw)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err)
	}

	// The verifier is used to validate that the value saved in the
	// session and the value "confirmed" by the user are the same
	// password value.  The worst case is that the wrong password
	// is provisioned.
	verifier, err := rand.Int(rand.Reader, big.NewInt(math.MaxUint32))
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err)
	}

	session, err := a.sessionStore.Get(c.Request(), "bg_login")
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized)
	}
	session.Values["account-pw-user"] = pwHash
	session.Values["account-pw-mschapv2"] = mschapHash
	session.Values["account-pw-verifier"] = verifier.String()
	err = session.Save(c.Request(), c.Response())
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err)
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
//
// XXX ui.Update() waits, which is probably not what we want.
func (a *accountHandler) postAccountSelfProvision(c echo.Context) error {
	ctx := c.Request().Context()
	sessionAccountUUID, ok := c.Get("account_uuid").(uuid.UUID)
	if !ok || sessionAccountUUID == uuid.Nil {
		return echo.NewHTTPError(http.StatusUnauthorized)
	}
	targetUUIDParam := c.Param("acct_uuid")
	targetUUID, err := uuid.FromString(targetUUIDParam)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest)
	}
	// Only ever allowed for yourself
	if sessionAccountUUID != targetUUID {
		return echo.NewHTTPError(http.StatusUnauthorized)
	}

	session, err := a.sessionStore.Get(c.Request(), "bg_login")
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized)
	}
	userpwSessionVal, ok := session.Values["account-pw-user"].(string)
	if !ok {
		c.Logger().Warnf("Didn't find account-pw-user in session")
		return echo.NewHTTPError(http.StatusBadRequest, err)
	}
	mschapSessionVal, ok := session.Values["account-pw-mschapv2"].(string)
	if !ok {
		c.Logger().Warnf("Didn't find account-pw-mschapv2 in session")
		return echo.NewHTTPError(http.StatusBadRequest, err)
	}
	verifierSessionVal, ok := session.Values["account-pw-verifier"].(string)
	if !ok {
		c.Logger().Warnf("Didn't find account-pw-verifier in session")
		return echo.NewHTTPError(http.StatusBadRequest, err)
	}

	var provReq accountSelfProvisionRequest
	if err := c.Bind(&provReq); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err)
	}
	if provReq.Verifier == "" || provReq.Verifier != verifierSessionVal {
		c.Logger().Warnf("provInfo.Verifier %s != verifierSessionVal %s", provReq.Verifier, verifierSessionVal)
		return echo.NewHTTPError(http.StatusBadRequest, "stale request, verifier did not match")
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
		return echo.NewHTTPError(http.StatusInternalServerError, err)
	}

	err = registry.SyncAccountSelfProv(ctx, a.db, a.getConfigHandle, sessionAccountUUID, nil)
	if err != nil {
		c.Logger().Errorf("registry.SyncAccountSelfProv failed: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, err)
	}

	return c.Redirect(http.StatusFound, "/client-web")
}

// postAccountDeprovision removes self-provisioning for an account.
func (a *accountHandler) postAccountDeprovision(c echo.Context) error {
	var err error
	ctx := c.Request().Context()

	accountUUID, err := uuid.FromString(c.Param("acct_uuid"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest)
	}

	err = registry.AccountDeprovision(ctx, a.db, a.getConfigHandle, accountUUID)
	if err != nil {
		c.Logger().Errorf("registry.AccountDeprovision failed: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, err)
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
		return echo.NewHTTPError(http.StatusUnauthorized)
	}

	accountUUID, err := uuid.FromString(c.Param("acct_uuid"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest)
	}
	account, err := a.db.AccountByUUID(ctx, accountUUID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, err)
	}

	roles := c.Get("matched_roles").(matchedRoles)
	// Only admins may look at other account's information
	if !roles["admin"] && accountUUID != sessionAccountUUID {
		return echo.NewHTTPError(http.StatusUnauthorized)
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
		return echo.NewHTTPError(http.StatusBadRequest)
	}
	err = registry.DeleteAccountInformation(ctx, a.db, a.getConfigHandle, accountUUID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError)
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
		return echo.NewHTTPError(http.StatusBadRequest)
	}
	aoRoles, err := a.db.AccountOrgRolesByAccount(ctx, accountUUID)
	if err != nil {
		c.Logger().Errorf("failed getting AccountOrgRolesByAccount: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError)
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
		return echo.NewHTTPError(http.StatusBadRequest)
	}
	tgtAcct, err := a.db.AccountByUUID(ctx, tgtAcctUUID)
	if err != nil {
		if _, ok := err.(appliancedb.NotFoundError); ok {
			return echo.NewHTTPError(http.StatusNotFound)
		}
		return echo.NewHTTPError(http.StatusInternalServerError)
	}
	tgtRole := c.Param("tgt_role")
	if !appliancedb.ValidRole(tgtRole) {
		return echo.NewHTTPError(http.StatusNotFound)
	}
	tgtOrgUUID, err := uuid.FromString(c.Param("tgt_org_uuid"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound)
	}

	type roleValue struct {
		Value bool `json:"value"`
	}
	var rv roleValue
	if err := c.Bind(&rv); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err)
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
			return echo.NewHTTPError(http.StatusBadRequest, err)
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err)
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
				return echo.NewHTTPError(http.StatusUnauthorized)
			}

			targetUUIDParam := c.Param("acct_uuid")
			targetUUID, err := uuid.FromString(targetUUIDParam)
			if err != nil {
				return echo.NewHTTPError(http.StatusBadRequest)
			}

			tgtAcct, err := a.db.AccountByUUID(ctx, targetUUID)
			if err != nil {
				if _, ok := err.(appliancedb.NotFoundError); ok {
					return echo.NewHTTPError(http.StatusNotFound)
				}
				c.Logger().Errorf("failed to get account: %v", err)
				return echo.NewHTTPError(http.StatusInternalServerError)
			}
			// See what the session's account is allowed to do to the target's
			// org.
			aoRoles, err := a.db.AccountOrgRolesByAccountTarget(ctx,
				sessionAccountUUID, tgtAcct.OrganizationUUID)
			if err != nil {
				c.Logger().Errorf("failed to get account roles: %v", err)
				return echo.NewHTTPError(http.StatusInternalServerError, err)
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
			return echo.NewHTTPError(http.StatusUnauthorized)
		}
	}
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
	acct.GET("/:acct_uuid/avatar", h.getAccountAvatar, user)
	acct.GET("/:acct_uuid/selfprovision", h.getAccountSelfProvision, user)
	acct.POST("/:acct_uuid/selfprovision", h.postAccountSelfProvision, user)
	acct.POST("/:acct_uuid/deprovision", h.postAccountDeprovision, admin)
	acct.GET("/:acct_uuid/roles", h.getAccountRoles, user)
	acct.POST("/:acct_uuid/roles/:tgt_org_uuid/:tgt_role", h.postAccountRoles, admin)
	acct.DELETE("/:acct_uuid", h.deleteAccount, admin)
	return h
}
