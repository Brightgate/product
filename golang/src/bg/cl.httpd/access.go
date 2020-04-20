//
// COPYRIGHT 2020 Brightgate Inc.  All rights reserved.
//
// This copyright notice is Copyright Management Information under 17 USC 1202
// and is included to protect this work and deter copyright infringement.
// Removal or alteration of this Copyright Management Information without the
// express written permission of Brightgate Inc is prohibited, and any
// such unauthorized removal or alteration will be a violation of federal law.
//

package main

import (
	"net/http"
	"strings"

	"github.com/gorilla/sessions"
	"github.com/labstack/echo"

	"bg/cloud_models/appliancedb"
)

type accessHandler struct {
	db           appliancedb.DataStore
	sessionStore sessions.Store
}

type accessDiscoveryNet struct {
	SSID string `json:"ssid"`
	Auth string `json:"auth"` // "none", "wep", "wpa-psk", "wpa-eap", "wpa2-psk", "wpa2-eap", ...
}

type accessDiscoveryReq struct {
	Networks map[string]*accessDiscoveryNet `json:"networks"`
}

type accessDiscoveryTokenAuth struct {
	AuthLevel string `json:"authLevel"` // "user", "guest", "none"
}

type accessDiscoveryTokenMap struct {
	TokenAccess map[string]*accessDiscoveryTokenAuth `json:"tokenAccess"`
}

type accessDiscoveryRes struct {
	Networks map[string]*accessDiscoveryTokenMap `json:"networks"`
}

// postDiscover implements /access/discover; this POST method takes
// as input a JSON document representing the networks the mobile app
// can see.  The result is a JSON document which describes the networks and
// indicates whether they can be auto-configured.
//
// > GET /access/discover
// > Authorization: bearer <token1>
// > Authorization: bearer <token2>
// > {
// >   networks: {
// >     "<bssid>": {
// >       ssid: "<ssid>",
// >       auth: "<open|...|wpa2-psk|wpa2-eap]"
// >     },
// >     ...
// >   }
// > }
// ---
// < {
// <   networks: {
// <    "<bssid>": {
// <      tokenAccess: {
// <        "<token1>": {
// <          "authLevel": "[user|guest|none]"
// <        },
// <        "<token2>": {
// <          "authLevel": "[user|guest|none]"
// <        }
// <      }
// <    }
// <  }
func (a *accessHandler) postDiscover(c echo.Context) error {
	var req accessDiscoveryReq
	var res = &accessDiscoveryRes{
		Networks: make(map[string]*accessDiscoveryTokenMap),
	}
	var err error

	var tokens = make([]string, 0)
	for _, header := range c.Request().Header["Authorization"] {
		h := strings.SplitN(header, " ", 2)
		if len(h) == 2 && h[0] == "bearer" && h[1] != "" {
			tokens = append(tokens, h[1])
		}
	}
	if len(tokens) == 0 {
		return newHTTPError(http.StatusUnauthorized, "need token")
	}

	if err = c.Bind(&req); err != nil {
		return err
	}
	// XXX later bssid, netinfo, but for now we have nothing to do with netinfo
	for bssid := range req.Networks {
		for _, t := range tokens {
			if res.Networks[bssid] == nil {
				res.Networks[bssid] = &accessDiscoveryTokenMap{
					TokenAccess: make(map[string]*accessDiscoveryTokenAuth),
				}
			}
			res.Networks[bssid].TokenAccess[t] = &accessDiscoveryTokenAuth{
				AuthLevel: "user",
			}
		}
	}
	return c.JSON(http.StatusOK, &res)
}

type accessEnrollRes struct {
	AuthType        string `json:"authType"` // "none" (open), "wpa2-psk", "wpa2-eap"
	WPA2EAPUser     string `json:"wpa2EAPUser,omitempty"`
	WPA2EAPPassword string `json:"wpa2EAPPassword,omitempty"`
	WPA2PSKSecret   string `json:"wpa2PSKSecret,omitempty"`
}

// getEnroll implements /access/bssid/:bssid/enroll, which returns
// credentials for use on the specified bssid.
//
// The anticipated flow is (Authorization: is not yet checked):
//
// > GET /access/bssid/01:23:45:67:89:a0/enroll
// > Authorization: bearer <token>
// ---
// < {
// <  authType: "wpa2-eap",
// <  wpa2EAPUser: "<username>",
// <  wpa2EAPPassword: "<password>"
// < }
//
// For now the output is mocked.  In the event that the network was
// PSK, this would look something like:
//
// < {
// <  authType: "wpa2-psk",
// <  wpa2PSKSecret: "<secret>"
// < }
//
func (a *accessHandler) getEnroll(c echo.Context) error {
	r := &accessEnrollRes{
		AuthType:        "wpa2-eap",
		WPA2EAPUser:     "setup",
		WPA2EAPPassword: "PrototypeOnly",
	}
	return c.JSON(http.StatusOK, r)
}

// newAccessHandler creates an accessHandler instance; this is used
// to manage user interactions for mobile apps.
func newAccessHandler(r *echo.Echo, db appliancedb.DataStore, sessionStore sessions.Store) *accessHandler {
	h := &accessHandler{db, sessionStore}
	r.POST("/access/discover", h.postDiscover)
	r.GET("/access/bssid/:bssid/enroll", h.getEnroll)
	return h
}
