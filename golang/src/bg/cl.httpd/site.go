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
	"strconv"
	"time"

	"bg/cloud_models/appliancedb"
	"bg/common/cfgapi"
	"bg/common/deviceid"

	"github.com/labstack/echo"
	"github.com/satori/uuid"
	"github.com/sfreiberg/gotwilio"
	"github.com/ttacon/libphonenumber"
)

type getClientHandleFunc func(uuid string) (*cfgapi.Handle, error)

type siteHandler struct {
	db              appliancedb.DataStore
	getClientHandle getClientHandleFunc
	twilio          *gotwilio.Twilio
}

type siteResponse struct {
	UUID uuid.UUID `json:"uuid"`
	Name string    `json:"name"`
}

// getSites implements /api/sites, which presents a filtered list of
// applicable sites for the account.
func (a *siteHandler) getSites(c echo.Context) error {
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
	apiSites := make([]siteResponse, len(sites))
	for i, site := range sites {
		// XXX Today, we derive Name from the registry name.  However,
		// customers will want to have control over the site name, and
		// this is best seen as a temporary measure.
		apiSites[i] = siteResponse{
			UUID: site.UUID,
			Name: site.Name,
		}
	}
	return c.JSON(http.StatusOK, &apiSites)
}

// getSitesUUID implements /api/sites/:uuid
func (a *siteHandler) getSitesUUID(c echo.Context) error {
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
func (a *siteHandler) getConfig(c echo.Context) error {
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
func (a *siteHandler) getConfigTree(c echo.Context) error {
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
func (a *siteHandler) postConfig(c echo.Context) error {
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
	ConnVAP         string                 `json:"ConnVAP,omitempty"`
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
		ConnVAP:         client.ConnVAP,
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
func (a *siteHandler) getDevices(c echo.Context) error {
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

type siteEnrollGuestRequest struct {
	Kind        string `json:"kind"`
	Email       string `json:"email"`
	PhoneNumber string `json:"phoneNumber"`
}

type siteEnrollGuestResponse struct {
	SMSDelivered bool   `json:"smsDelivered"`
	SMSErrorCode int    `json:"smsErrorCode"`
	SMSError     string `json:"smsError"`
}

// sendOneSMS is a utility helper for the Enroll handler.
func (a *siteHandler) sendOneSMS(from, to, message string) (*siteEnrollGuestResponse, error) {
	var response *siteEnrollGuestResponse
	smsResponse, exception, err := a.twilio.SendSMS(from, to, message, "", "")
	if err != nil {
		return nil, err
	}
	if exception != nil {
		rstr := "Twilio failed sending SMS."
		if exception.Code >= 21210 && exception.Code <= 21217 {
			rstr = "Invalid Phone Number"
		}
		response = &siteEnrollGuestResponse{false, exception.Code, rstr}
	} else {
		response = &siteEnrollGuestResponse{true, 0, "Current Status: " + smsResponse.Status}
	}
	return response, nil
}

func (a *siteHandler) postEnrollGuest(c echo.Context) error {
	var err error

	if a.twilio == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "no twilio client configured")
	}

	accountUUID := c.Get("account_uuid").(uuid.UUID)
	siteUUID := c.Param("uuid")

	var gr siteEnrollGuestRequest
	if err := c.Bind(&gr); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "bad guest info")
	}

	config, err := a.getClientHandle(c.Param("uuid"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest)
	}
	defer config.Close()

	vaps := config.GetVirtualAPs()
	guestVAP, ok := vaps["guest"]
	if !ok {
		return echo.NewHTTPError(http.StatusForbidden, "no guest vap")
	}
	if len(guestVAP.Rings) == 0 {
		return echo.NewHTTPError(http.StatusForbidden, "guest vap not enabled")
	}

	c.Logger().Infof("Guest Entrollment by %v for %v at site %v network %s",
		accountUUID, gr, siteUUID, guestVAP.SSID)
	if gr.Kind != "psk" {
		return echo.NewHTTPError(http.StatusBadRequest, "missing kind={psk}")
	}
	if gr.PhoneNumber == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "missing phoneNumber")
	}

	// XXX need to solve phone region eventually
	to, err := libphonenumber.Parse(gr.PhoneNumber, "US")
	if err != nil {
		return c.JSON(http.StatusOK, &siteEnrollGuestResponse{false, 0, "Invalid Phone Number"})
	}
	formattedTo := libphonenumber.Format(to, libphonenumber.INTERNATIONAL)
	from := "+16507694283"
	c.Logger().Infof("Guest Enroll Handler: from='%v' formattedTo='%v'\n", from, formattedTo)

	// See above for notes on structure
	messages := []string{
		fmt.Sprintf("Brightgate Wi-Fi\nHelp: bit.ly/2yhPDQz\n"+
			"Network: %s\n<password follows>", guestVAP.SSID),
		guestVAP.Passphrase,
	}

	var response *siteEnrollGuestResponse
	for _, message := range messages {
		response, err = a.sendOneSMS(from, formattedTo, message)
		if err != nil {
			c.Logger().Warnf("Enroll Guest Handler: twilio err='%v'\n", err)
			return echo.NewHTTPError(http.StatusInternalServerError, "Twilio Error")
		}
		// if not sent then give up sending more
		if response.SMSDelivered == false {
			break
		}
	}
	return c.JSON(http.StatusOK, response)
}

// getNetworkVAP implements GET /api/site/:uuid/network/vap, returning the list of VAPs
func (a *siteHandler) getNetworkVAP(c echo.Context) error {
	hdl, err := a.getClientHandle(c.Param("uuid"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest)
	}
	defer hdl.Close()

	vapNames := make([]string, 0)
	vaps := hdl.GetVirtualAPs()
	for vapName := range vaps {
		vapNames = append(vapNames, vapName)
	}
	return c.JSON(http.StatusOK, &vapNames)
}

// getNetworkVAPName implements GET /api/site/:uuid/network/vap/:name,
// returning information about a VAP.
func (a *siteHandler) getNetworkVAPName(c echo.Context) error {
	hdl, err := a.getClientHandle(c.Param("uuid"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest)
	}

	defer hdl.Close()
	vaps := hdl.GetVirtualAPs()
	vap, ok := vaps[c.Param("vapname")]
	if !ok {
		return echo.NewHTTPError(http.StatusNotFound)
	}
	return c.JSON(http.StatusOK, vap)
}

// postNetworkVAPName implements POST /api/site/:uuid/network/vap/:name, allowing updates
// to select VAP fields.
func (a *siteHandler) postNetworkVAPName(c echo.Context) error {
	return nil
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
	SelfProvisioning  bool
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

	cu.SelfProvisioning = user.SelfProvisioning

	return &cu
}

// apiUsers is the envelope for multi-user responses
type apiUsers struct {
	Users map[string]*apiUserInfo
}

// getUsers implements /api/sites/:uuid/users
func (a *siteHandler) getUsers(c echo.Context) error {
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
func (a *siteHandler) getUserByUUID(c echo.Context) error {
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
func (a *siteHandler) postUserByUUID(c echo.Context) error {
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
	cmdHdl, err := ui.Update(extraOps...)
	if err != nil {
		c.Logger().Errorf("failed setup update for user '%s': %v\n", au.UID, err)
		return echo.NewHTTPError(http.StatusBadRequest, "failed to save user")
	}
	_, err = cmdHdl.Wait(c.Request().Context())
	if err != nil {
		c.Logger().Errorf("failed update for user '%s': %v\n", au.UID, err)
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
func (a *siteHandler) deleteUserByUUID(c echo.Context) error {
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
func (a *siteHandler) getRings(c echo.Context) error {
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

func (a *siteHandler) siteMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
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

// newSiteHandler creates a siteHandler instance for the given DataStore and
// session Store, and routes the handler into the echo instance.
func newSiteHandler(r *echo.Echo, db appliancedb.DataStore, middlewares []echo.MiddlewareFunc, getClientHandle getClientHandleFunc, twilio *gotwilio.Twilio) *siteHandler {
	h := &siteHandler{db, getClientHandle, twilio}
	r.GET("/api/sites", h.getSites, middlewares...)

	mw := middlewares
	mw = append(mw, h.siteMiddleware)
	siteU := r.Group("/api/sites/:uuid", mw...)
	siteU.GET("", h.getSitesUUID)
	siteU.GET("/config", h.getConfig)
	siteU.POST("/config", h.postConfig)
	siteU.GET("/configtree", h.getConfigTree)
	siteU.GET("/devices", h.getDevices)
	siteU.POST("/enroll_guest", h.postEnrollGuest)
	siteU.GET("/network/vap", h.getNetworkVAP)
	siteU.GET("/network/vap/:vapname", h.getNetworkVAPName)
	siteU.POST("/network/vap/:vapname", h.postNetworkVAPName)
	siteU.GET("/users", h.getUsers)
	siteU.GET("/users/:useruuid", h.getUserByUUID)
	siteU.POST("/users/:useruuid", h.postUserByUUID)
	siteU.DELETE("/users/:useruuid", h.deleteUserByUUID)
	siteU.GET("/rings", h.getRings)
	return h
}
