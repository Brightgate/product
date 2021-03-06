//
// Copyright 2020 Brightgate Inc.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//


package main

import (
	"context"
	"net/http"
	"time"

	"bg/cl_common/pgutils"
	"bg/cl_common/vaultdb"
	"bg/cloud_models/appliancedb"
	"bg/cloud_models/sessiondb"

	"github.com/labstack/echo"
)

type checkHandler struct {
	getClientHandle getClientHandleFunc
	sessionVDBC     *vaultdb.Connector
	applianceVDBC   *vaultdb.Connector
}

type checkResponse struct {
	ApplianceDBStatus string   `json:"applianceDBStatus"`
	SessionDBStatus   string   `json:"sessionDBStatus"`
	ConfigdStatus     string   `json:"configdStatus"`
	EnvironProblems   []string `json:"environProblems"`
}

// getCheckProduction implements /check/production, which checks that
// the httpd is running smoothly and suitable in production.  The check
// is designed to run in less than 20 seconds; a 30 second timeout is
// advisable.
func (h *checkHandler) getCheckProduction(c echo.Context) error {
	var fail bool
	r := checkResponse{
		ApplianceDBStatus: "unknown",
		SessionDBStatus:   "unknown",
		ConfigdStatus:     "unknown",
		EnvironProblems:   nil,
	}

	var err error
	var applianceDB appliancedb.DataStore

	if h.applianceVDBC != nil {
		applianceDB, err = appliancedb.VaultConnect(h.applianceVDBC)
	} else {
		dbURI := pgutils.AddConnectTimeout(environ.ApplianceDB, "3")
		dbURI = pgutils.AddApplication(dbURI, pname)
		applianceDB, err = appliancedb.Connect(dbURI)
	}
	if err != nil {
		c.Logger().Errorf("check failed for applianceDB connect: %v", err)
		fail = true
		r.ApplianceDBStatus = err.Error()
	} else {
		err = applianceDB.PingContext(c.Request().Context())
		applianceDB.Close()
		if err != nil {
			fail = true
			r.ApplianceDBStatus = err.Error()
			c.Logger().Errorf("check failed for applianceDB ping: %v", err)
		} else {
			r.ApplianceDBStatus = "ok"
		}
	}

	var sessionDB sessiondb.DataStore
	if h.sessionVDBC != nil {
		sessionDB, err = sessiondb.VaultConnect(h.sessionVDBC, false)
	} else {
		dbURI := pgutils.AddConnectTimeout(environ.SessionDB, "3")
		dbURI = pgutils.AddApplication(dbURI, pname)
		sessionDB, err = sessiondb.Connect(dbURI, false)
	}
	if err != nil {
		c.Logger().Errorf("check failed for sessionDB connect: %v", err)
		fail = true
		r.SessionDBStatus = err.Error()
	} else {
		err = sessionDB.PingContext(c.Request().Context())
		sessionDB.Close()
		if err != nil {
			fail = true
			r.SessionDBStatus = err.Error()
			c.Logger().Errorf("check failed for sessionDB ping: %v", err)
		} else {
			r.SessionDBStatus = "ok"
		}
	}
	if environ.Developer {
		c.Logger().Errorf("production check failed: developer mode enabled")
		r.EnvironProblems = append(r.EnvironProblems, "developer mode enabled")
	}
	if environ.DisableTLS {
		c.Logger().Errorf("production check failed: TLS disabled")
		r.EnvironProblems = append(r.EnvironProblems, "tls disabled")
	}
	// In the future, we need some sort of aliveness check for our cluster of
	// cl.configds.
	hdl, err := h.getClientHandle(appliancedb.NullSiteUUID.String())
	if err != nil {
		c.Logger().Errorf("get configd handle failed: %v", err)
		r.ConfigdStatus = err.Error()
		fail = true
	} else {
		ctx, cancel := context.WithTimeout(c.Request().Context(), time.Duration(time.Second)*3)
		defer cancel()
		err = hdl.Ping(ctx)
		hdl.Close()
		if err != nil {
			c.Logger().Errorf("configd ping failed: %v", err)
			r.ConfigdStatus = err.Error()
			fail = true
		} else {
			r.ConfigdStatus = "ok"
		}
	}
	code := http.StatusOK
	if fail {
		code = http.StatusInternalServerError
	}
	return c.JSONPretty(code, &r, "  ")
}

// newCheckHandler creates a checkHandler to handle uptime check endpoints
func newCheckHandler(state *routerState, getClientHandle getClientHandleFunc) *checkHandler {
	h := &checkHandler{
		getClientHandle: getClientHandle,
		sessionVDBC:     state.sessionVDBC,
		applianceVDBC:   state.applianceVDBC,
	}
	// A production-quality, in-depth health check
	state.echo.GET("/check/production", h.getCheckProduction)
	// A basic, rapid-response health check
	state.echo.GET("/check/pulse", func(c echo.Context) error {
		return c.String(http.StatusOK, "healthy\n")
	})
	return h
}

