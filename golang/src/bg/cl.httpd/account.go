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
	"math"
	"math/big"
	"net/http"
	"time"

	"bg/cl_common/registry"
	"bg/cloud_models/appliancedb"
	"bg/common/cfgapi"
	"bg/common/passwordgen"

	"github.com/gorilla/sessions"
	"github.com/labstack/echo"
	"github.com/satori/uuid"
)

type accountHandler struct {
	db              appliancedb.DataStore
	sessionStore    sessions.Store
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

var pwRegime = passwordgen.HumanPasswordSpec.String()

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
				return echo.NewHTTPError(http.StatusInternalServerError)
			}
			// See what the session's account is allowed to do to the target's
			// org.
			roles, err := a.db.AccountOrgRolesByAccountTarget(ctx,
				sessionAccountUUID, tgtAcct.OrganizationUUID)
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError)
			}
			matches := make(matchedRoles)
			var matched bool
			for _, ur := range roles {
				matches[ur.Role] = false
				for _, rr := range allowedRoles {
					if ur.Role == rr {
						matches[ur.Role] = true
						matched = true
					}
				}
			}
			if matched {
				c.Set("matched_roles", matches)
				return next(c)
			}
			c.Logger().Debugf("Unauthorized: %s acct=%v, acc=%v, ur=%v, ar=%v",
				c.Path(), sessionAccountUUID, targetUUID, roles, allowedRoles)
			return echo.NewHTTPError(http.StatusUnauthorized)
		}
	}
}

// newAccountAPIHandler creates an accountHandler for the given DataStore and session
// Store, and routes the handler into the echo instance.
func newAccountHandler(r *echo.Echo, db appliancedb.DataStore, middlewares []echo.MiddlewareFunc, sessionStore sessions.Store, getConfigHandle registry.GetConfigHandleFunc) *accountHandler {
	h := &accountHandler{db, sessionStore, getConfigHandle}
	acct := r.Group("/api/account")
	acct.Use(middlewares...)

	admin := h.mkAccountMiddleware([]string{"admin"})
	user := h.mkAccountMiddleware([]string{"admin", "user"})

	acct.GET("/passwordgen", h.getAccountPasswordGen)
	acct.GET("/:acct_uuid/selfprovision", h.getAccountSelfProvision, user)
	acct.POST("/:acct_uuid/selfprovision", h.postAccountSelfProvision, user)
	acct.POST("/:acct_uuid/deprovision", h.postAccountDeprovision, admin)
	acct.DELETE("/:acct_uuid", h.deleteAccount, admin)
	return h
}
