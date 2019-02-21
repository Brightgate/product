/*
 * COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package main

// Demo API implementation

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image/png"
	"log"
	"net"
	"net/http"
	"strconv"
	"time"

	"bg/common/cfgapi"
	"bg/common/deviceid"

	"github.com/gorilla/mux"
	"github.com/pquerna/otp"
	"github.com/satori/uuid"
)

// DAAlerts is a placeholder.
// XXX What would an Alert be?  A reference ID to a full Alert?
type daAlerts struct {
	DbgRequest string
	Alerts     []string
}

// GET alerts  () -> (...)
// Policy: GET(*_ADMIN)
// XXX Should a GUEST or USER be able to see the alerts that correspond
// to their behavior?
func demoAlertsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	rs := fmt.Sprintf("%v", r)
	as := daAlerts{
		//		Alerts:     {""},
		DbgRequest: rs,
	}
	b, err := json.Marshal(as)
	if err != nil {
		log.Printf("failed to json marshal alert '%v': %v\n", as, err)
		return
	}

	_, err = w.Write(b)
	if err != nil {
		log.Printf("failed to write json alert '%v': %v\n", b, err)
		return
	}
}

// Subset of cfgapi.VulnInfo
type daVulnInfo struct {
	FirstDetected  *time.Time `json:"first_detected"`
	LatestDetected *time.Time `json:"latest_detected"`
	Repaired       *time.Time `json:"repaired"`
	Active         bool       `json:"active"`
	Details        string     `json:"details"`
	Repair         *bool      `json:"repair,omitempty"`
}

type daScanInfo struct {
	Start  *time.Time `json:"start"`
	Finish *time.Time `json:"finish"`
}

type daDevice struct {
	HwAddr          string                `json:"HwAddr"`
	Manufacturer    string                `json:"Manufacturer"`
	Model           string                `json:"Model"`
	Kind            string                `json:"Kind"`
	Confidence      float64               `json:"Confidence"`
	Ring            string                `json:"Ring"`
	HumanName       string                `json:"HumanName,omitempty"`
	DNSName         string                `json:"DNSName,omitempty"`
	DHCPExpiry      string                `json:"DHCPExpiry,omitempty"`
	IPv4Addr        *net.IP               `json:"IPv4Addr,omitempty"`
	OSVersion       string                `json:"OSVersion,omitempty"`
	Active          bool                  `json:"Active"`
	ConnAuthType    string                `json:"ConnAuthType,omitempty"`
	ConnMode        string                `json:"ConnMode,omitempty"`
	ConnNode        *uuid.UUID            `json:"ConnNode,omitempty"`
	Scans           map[string]daScanInfo `json:"Scans,omitempty"`
	Vulnerabilities map[string]daVulnInfo `json:"Vulnerabilities,omitempty"`
}

type daDevices struct {
	DbgRequest string
	Devices    []daDevice
}

// mirrors RingConfig but omits Bridge and Vlan
type daRing struct {
	VirtualAP     string `json:"vap"`
	Subnet        string `json:"subnet"`
	LeaseDuration int    `json:"leaseDuration"`
}

type daRings map[string]daRing

func buildDeviceResponse(hwaddr string, client *cfgapi.ClientInfo,
	scanMap cfgapi.ScanMap, vulnMap cfgapi.VulnMap) daDevice {

	cd := daDevice{
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
		Scans:           make(map[string]daScanInfo),
		Vulnerabilities: make(map[string]daVulnInfo),
	}

	cd.HwAddr = hwaddr
	if client.DNSName != "" {
		cd.HumanName = client.DNSName
		cd.DNSName = client.DNSName
	} else if client.DHCPName != "" {
		cd.HumanName = client.DHCPName
		cd.DNSName = ""
	} else {
		cd.HumanName = ""
		cd.DNSName = ""
	}

	if client.Expires != nil {
		cd.DHCPExpiry = client.Expires.Format("2006-01-02T15:04")
	} else {
		cd.DHCPExpiry = "static"
	}

	for k, v := range scanMap {
		cd.Scans[k] = daScanInfo{
			Start:  v.Start,
			Finish: v.Finish,
		}
	}

	for k, v := range vulnMap {
		cd.Vulnerabilities[k] = daVulnInfo{
			FirstDetected:  v.FirstDetected,
			LatestDetected: v.LatestDetected,
			Repaired:       v.RepairedAt,
			Active:         v.Active,
			Details:        v.Details,
			Repair:         v.Repair,
		}
	}

	if client.Identity != "" {
		identity, err := strconv.Atoi(client.Identity)
		if err != nil {
			log.Printf("buildDeviceResponse unusual client identity '%v': %v\n", client.Identity, err)
			return cd
		}

		lpn, err := deviceid.GetDeviceByID(config, identity)
		if err != nil {
			log.Printf("buildDeviceResponse couldn't lookup @/devices/%d: %v\n", identity, err)
		} else {
			cd.Manufacturer = lpn.Vendor
			cd.Model = lpn.ProductName
			cd.Kind = lpn.Devtype
		}
	}

	return cd
}

// GET devices on ring (ring) -> (...)
// Policy: GET (*_USER, *_ADMIN)
func demoDevicesByRingHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	vars := mux.Vars(r)

	clientsRaw := config.GetClients()
	var devices daDevices

	for mac, client := range clientsRaw {
		var cd daDevice

		if client.Ring != vars["ring"] {
			continue
		}

		scans := config.GetClientScans(mac)
		vulns := config.GetVulnerabilities(mac)
		cd = buildDeviceResponse(mac, client, scans, vulns)

		devices.Devices = append(devices.Devices, cd)
	}

	devices.DbgRequest = fmt.Sprintf("%v", r)

	b, err := json.Marshal(devices)
	if err != nil {
		log.Printf("failed to json marshal ring devices '%v': %v\n", devices, err)
		return
	}

	_, err = w.Write(b)
	if err != nil {
		log.Printf("failed to write ring devices '%v': %v\n", b, err)
		return
	}
}

// GET rings () -> (...)
func demoRingsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var resp daRings = make(map[string]daRing)
	for ringName, ring := range config.GetRings() {
		resp[ringName] = daRing{
			VirtualAP:     ring.VirtualAP,
			Subnet:        ring.Subnet,
			LeaseDuration: ring.LeaseDuration,
		}
	}

	b, err := json.Marshal(resp)
	if err != nil {
		log.Printf("failed to json marshal response '%v': %v\n", resp, err)
		return
	}

	_, err = w.Write(b)
	if err != nil {
		log.Printf("failed to write rings '%v': %v\n", b, err)
		return
	}
}

// GET devices () -> (...)
// Policy: GET (*_USER, *_ADMIN)
func demoDevicesHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	clientsRaw := config.GetClients()
	var devices daDevices

	for mac, client := range clientsRaw {
		var cd daDevice

		scans := config.GetClientScans(mac)
		vulns := config.GetVulnerabilities(mac)
		cd = buildDeviceResponse(mac, client, scans, vulns)

		devices.Devices = append(devices.Devices, cd)
	}

	devices.DbgRequest = fmt.Sprintf("%v", r)

	b, err := json.Marshal(devices)
	if err != nil {
		log.Printf("failed to json marshal devices '%v': %v\n", devices, err)
		return
	}

	_, err = w.Write(b)
	if err != nil {
		log.Printf("failed to write devices '%v': %v\n", b, err)
		return
	}
}

// GET requests moves all unenrolled clients to standard.
func demoSupremeHandler(w http.ResponseWriter, r *http.Request) {
	clientsRaw := config.GetClients()
	count := 0

	for mac, client := range clientsRaw {
		if client.Ring == "unenrolled" {
			rp := fmt.Sprintf("@/clients/%s/ring", mac)
			err := config.SetProp(rp, "standard", nil)
			if err != nil {
				log.Printf("supreme set %v to standard failed: %v\n", rp, err)
				continue
			}
			log.Printf("supreme %v moved to standard\n", mac)
			count++
		}
	}

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, "{\"demoApiHandler\": \"GET supreme\", \"request\": \"%v\", \"changed\": \"%v\"}\n", r, count)
}

func demoConfigGetHandler(w http.ResponseWriter, r *http.Request) {
	t := time.Now()

	// Get setting from ap.configd
	//
	// From the command line:
	//     wget -q -O- http://127.0.0.1:8000/config?@/network/wlan0/ssid
	val, err := config.GetProp(r.URL.RawQuery)
	if err != nil {
		estr := fmt.Sprintf("%v", err)
		http.Error(w, estr, 400)
	} else {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, "\"%s\"", val)
	}
	if err == nil {
		metrics.latencies.Observe(time.Since(t).Seconds())
	}
}

func demoConfigPostHandler(w http.ResponseWriter, r *http.Request) {
	var ops []cfgapi.PropertyOp

	// Send property updates to ap.configd
	//
	// From the command line:
	//    wget -q --post-data '@/network/wlan0/ssid=newssid' \
	//           http://127.0.0.1:8000/config

	t := time.Now()

	err := r.ParseForm()
	if err != nil {
		http.Error(w, "failed to parse form", 400)
		return
	}
	for key, values := range r.Form {
		if len(values) != 1 {
			http.Error(w, "Properties may only have one value", 400)
			return
		}
		ops = append(ops, cfgapi.PropertyOp{
			Op:    cfgapi.PropCreate,
			Name:  key,
			Value: values[0],
		})
	}
	if len(ops) == 0 {
		return
	}
	_, err = config.Execute(nil, ops).Wait(nil)
	if err != nil {
		log.Printf("failed to set properties: %v", err)
		http.Error(w, "failed to set properties", 400)
	}

	if err == nil {
		metrics.latencies.Observe(time.Since(t).Seconds())
	}
}

type daUser struct {
	DbgRequest        string
	UID               string
	UUID              *uuid.UUID
	Role              *string
	DisplayName       *string
	Email             *string
	TelephoneNumber   *string
	PreferredLanguage *string
	SelfProvisioning  bool
	HasTOTP           bool
	HasPassword       bool
	SetPassword       *string
}

type daUsers struct {
	DbgRequest string
	Users      map[string]daUser
}

func buildUserResponse(user *cfgapi.UserInfo) daUser {
	var cu daUser

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

	return cu
}

//	demoAPIRouter.HandleFunc("/users/{uid}/otp"
func demoUserByUIDOTPQRHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	// uid
	uid := vars["uid"]
	// GetUser(uid)
	user, err := config.GetUser(uid)
	if err != nil {
		log.Printf("no such user '%v': %v\n", uid, err)
		http.Error(w, "not found", 404)
		return
	}

	// Encode TOTP field using module
	key, err := otp.NewKeyFromURL(user.TOTP)
	if err != nil {
		log.Printf("cannot convert TOTP to key: %v\n", err)
		http.Error(w, "internal server error", 500)
		return
	}

	// Convert TOTP key into a PNG
	var buf bytes.Buffer
	img, err := key.Image(200, 200)
	if err != nil {
		panic(err)
	}
	png.Encode(&buf, img)

	// Header: application/png
	w.Write(buf.Bytes())
}

// demoUserByUUIDGetHandler returns a JSON-formatted user object for the
// requested user uuid, typically in response to a GET request to
// "[demo_api_root]/users/{uuid}".
//
func demoUserByUUIDGetHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	vars := mux.Vars(r)

	// XXX what uuid if not present?
	ruuid, err := uuid.FromString(vars["uuid"])
	if err != nil {
		log.Printf("bad UUID %s: %v", vars["uuid"], err)
		http.Error(w, "bad uuid", 400)
		return
	}

	userRaw, err := config.GetUserByUUID(ruuid)
	if err != nil {
		log.Printf("no such user '%v': %v\n", ruuid, err)
		http.Error(w, "not found", 404)
		return
	}

	cu := buildUserResponse(userRaw)
	cu.DbgRequest = fmt.Sprintf("%v", r)

	b, err := json.Marshal(cu)
	if err != nil {
		log.Printf("failed to json marshal user '%v': %v\n", cu, err)
		http.Error(w, "bad request", 400)
		return
	}

	_, err = w.Write(b)
	if err != nil {
		log.Printf("failed to write user '%v': %v\n", b, err)
		http.Error(w, "bad request", 400)
		return
	}
}

// demoUserByUUIDPostHandler updates the user requested, using the
// JSON-formatted user object supplied.  It returns the updated record.
// The user must already exist.
//
func demoUserByUUIDPostHandler(w http.ResponseWriter, r *http.Request) {
	var err error
	w.Header().Set("Content-Type", "application/json")
	vars := mux.Vars(r)

	var dau daUser
	if err = json.NewDecoder(r.Body).Decode(&dau); err != nil {
		log.Printf("daUser decode failed: %v", err)
		http.Error(w, "invalid user", 400)
		return
	}

	var ui *cfgapi.UserInfo
	log.Printf("vars[uuid] = '%s'", vars["uuid"])
	if vars["uuid"] == "NEW" {
		ui, err = config.NewUserInfo(dau.UID)
		if err != nil {
			log.Printf("config.NewUserInfo(%v): %v:", dau.UID, err)
			http.Error(w, "invalid uid or user exists", 400)
			return
		}
	} else {
		var ruuid uuid.UUID
		ruuid, err = uuid.FromString(vars["uuid"])
		if err != nil {
			http.Error(w, "invalid uuid", 400)
			return
		}
		ui, err = config.GetUserByUUID(ruuid)
		if err != nil {
			log.Printf("config.GetUserByUUID(%v): %v:", ruuid, err)
			http.Error(w, "invalid or unknown user", 400)
			return
		}
	}

	// propagate daUser to UserInfo
	if dau.DisplayName != nil {
		ui.DisplayName = *dau.DisplayName
	}
	if dau.Email != nil {
		ui.Email = *dau.Email
	}
	if dau.PreferredLanguage != nil {
		ui.PreferredLanguage = *dau.PreferredLanguage
	}
	if dau.TelephoneNumber != nil {
		ui.TelephoneNumber = *dau.TelephoneNumber
	}
	if dau.Role != nil {
		ui.Role = *dau.Role
	}
	var extraOps []cfgapi.PropertyOp
	if dau.SetPassword != nil {
		extraOps, err = ui.PropOpsFromPassword(*dau.SetPassword)
		if err != nil {
			log.Printf("failed to get generate PropOps from password")
			http.Error(w, "unexpected failure", 500)
			return
		}
	}
	err = ui.Update(extraOps...)
	if err != nil {
		log.Printf("failed to save user '%s': %v\n", dau.UID, err)
		http.Error(w, fmt.Sprintf("failed to save: %v", err), 500)
		return
	}

	// Reget to reflect password, etc. changes from backend
	ui, err = config.GetUserByUUID(ui.UUID)
	if err != nil {
		log.Printf("failed to get user by uuid: %v\n", err)
		http.Error(w, "unexpected failure", 500)
		return
	}

	cu := buildUserResponse(ui)
	cu.DbgRequest = fmt.Sprintf("%v", r)

	if err = json.NewEncoder(w).Encode(cu); err != nil {
		panic(err)
	}
}

// demoUserByUUIDDeleteHandler removes the user requested
// The user must already exist.
//
func demoUserByUUIDDeleteHandler(w http.ResponseWriter, r *http.Request) {
	var err error
	w.Header().Set("Content-Type", "application/json")
	vars := mux.Vars(r)

	ruuid, err := uuid.FromString(vars["uuid"])
	if err != nil {
		http.Error(w, "invalid uuid", 400)
		return
	}
	ui, err := config.GetUserByUUID(ruuid)
	if err != nil {
		log.Printf("config.GetUserByUUID(%v): %v:", ruuid, err)
		http.Error(w, "invalid or unknown user", 400)
		return
	}

	err = ui.Delete()
	if err != nil {
		log.Printf("failed to delete user '%s': %v\n", ui.UID, err)
		http.Error(w, "failed to delete user", 400)
		return
	}
}

// demoUsersHandler returns a JSON-formatted map of configured users, keyed by
// UUID, typically in response to a GET request to "[demo_api_root]/users".
func demoUsersHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var users daUsers
	users.Users = make(map[string]daUser)

	for _, user := range config.GetUsers() {
		cu := buildUserResponse(user)
		users.Users[user.UUID.String()] = cu
	}

	users.DbgRequest = fmt.Sprintf("%v", r)

	b, err := json.Marshal(users)
	if err != nil {
		log.Printf("failed to json marshal users '%v': %v\n", users, err)
		http.Error(w, "bad request", 400)
		return
	}

	_, err = w.Write(b)
	if err != nil {
		log.Printf("failed to write users '%v': %v\n", b, err)
		http.Error(w, "bad request", 400)
		return
	}
}

type daSite struct {
	// The site breaks the UUID contract by using '0' as its
	// reserved UUID; hence we have to use a string here.
	UUID string `json:"uuid"`
	Name string `json:"name"`
}

var site0 = daSite{
	UUID: "0",
	Name: "Local Site",
}

func demoSitesHandler(w http.ResponseWriter, r *http.Request) {
	var sites = []daSite{site0}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(sites); err != nil {
		panic(err)
	}
}

func demoSitesUUIDHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(site0); err != nil {
		panic(err)
	}
}

func makeDemoAPIRouter() *mux.Router {
	router := mux.NewRouter()
	// Per-site operations
	router.HandleFunc("/sites", demoSitesHandler).Methods("GET")
	router.HandleFunc("/sites/{s}", demoSitesUUIDHandler).Methods("GET")
	router.HandleFunc("/sites/{s}/alerts", demoAlertsHandler).Methods("GET")
	router.HandleFunc("/sites/{s}/config", demoConfigGetHandler).Methods("GET")
	router.HandleFunc("/sites/{s}/config", demoConfigPostHandler).Methods("POST")
	router.HandleFunc("/sites/{s}/devices/{ring}", demoDevicesByRingHandler).Methods("GET")
	router.HandleFunc("/sites/{s}/devices", demoDevicesHandler).Methods("GET")
	router.HandleFunc("/sites/{s}/rings", demoRingsHandler).Methods("GET")
	router.HandleFunc("/sites/{s}/supreme", demoSupremeHandler).Methods("GET")
	router.HandleFunc("/sites/{s}/users", demoUsersHandler).Methods("GET")
	router.HandleFunc("/sites/{s}/users/{uuid}", demoUserByUUIDGetHandler).Methods("GET")
	router.HandleFunc("/sites/{s}/users/{uuid}", demoUserByUUIDPostHandler).Methods("POST")
	router.HandleFunc("/sites/{s}/users/{uuid}", demoUserByUUIDDeleteHandler).Methods("DELETE")
	router.Use(cookieAuthMiddleware)
	return router
}
