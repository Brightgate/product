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
	"crypto/rand"
	"math"
	"math/big"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/openpgp"
	"golang.org/x/crypto/openpgp/armor"

	"bg/cloud_models/appliancedb"
	"bg/common/cfgapi"
	"bg/common/passwordgen"

	"github.com/gorilla/sessions"
	"github.com/labstack/echo"
	"github.com/pkg/errors"
	"github.com/satori/uuid"
)

type accountHandler struct {
	db               appliancedb.DataStore
	sessionStore     sessions.Store
	getClientHandle  getClientHandleFunc
	accountSecretKey []byte
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
	accountUUID, ok := c.Get("account_uuid").(uuid.UUID)
	if !ok || accountUUID == uuid.Nil {
		return echo.NewHTTPError(http.StatusUnauthorized)
	}

	account, err := a.db.AccountByUUID(ctx, accountUUID)
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

func pgpSymEncrypt(plaintext string, passphrase []byte) (string, error) {
	b := &strings.Builder{}
	armorW, err := armor.Encode(b, "PGP MESSAGE", nil)
	if err != nil {
		return "", errors.Wrap(err, "Could not prepare message armor")
	}
	defer armorW.Close()

	plaintextW, err := openpgp.SymmetricallyEncrypt(armorW, passphrase, nil, nil)
	if err != nil {
		return "", errors.Wrap(err, "Could not prepare message encryptor")
	}

	defer plaintextW.Close()
	_, err = plaintextW.Write([]byte(plaintext))
	if err != nil {
		return "", errors.Wrap(err, "Could not write plaintext")
	}
	plaintextW.Close()
	armorW.Close()
	return b.String(), nil
}

func (a *accountHandler) savePasswords(ctx context.Context, accountUUID uuid.UUID, userpw, userpwRegime, mschapv2pw, mschapv2pwRegime string) error {
	userpwCipherText, err := pgpSymEncrypt(userpw, a.accountSecretKey)
	if err != nil {
		return err
	}
	mschapv2pwCipherText, err := pgpSymEncrypt(mschapv2pw, a.accountSecretKey)
	if err != nil {
		return err
	}
	now := time.Now()
	accountSecrets := &appliancedb.AccountSecrets{
		AccountUUID:                 accountUUID,
		ApplianceUserBcrypt:         userpwCipherText,
		ApplianceUserBcryptRegime:   userpwRegime,
		ApplianceUserBcryptTs:       now,
		ApplianceUserMSCHAPv2:       mschapv2pwCipherText,
		ApplianceUserMSCHAPv2Regime: mschapv2pwRegime,
		ApplianceUserMSCHAPv2Ts:     now,
	}
	err = a.db.UpsertAccountSecrets(ctx, accountSecrets)
	if err != nil {
		return err
	}
	return nil
}

// postAccountSelfProvision takes the verifier code discussed
// above as input, as well as information from the session.
//
// After a bunch of pre-flight checks, it creates and saves
// the user to all of the customer sites.
//
// XXX ui.Update() waits, which is probably not what we want.
func (a *accountHandler) postAccountSelfProvision(c echo.Context) error {
	accountUUID, ok := c.Get("account_uuid").(uuid.UUID)
	if !ok || accountUUID == uuid.Nil {
		return echo.NewHTTPError(http.StatusUnauthorized)
	}
	ctx := c.Request().Context()

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

	sites, err := a.db.CustomerSitesByAccount(ctx, accountUUID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err)
	}

	account, err := a.db.AccountByUUID(ctx, accountUUID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err)
	}

	person, err := a.db.PersonByUUID(ctx, account.PersonUUID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err)
	}

	// We've now got confirmation that the user wants to provision this
	// password; so stash it in the database.
	err = a.savePasswords(ctx, accountUUID, userpwSessionVal, pwRegime, mschapSessionVal, pwRegime)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err)
	}

	// Try to build up all of the config we want first, then run the
	// updates.  We want to do our best to see that this operation succeeds
	// or fails as a whole.
	uis := make([]*cfgapi.UserInfo, 0)
	for _, site := range sites {
		var hdl *cfgapi.Handle
		hdl, err := a.getClientHandle(site.UUID.String())
		if err != nil {
			c.Logger().Errorf("getClientHandle failed: %v", err)
			return echo.NewHTTPError(http.StatusInternalServerError, err)
		}
		// Try to get a single property; helps us detect if there is no
		// config at all for this site.
		_, err = hdl.GetProp("@/apversion")
		if err != nil && errors.Cause(err) == cfgapi.ErrNoConfig {
			// No config for this site, keep going.
			continue
		} else if err != nil {
			c.Logger().Errorf("GetProp @/apversion failed: %v", err)
			return echo.NewHTTPError(http.StatusInternalServerError, err)
		}
		var ui *cfgapi.UserInfo
		ui, err = hdl.NewSelfProvisionUserInfo(account.Email, accountUUID)
		if err != nil {
			c.Logger().Errorf("NewSelfProvisionUserInfo failed: %#v %#v", err, errors.Cause(err))
			return echo.NewHTTPError(http.StatusInternalServerError, err)
		}
		ui.DisplayName = person.Name
		ui.Email = account.Email
		ui.TelephoneNumber = account.PhoneNumber
		if ui.TelephoneNumber == "" {
			ui.TelephoneNumber = "650-555-1212"
		}
		c.Logger().Infof("Adding new user %v to site %v", ui.UID, site.UUID)
		uis = append(uis, ui)
	}
	for _, ui := range uis {
		c.Logger().Infof("Hitting send on %v", ui)
		ops := ui.PropOpsFromPasswordHashes(userpwSessionVal, mschapSessionVal)

		_, err := ui.Update(ops...)
		if err != nil {
			c.Logger().Errorf("Failed to start update: %v", err)
		}
		// XXX for now we don't wait around to see if the update succeeds.
		// More work is needed to give the user progress and/or partial
		// results.
	}
	return c.Redirect(http.StatusFound, "/client-web")
}

// getAccountSelfProvision returns self provisioning information for
// the user's account.
func (a *accountHandler) getAccountSelfProvision(c echo.Context) error {
	accountUUID, ok := c.Get("account_uuid").(uuid.UUID)
	if !ok || accountUUID == uuid.Nil {
		return echo.NewHTTPError(http.StatusUnauthorized)
	}
	ctx := c.Request().Context()

	_, err := a.sessionStore.Get(c.Request(), "bg_login")
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized)
	}

	account, err := a.db.AccountByUUID(ctx, accountUUID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, err)
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

// newAccountAPIHandler creates an accountHandler for the given DataStore and session
// Store, and routes the handler into the echo instance.
func newAccountHandler(r *echo.Echo, db appliancedb.DataStore, middlewares []echo.MiddlewareFunc, sessionStore sessions.Store, getClientHandle getClientHandleFunc, accountSecretKey []byte) *accountHandler {
	if len(accountSecretKey) != 32 {
		panic("bad accountSecretKey")
	}

	h := &accountHandler{db, sessionStore, getClientHandle, accountSecretKey}
	acct := r.Group("/api/account")
	acct.Use(middlewares...)
	acct.GET("/:uuid/passwordgen", h.getAccountPasswordGen)
	acct.GET("/:uuid/selfprovision", h.getAccountSelfProvision)
	acct.POST("/:uuid/selfprovision", h.postAccountSelfProvision)
	return h
}
