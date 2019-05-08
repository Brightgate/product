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
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"strconv"
	"time"

	"bg/common/cfgapi"
	"bg/common/deviceid"

	"github.com/gorilla/mux"
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
	ConnVAP         string                `json:"ConnVAP,omitempty"`
	ConnBand        string                `json:"ConnBand,omitempty"`
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
		ConnVAP:         client.ConnVAP,
		ConnBand:        client.ConnBand,
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

func demoVAPGetHandler(w http.ResponseWriter, r *http.Request) {
	vapNames := make([]string, 0)
	vaps := config.GetVirtualAPs()
	for vapName := range vaps {
		vapNames = append(vapNames, vapName)
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(&vapNames); err != nil {
		panic(err)
	}
}

func demoVAPNameGetHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	vaps := config.GetVirtualAPs()
	vap, ok := vaps[vars["vapname"]]
	if !ok {
		http.Error(w, "no such vap", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(&vap); err != nil {
		panic(err)
	}
}

type demoVAPUpdate struct {
	SSID       string `json:"ssid"`
	Passphrase string `json:"passphrase"`
}

func demoVAPNamePostHandler(w http.ResponseWriter, r *http.Request) {
	var err error
	vars := mux.Vars(r)

	var dvu demoVAPUpdate
	if err = json.NewDecoder(r.Body).Decode(&dvu); err != nil {
		log.Printf("demoVAPUpdate decode failed: %v", err)
		http.Error(w, "bad vap", http.StatusBadRequest)
		return
	}

	vaps := config.GetVirtualAPs()
	vap, ok := vaps[vars["vapname"]]
	if !ok {
		http.Error(w, "no such vap", http.StatusNotFound)
		return
	}

	var ops []cfgapi.PropertyOp
	if dvu.SSID != "" && vap.SSID != dvu.SSID {
		ops = append(ops, cfgapi.PropertyOp{
			Op:    cfgapi.PropCreate,
			Name:  fmt.Sprintf("@/network/vap/%s/ssid", vars["vapname"]),
			Value: dvu.SSID,
		})
	}
	if dvu.Passphrase != "" && vap.Passphrase != dvu.Passphrase {
		ops = append(ops, cfgapi.PropertyOp{
			Op:    cfgapi.PropCreate,
			Name:  fmt.Sprintf("@/network/vap/%s/passphrase", vars["vapname"]),
			Value: dvu.Passphrase,
		})
	}
	if len(ops) == 0 {
		return
	}
	_, err = config.Execute(r.Context(), ops).Wait(r.Context())
	if err != nil {
		http.Error(w, "failed to set properties", http.StatusBadRequest)
		return
	}
	return
}

// Implements GET /api/site/:uuid/network/wan, returning information about the
// WAN link
func demoWanGetHandler(w http.ResponseWriter, r *http.Request) {
	wan := config.GetWanInfo()
	if wan == nil {
		wan = &cfgapi.WanInfo{}
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(&wan); err != nil {
		panic(err)
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
	cu.HasPassword = user.Password != ""

	// XXX We are not reporting our password back in this
	// call.

	cu.SelfProvisioning = user.SelfProvisioning

	return cu
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

	if err = json.NewEncoder(w).Encode(cu); err != nil {
		panic(err)
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
	hdl, err := ui.Update(extraOps...)
	if err != nil {
		log.Printf("failed to setup update of user '%s': %v\n", dau.UID, err)
		http.Error(w, fmt.Sprintf("failed to save: %v", err), 500)
		return
	}
	_, err = hdl.Wait(r.Context())
	if err != nil {
		log.Printf("update wait failed '%s': %v\n", dau.UID, err)
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
	UUID  string   `json:"uuid"`
	Name  string   `json:"name"`
	Roles []string `json:"roles"`
}

var site0 = daSite{
	UUID:  "0",
	Name:  "Local Site",
	Roles: []string{"admin"},
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
	router.HandleFunc("/sites/{s}/network/vap", demoVAPGetHandler).Methods("GET")
	router.HandleFunc("/sites/{s}/network/vap/{vapname}", demoVAPNameGetHandler).Methods("GET")
	router.HandleFunc("/sites/{s}/network/vap/{vapname}", demoVAPNamePostHandler).Methods("POST")
	router.HandleFunc("/sites/{s}/network/wan", demoWanGetHandler).Methods("GET")
	router.HandleFunc("/sites/{s}/rings", demoRingsHandler).Methods("GET")
	router.HandleFunc("/sites/{s}/supreme", demoSupremeHandler).Methods("GET")
	router.HandleFunc("/sites/{s}/users", demoUsersHandler).Methods("GET")
	router.HandleFunc("/sites/{s}/users/{uuid}", demoUserByUUIDGetHandler).Methods("GET")
	router.HandleFunc("/sites/{s}/users/{uuid}", demoUserByUUIDPostHandler).Methods("POST")
	router.HandleFunc("/sites/{s}/users/{uuid}", demoUserByUUIDDeleteHandler).Methods("DELETE")
	router.Use(cookieAuthMiddleware)
	return router
}
