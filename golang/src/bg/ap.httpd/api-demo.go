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
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"bg/ap_common/apcfg"

	"github.com/gorilla/mux"
)

// What would an Alert be?  A reference ID to a full Alert?
type DAAlerts struct {
	DbgRequest string
	Alerts     []string
}

// GET alerts  () -> (...)
func demoAlertsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	rs := fmt.Sprintf("%v", r)
	as := DAAlerts{
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

type DADevice struct {
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

type DADevices struct {
	DbgRequest string
	Devices    []DADevice
}

func buildDeviceResponse(hwaddr string, client *apcfg.ClientInfo) DADevice {
	var cd DADevice

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
		cd.DHCPExpiry = client.Expires.Format("2006-02-01T15:04")
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
func demoDevicesByRingHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	vars := mux.Vars(r)

	clientsRaw := config.GetClients()
	var devices DADevices

	for mac, client := range clientsRaw {
		var cd DADevice

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

// GET devices () -> (...)
func demoDevicesHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	clientsRaw := config.GetClients()
	var devices DADevices

	for mac, client := range clientsRaw {
		var cd DADevice

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

// GET my access parameters (device ID) -> (duration, target ring?)
// POST access parameters for a device on setup network (device ID,
func demoAccessByIDHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	vars := mux.Vars(r)
	if r.Method == "GET" {
		fmt.Fprintf(w, "{\"demoApiHandler\": \"GET access-by-id\", \"request\": \"%v\", \"vars\": \"%v\"}\n", r, vars)
	} else if r.Method == "POST" {
		fmt.Fprintf(w, "{\"demoApiHandler\": \"POST access-by-id\", \"request\": \"%v\"}\n", r)
	} else {
		fmt.Fprintf(w, "{\"demoApiHandler\": \"UNKNOWN access-by-id\", \"request\": \"%v\"}\n", r)
	}
}

// duration, target ring)
func demoAccessHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, "{\"demoApiHandler\": \"GET access\", \"request\": \"%v\"}\n", r)
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

func demoPropertyByNameHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	vars := mux.Vars(r)

	if r.Method == "GET" {
		fmt.Fprintf(w, "{\"demoApiHandler\": \"GET property-by-name\", \"request\": \"%v\", \"vars\": \"%v\"}\n", r, vars)
	} else if r.Method == "POST" {
		fmt.Fprintf(w, "{\"demoApiHandler\": \"POST property-by-name\", \"request\": \"%v\"}\n", r)
	} else {
		fmt.Fprintf(w, "{\"demoApiHandler\": \"UNKNOWN property-by-name\", \"request\": \"%v\"}\n", r)
	}

}

// XXX broken-- unexpected end of JSON
func demoPropertyHandler(w http.ResponseWriter, r *http.Request) {
	var err error
	w.Header().Set("Content-Type", "application/json")

	t := time.Now()

	if r.Method != "GET" && r.Method != "POST" {
		http.Error(w, "Invalid request method.", 405)
		return
	}

	if r.Method == "GET" {
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
			fmt.Fprintf(w, "%s", val)
		}
	} else {
		// Send property updates to ap.configd
		//
		// From the command line:
		//    wget -q --post-data '@/network/wlan0/ssid=newssid' \
		//           http://127.0.0.1:8000/config

		err = r.ParseForm()
		for key, values := range r.Form {
			if len(values) != 1 {
				http.Error(w, "Properties may only have one value", 400)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			err = config.SetProp(key, values[0], nil)
			if err == nil {
				// ok!
				fmt.Fprintf(w, "{}")
			}
		}
	}

	if err == nil {
		latencies.Observe(time.Since(t).Seconds())
	}
}

// StatsContent contains information for filling out the stats template.
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

	statsTemplate, err := openTemplate("stats")
	if err == nil {
		conf := &StatsContent{
			URLPath:    r.URL.Path,
			NPings:     strconv.Itoa(pings),
			NConfigs:   strconv.Itoa(configs),
			NEntities:  strconv.Itoa(entities),
			NResources: strconv.Itoa(resources),
			NRequests:  strconv.Itoa(requests),
			Host:       r.Host,
		}

		err = statsTemplate.Execute(w, conf)
	}
	if err != nil {
		http.Error(w, "Internal server error", 501)

	} else {
		latencies.Observe(time.Since(lt).Seconds())
	}
}
