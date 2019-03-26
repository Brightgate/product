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
	"net/http"
	"time"

	"bg/cloud_models/appliancedb"
	"bg/cloud_models/sessiondb"

	"github.com/labstack/echo"
)

type checkHandler struct {
	getClientHandle getClientHandleFunc
}

type checkResponse struct {
	ApplianceDBStatus string   `json:"applianceDBStatus"`
	SessionDBStatus   string   `json:"sessionDBStatus"`
	ConfigdStatus     string   `json:"configdStatus"`
	EnvironProblems   []string `json:"environProblems"`
}

// getCheckProduction implements /checks/production, which checks that
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
	dbURI := environ.ApplianceDB + "&connect_timeout=3"
	applianceDB, err := appliancedb.Connect(dbURI)
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

	dbURI = environ.SessionDB + "&connect_timeout=3"
	sessionDB, err := sessiondb.Connect(dbURI, false)
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
func newCheckHandler(r *echo.Echo, getClientHandle getClientHandleFunc) *checkHandler {
	h := &checkHandler{getClientHandle}
	r.GET("/check/production", h.getCheckProduction)
	return h
}
