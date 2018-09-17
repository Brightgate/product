//
// COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
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

	"github.com/gorilla/sessions"
	"github.com/satori/uuid"

	"github.com/labstack/echo"

	"bg/cloud_models/appliancedb"
)

type apiHandler struct {
	db           appliancedb.DataStore
	sessionStore sessions.Store
}

// getAppliances implements /api/appliances
// XXX needs filtering by userid
func (a *apiHandler) getAppliances(c echo.Context) error {
	session, err := a.sessionStore.Get(c.Request(), "bg_login")
	if session == nil || err != nil || session.Values["userid"] == nil {
		return echo.NewHTTPError(http.StatusUnauthorized)
	}
	ids, err := a.db.AllApplianceIDs(context.Background())
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError)
	}
	idus := []uuid.UUID{}
	for _, id := range ids {
		idus = append(idus, id.CloudUUID)
	}
	return c.JSON(http.StatusOK, &idus)
}

// getAppliancesUUID implements /api/appliances/:uuid
func (a *apiHandler) getAppliancesUUID(c echo.Context) error {
	session, err := a.sessionStore.Get(c.Request(), "bg_login")
	if session == nil || err != nil || session.Values["userid"] == nil {
		return echo.NewHTTPError(http.StatusUnauthorized)
	}
	// Parsing UUID from string input
	u, err := uuid.FromString(c.Param("uuid"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest)
	}
	id, err := a.db.ApplianceIDByUUID(context.Background(), u)
	if err != nil {
		if _, ok := err.(appliancedb.NotFoundError); ok {
			return echo.NewHTTPError(http.StatusNotFound, "No such appliance")
		}
		return echo.NewHTTPError(http.StatusInternalServerError)
	}
	return c.JSON(http.StatusOK, &id)
}

// newApiHandler creates an apiHandler for the given DataStore and session
// Store, and routes the handler into the echo instance.
func newAPIHandler(r *echo.Echo, db appliancedb.DataStore, sessionStore sessions.Store) *apiHandler {
	h := &apiHandler{db, sessionStore}
	r.GET("/api/appliances", h.getAppliances)
	r.GET("/api/appliances/:uuid", h.getAppliancesUUID)
	return h
}
