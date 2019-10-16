/*
 * COPYRIGHT 2020 Brightgate Inc.  All rights reserved.
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
	"time"

	"bg/common/cfgapi"
	"bg/common/mfg"
	"bg/common/network"
	"bg/common/vpn"

	"github.com/gorilla/mux"
	"github.com/pkg/errors"
	"github.com/satori/uuid"
)

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
	HwAddr          string                `json:"hwAddr"`
	Ring            string                `json:"ring"`
	DisplayName     string                `json:"displayName"`
	DNSName         string                `json:"dnsName,omitempty"`
	DHCPName        string                `json:"dhcpName"`
	DHCPExpiry      string                `json:"dhcpExpiry,omitempty"`
	FriendlyName    string                `json:"friendlyName,omitempty"`
	FriendlyDNS     string                `json:"friendlyDNS,omitempty"`
	IPv4Addr        *net.IP               `json:"ipv4Addr,omitempty"`
	OSVersion       string                `json:"osVersion,omitempty"`
	Active          bool                  `json:"active"`
	Wireless        bool                  `json:"wireless"`
	ConnBand        string                `json:"connBand,omitempty"`
	ConnNode        string                `json:"connNode,omitempty"`
	ConnVAP         string                `json:"connVAP,omitempty"`
	Username        string                `json:"username,omitempty"`
	AllowedRings    []string              `json:"allowedRings"`
	DevID           *cfgapi.DevIDInfo     `json:"devID,omitempty"`
	Scans           map[string]daScanInfo `json:"scans,omitempty"`
	Vulnerabilities map[string]daVulnInfo `json:"vulnerabilities,omitempty"`
	LastActivity    *time.Time            `json:"lastActivity,omitempty"`
	SignalStrength  *int                  `json:"signalStrength,omitempty"`
}

// mirrors RingConfig but omits Bridge and Vlan
type daRing struct {
	VirtualAPs    []string `json:"vaps"`
	Subnet        string   `json:"subnet"`
	LeaseDuration int      `json:"leaseDuration"`
}

type daRings map[string]daRing

func buildDeviceResponse(hwaddr string, client *cfgapi.ClientInfo,
	allowedRings []string,
	scanMap cfgapi.ScanMap, vulnMap cfgapi.VulnMap,
	metrics *cfgapi.ClientMetrics) *daDevice {

	cd := daDevice{
		HwAddr:          hwaddr,
		Ring:            client.Ring,
		DisplayName:     client.DisplayName(),
		DNSName:         client.DNSName,
		DHCPName:        client.DHCPName,
		DHCPExpiry:      "static",
		FriendlyDNS:     client.FriendlyDNS,
		FriendlyName:    client.FriendlyName,
		IPv4Addr:        &client.IPv4,
		OSVersion:       "",
		Active:          client.IsActive(),
		Wireless:        client.Wireless,
		ConnBand:        client.ConnBand,
		ConnNode:        client.ConnNode,
		ConnVAP:         client.ConnVAP,
		Username:        client.Username,
		AllowedRings:    allowedRings,
		DevID:           client.DevID,
		Scans:           make(map[string]daScanInfo),
		Vulnerabilities: make(map[string]daVulnInfo),
	}

	if metrics != nil {
		cd.LastActivity = metrics.LastActivity
		cd.SignalStrength = &metrics.SignalStrength
	}

	if client.Expires != nil {
		cd.DHCPExpiry = client.Expires.Format(time.RFC3339)
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

	return &cd
}

// GET rings () -> (...)
func demoRingsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var resp daRings = make(map[string]daRing)
	for ringName, ring := range config.GetRings() {
		resp[ringName] = daRing{
			VirtualAPs:    ring.VirtualAPs,
			Subnet:        ring.Subnet,
			LeaseDuration: ring.LeaseDuration,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(&resp); err != nil {
		panic(err)
	}
}

// GET devices () -> (...)
// Policy: GET (*_USER, *_ADMIN)
func demoDevicesHandler(w http.ResponseWriter, r *http.Request) {
	clientsRaw := config.GetClients()
	devices := make([]*daDevice, 0)

	allRings := config.GetRings()
	for mac, client := range clientsRaw {
		scans := config.GetClientScans(mac)
		vulns := config.GetVulnerabilities(mac)
		metrics := config.GetClientMetrics(mac)
		allowedRings := config.GetClientRings(client, allRings)
		cd := buildDeviceResponse(mac, client, allowedRings, scans, vulns, metrics)
		devices = append(devices, cd)
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(&devices); err != nil {
		panic(err)
	}
}

type demoPostDevice struct {
	FriendlyName *string `json:"friendlyName"`
	Ring         *string `json:"ring"`
}

// JSONError is an echo-compatible JSON response to be sent with an HTTP error
type JSONError struct {
	Message string `json:"message"`
}

// HTTPJSONError is an analogue to http.Error, which emits an echo-style
// { message: "something failed" } when something goes wrong.
func HTTPJSONError(w http.ResponseWriter, error string, code int) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(code)

	/* mirrors logic in echo NewHTTPError */
	msg := http.StatusText(code)
	if len(error) > 0 {
		msg = error
	}
	jsonErr := JSONError{
		Message: msg,
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(&jsonErr); err != nil {
		panic(err)
	}
}

// Presently this only allows for ring changes
func demoDevicePostHandler(w http.ResponseWriter, r *http.Request) {
	var err error
	vars := mux.Vars(r)
	deviceID := vars["deviceid"]

	var input, empty demoPostDevice
	if err = json.NewDecoder(r.Body).Decode(&input); err != nil {
		log.Printf("demoPostDevice decode failed: %v", err)
		http.Error(w, "bad device", http.StatusBadRequest)
		return
	}

	if input == empty {
		http.Error(w, "must specify a field to modify", http.StatusBadRequest)
		return
	}

	if input.FriendlyName != nil {
		features, err := config.GetFeatures()
		if err != nil {
			err = errors.Wrap(err, "could not get features")
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if !features[cfgapi.FeatureClientFriendlyName] {
			http.Error(w, "friendly names not supported for this client",
				http.StatusBadRequest)
			return
		}
		// allow '', it means "return to the default"
		if *input.FriendlyName != "" {
			dnsName := network.GenerateDNSName(*input.FriendlyName)
			if dnsName == "" {
				HTTPJSONError(w,
					"invalid name; must contain some alphanumeric characters",
					http.StatusBadRequest)
				return
			}
		}
	}

	ops := []cfgapi.PropertyOp{
		{
			Op:   cfgapi.PropTest,
			Name: fmt.Sprintf("@/clients/%s", deviceID),
		},
	}

	if input.Ring != nil {
		ops = append(ops, cfgapi.PropertyOp{
			Op:    cfgapi.PropCreate,
			Name:  fmt.Sprintf("@/clients/%s/ring", deviceID),
			Value: *input.Ring,
		})
	}

	if input.FriendlyName != nil {
		op := cfgapi.PropCreate
		if *input.FriendlyName == "" {
			op = cfgapi.PropDelete
		}
		ops = append(ops, cfgapi.PropertyOp{
			Op:    op,
			Name:  fmt.Sprintf("@/clients/%s/friendly_name", deviceID),
			Value: *input.FriendlyName,
		})
	}

	_, err = config.Execute(r.Context(), ops).Wait(r.Context())
	if err != nil {
		log.Printf("demoDevicePost failed to execute: %v", err)
		http.Error(w, "failed to set properties", http.StatusBadRequest)
	}
}

func demoDeviceMetricsHandler(w http.ResponseWriter, r *http.Request) {

	vars := mux.Vars(r)
	deviceID := vars["deviceid"]

	metrics := config.GetClientMetrics(deviceID)
	if metrics == nil {
		http.Error(w, "no metrics for client", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(&metrics); err != nil {
		panic(err)
	}
}

func demoDNSInfoGetHandler(w http.ResponseWriter, r *http.Request) {
	dns := config.GetDNSInfo()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(&dns); err != nil {
		panic(err)
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
		log.Printf("demoVAPNamePost failed to execute: %v", err)
		http.Error(w, "failed to set properties", http.StatusBadRequest)
		return
	}
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

// Implements GET /api/site/:uuid/network/wg, returning information about the
// WG VPN.
func demoWGGetHandler(w http.ResponseWriter, r *http.Request) {
	vpnMod, err := vpn.NewVpn(config)
	if err != nil {
		err = errors.Wrap(err, "getting vpn handle")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	serverCfg, err := vpnMod.ServerConfig()
	if err != nil {
		err = errors.Wrap(err, "getting server config")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(&serverCfg); err != nil {
		panic(err)
	}
}

// Implements POST /api/site/:uuid/network/wg, returning information about the
// WG VPN.
func demoWGPostHandler(w http.ResponseWriter, r *http.Request) {
	var err error
	var input vpn.ServerConfig
	if err = json.NewDecoder(r.Body).Decode(&input); err != nil {
		log.Printf("demoWGPost decode failed: %v", err)
		http.Error(w, errors.Wrap(err, "bad input").Error(), http.StatusBadRequest)
		return
	}
	// User should not be passing this
	if input.PublicKey != "" {
		http.Error(w, errors.New("cannot pass publicKey").Error(), http.StatusBadRequest)
		return
	}

	ops := []cfgapi.PropertyOp{
		{
			Op:    cfgapi.PropCreate,
			Name:  vpn.AddressProp,
			Value: input.Address,
		},
		{
			Op:    cfgapi.PropCreate,
			Name:  vpn.PortProp,
			Value: fmt.Sprintf("%d", input.Port),
		},
		{
			Op:    cfgapi.PropCreate,
			Name:  vpn.EnabledProp,
			Value: fmt.Sprintf("%t", input.Enabled),
		},
	}
	_, err = config.Execute(r.Context(), ops).Wait(r.Context())
	if err != nil {
		log.Printf("demoWGPostHandler failed to execute: %v", err)
		http.Error(w, "failed to set properties", http.StatusInternalServerError)
	}
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

type demoNodeNic struct {
	Name       string           `json:"name"`
	MacAddr    string           `json:"macaddr"`
	Kind       string           `json:"kind"`
	Ring       string           `json:"ring"`
	Silkscreen string           `json:"silkscreen"`
	WifiInfo   *cfgapi.WifiInfo `json:"wifiInfo,omitempty"`
}

type demoNodeInfo struct {
	ID           string        `json:"id"`
	Name         string        `json:"name"`
	Role         string        `json:"role"` // gateway|satellite
	BootTime     *time.Time    `json:"bootTime"`
	Alive        *time.Time    `json:"alive"`
	Addr         net.IP        `json:"addr"`
	Nics         []demoNodeNic `json:"nics"`
	SerialNumber string        `json:"serialNumber"` // registry SN
	HWModel      string        `json:"hwModel"`
}

func demoNodesGetHandler(w http.ResponseWriter, r *http.Request) {
	result := make([]demoNodeInfo, 0)
	nodes, err := config.GetNodes()
	if err != nil {
		http.Error(w, "error getting nodes", http.StatusInternalServerError)
		return
	}

	for _, node := range nodes {

		ni := demoNodeInfo{
			ID:       node.ID,
			Name:     node.Name,
			Role:     node.Role,
			BootTime: node.BootTime,
			Alive:    node.Alive,
			Addr:     node.Addr,
		}

		// On-Appliance, this is the best we can do for now
		if mfg.ValidExtSerial(node.ID) {
			ni.SerialNumber = node.ID
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

		ni.Nics = make([]demoNodeNic, 0)
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
			ni.Nics = append(ni.Nics, demoNodeNic{
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

	w.Header().Set("Content-Type", "application/json")
	if err = json.NewEncoder(w).Encode(result); err != nil {
		panic(err)
	}
}

type demoPostNode struct {
	Name string `json:"name"`
}

// demoNodePostHandler implements POST /api/sites/{s}/nodes/{nodeid}
// to adjust per-node settings; presently only setting the name is
// supported.
func demoNodePostHandler(w http.ResponseWriter, r *http.Request) {
	var err error
	vars := mux.Vars(r)
	nodeID := vars["nodeid"]

	var input demoPostNode
	if err = json.NewDecoder(r.Body).Decode(&input); err != nil {
		log.Printf("demoPostNode decode failed: %v", err)
		http.Error(w, "bad node", http.StatusBadRequest)
		return
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
	_, err = config.Execute(r.Context(), ops).Wait(r.Context())
	if err != nil {
		log.Printf("demoPostNode failed to execute: %v", err)
		http.Error(w, "failed to set properties", http.StatusBadRequest)
	}
}

type demoPostNodePort struct {
	Ring    *string `json:"ring"`
	Channel *int    `json:"channel"`
}

// demoPostNodePortHandler implements POST
// /api/sites/:uuid/nodes/:nodeID/ports/:portID to adjust per-port settings;
// presently supports:
//   setting the ring of LAN ports
//   setting the channel of wireless ports
func demoNodePortPostHandler(w http.ResponseWriter, r *http.Request) {
	var err error
	vars := mux.Vars(r)
	nodeID := vars["nodeid"]
	portID := vars["portid"]

	var input demoPostNodePort
	if err = json.NewDecoder(r.Body).Decode(&input); err != nil {
		log.Printf("demoPostNodePort decode failed: %v", err)
		http.Error(w, "bad input", http.StatusBadRequest)
		return
	}

	nic, err := config.GetNic(nodeID, portID)
	if err != nil {
		http.Error(w, "nic", http.StatusBadRequest)
		return
	}
	if input.Ring != nil && nic.Kind != "wired" {
		http.Error(w, "ring, wired", http.StatusBadRequest)
		return
	}
	if input.Channel != nil && (nic.Kind != "wireless" || nic.WifiInfo == nil) {
		http.Error(w, "chan, wireless", http.StatusBadRequest)
		return
	}

	var ops []cfgapi.PropertyOp
	if input.Ring != nil {
		// Check that the user isn't trying to re-ring the WAN port
		// XXX need a better check here; the uplink port could also be
		// 'internal'; T466.
		if portID == "wan" || nic.Ring == "wan" {
			http.Error(w, "uplink", http.StatusForbidden)
			return
		}
		if !cfgapi.ValidRings[*input.Ring] {
			http.Error(w, "bad ring", http.StatusBadRequest)
			return
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
			http.Error(w, "invalid channel", http.StatusBadRequest)
			return
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

	_, err = config.Execute(r.Context(), ops).Wait(r.Context())
	if err != nil {
		log.Printf("demoPostNodePort failed to execute: %v", err)
		http.Error(w, "failed to set properties", http.StatusBadRequest)
	}
	return
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

// getFeatures implements GET /api/sites/:uuid/features
func demoFeaturesGetHandler(w http.ResponseWriter, r *http.Request) {
	features, err := config.GetFeatures()
	if err != nil {
		http.Error(w, "failed to get features",
			http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err = json.NewEncoder(w).Encode(features); err != nil {
		panic(err)
	}
	return
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
		log.Printf("demoConfigPost failed to set properties: %v", err)
		http.Error(w, "failed to set properties", 400)
	}

	if err == nil {
		metrics.latencies.Observe(time.Since(t).Seconds())
	}
}

type daUser struct {
	UID              string
	UUID             *uuid.UUID
	Role             *string
	DisplayName      *string
	Email            *string
	TelephoneNumber  *string
	SelfProvisioning bool
	HasPassword      bool
	SetPassword      *string
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

	w.Header().Set("Content-Type", "application/json")
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
	if dau.TelephoneNumber != nil {
		ui.TelephoneNumber = *dau.TelephoneNumber
	}
	if dau.Role != nil {
		ui.Role = *dau.Role
	}
	if dau.SetPassword != nil {
		err = ui.SetPassword(*dau.SetPassword)
		if err != nil {
			log.Printf("SetPassword failed: %v", err)
			http.Error(w, "unexpected failure", 500)
			return
		}
	}
	hdl, err := ui.Update(r.Context())
	if err != nil {
		log.Printf("Update '%s' failed: %v\n", dau.UID, err)
		http.Error(w, fmt.Sprintf("failed to save: %v", err), 500)
		return
	}
	_, err = hdl.Wait(r.Context())
	if err != nil {
		log.Printf("Wait '%s' failed: %v\n", dau.UID, err)
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

	w.Header().Set("Content-Type", "application/json")
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
		log.Printf("GetUserByUUID(%v): %v:", ruuid, err)
		http.Error(w, "invalid or unknown user", 400)
		return
	}

	_, err = ui.Delete(r.Context()).Wait(r.Context())
	if err != nil {
		log.Printf("Delete user '%s' failed: %v\n", ui.UID, err)
		http.Error(w, "failed to delete user", 400)
		return
	}
}

// demoUsersHandler returns a JSON-formatted map of configured users, keyed by
// UUID, typically in response to a GET request to "[demo_api_root]/users".
func demoUsersHandler(w http.ResponseWriter, r *http.Request) {
	users := make(map[string]daUser)

	for _, user := range config.GetUsers() {
		ur := buildUserResponse(user)
		users[ur.UUID.String()] = ur
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(users); err != nil {
		panic(err)
	}
}

type daSite struct {
	// The site breaks the UUID contract by using '0' as its
	// reserved UUID; hence we have to use a string here.
	UUID             string `json:"UUID"`
	Name             string `json:"name"`
	OrganizationUUID string `json:"organizationUUID"`
}

var site0 = daSite{
	UUID:             "0",
	Name:             "Local Site",
	OrganizationUUID: "0",
}

// demoSitesHandler responds to the /api/sites endpoint with a stub in order to
// make coding the frontend app simpler.
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

type daOrg struct {
	// The org breaks the UUID contract by using '0' as its
	// reserved UUID; hence we have to use a string here.
	OrganizationUUID string `json:"organizationUUID"`
	Name             string `json:"name"`
	Relationship     string `json:"relationship"`
}

var org0 = daOrg{
	OrganizationUUID: "0",
	Name:             "Local",
	Relationship:     "self",
}

// demoOrgsHandler responds to the /api/org endpoint with a constant response
// in order to make coding the frontend app simpler.
func demoOrgsHandler(w http.ResponseWriter, r *http.Request) {
	var orgs = []daOrg{org0}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(orgs); err != nil {
		panic(err)
	}
}

func makeDemoAPIRouter() *mux.Router {
	router := mux.NewRouter()
	// Per-site operations
	router.HandleFunc("/sites", demoSitesHandler).Methods("GET")
	router.HandleFunc("/sites/{s}", demoSitesUUIDHandler).Methods("GET")
	router.HandleFunc("/sites/{s}/config", demoConfigGetHandler).Methods("GET")
	router.HandleFunc("/sites/{s}/config", demoConfigPostHandler).Methods("POST")
	router.HandleFunc("/sites/{s}/devices", demoDevicesHandler).Methods("GET")
	router.HandleFunc("/sites/{s}/devices/{deviceid}", demoDevicePostHandler).Methods("POST")
	router.HandleFunc("/sites/{s}/devices/{deviceid}/metrics", demoDeviceMetricsHandler).Methods("GET")
	router.HandleFunc("/sites/{s}/features", demoFeaturesGetHandler).Methods("GET")
	router.HandleFunc("/sites/{s}/network/dns", demoDNSInfoGetHandler).Methods("GET")
	router.HandleFunc("/sites/{s}/network/vap", demoVAPGetHandler).Methods("GET")
	router.HandleFunc("/sites/{s}/network/vap/{vapname}", demoVAPNameGetHandler).Methods("GET")
	router.HandleFunc("/sites/{s}/network/vap/{vapname}", demoVAPNamePostHandler).Methods("POST")
	router.HandleFunc("/sites/{s}/network/wan", demoWanGetHandler).Methods("GET")
	router.HandleFunc("/sites/{s}/network/wg", demoWGGetHandler).Methods("GET")
	router.HandleFunc("/sites/{s}/network/wg", demoWGPostHandler).Methods("POST")
	router.HandleFunc("/sites/{s}/nodes", demoNodesGetHandler).Methods("GET")
	router.HandleFunc("/sites/{s}/nodes/{nodeid}", demoNodePostHandler).Methods("POST")
	router.HandleFunc("/sites/{s}/nodes/{nodeid}/ports/{portid}", demoNodePortPostHandler).Methods("POST")
	router.HandleFunc("/sites/{s}/rings", demoRingsHandler).Methods("GET")
	router.HandleFunc("/sites/{s}/users", demoUsersHandler).Methods("GET")
	router.HandleFunc("/sites/{s}/users/{uuid}", demoUserByUUIDGetHandler).Methods("GET")
	router.HandleFunc("/sites/{s}/users/{uuid}", demoUserByUUIDPostHandler).Methods("POST")
	router.HandleFunc("/sites/{s}/users/{uuid}", demoUserByUUIDDeleteHandler).Methods("DELETE")
	router.HandleFunc("/org", demoOrgsHandler).Methods("GET")
	router.Use(cookieAuthMiddleware)
	return router
}
