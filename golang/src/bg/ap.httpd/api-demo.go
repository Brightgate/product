/*
 * COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
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
	"net/http"
	"regexp"
	"strconv"
	"time"

	"bg/ap_common/device"
	"bg/common/cfgapi"

	"github.com/gorilla/mux"
	"github.com/pkg/errors"
	"github.com/pquerna/otp"
	"github.com/satori/uuid"
	"github.com/sethvargo/go-password/password"
	"github.com/sfreiberg/gotwilio"
	"github.com/ttacon/libphonenumber"
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
	Active         bool       `json:"active"`
}

type daScanInfo struct {
	Start  *time.Time `json:"start"`
	Finish *time.Time `json:"finish"`
}

type daDevice struct {
	HwAddr          string
	Manufacturer    string
	Model           string
	Kind            string
	Confidence      float64
	Ring            string
	HumanName       string
	DNSName         string
	DHCPExpiry      string
	IPv4Addr        string
	OSVersion       string
	OwnerName       string
	OwnerPhone      string
	MediaLink       string
	Active          bool
	Scans           map[string]daScanInfo
	Vulnerabilities map[string]daVulnInfo
}

type daDevices struct {
	DbgRequest string
	Devices    []daDevice
}

func buildDeviceResponse(hwaddr string, client *cfgapi.ClientInfo,
	scanMap cfgapi.ScanMap, vulnMap cfgapi.VulnMap) daDevice {

	var cd daDevice

	/* JavaScript from devices.vue:
	{
		device: 'Apple iPhone 8',
		network_name: 'nosy-neighbor',
		os_version: 'iOS 11.0.1',
		owner: 'unknown',
		activated: '',
		owner_phone: '',
		owner_email: '',
		media: '<img src="img/nova-solid-mobile-phone-1.png" width=32 height=32>'
	},
	*/
	cd.HwAddr = hwaddr

	if client.DNSName != "" {
		cd.HumanName = client.DNSName
		cd.DNSName = client.DNSName
	} else if client.DHCPName != "" {
		cd.HumanName = client.DHCPName
		cd.DNSName = "-"
	} else {
		cd.HumanName = "Unnamed"
		cd.DNSName = "-"
	}

	if client.IPv4 != nil {
		cd.IPv4Addr = client.IPv4.String()
	} else {
		cd.IPv4Addr = "-"
	}

	if client.Expires != nil {
		cd.DHCPExpiry = client.Expires.Format("2006-01-02T15:04")
	} else {
		cd.DHCPExpiry = "static"
	}

	cd.Ring = client.Ring
	cd.IPv4Addr = client.IPv4.String()
	cd.Active = client.IsActive()
	cd.Scans = make(map[string]daScanInfo)
	for k, v := range scanMap {
		cd.Scans[k] = daScanInfo{
			Start:  v.Start,
			Finish: v.Finish,
		}
	}

	cd.Vulnerabilities = make(map[string]daVulnInfo)
	for k, v := range vulnMap {
		cd.Vulnerabilities[k] = daVulnInfo{
			FirstDetected:  v.FirstDetected,
			LatestDetected: v.LatestDetected,
			Active:         v.Active,
		}
	}

	identity, err := strconv.Atoi(client.Identity)
	if err != nil {
		log.Printf("buildDeviceResponse unusual client identity '%v': %v\n", client.Identity, err)
		return cd
	}

	lpn, err := device.GetDeviceByID(config, identity)
	if err != nil {
		log.Printf("buildDeviceResponse couldn't lookup @/devices/%d: %v\n", identity, err)
	} else {
		cd.Manufacturer = lpn.Vendor
		cd.Model = lpn.ProductName
		cd.Kind = lpn.Devtype
		cd.Confidence = client.Confidence
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

	props, err := config.GetProps("@/rings")
	if err != nil {
		log.Printf("Failed to get ring list: %v\n", err)
		http.Error(w, "failed to get rings", 500)
		return
	}

	var rings []string
	for name := range props.Children {
		rings = append(rings, name)
	}

	b, err := json.Marshal(rings)
	if err != nil {
		log.Printf("failed to json marshal rings '%v': %v\n", rings, err)
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
	err = ui.Update()
	if err != nil {
		log.Printf("failed to save user '%s': %v\n", dau.UID, err)
		http.Error(w, fmt.Sprintf("failed to save: %v", err), 400)
		return
	}

	if dau.SetPassword != nil {
		if err = ui.SetPassword(*dau.SetPassword); err != nil {
			log.Printf("failed to set password for %s: %v\n", dau.UID, err)
			http.Error(w, "updated user but failed to set password", 400)
			return
		}
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

	j, err := json.Marshal(cu)
	if err != nil {
		log.Printf("failed to json marshal user '%v': %v\n", cu, err)
		http.Error(w, "bad request", 400)
		return
	}

	_, err = w.Write(j)
	if err != nil {
		log.Printf("failed to write user '%v': %v\n", j, err)
		http.Error(w, "bad request", 400)
		return
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

type enrollResponse struct {
	SMSDelivered bool    `json:"smsdelivered"`
	SMSErrorCode int     `json:"smserrorcode"`
	SMSError     string  `json:"smserror"`
	User         *daUser `json:"user"` // If a user was created
}

// sendOneSMS is a utility helper for the Enroll handler.
func sendOneSMS(twilio *gotwilio.Twilio, from, to, message string) (*enrollResponse, error) {
	var response *enrollResponse
	smsResponse, exception, err := twilio.SendSMS(from, to, message, "", "")
	if err != nil {
		return nil, err
	}
	if exception != nil {
		rstr := "Twilio failed sending SMS."
		if exception.Code >= 21210 && exception.Code <= 21217 {
			rstr = "Invalid Phone Number"
		}
		response = &enrollResponse{false, exception.Code, rstr, nil}
	} else {
		response = &enrollResponse{true, 0, "Current Status: " + smsResponse.Status, nil}
	}
	return response, nil
}

func mkTwilio() *gotwilio.Twilio {
	twilioSID := "ACaa018fa0f7631d585a56f6806a5bfc74"
	twilioAuthToken := "cfe70c8ed40429f0ba961189f554dc90"
	return gotwilio.NewTwilioClient(twilioSID, twilioAuthToken)
}

var guestMatcher = regexp.MustCompile("guest([0-9]+)")

func findGuestUID() (int, error) {
	var maxuid int
	props, err := config.GetProps("@/users")
	if err != nil {
		return 0, errors.Wrap(err, "Failed to get @/users")
	}
	for name := range props.Children {
		uid, err := config.GetProp(fmt.Sprintf("@/users/%s/uid", name))
		if err != nil {
			continue
		}
		matches := guestMatcher.FindStringSubmatch(uid)
		if matches == nil || len(matches) == 1 {
			continue
		}
		uidi, err := strconv.Atoi(matches[1])
		if err != nil {
			continue
		}
		if uidi > maxuid {
			maxuid = uidi
		}
	}
	return maxuid + 1, nil
}

func makePassword() (string, error) {
	p, err := password.Generate(16, 5, 2, false, false)
	return p, errors.Wrap(err, "failed to generate password")
}

func makeGuestUser(phone, email string) (*daUser, string, error) {
	guestNum, err := findGuestUID()
	if err != nil {
		return nil, "", err
	}
	uid := fmt.Sprintf("guest%d", guestNum)
	ui, err := config.NewUserInfo(uid)
	if err != nil {
		return nil, "", err
	}

	pass, err := makePassword()
	if err != nil {
		return nil, "", err
	}

	ui.TelephoneNumber = phone
	ui.Email = email
	ui.DisplayName = fmt.Sprintf("Guest User %d", guestNum)

	err = ui.Update()
	if err != nil {
		return nil, "", errors.Wrapf(err, "failed to save user %s", uid)
	}
	err = ui.SetPassword(pass)
	if err != nil {
		return nil, "", errors.Wrapf(err, "failed to set password for user %s", uid)
	}
	// Fetch out from config store
	user, err := config.GetUser(uid)
	if err != nil {
		return nil, "", errors.Wrapf(err, "failed to get user %s", uid)
	}
	userResponse := buildUserResponse(user)
	return &userResponse, pass, nil
}

func demoEnrollGuestHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	var err error

	t := time.Now()

	log.Printf("EAP Enroll Handler: phone='%v' email='%v'\n",
		r.PostFormValue("phone"), r.PostFormValue("email"))

	rType := r.PostFormValue("type")
	rEmail := r.PostFormValue("email")
	rPhone := r.PostFormValue("phone")

	if rType != "eap" && rType != "psk" {
		http.Error(w, "Invalid request, missing type={eap,psk}", 400)
		return
	}
	// XXX on the client side we do a basic validate on the email address--
	// should do that here too.
	if rType == "eap" && rEmail == "" {
		http.Error(w, "Invalid request, missing email", 400)
		return
	}
	if rPhone == "" {
		http.Error(w, "Invalid request, missing phone", 400)
		return
	}
	// XXX need to solve phone region eventually
	to, err := libphonenumber.Parse(rPhone, "US")
	if err != nil {
		response := enrollResponse{false, 0, "Invalid Phone Number", nil}
		if err = json.NewEncoder(w).Encode(response); err != nil {
			panic(err)
		}
		return
	}
	formattedTo := libphonenumber.Format(to, libphonenumber.INTERNATIONAL)
	from := "+16507694283"
	log.Printf("EAP Enroll Handler: from='%v' formattedTo='%v'\n", from, formattedTo)

	networkSSID, err := config.GetProp("@/network/ssid")
	if err != nil {
		http.Error(w, "Internal Error", 500)
		return
	}
	networkEAPSSID := networkSSID + "-eap"

	var daGuest *daUser
	var messages []string
	if rType == "eap" {
		var guestPass string
		daGuest, guestPass, err = makeGuestUser(rPhone, rEmail)
		if err != nil {
			log.Printf("EAP Enroll Handler: failed to make Guest: '%+v'\n", err)
			http.Error(w, "Internal Error", 500)
			return
		}
		log.Printf("made guest user %v", daGuest)
		// The SMS to the customer is structured as two messages, one with
		// help and the network name, and the other with the passphrase.
		// This is because on most iOS and Android SMS clients, it's easy to
		// copy a whole SMS message, but range selection is disabled.
		messages = []string{
			fmt.Sprintf("Brightgate Wi-Fi\nHelp: bit.ly/2yhPDQz\n"+
				"Network: %s\nUsername: %s\n<password follows>",
				networkEAPSSID, daGuest.UID),
			fmt.Sprintf("%s", guestPass),
		}
	} else {
		// See above for notes on structure
		networkPassphrase, _ := config.GetProp("@/network/passphrase")
		messages = []string{
			fmt.Sprintf("Brightgate Wi-Fi\nHelp: bit.ly/2yhPDQz\n"+
				"Network: %s\n<password follows>", networkSSID),
			fmt.Sprintf("%s", networkPassphrase),
		}
	}

	twilio := mkTwilio()
	var response *enrollResponse
	for _, message := range messages {
		response, err = sendOneSMS(twilio, from, formattedTo, message)
		if err != nil {
			log.Printf("Enroll Handler: twilio go err='%v'\n", err)
			http.Error(w, "Twilio Error", 500)
			return
		}
		// if not sent then give up sending more
		if response.SMSDelivered == false {
			break
		}
	}
	if rType == "eap" {
		response.User = daGuest
	}
	if err = json.NewEncoder(w).Encode(response); err != nil {
		panic(err)
	}

	if err == nil {
		metrics.latencies.Observe(time.Since(t).Seconds())
	}
}

func demoAppliancesHandler(w http.ResponseWriter, r *http.Request) {
	var appliances = []string{"0"}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(appliances); err != nil {
		panic(err)
	}
}

func demoAppliancesUUIDHandler(w http.ResponseWriter, r *http.Request) {
	var appliance = map[string]string{}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(appliance); err != nil {
		panic(err)
	}
}

func makeDemoAPIRouter() *mux.Router {
	router := mux.NewRouter()
	// Per-appliance operations
	router.HandleFunc("/appliances", demoAppliancesHandler).Methods("GET")
	router.HandleFunc("/appliances/{auuid}", demoAppliancesUUIDHandler).Methods("GET")
	router.HandleFunc("/appliances/{auuid}/alerts", demoAlertsHandler).Methods("GET")
	router.HandleFunc("/appliances/{auuid}/config", demoConfigGetHandler).Methods("GET")
	router.HandleFunc("/appliances/{auuid}/config", demoConfigPostHandler).Methods("POST")
	router.HandleFunc("/appliances/{auuid}/devices/{ring}", demoDevicesByRingHandler).Methods("GET")
	router.HandleFunc("/appliances/{auuid}/devices", demoDevicesHandler).Methods("GET")
	router.HandleFunc("/appliances/{auuid}/enroll_guest", demoEnrollGuestHandler).Methods("POST")
	router.HandleFunc("/appliances/{auuid}/rings", demoRingsHandler).Methods("GET")
	router.HandleFunc("/appliances/{auuid}/supreme", demoSupremeHandler).Methods("GET")
	router.HandleFunc("/appliances/{auuid}/users", demoUsersHandler).Methods("GET")
	router.HandleFunc("/appliances/{auuid}/users/{uuid}", demoUserByUUIDGetHandler).Methods("GET")
	router.HandleFunc("/appliances/{auuid}/users/{uuid}", demoUserByUUIDPostHandler).Methods("POST")
	router.HandleFunc("/appliances/{auuid}/users/{uuid}", demoUserByUUIDDeleteHandler).Methods("DELETE")
	router.Use(cookieAuthMiddleware)
	return router
}
