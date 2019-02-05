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
	"fmt"
	"math"
	"math/big"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/openpgp"
	"golang.org/x/crypto/openpgp/armor"

	"bg/cloud_models/appliancedb"
	"bg/common/cfgapi"
	"bg/common/deviceid"
	"bg/common/passwordgen"

	"github.com/gorilla/sessions"
	"github.com/labstack/echo"
	"github.com/pkg/errors"
	"github.com/satori/uuid"
)

type getClientHandleFunc func(uuid string) (*cfgapi.Handle, error)

type apiHandler struct {
	db               appliancedb.DataStore
	sessionStore     sessions.Store
	getClientHandle  getClientHandleFunc
	accountSecretKey []byte
}

type apiSelfProvisionInfo struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Verifier string `json:"verifier"`
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
func (a *apiHandler) getAccountPasswordGen(c echo.Context) error {
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
	return c.JSON(http.StatusOK, &apiSelfProvisionInfo{
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

func (a *apiHandler) savePasswords(ctx context.Context, accountUUID uuid.UUID, userpw, mschapv2pw string) error {
	userpwCipherText, err := pgpSymEncrypt(userpw, a.accountSecretKey)
	if err != nil {
		return err
	}
	mschapv2pwCipherText, err := pgpSymEncrypt(mschapv2pw, a.accountSecretKey)
	if err != nil {
		return err
	}
	accountSecrets := &appliancedb.AccountSecrets{
		AccountUUID:           accountUUID,
		ApplianceUserBcrypt:   userpwCipherText,
		ApplianceUserMSCHAPv2: mschapv2pwCipherText,
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
//
// XXX probably move this out of here since it is cloud only.
func (a *apiHandler) postAccountSelfProvision(c echo.Context) error {
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

	var provInfo apiSelfProvisionInfo
	if err := c.Bind(&provInfo); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err)
	}
	if provInfo.Verifier == "" || provInfo.Verifier != verifierSessionVal {
		c.Logger().Warnf("provInfo.Verifier %s != verifierSessionVal %s", provInfo.Verifier, verifierSessionVal)
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
	err = a.savePasswords(ctx, accountUUID, userpwSessionVal, mschapSessionVal)
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

		err = ui.Update(ops...)
		if err != nil {
			c.Logger().Errorf("Failed to update: %v", err)
		}
	}
	return c.Redirect(http.StatusFound, "/client-web")
}

type apiSite struct {
	UUID uuid.UUID `json:"uuid"`
	Name string    `json:"name"`
}

// getSites implements /api/sites, which presents a filtered list of
// applicable sites for the account.
func (a *apiHandler) getSites(c echo.Context) error {
	accountUUID, ok := c.Get("account_uuid").(uuid.UUID)
	if !ok || accountUUID == uuid.Nil {
		return echo.NewHTTPError(http.StatusUnauthorized)
	}
	ctx := c.Request().Context()

	sites, err := a.db.CustomerSitesByAccount(ctx, accountUUID)
	if err != nil {
		c.Logger().Errorf("Failed to get Sites by Account: %+v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, err)
	}
	apiSites := make([]apiSite, len(sites))
	for i, site := range sites {
		// XXX Today, we derive Name from the registry name.  However,
		// customers will want to have control over the site name, and
		// this is best seen as a temporary measure.
		apiSites[i] = apiSite{
			UUID: site.UUID,
			Name: site.Name,
		}
	}
	return c.JSON(http.StatusOK, &apiSites)
}

// getSitesUUID implements /api/sites/:uuid
func (a *apiHandler) getSitesUUID(c echo.Context) error {
	// Parsing UUID from string input
	u, err := uuid.FromString(c.Param("uuid"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest)
	}
	site, err := a.db.CustomerSiteByUUID(context.Background(), u)
	if err != nil {
		if _, ok := err.(appliancedb.NotFoundError); ok {
			return echo.NewHTTPError(http.StatusNotFound, "No such site")
		}
		return echo.NewHTTPError(http.StatusInternalServerError)
	}
	return c.JSON(http.StatusOK, &site)
}

// getConfig implements GET /api/sites/:uuid/config
func (a *apiHandler) getConfig(c echo.Context) error {
	hdl, err := a.getClientHandle(c.Param("uuid"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest)
	}
	defer hdl.Close()

	pnode, err := hdl.GetProps(c.QueryString())
	if err != nil {
		// XXX improve?
		return echo.NewHTTPError(http.StatusBadRequest)
	}

	return c.JSON(http.StatusOK, pnode.Value)
}

// getConfig implements GET /api/sites/:uuid/configtree
func (a *apiHandler) getConfigTree(c echo.Context) error {
	hdl, err := a.getClientHandle(c.Param("uuid"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest)
	}
	defer hdl.Close()

	pnode, err := hdl.GetProps(c.QueryString())
	if err != nil {
		// XXX improve?
		return echo.NewHTTPError(http.StatusBadRequest)
	}

	return c.JSON(http.StatusOK, pnode)
}

// postConfig implements POST /api/sites/:uuid/config
func (a *apiHandler) postConfig(c echo.Context) error {
	hdl, err := a.getClientHandle(c.Param("uuid"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest)
	}
	defer hdl.Close()

	values, err := c.FormParams()
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest)
	}

	if len(values) == 0 {
		return echo.NewHTTPError(
			http.StatusBadRequest,
			"Empty request")
	}

	var ops []cfgapi.PropertyOp
	for param, paramValues := range values {
		if len(paramValues) != 1 {
			return echo.NewHTTPError(
				http.StatusBadRequest,
				"Properties may only have one value")
		}
		ops = append(ops, cfgapi.PropertyOp{
			Op:    cfgapi.PropCreate,
			Name:  param,
			Value: paramValues[0],
		})
	}

	_, err = hdl.Execute(context.TODO(), ops).Wait(context.TODO())
	if err != nil {
		c.Logger().Errorf("failed to set properties: %v", err)
		return echo.NewHTTPError(
			http.StatusBadRequest,
			"failed to set properties")
	}
	return nil
}

// apiVulnInfo describes a detected vulnerability.  It is a subset
// of cfgapi.VulnInfo.
type apiVulnInfo struct {
	FirstDetected  *time.Time `json:"first_detected"`
	LatestDetected *time.Time `json:"latest_detected"`
	Repaired       *time.Time `json:"repaired"`
	Active         bool       `json:"active"`
	Details        string     `json:"details"`
	Repair         *bool      `json:"repair,omitempty"`
}

// apiScanInfo describes a scan.
type apiScanInfo struct {
	Start  *time.Time `json:"start"`
	Finish *time.Time `json:"finish"`
}

// apiDevice describes a device, merging information from
// the @/clients/<clientid> and the devicedb.
type apiDevice struct {
	HwAddr          string                 `json:"HwAddr"`
	Manufacturer    string                 `json:"Manufacturer"`
	Model           string                 `json:"Model"`
	Kind            string                 `json:"Kind"`
	Confidence      float64                `json:"Confidence"`
	Ring            string                 `json:"Ring"`
	HumanName       string                 `json:"HumanName,omitempty"`
	DNSName         string                 `json:"DNSName,omitempty"`
	DHCPExpiry      string                 `json:"DHCPExpiry,omitempty"`
	IPv4Addr        *net.IP                `json:"IPv4Addr,omitempty"`
	OSVersion       string                 `json:"OSVersion,omitempty"`
	Active          bool                   `json:"Active"`
	ConnAuthType    string                 `json:"ConnAuthType,omitempty"`
	ConnMode        string                 `json:"ConnMode,omitempty"`
	ConnNode        *uuid.UUID             `json:"ConnNode,omitempty"`
	Scans           map[string]apiScanInfo `json:"Scans,omitempty"`
	Vulnerabilities map[string]apiVulnInfo `json:"Vulnerabilities,omitempty"`
}

// ApiDevices is the envelope for multi-device responses.
type apiDevices struct {
	Devices []*apiDevice
}

func buildDeviceResponse(c echo.Context, hdl *cfgapi.Handle,
	hwaddr string, client *cfgapi.ClientInfo,
	scanMap cfgapi.ScanMap, vulnMap cfgapi.VulnMap) *apiDevice {

	d := apiDevice{
		HwAddr:          hwaddr,
		Manufacturer:    "unknown",
		Model:           fmt.Sprintf("unknown (id=%s)", client.Identity),
		Kind:            "unknown",
		Confidence:      client.Confidence,
		Ring:            client.Ring,
		IPv4Addr:        &client.IPv4,
		Active:          client.IsActive(),
		ConnAuthType:    client.ConnAuthType,
		ConnMode:        client.ConnMode,
		ConnNode:        client.ConnNode,
		Scans:           make(map[string]apiScanInfo),
		Vulnerabilities: make(map[string]apiVulnInfo),
	}

	if client.Identity != "" {
		id, err := strconv.Atoi(client.Identity)
		if err != nil {
			c.Logger().Warnf("buildDeviceResponse: bad Identity %s", client.Identity)
		} else {
			lpn, err := deviceid.GetDeviceByID(hdl, id)
			if err != nil {
				c.Logger().Warnf("buildDeviceResponse couldn't lookup @/devices/%d: %v\n", id, err)
			} else {
				d.Manufacturer = lpn.Vendor
				d.Model = lpn.ProductName
				d.Kind = lpn.Devtype
			}
		}
	}

	if client.DNSName != "" {
		d.HumanName = client.DNSName
		d.DNSName = client.DNSName
	} else if client.DHCPName != "" {
		d.HumanName = client.DHCPName
		d.DNSName = ""
	} else {
		d.HumanName = fmt.Sprintf("Unnamed (%s)", hwaddr)
		d.DNSName = ""
	}

	if client.Expires != nil {
		d.DHCPExpiry = client.Expires.Format("2006-01-02T15:04")
	} else {
		d.DHCPExpiry = "static"
	}

	for k, v := range scanMap {
		d.Scans[k] = apiScanInfo{
			Start:  v.Start,
			Finish: v.Finish,
		}
	}

	for k, v := range vulnMap {
		d.Vulnerabilities[k] = apiVulnInfo{
			FirstDetected:  v.FirstDetected,
			LatestDetected: v.LatestDetected,
			Repaired:       v.RepairedAt,
			Active:         v.Active,
			Details:        v.Details,
			Repair:         v.Repair,
		}
	}

	return &d
}

// getDevices implements /api/sites/:uuid/devices
func (a *apiHandler) getDevices(c echo.Context) error {
	hdl, err := a.getClientHandle(c.Param("uuid"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest)
	}
	defer hdl.Close()

	var response apiDevices
	for mac, client := range hdl.GetClients() {
		scans := hdl.GetClientScans(mac)
		vulns := hdl.GetVulnerabilities(mac)
		d := buildDeviceResponse(c, hdl, mac, client, scans, vulns)
		response.Devices = append(response.Devices, d)
	}

	// XXX for now, return an empty list
	return c.JSON(http.StatusOK, response)
}

// apiUserInfo describes a user.  It is similar to cfgapi.UserInfo but with
// fields customized for partial updates and password setting.
type apiUserInfo struct {
	UID               string
	UUID              *uuid.UUID
	Role              *string
	DisplayName       *string
	Email             *string
	TelephoneNumber   *string
	PreferredLanguage *string
	HasTOTP           bool
	HasPassword       bool
	SetPassword       *string
}

// newAPIUserInfo constructs an apiUserInfo from a cfgapi.UserInfo
func newAPIUserInfo(user *cfgapi.UserInfo) *apiUserInfo {
	var cu apiUserInfo

	// XXX mismatch possible between uid and user.uid?
	cu.UID = user.UID
	cu.UUID = &user.UUID
	cu.Role = &user.Role
	cu.DisplayName = &user.DisplayName
	cu.Email = &user.Email
	cu.TelephoneNumber = &user.TelephoneNumber
	cu.PreferredLanguage = &user.PreferredLanguage

	// XXX These could have stricter tests for correctness/lack of
	// corruption.
	cu.HasTOTP = user.TOTP != ""
	cu.HasPassword = user.Password != ""

	// XXX We are not reporting our password or TOTP back in this
	// call.

	return &cu
}

// apiUsers is the envelope for multi-user responses
type apiUsers struct {
	Users map[string]*apiUserInfo
}

// getUsers implements /api/sites/:uuid/users
func (a *apiHandler) getUsers(c echo.Context) error {
	var users apiUsers
	users.Users = make(map[string]*apiUserInfo)

	hdl, err := a.getClientHandle(c.Param("uuid"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest)
	}
	defer hdl.Close()

	for _, userInfo := range hdl.GetUsers() {
		apiU := newAPIUserInfo(userInfo)
		users.Users[apiU.UUID.String()] = apiU
	}
	return c.JSON(http.StatusOK, users)
}

// getUserByUUID implements GET /api/sites/:uuid/users/:useruuid
func (a *apiHandler) getUserByUUID(c echo.Context) error {
	// Parsing User UUID from string input
	ruuid, err := uuid.FromString(c.Param("useruuid"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "bad user uuid")
	}

	hdl, err := a.getClientHandle(c.Param("uuid"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest)
	}
	defer hdl.Close()

	userInfo, err := hdl.GetUserByUUID(ruuid)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "not found")
	}

	cu := newAPIUserInfo(userInfo)
	return c.JSON(http.StatusOK, &cu)
}

// postUserByUUID implements POST /api/sites/:uuid/users/:useruuid
func (a *apiHandler) postUserByUUID(c echo.Context) error {
	var au apiUserInfo
	if err := c.Bind(&au); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "bad user")
	}

	hdl, err := a.getClientHandle(c.Param("uuid"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest)
	}
	defer hdl.Close()

	var ui *cfgapi.UserInfo
	if c.Param("useruuid") == "NEW" {
		ui, err = hdl.NewUserInfo(au.UID)
		if err != nil {
			c.Logger().Warnf("error making new user: %v %v", au, err)
			return echo.NewHTTPError(http.StatusBadRequest,
				"invalid uid or user exists")
		}
	} else {
		var userUUID uuid.UUID
		// Parsing User UUID from string input
		userUUID, err = uuid.FromString(c.Param("useruuid"))
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "bad uuid")
		}
		ui, err = hdl.GetUserByUUID(userUUID)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid or unknown user")
		}
	}
	// propagate daUser to UserInfo
	if au.DisplayName != nil {
		ui.DisplayName = *au.DisplayName
	}
	if au.Email != nil {
		ui.Email = *au.Email
	}
	if au.PreferredLanguage != nil {
		ui.PreferredLanguage = *au.PreferredLanguage
	}
	if au.TelephoneNumber != nil {
		ui.TelephoneNumber = *au.TelephoneNumber
	}
	if au.Role != nil {
		ui.Role = *au.Role
	}
	var extraOps []cfgapi.PropertyOp
	if au.SetPassword != nil {
		extraOps, err = ui.PropOpsFromPassword(*au.SetPassword)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "failed generate passwords")
		}
	}
	err = ui.Update(extraOps...)
	if err != nil {
		c.Logger().Errorf("failed to save user '%s': %v\n", au.UID, err)
		return echo.NewHTTPError(http.StatusBadRequest, "failed to save user")
	}

	// Reget to reflect password, etc. changes from backend
	ui, err = hdl.GetUserByUUID(ui.UUID)
	if err != nil {
		return err // promoted to 500
	}

	cu := newAPIUserInfo(ui)
	return c.JSON(http.StatusOK, &cu)
}

// deleteUserByUUID implements DELETE /api/sites/:uuid/users/:useruuid
func (a *apiHandler) deleteUserByUUID(c echo.Context) error {
	hdl, err := a.getClientHandle(c.Param("uuid"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest)
	}
	defer hdl.Close()

	var ui *cfgapi.UserInfo
	// Parsing User UUID from string input
	userUUID, err := uuid.FromString(c.Param("useruuid"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "bad uuid")
	}
	if ui, err = hdl.GetUserByUUID(userUUID); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid or unknown user")
	}
	if err = ui.Delete(); err != nil {
		return err
	}

	return nil
}

// mirrors RingConfig but omits Bridge and Vlan
type apiRing struct {
	VirtualAP     string `json:"vap"`
	Subnet        string `json:"subnet"`
	LeaseDuration int    `json:"leaseDuration"`
}

type apiRings map[string]apiRing

// getRings implements /api/sites/:uuid/rings
func (a *apiHandler) getRings(c echo.Context) error {
	hdl, err := a.getClientHandle(c.Param("uuid"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest)
	}
	defer hdl.Close()

	var resp apiRings = make(map[string]apiRing)
	for ringName, ring := range hdl.GetRings() {
		resp[ringName] = apiRing{
			VirtualAP:     ring.VirtualAP,
			Subnet:        ring.Subnet,
			LeaseDuration: ring.LeaseDuration,
		}
	}
	return c.JSON(http.StatusOK, resp)
}

func (a *apiHandler) sessionMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		session, err := a.sessionStore.Get(c.Request(), "bg_login")
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

func (a *apiHandler) siteMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		accountUUID, ok := c.Get("account_uuid").(uuid.UUID)
		if !ok || accountUUID == uuid.Nil {
			return echo.NewHTTPError(http.StatusUnauthorized)
		}
		sites, err := a.db.CustomerSitesByAccount(context.Background(), accountUUID)
		if err != nil {
			c.Logger().Errorf("Failed to get Sites by Account: %+v", err)
			return echo.NewHTTPError(http.StatusInternalServerError, err)
		}
		// Special, doesn't present a site UUID
		if c.Path() == "/api/sites" {
			return next(c)
		}
		// All the other endpoints come through here.
		// This checks that the user has access in some form to this
		// resource.  It does not check suitability beyond that.
		siteUUIDParam := c.Param("uuid")
		siteUUID, err := uuid.FromString(siteUUIDParam)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest)
		}
		for _, site := range sites {
			if site.UUID == siteUUID {
				return next(c)
			}
		}
		// Pretend it isn't there.
		return echo.NewHTTPError(http.StatusNotFound)
	}
}

// newAPIHandler creates an apiHandler for the given DataStore and session
// Store, and routes the handler into the echo instance.
func newAPIHandler(r *echo.Echo, db appliancedb.DataStore, sessionStore sessions.Store, getClientHandle getClientHandleFunc, accountSecretKey []byte) *apiHandler {
	if len(accountSecretKey) != 32 {
		panic("bad accountSecretKey")
	}

	h := &apiHandler{db, sessionStore, getClientHandle, accountSecretKey}
	api := r.Group("/api")
	api.Use(h.sessionMiddleware)
	api.GET("/account/:uuid/passwordgen", h.getAccountPasswordGen)
	api.POST("/account/:uuid/selfprovision", h.postAccountSelfProvision)
	api.GET("/sites", h.getSites)

	siteU := r.Group("/api/sites/:uuid", h.sessionMiddleware, h.siteMiddleware)
	siteU.GET("", h.getSitesUUID)
	siteU.GET("/config", h.getConfig)
	siteU.POST("/config", h.postConfig)
	siteU.GET("/configtree", h.getConfigTree)
	siteU.GET("/devices", h.getDevices)
	siteU.GET("/users", h.getUsers)
	siteU.GET("/users/:useruuid", h.getUserByUUID)
	siteU.POST("/users/:useruuid", h.postUserByUUID)
	siteU.DELETE("/users/:useruuid", h.deleteUserByUUID)
	siteU.GET("/rings", h.getRings)
	return h
}
