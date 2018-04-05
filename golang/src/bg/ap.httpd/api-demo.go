/*
 * COPYRIGHT 2017 Brightgate Inc.  All rights reserved.
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
	"io"
	"log"
	"net/http"
	"strconv"
	"time"

	"golang.org/x/crypto/bcrypt"

	"bg/ap_common/apcfg"

	"github.com/gorilla/mux"
	"github.com/pquerna/otp"
	"github.com/satori/uuid"
	"github.com/sfreiberg/gotwilio"
	"github.com/ttacon/libphonenumber"
)

const (
	cookieName = "com.brightgate.appliance"
)

// DAAlerts is a placeholder.
// XXX What would an Alert be?  A reference ID to a full Alert?
type daAlerts struct {
	DbgRequest string
	Alerts     []string
}

// POST login () -> (...)
// POST uid, userPassword[, totppass]
func demoLoginHandler(w http.ResponseWriter, r *http.Request) {
	err := r.ParseForm()
	if err != nil {
		log.Printf("cannot parse form: %v\n", err)
		http.Error(w, "bad request", 400)
		return
	}

	// Must have user and password.
	uids, present := r.Form["uid"]
	if !present || len(uids) == 0 {
		log.Printf("incomplete form, uid\n")
		http.Error(w, "bad request", 400)
		return
	}

	uid := uids[0]
	if len(uids) > 1 {
		log.Printf("multiple uids in form submission: %v\n", uids)
		http.Error(w, "bad request", 400)
	}

	userPasswords, present := r.Form["userPassword"]
	if !present || len(userPasswords) == 0 {
		log.Printf("incomplete form, userPassword\n")
		http.Error(w, "bad request", 400)
		return
	}

	userPassword := userPasswords[0]
	if len(userPasswords) > 1 {
		log.Printf("multiple userPasswords in form submission: %v\n", userPasswords)
		http.Error(w, "bad request", 400)
	}

	// Retrieve user record
	ui, err := config.GetUser(uid)
	if err != nil {
		log.Printf("demo login for '%s' denied: %v\n", uid, err)
		http.Error(w, "login denied", 401)
		return
	}

	cmp := bcrypt.CompareHashAndPassword([]byte(ui.Password),
		[]byte(userPassword))
	if cmp != nil {
		log.Printf("demo login for '%s' denied: password comparison\n", uid)
		http.Error(w, "login denied", 401)
		return
	}

	// XXX How would 2FA work?  If TOTP defined for this user, send
	// back 2FA required?

	filling := map[string]string{
		"uid": uid,
	}

	if encoded, err := cutter.Encode(cookieName, filling); err == nil {
		cookie := &http.Cookie{
			Name:  cookieName,
			Value: encoded,
			// Default lifetime is 30 days.
		}

		if cookie.String() == "" {
			log.Printf("cookie is empty and will be dropped: %v -> %v\n", cookie, cookie.String())
		}

		http.SetCookie(w, cookie)

	} else {
		log.Printf("cookie encoding failed: %v\n", err)
	}

	io.WriteString(w, "OK login\n")
}

// GET logout () -> (...)
func demoLogoutHandler(w http.ResponseWriter, r *http.Request) {
	var value map[string]string

	// XXX Should only logout if logged in.
	if cookie, err := r.Cookie(cookieName); err == nil {
		value = make(map[string]string)
		if err = cutter.Decode(cookieName, cookie.Value, &value); err == nil {
			log.Printf("Logging out '%s'\n", value["uid"])
		} else {
			log.Printf("Could not decode cookie\n")
			http.Error(w, "bad request", 400)
			return
		}
	} else {
		// No cookie defined.
		log.Printf("Could not find cookie for logout\n")
		http.Error(w, "bad request", 400)
		return
	}

	filling := map[string]string{
		"uid": "",
	}

	if encoded, err := cutter.Encode(cookieName, filling); err == nil {
		cookie := &http.Cookie{
			Name:   cookieName,
			Value:  encoded,
			MaxAge: -1,
		}
		http.SetCookie(w, cookie)
	}

	io.WriteString(w, "OK logout\n")
}

func getRequestUID(r *http.Request) string {
	var value map[string]string

	cookie, err := r.Cookie(cookieName)
	if err != nil {
		// No cookie.
		return ""
	}

	value = make(map[string]string)
	if err = cutter.Decode(cookieName, cookie.Value, &value); err != nil {
		log.Printf("request contains undecryptable cookie value: %v\n", err)
		return ""
	}

	// Lookup uid.
	uid := value["uid"]

	// Retrieve user node.
	ui, err := config.GetUser(uid)
	if err != nil {
		log.Printf("demo login for '%s' denied: %v\n", uid, err)
		return ""
	}

	if ui.Password != "" {
		return ui.UID
	}

	// Accounts with empty passwords can't be logged into.
	return ""
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

type daDevice struct {
	HwAddr       string
	Manufacturer string
	Model        string
	Kind         string
	Ring         string
	HumanName    string
	DNSName      string
	DHCPExpiry   string
	IPv4Addr     string
	OSVersion    string
	OwnerName    string
	OwnerPhone   string
	MediaLink    string
}

type daDevices struct {
	DbgRequest string
	Devices    []daDevice
}

func buildDeviceResponse(hwaddr string, client *apcfg.ClientInfo) daDevice {
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

	identity, err := strconv.Atoi(client.Identity)
	if err != nil {
		log.Printf("buildDeviceResponse unusual client identity '%v': %v\n", client.Identity, err)
		return cd
	}

	lpn, err := config.GetDevice(identity)
	if err != nil {
		log.Printf("buildDeviceResponse couldn't lookup @/devices/%d: %v\n", identity, err)
	} else {
		cd.Manufacturer = lpn.Vendor
		cd.Model = lpn.ProductName
		cd.Kind = lpn.Devtype
	}

	// XXX We are not reporting our confidence value back.  Maybe of
	// use in UX.

	return cd
}

// GET devices on ring (ring) -> (...)
// Policy: GET (*_USER, *_ADMIN)
func demoDevicesByRingHandler(w http.ResponseWriter, r *http.Request) {
	uid := getRequestUID(r)
	log.Printf("/devices [uid '%s']\n", uid)
	if uid == "" {
		http.Error(w, "forbidden", 403)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	vars := mux.Vars(r)

	clientsRaw := config.GetClients()
	var devices daDevices

	for mac, client := range clientsRaw {
		var cd daDevice

		if client.Ring != vars["ring"] {
			continue
		}

		cd = buildDeviceResponse(mac, client)

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
	uid := getRequestUID(r)

	log.Printf("/devices [uid '%s']\n", uid)

	if uid == "" {
		http.Error(w, "forbidden", 403)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	clientsRaw := config.GetClients()
	var devices daDevices

	for mac, client := range clientsRaw {
		var cd daDevice

		cd = buildDeviceResponse(mac, client)

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

	uid := getRequestUID(r)
	log.Printf("/supreme [uid '%s']\n", uid)
	if uid == "" {
		http.Error(w, "forbidden", 403)
		return
	}

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
	uid := getRequestUID(r)
	log.Printf("/config GET [uid '%s']\n", uid)
	if uid == "" {
		http.Error(w, "forbidden", 403)
		return
	}

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
		latencies.Observe(time.Since(t).Seconds())
	}
}

func demoConfigPostHandler(w http.ResponseWriter, r *http.Request) {
	var ops []apcfg.PropertyOp

	uid := getRequestUID(r)
	log.Printf("/config POST [uid '%s']\n", uid)
	if uid == "" {
		http.Error(w, "forbidden", 403)
		return
	}

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
		ops = append(ops, apcfg.PropertyOp{
			Op:    apcfg.PropCreate,
			Name:  key,
			Value: values[0],
		})
	}
	if len(ops) == 0 {
		return
	}
	_, err = config.Execute(ops)
	if err != nil {
		log.Printf("failed to set properties: %v", err)
		http.Error(w, "failed to set properties", 400)
	}

	if err == nil {
		latencies.Observe(time.Since(t).Seconds())
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

func buildUserResponse(user *apcfg.UserInfo) daUser {
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
// requested uuid, typically in response to a GET request to
// "[demo_api_root]/users/{uuid}".
//
func demoUserByUUIDGetHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	uid := getRequestUID(r)
	log.Printf("/users/{uuid} [uid '%s']\n", uid)
	if uid == "" {
		http.Error(w, "forbidden", 403)
		return
	}

	// XXX what uuid if not present?
	vars := mux.Vars(r)
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

	uid := getRequestUID(r)
	log.Printf("/users/{uuid} [uid '%s']\n", uid)
	if uid == "" {
		http.Error(w, "forbidden", 403)
		return
	}
	vars := mux.Vars(r)

	var dau daUser
	if err = json.NewDecoder(r.Body).Decode(&dau); err != nil {
		log.Printf("daUser decode failed: %v", err)
		http.Error(w, "invalid user", 400)
		return
	}

	var ui *apcfg.UserInfo
	log.Printf("vars[uuid] = '%s'", vars["uuid"])
	if vars["uuid"] == "NEW" {
		ui, err = config.NewUserInfo(dau.UID)
		if err != nil {
			log.Printf("config.NewUserInfo(%v): %v:", uid, err)
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

	uid := getRequestUID(r)
	log.Printf("/users/{uuid} [uid '%s']\n", uid)
	if uid == "" {
		http.Error(w, "forbidden", 403)
		return
	}
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

	uid := getRequestUID(r)
	log.Printf("/users [uid '%s']\n", uid)
	if uid == "" {
		http.Error(w, "forbidden", 403)
		return
	}

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
	SMSDelivered bool   `json:"smsdelivered"`
	SMSErrorCode int    `json:"smserrorcode"`
	SMSError     string `json:"smserror"`
}

// sendOneSMS is a utility helper for the Enroll handler.
func sendOneSMS(twilio *gotwilio.Twilio, from string, to string, message string) (*enrollResponse, error) {
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
		response = &enrollResponse{false, exception.Code, rstr}
	} else {
		response = &enrollResponse{true, 0,
			"Current Status: " + smsResponse.Status}
	}
	return response, nil
}

func demoEnrollHandler(w http.ResponseWriter, r *http.Request) {
	var err error

	t := time.Now()

	uid := getRequestUID(r)
	log.Printf("/enroll [uid '%s']\n", uid)
	if uid == "" {
		http.Error(w, "forbidden", 403)
		return
	}

	twilioSID := "ACaa018fa0f7631d585a56f6806a5bfc74"
	twilioAuthToken := "cfe70c8ed40429f0ba961189f554dc90"
	from := "+16507694283"

	networkSSID, err := config.GetProp("@/network/ssid")
	if err != nil {
		http.Error(w, "Internal Error", 500)
	}
	networkPassphrase, err := config.GetProp("@/network/passphrase")

	// The SMS to the customer is structured as two messages, one with
	// help and the network name, and the other with the passphrase.
	// This is because on most iOS and Android SMS clients, it's easy to
	// copy a whole SMS message, but range selection is disabled.
	message1 := fmt.Sprintf("Brightgate Wi-Fi\nHelp: bit.ly/2yhPDQz\n"+
		"Network: %s\n<password follows>", networkSSID)
	message2 := fmt.Sprintf("%s", networkPassphrase)

	log.Printf("Enroll Handler: phone='%v'\n", r.PostFormValue("phone"))
	if r.PostFormValue("phone") == "" {
		http.Error(w, "Invalid request.", 400)
		return
	}

	to, err := libphonenumber.Parse(r.PostFormValue("phone"), "US")
	if err != nil {
		response := enrollResponse{false, 0, "Invalid Phone Number"}
		if err := json.NewEncoder(w).Encode(response); err != nil {
			panic(err)
		}
		return
	}
	formattedTo := libphonenumber.Format(to, libphonenumber.INTERNATIONAL)
	log.Printf("Enroll Handler: formattedTo='%v'\n", formattedTo)

	twilio := gotwilio.NewTwilioClient(twilioSID, twilioAuthToken)
	var response *enrollResponse
	for _, message := range []string{message1, message2} {
		response, err = sendOneSMS(twilio, from, formattedTo, message)
		if err != nil {
			log.Printf("Enroll Handler: twilio go err='%v'\n", err)
			http.Error(w, "Twilio Error.", 500)
			return
		}
		// if not sent then give up sending more
		if response.SMSDelivered == false {
			break
		}
	}
	if err := json.NewEncoder(w).Encode(response); err != nil {
		panic(err)
	}

	if err == nil {
		latencies.Observe(time.Since(t).Seconds())
	}
}

// StatsContent contains information for filling out the stats request
// Policy: GET(*)
type StatsContent struct {
	URLPath string

	NPings     string
	NConfigs   string
	NEntities  string
	NResources string
	NRequests  string

	Host string
}

func demoStatsHandler(w http.ResponseWriter, r *http.Request) {
	lt := time.Now()

	conf := StatsContent{
		URLPath:    r.URL.Path,
		NPings:     strconv.Itoa(pings),
		NConfigs:   strconv.Itoa(configs),
		NEntities:  strconv.Itoa(entities),
		NResources: strconv.Itoa(resources),
		NRequests:  strconv.Itoa(requests),
		Host:       r.Host,
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(conf); err != nil {
		http.Error(w, "Internal server error", 501)
		return
	}

	latencies.Observe(time.Since(lt).Seconds())
}

func makeDemoAPIRouter() *mux.Router {
	router := mux.NewRouter()
	router.HandleFunc("/alerts", demoAlertsHandler).Methods("GET")
	router.HandleFunc("/config", demoConfigGetHandler).Methods("GET")
	router.HandleFunc("/config", demoConfigPostHandler).Methods("POST")
	router.HandleFunc("/devices/{ring}", demoDevicesByRingHandler).Methods("GET")
	router.HandleFunc("/devices", demoDevicesHandler).Methods("GET")
	router.HandleFunc("/enroll", demoEnrollHandler).Methods("POST")
	router.HandleFunc("/login", demoLoginHandler).Methods("POST")
	router.HandleFunc("/logout", demoLogoutHandler).Methods("GET")
	router.HandleFunc("/rings", demoRingsHandler).Methods("GET")
	router.HandleFunc("/supreme", demoSupremeHandler).Methods("GET")
	router.HandleFunc("/users", demoUsersHandler).Methods("GET")
	router.HandleFunc("/users/{uuid}", demoUserByUUIDGetHandler).Methods("GET")
	router.HandleFunc("/users/{uuid}", demoUserByUUIDPostHandler).Methods("POST")
	router.HandleFunc("/users/{uuid}", demoUserByUUIDDeleteHandler).Methods("DELETE")
	router.HandleFunc("/stats", demoStatsHandler).Methods("GET")
	return router
}
