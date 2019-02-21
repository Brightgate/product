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
)

type getClientHandleFunc func(uuid string) (*cfgapi.Handle, error)

type siteHandler struct {
	db              appliancedb.DataStore
	getClientHandle getClientHandleFunc
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
func newSiteHandler(r *echo.Echo, db appliancedb.DataStore, middlewares []echo.MiddlewareFunc, getClientHandle getClientHandleFunc) *siteHandler {
	h := &siteHandler{db, getClientHandle}
	r.GET("/api/sites", h.getSites, middlewares...)

	mw := middlewares
	mw = append(mw, h.siteMiddleware)
	siteU := r.Group("/api/sites/:uuid", mw...)
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
