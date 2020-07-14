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
	"context"
	"net/http"
	"time"

	"bg/cl_common/pgutils"
	"bg/cl_common/vaultdb"
	"bg/cloud_models/appliancedb"

	"github.com/labstack/echo"
)

type checkHandler struct {
	getClientHandle getClientHandleFunc
	vdbc            *vaultdb.Connector
}

type checkResponse struct {
	ApplianceDBStatus string   `json:"applianceDBStatus"`
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
		ConfigdStatus:     "unknown",
		EnvironProblems:   nil,
	}
	var applianceDB appliancedb.DataStore
	var err error
	if h.vdbc != nil {
		applianceDB, err = appliancedb.VaultConnect(h.vdbc)
	} else {
		dbURI := pgutils.AddConnectTimeout(environ.PostgresConnection, "3")
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
func newCheckHandler(r *echo.Echo, getClientHandle getClientHandleFunc, vdbc *vaultdb.Connector) *checkHandler {
	h := &checkHandler{
		getClientHandle: getClientHandle,
		vdbc:            vdbc,
	}
	// A production-quality, in-depth health check
	r.GET("/check/production", h.getCheckProduction)
	// This is a really stupid health check, but we have to have something
	// that returns 200 for a simple GET.  We can't configure the method,
	// headers, or posted data for a health check in GCP.
	// K8s allows you to run a command as a health check, which would allow
	// us to use https://github.com/grpc-ecosystem/grpc-health-probe.
	r.GET("/check/pulse", func(c echo.Context) error {
		return c.String(http.StatusOK, "healthy\n")
	})
	return h
}
