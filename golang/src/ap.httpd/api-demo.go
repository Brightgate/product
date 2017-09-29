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
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"
)

// GET alerts  () -> (...)
func demoAlertsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, "{\"demoApiHandler\": \"GET alerts\", \"request\": \"%v\", \"nalerts\": 0, \"alerts\": []}\n", r)
}

// GET devices on ring (ring) -> (...)
func demoDevicesByRingHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	vars := mux.Vars(r)

	fmt.Fprintf(w, "{\"demoApiHandler\": \"GET devices-by-ring\", \"request\": \"%v\", \"vars\": \"%v\"}\n", r, vars)
}

// GET devices () -> (...)
func demoDevicesHandler(w http.ResponseWriter, r *http.Request) {
	ctree := config.GetClients()

	w.Header().Set("Content-Type", "application/json")

	fmt.Fprintf(w, "{\"demoApiHandler\": \"GET devices\", \"request\": \"%v\", \"ctree\": \"%v\"}\n", r, ctree)
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

func demoPropertyByNameHandler(w http.ResponseWriter, r *http.Request) {
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
