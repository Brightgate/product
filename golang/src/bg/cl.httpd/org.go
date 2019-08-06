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
	"net/http"

	"bg/cloud_models/appliancedb"

	"github.com/gorilla/sessions"
	"github.com/labstack/echo"
	"github.com/satori/uuid"
)

type orgHandler struct {
	db           appliancedb.DataStore
	sessionStore sessions.Store
}

func (o *orgHandler) getOrgAccounts(c echo.Context) error {
	ctx := c.Request().Context()

	orgUUID, err := uuid.FromString(c.Param("org_uuid"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest)
	}

	accounts, err := o.db.AccountInfosByOrganization(ctx, orgUUID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err)
	}
	return c.JSON(http.StatusOK, accounts)
}

// mkOrgMiddleware manufactures a middleware which protects a route; only
// users with one or more of the allowedRoles can pass through the checks; the
// middleware adds "matched_roles" to the echo context, indicating which of the
// allowed_roles the user actually has.
func (o *orgHandler) mkOrgMiddleware(allowedRoles []string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			ctx := c.Request().Context()
			accountUUID, ok := c.Get("account_uuid").(uuid.UUID)
			if !ok || accountUUID == uuid.Nil {
				return echo.NewHTTPError(http.StatusUnauthorized)
			}

			orgUUID, err := uuid.FromString(c.Param("org_uuid"))
			if err != nil {
				return echo.NewHTTPError(http.StatusBadRequest)
			}
			roles, err := o.db.AccountOrgRolesByAccountTarget(ctx,
				accountUUID, orgUUID)
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError)
			}
			matches := make(matchedRoles)
			var matched bool
			for _, ur := range roles {
				matches[ur.Role] = false
				for _, rr := range allowedRoles {
					if ur.Role == rr {
						matches[ur.Role] = true
						matched = true
					}
				}
			}
			if matched {
				c.Set("matched_roles", matches)
				return next(c)
			}
			c.Logger().Debugf("Unauthorized: %s org=%v, acc=%v, ur=%v, ar=%v",
				c.Path(), orgUUID, accountUUID, roles, allowedRoles)
			return echo.NewHTTPError(http.StatusUnauthorized)
		}
	}
}

// newOrgAPIHandler creates an orgHandler for the given DataStore and session
// Store, and routes the handler into the echo instance.
func newOrgHandler(r *echo.Echo, db appliancedb.DataStore, middlewares []echo.MiddlewareFunc, sessionStore sessions.Store) *orgHandler {
	h := &orgHandler{db, sessionStore}
	org := r.Group("/api/org")

	admin := h.mkOrgMiddleware([]string{"admin"})
	org.Use(middlewares...)
	org.GET("/:org_uuid/accounts", h.getOrgAccounts, admin)
	return h
}
