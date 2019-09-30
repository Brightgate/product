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
	"bg/common/mfg"

	"github.com/labstack/echo"
	"github.com/satori/uuid"
	"github.com/sfreiberg/gotwilio"
	"github.com/ttacon/libphonenumber"
)

// Utility function for executing property changes
func executePropChange(c echo.Context, hdl *cfgapi.Handle, ops []cfgapi.PropertyOp) error {
	var err error
	// XXX Until we fix T470, the gyrations in this code around context
	// timeouts mostly don't work.  So as a compromise we make the timeout
	// long enough to allow the operation to timeout "naturally" using the
	// cfgapi.Handle's builtin timeout.
	var timeout = 20000

	timeoutHdr, ok := c.Request().Header["X-Timeout"]
	if ok {
		timeoutStr := timeoutHdr[0]
		timeout, err = strconv.Atoi(timeoutStr)
		if err != nil || timeout < 5000 {
			return echo.NewHTTPError(http.StatusBadRequest, "bad X-Timeout")
		}
	}

	ctx := c.Request().Context()
	// Give ourselves until 2 seconds before the ultimate timeout to do cfg stuff
	cfgctx, cfgctxcancel := context.WithTimeout(ctx, time.Millisecond*time.Duration(timeout-2000))
	defer cfgctxcancel()

	// use the outer context for this
	cmdHdl := hdl.Execute(ctx, ops)
	// use the inner context just for waiting
	errStr, err := cmdHdl.Wait(cfgctx)
	if err != nil {
		// XXX it seems wrong that it returns errcomm for a deadline cancellation
		c.Logger().Infof("wait failed, err is %s: %v", errStr, err)
		// use the outer context for this
		errStr, err = cmdHdl.Status(ctx)
		c.Logger().Infof("After Status(): status is %v, %v", errStr, err)

		if err == cfgapi.ErrQueued || err == cfgapi.ErrInProgress {
			c.Logger().Warnf("request %v did not finish before timeout: %v", ops, err)
			return c.NoContent(http.StatusAccepted)
		}
		c.Logger().Errorf("request %v failed: %v", ops, err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Execution failed on appliance")
	}
	return nil
}

type siteHandler struct {
	db              appliancedb.DataStore
	getClientHandle getClientHandleFunc
	twilio          *gotwilio.Twilio
}

type siteResponse struct {
	UUID             uuid.UUID `json:"UUID"`
	Name             string    `json:"name"`
	OrganizationUUID uuid.UUID `json:"organizationUUID"`
}

// getSites implements /api/sites, which presents a filtered list of
// applicable sites for the account.
func (a *siteHandler) getSites(c echo.Context) error {
	ctx := c.Request().Context()
	accountUUID, ok := c.Get("account_uuid").(uuid.UUID)
	if !ok || accountUUID == uuid.Nil {
		return echo.NewHTTPError(http.StatusUnauthorized)
	}

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
			UUID:             site.UUID,
			Name:             site.Name,
			OrganizationUUID: site.OrganizationUUID,
		}
	}
	return c.JSON(http.StatusOK, &apiSites)
}

// getSitesUUID implements /api/sites/:uuid
func (a *siteHandler) getSitesUUID(c echo.Context) error {
	ctx := c.Request().Context()
	accountUUID, ok := c.Get("account_uuid").(uuid.UUID)
	if !ok || accountUUID == uuid.Nil {
		return echo.NewHTTPError(http.StatusUnauthorized)
	}
	// Parsing UUID from string input
	u, err := uuid.FromString(c.Param("uuid"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest)
	}
	site, err := a.db.CustomerSiteByUUID(ctx, u)
	if err != nil {
		if _, ok := err.(appliancedb.NotFoundError); ok {
			return echo.NewHTTPError(http.StatusNotFound, "No such site")
		}
		return echo.NewHTTPError(http.StatusInternalServerError)
	}
	aoRoles, err := a.db.AccountOrgRolesByAccountTarget(ctx, accountUUID,
		site.OrganizationUUID)
	if err != nil {
		c.Logger().Errorf("Failed to get roles: %+v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, err)
	}
	// Zero roles returned means the user has tried to access a site
	// for which they are not authorized; this could be 404, but for
	// now we match the response the middleware gives.
	if len(aoRoles) == 0 {
		return echo.NewHTTPError(http.StatusUnauthorized)
	}
	resp := siteResponse{
		UUID:             site.UUID,
		Name:             site.Name,
		OrganizationUUID: site.OrganizationUUID,
	}
	return c.JSON(http.StatusOK, resp)
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

	_, err = hdl.Execute(c.Request().Context(), ops).Wait(c.Request().Context())
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
	HwAddr          string                 `json:"hwAddr"`
	Manufacturer    string                 `json:"manufacturer"`
	Model           string                 `json:"model"`
	Kind            string                 `json:"kind"`
	Confidence      float64                `json:"confidence"`
	Ring            string                 `json:"ring"`
	DisplayName     string                 `json:"displayName"`
	DHCPName        string                 `json:"dhcpName,omitempty"`
	DNSName         string                 `json:"dnsName,omitempty"`
	DHCPExpiry      string                 `json:"dhcpExpiry,omitempty"`
	IPv4Addr        *net.IP                `json:"ipv4Addr,omitempty"`
	OSVersion       string                 `json:"osVersion,omitempty"`
	Active          bool                   `json:"active"`
	Wireless        bool                   `json:"wireless"`
	ConnBand        string                 `json:"connBand,omitempty"`
	ConnNode        string                 `json:"connNode,omitempty"`
	ConnVAP         string                 `json:"connVAP,omitempty"`
	Scans           map[string]apiScanInfo `json:"scans,omitempty"`
	Vulnerabilities map[string]apiVulnInfo `json:"vulnerabilities,omitempty"`
	LastActivity    *time.Time             `json:"lastActivity,omitempty"`
	SignalStrength  *int                   `json:"signalStrength,omitempty"`
}

func buildDeviceResponse(c echo.Context, hdl *cfgapi.Handle,
	hwaddr string, client *cfgapi.ClientInfo,
	scanMap cfgapi.ScanMap, vulnMap cfgapi.VulnMap,
	metrics *cfgapi.ClientMetrics) *apiDevice {

	d := apiDevice{
		HwAddr:          hwaddr,
		Manufacturer:    "unknown",
		Model:           fmt.Sprintf("unknown (id=%s)", client.Identity),
		Kind:            "unknown",
		Confidence:      client.Confidence,
		Ring:            client.Ring,
		DisplayName:     client.DisplayName(),
		DHCPName:        client.DHCPName,
		DNSName:         client.DNSName,
		DHCPExpiry:      "static",
		IPv4Addr:        &client.IPv4,
		OSVersion:       "",
		Active:          client.IsActive(),
		Wireless:        client.Wireless,
		ConnBand:        client.ConnBand,
		ConnNode:        client.ConnNode,
		ConnVAP:         client.ConnVAP,
		Scans:           make(map[string]apiScanInfo),
		Vulnerabilities: make(map[string]apiVulnInfo),
	}

	if metrics != nil {
		d.LastActivity = metrics.LastActivity
		d.SignalStrength = &metrics.SignalStrength
	}

	if client.Expires != nil {
		d.DHCPExpiry = client.Expires.Format(time.RFC3339)
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

	return &d
}

// getDevices implements /api/sites/:uuid/devices
func (a *siteHandler) getDevices(c echo.Context) error {
	hdl, err := a.getClientHandle(c.Param("uuid"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest)
	}
	defer hdl.Close()

	response := make([]*apiDevice, 0)
	for mac, client := range hdl.GetClients() {
		scans := hdl.GetClientScans(mac)
		vulns := hdl.GetVulnerabilities(mac)
		metrics := hdl.GetClientMetrics(mac)
		d := buildDeviceResponse(c, hdl, mac, client, scans, vulns, metrics)
		response = append(response, d)
	}
	return c.JSON(http.StatusOK, response)
}

type apiPostDevice struct {
	Ring *string `json:"ring"`
}

// postDevice implements POST /api/sites/:uuid/devices/:deviceID
// Presently this only allows for ring changes
func (a *siteHandler) postDevice(c echo.Context) error {
	hdl, err := a.getClientHandle(c.Param("uuid"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest)
	}
	defer hdl.Close()

	deviceID := c.Param("deviceid")

	var input apiPostDevice
	if err := c.Bind(&input); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "bad device")
	}
	// For now, Ring is the only modifiable property.
	if input.Ring == nil {
		return echo.NewHTTPError(http.StatusBadRequest, "bad ring value")
	}

	ops := []cfgapi.PropertyOp{
		{
			Op:   cfgapi.PropTest,
			Name: fmt.Sprintf("@/clients/%s", deviceID),
		},
		{
			Op:    cfgapi.PropCreate,
			Name:  fmt.Sprintf("@/clients/%s/ring", deviceID),
			Value: *input.Ring,
		},
	}
	return executePropChange(c, hdl, ops)
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
		response = &siteEnrollGuestResponse{false, int(exception.Code), rstr}
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
		if !response.SMSDelivered {
			break
		}
	}
	return c.JSON(http.StatusOK, response)
}

type siteHealth struct {
	HeartbeatProblem bool `json:"heartbeatProblem"`
	ConfigProblem    bool `json:"configProblem"`
}

// getHealth implements /api/sites/:uuid/health
func (a *siteHandler) getHealth(c echo.Context) error {
	hdl, err := a.getClientHandle(c.Param("uuid"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest)
	}
	defer hdl.Close()

	siteUUID, err := uuid.FromString(c.Param("uuid"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "bad site uuid")
	}

	ctx := c.Request().Context()
	var response siteHealth
	hb, err := a.db.LatestHeartbeatBySiteUUID(ctx, siteUUID)
	if err != nil {
		c.Logger().Warnf("Failed to get latest heartbeat for %v: %v", siteUUID, err)
		response.HeartbeatProblem = true
	} else {
		// Heartbeats are every 7 minutes, so 15 minutes means we've missed two.
		if time.Since(hb.RecordTS) > 15*time.Minute {
			response.HeartbeatProblem = true
		}
	}

	siteNullUUID := uuid.NullUUID{UUID: siteUUID, Valid: true}
	cmds, err := a.db.CommandAuditHealth(ctx, siteNullUUID, time.Now().Add(-1*(time.Minute*3)))
	if err == nil && len(cmds) > 0 {
		response.ConfigProblem = true
	}
	c.Logger().Infof("got cmds response: %v", cmds)

	return c.JSON(http.StatusOK, response)
}

// getNetworkDNS implements GET /api/sites/:uuid/network/dns, returning DNS
// configuration information for the site.
func (a *siteHandler) getNetworkDNS(c echo.Context) error {
	hdl, err := a.getClientHandle(c.Param("uuid"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest)
	}
	defer hdl.Close()
	dns := hdl.GetDNSInfo()
	return c.JSON(http.StatusOK, &dns)
}

// getNetworkVAP implements GET /api/sites/:uuid/network/vap, returning the list of VAPs
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

// getNetworkVAPName implements GET /api/sites/:uuid/network/vap/:name,
// returning information about a VAP.
func (a *siteHandler) getNetworkVAPName(c echo.Context) error {
	hdl, err := a.getClientHandle(c.Param("uuid"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest)
	}
	defer hdl.Close()

	roles := c.Get("matched_roles").(matchedRoles)

	vaps := hdl.GetVirtualAPs()
	vap, ok := vaps[c.Param("vapname")]
	// Remove sensitive material for non-admins
	if c.Param("vapname") != "guest" && !roles["admin"] {
		vap.Passphrase = ""
	}
	if !ok {
		return echo.NewHTTPError(http.StatusNotFound)
	}
	return c.JSON(http.StatusOK, vap)
}

type apiVAPUpdate struct {
	SSID       string `json:"ssid"`
	Passphrase string `json:"passphrase"`
}

// postNetworkVAPName implements POST /api/sites/:uuid/network/vap/:name,
// allowing updates to select VAP fields.
func (a *siteHandler) postNetworkVAPName(c echo.Context) error {
	hdl, err := a.getClientHandle(c.Param("uuid"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest)
	}
	defer hdl.Close()

	var av apiVAPUpdate
	if err := c.Bind(&av); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "bad vap")
	}
	vaps := hdl.GetVirtualAPs()
	vap, ok := vaps[c.Param("vapname")]
	if !ok {
		return echo.NewHTTPError(http.StatusNotFound)
	}
	var ops []cfgapi.PropertyOp
	if av.SSID != "" && vap.SSID != av.SSID {
		ops = append(ops, cfgapi.PropertyOp{
			Op:    cfgapi.PropCreate,
			Name:  fmt.Sprintf("@/network/vap/%s/ssid", c.Param("vapname")),
			Value: av.SSID,
		})
	}
	if av.Passphrase != "" && vap.Passphrase != av.Passphrase {
		ops = append(ops, cfgapi.PropertyOp{
			Op:    cfgapi.PropCreate,
			Name:  fmt.Sprintf("@/network/vap/%s/passphrase", c.Param("vapname")),
			Value: av.Passphrase,
		})
	}
	if len(ops) == 0 {
		return nil
	}
	return executePropChange(c, hdl, ops)
}

// getNetworkWan implements GET /api/sites/:uuid/network/wan
// returning information about the Wan link
func (a *siteHandler) getNetworkWan(c echo.Context) error {
	hdl, err := a.getClientHandle(c.Param("uuid"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest)
	}
	defer hdl.Close()

	wan := hdl.GetWanInfo()
	if wan == nil {
		wan = &cfgapi.WanInfo{}
	}
	return c.JSON(http.StatusOK, wan)
}

type apiNodeNic struct {
	Name       string           `json:"name"`
	MacAddr    string           `json:"macaddr"`
	Kind       string           `json:"kind"`
	Ring       string           `json:"ring"`
	Silkscreen string           `json:"silkscreen"`
	WifiInfo   *cfgapi.WifiInfo `json:"wifiInfo,omitempty"`
}

type apiNodeInfo struct {
	ID           string       `json:"id"`
	Name         string       `json:"name"`
	Role         string       `json:"role"` // gateway|satellite
	BootTime     *time.Time   `json:"bootTime"`
	Alive        *time.Time   `json:"alive"`
	Addr         net.IP       `json:"addr"`
	Nics         []apiNodeNic `json:"nics"`
	SerialNumber string       `json:"serialNumber"` // registry SN
	HWModel      string       `json:"hwModel"`
}

func (a *siteHandler) lookupApplianceByNodeID(ctx context.Context, nodeID string) *appliancedb.ApplianceID {
	// Some (developer) nodes are not necessarily in the registry
	if !mfg.ValidExtSerial(nodeID) {
		return nil
	}

	app, _ := a.db.ApplianceIDByHWSerial(ctx, nodeID)
	return app
}

// XXX I couldn't work out where to best put this code; ap_common/platform is
// AP specific.  Should it go in cfgapi?  Maybe the platform should publish
// the silkscreen into the config tree?
func nicInfoToSilkscreen(nicInfo *cfgapi.NicInfo, nodeInfo *cfgapi.NodeInfo) string {
	if nodeInfo.Platform == "mt7623" {
		switch nicInfo.Name {
		case "wlan0":
			return "1"
		case "wlan1":
			return "2"
		case "lan0":
			return "1"
		case "lan1":
			return "2"
		case "lan2":
			return "3"
		case "lan3":
			return "4"
		case "wan":
			return "wan"
		default:
			// XXX temporary
			return "???" + nicInfo.Name
		}
	}

	if nodeInfo.Platform == "rpi3" {
		// This could be more elaborate if needed.
		switch nicInfo.Name {
		case "wlan0":
			return "0"
		case "wlan1":
			return "1"
		case "wlan2":
			return "2"
		case "eth0":
			return "0"
		case "eth1":
			return "1"
		default:
			return nicInfo.Name
		}
	}

	return nicInfo.Name
}

// getNodes implements GET /api/sites/:uuid/nodes
// returning information about network nodes (appliances) at the site
func (a *siteHandler) getNodes(c echo.Context) error {
	ctx := c.Request().Context()
	hdl, err := a.getClientHandle(c.Param("uuid"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest)
	}
	defer hdl.Close()

	result := make([]apiNodeInfo, 0)
	nodes, err := hdl.GetNodes()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err)
	}
	for _, node := range nodes {
		ni := apiNodeInfo{
			ID:       node.ID,
			Name:     node.Name,
			Role:     node.Role,
			BootTime: node.BootTime,
			Alive:    node.Alive,
			Addr:     node.Addr,
		}

		applianceID := a.lookupApplianceByNodeID(ctx, node.ID)
		if applianceID != nil {
			if applianceID.SystemReprHWSerial.Valid {
				ni.SerialNumber = applianceID.SystemReprHWSerial.String
			}
		}

		if node.Platform == "mt7623" {
			// XXX?
			ni.HWModel = "model100"
		} else if node.Platform == "rpi3" {
			// XXX?
			ni.HWModel = "rpi3"
		} else {
			ni.HWModel = node.Platform
		}

		ni.Nics = make([]apiNodeNic, 0)
		for _, nicInfo := range node.Nics {
			if nicInfo.Pseudo {
				continue
			}

			kind := nicInfo.Kind
			if kind == "wired" {
				if nicInfo.Ring == "wan" || nicInfo.Name == "wan" {
					kind = kind + ":uplink"
				} else if node.Platform == "rpi3" && nicInfo.Ring == "internal" {
					// Pi with interface operating as a satellite
					kind = kind + ":uplink"
				} else {
					kind = kind + ":lan"
				}
			}
			ni.Nics = append(ni.Nics, apiNodeNic{
				Name:       nicInfo.Name,
				MacAddr:    nicInfo.MacAddr,
				Kind:       kind,
				Ring:       nicInfo.Ring,
				Silkscreen: nicInfoToSilkscreen(&nicInfo, &node),
				WifiInfo:   nicInfo.WifiInfo,
			})
		}
		result = append(result, ni)
	}
	return c.JSON(http.StatusOK, result)
}

type apiPostNode struct {
	Name string `json:"name"`
}

// postNode implements POST /api/sites/:uuid/nodes/:nodeID
// to adjust per-node settings; presently only setting the name is
// supported.
func (a *siteHandler) postNode(c echo.Context) error {
	hdl, err := a.getClientHandle(c.Param("uuid"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest)
	}
	defer hdl.Close()

	nodeID := c.Param("nodeid")

	var input apiPostNode
	if err := c.Bind(&input); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest)
	}

	ops := []cfgapi.PropertyOp{
		{
			Op:   cfgapi.PropTest,
			Name: fmt.Sprintf("@/nodes/%s", nodeID),
		},
		{
			Op:    cfgapi.PropCreate,
			Name:  fmt.Sprintf("@/nodes/%s/name", nodeID),
			Value: input.Name,
		},
	}
	return executePropChange(c, hdl, ops)
}

type apiPostNodePort struct {
	Ring    *string `json:"ring"`
	Channel *int    `json:"channel"`
}

// postNodePort implements POST /api/sites/:uuid/nodes/:nodeID/ports/:portID
// to adjust per-port settings; presently supports:
//   setting the ring of LAN ports
//   setting the channel of wireless ports
func (a *siteHandler) postNodePort(c echo.Context) error {
	hdl, err := a.getClientHandle(c.Param("uuid"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest)
	}
	defer hdl.Close()

	nodeID := c.Param("nodeid")
	if len(nodeID) == 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "nodeid")
	}
	portID := c.Param("portid")
	if len(portID) == 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "portid")
	}
	var input apiPostNodePort
	if err := c.Bind(&input); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "input binding")
	}
	nic, err := hdl.GetNic(nodeID, portID)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "nic")
	}
	if input.Ring != nil && nic.Kind != "wired" {
		return echo.NewHTTPError(http.StatusBadRequest, "ring, wired")
	}
	if input.Channel != nil && (nic.Kind != "wireless" || nic.WifiInfo == nil) {
		return echo.NewHTTPError(http.StatusBadRequest, "chan, wireless")
	}

	var ops []cfgapi.PropertyOp
	if input.Ring != nil {
		// Check that the user isn't trying to re-ring the WAN port
		// XXX need a better check here; the uplink port could also be
		// 'internal'; T466.
		if portID == "wan" || nic.Ring == "wan" {
			return echo.NewHTTPError(http.StatusForbidden)
		}
		if !cfgapi.ValidRings[*input.Ring] {
			return echo.NewHTTPError(http.StatusBadRequest, "bad ring")
		}
		path := fmt.Sprintf("@/nodes/%s/nics/%s/ring", nodeID, portID)
		ops = append(ops, []cfgapi.PropertyOp{
			{
				Op:   cfgapi.PropTest,
				Name: path,
			},
			{
				Op:    cfgapi.PropCreate,
				Name:  path,
				Value: *input.Ring,
			},
		}...)
	}

	if input.Channel != nil {
		if !nic.WifiInfo.ValidChannel(*input.Channel) {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid channel")
		}
		testPath := fmt.Sprintf("@/nodes/%s/nics/%s", nodeID, portID)
		path := fmt.Sprintf("@/nodes/%s/nics/%s/cfg_channel", nodeID, portID)
		ops = append(ops, []cfgapi.PropertyOp{
			{
				Op:   cfgapi.PropTest,
				Name: testPath,
			},
			{
				Op:    cfgapi.PropCreate,
				Name:  path,
				Value: fmt.Sprintf("%d", *input.Channel),
			},
		}...)
	}

	return executePropChange(c, hdl, ops)
}

// apiUserInfo describes a user.  It is similar to cfgapi.UserInfo but with
// fields customized for partial updates and password setting.
type apiUserInfo struct {
	UID              string
	UUID             *uuid.UUID
	Role             *string
	DisplayName      *string
	Email            *string
	TelephoneNumber  *string
	HasPassword      bool
	SetPassword      *string
	SelfProvisioning bool
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

	// XXX These could have stricter tests for correctness/lack of
	// corruption.
	cu.HasPassword = user.Password != ""

	// XXX We are not reporting our password back in this call.

	cu.SelfProvisioning = user.SelfProvisioning

	return &cu
}

// getUsers implements /api/sites/:uuid/users
func (a *siteHandler) getUsers(c echo.Context) error {
	users := make(map[string]*apiUserInfo)

	hdl, err := a.getClientHandle(c.Param("uuid"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest)
	}
	defer hdl.Close()

	for _, userInfo := range hdl.GetUsers() {
		apiU := newAPIUserInfo(userInfo)
		users[apiU.UUID.String()] = apiU
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

// mkSiteMiddleware manufactures a middleware which protects a route; only
// users with one or more of the allowedRoles can pass through the checks; the
// middleware adds "matched_roles" to the echo context, indicating which of the
// allowed_roles the user actually has.
func (a *siteHandler) mkSiteMiddleware(allowedRoles []string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			ctx := c.Request().Context()
			accountUUID, ok := c.Get("account_uuid").(uuid.UUID)
			if !ok || accountUUID == uuid.Nil {
				return echo.NewHTTPError(http.StatusUnauthorized)
			}

			siteUUIDParam := c.Param("uuid")
			siteUUID, err := uuid.FromString(siteUUIDParam)
			if err != nil {
				return echo.NewHTTPError(http.StatusBadRequest)
			}
			// XXX could merge these two db calls
			site, err := a.db.CustomerSiteByUUID(ctx, siteUUID)
			if err != nil {
				if _, ok := err.(appliancedb.NotFoundError); ok {
					return echo.NewHTTPError(http.StatusNotFound)
				}
				return echo.NewHTTPError(http.StatusInternalServerError)
			}
			aoRoles, err := a.db.AccountOrgRolesByAccountTarget(ctx,
				accountUUID, site.OrganizationUUID)
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError)
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
			c.Logger().Debugf("Unauthorized: %s site=%v, acc=%v, ur=%v, ar=%v",
				c.Path(), siteUUID, accountUUID, aoRoles, allowedRoles)
			return echo.NewHTTPError(http.StatusUnauthorized)
		}
	}
}

// newSiteHandler creates a siteHandler instance for the given DataStore and
// session Store, and routes the handler into the echo instance.
func newSiteHandler(r *echo.Echo, db appliancedb.DataStore, middlewares []echo.MiddlewareFunc, getClientHandle getClientHandleFunc, twilio *gotwilio.Twilio) *siteHandler {
	h := &siteHandler{db, getClientHandle, twilio}
	r.GET("/api/sites", h.getSites, middlewares...)

	mw := middlewares
	user := h.mkSiteMiddleware([]string{"user", "admin"})
	admin := h.mkSiteMiddleware([]string{"admin"})

	siteU := r.Group("/api/sites/:uuid", mw...)
	siteU.GET("", h.getSitesUUID, user)
	siteU.GET("/config", h.getConfig, admin)
	siteU.POST("/config", h.postConfig, admin)
	siteU.GET("/configtree", h.getConfigTree, admin)
	siteU.GET("/devices", h.getDevices, admin)
	siteU.POST("/devices/:deviceid", h.postDevice, admin)
	siteU.POST("/enroll_guest", h.postEnrollGuest, user)
	siteU.GET("/health", h.getHealth, user)
	siteU.GET("/network/vap", h.getNetworkVAP, user)
	siteU.GET("/network/dns", h.getNetworkDNS, user)
	siteU.GET("/network/vap/:vapname", h.getNetworkVAPName, user)
	siteU.POST("/network/vap/:vapname", h.postNetworkVAPName, admin)
	siteU.GET("/network/wan", h.getNetworkWan, admin)
	siteU.GET("/nodes", h.getNodes, admin)
	siteU.POST("/nodes/:nodeid", h.postNode, admin)
	siteU.POST("/nodes/:nodeid/ports/:portid", h.postNodePort, admin)
	siteU.GET("/users", h.getUsers, admin)
	siteU.GET("/users/:useruuid", h.getUserByUUID, admin)
	siteU.POST("/users/:useruuid", h.postUserByUUID, admin)
	siteU.DELETE("/users/:useruuid", h.deleteUserByUUID, admin)
	siteU.GET("/rings", h.getRings, admin)
	return h
}
