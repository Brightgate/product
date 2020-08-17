//
// Copyright 2020 Brightgate Inc.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
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

type orgsResponse struct {
	OrganizationUUID uuid.UUID `json:"organizationUUID"`
	Name             string    `json:"name"`
	Relationship     string    `json:"relationship"`
}

func (o *orgHandler) getOrgs(c echo.Context) error {
	ctx := c.Request().Context()

	accountUUID, ok := c.Get("account_uuid").(uuid.UUID)
	if !ok || accountUUID == uuid.Nil {
		return newHTTPError(http.StatusUnauthorized)
	}
	acct, err := o.db.AccountByUUID(ctx, accountUUID)
	if err != nil {
		return newHTTPError(http.StatusInternalServerError)
	}
	rels, err := o.db.OrgOrgRelationshipsByOrg(ctx, acct.OrganizationUUID)
	if err != nil {
		return newHTTPError(http.StatusInternalServerError)
	}
	response := make([]orgsResponse, len(rels))
	for idx, rel := range rels {
		tgtOrg, err := o.db.OrganizationByUUID(ctx, rel.TargetOrganizationUUID)
		if err != nil {
			return newHTTPError(http.StatusInternalServerError)
		}
		response[idx] = orgsResponse{
			OrganizationUUID: tgtOrg.UUID,
			Name:             tgtOrg.Name,
			Relationship:     rel.Relationship,
		}
	}
	return c.JSON(http.StatusOK, response)
}

func (o *orgHandler) getOrgAccounts(c echo.Context) error {
	ctx := c.Request().Context()
	accountUUID, ok := c.Get("account_uuid").(uuid.UUID)
	if !ok || accountUUID == uuid.Nil {
		return newHTTPError(http.StatusUnauthorized)
	}

	orgUUID, err := uuid.FromString(c.Param("org_uuid"))
	if err != nil {
		return newHTTPError(http.StatusBadRequest)
	}

	mr := c.Get("matched_roles").(matchedRoles)
	var accounts []appliancedb.AccountInfo
	if !mr["admin"] && mr["user"] {
		// Get session's own AccountInfo
		acct, err := o.db.AccountInfoByUUID(ctx, accountUUID)
		if err != nil {
			return newHTTPError(http.StatusInternalServerError, err)
		}
		accounts = append(accounts, *acct)
	} else if mr["admin"] {
		var err error
		accounts, err = o.db.AccountInfosByOrganization(ctx, orgUUID)
		if err != nil {
			return newHTTPError(http.StatusInternalServerError, err)
		}
	}
	if accounts == nil {
		accounts = make([]appliancedb.AccountInfo, 0)
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
				return newHTTPError(http.StatusUnauthorized)
			}

			orgUUID, err := uuid.FromString(c.Param("org_uuid"))
			if err != nil {
				return newHTTPError(http.StatusBadRequest)
			}
			aoRoles, err := o.db.AccountOrgRolesByAccountTarget(ctx,
				accountUUID, orgUUID)
			if err != nil {
				return newHTTPError(http.StatusInternalServerError)
			}
			matches := make(matchedRoles)
			for _, aor := range aoRoles {
				for _, r := range aor.Roles {
					for _, rr := range allowedRoles {
						if r == rr {
							matches[r] = true
						}
					}
				}
			}
			if len(matches) > 0 {
				c.Set("matched_roles", matches)
				return next(c)
			}
			c.Logger().Debugf("Unauthorized: %s org=%v, acc=%v, ur=%v, ar=%v",
				c.Path(), orgUUID, accountUUID, aoRoles, allowedRoles)
			return newHTTPError(http.StatusUnauthorized)
		}
	}
}

// newOrgAPIHandler creates an orgHandler for the given DataStore and session
// Store, and routes the handler into the echo instance.
func newOrgHandler(r *echo.Echo, db appliancedb.DataStore, middlewares []echo.MiddlewareFunc, sessionStore sessions.Store) *orgHandler {
	h := &orgHandler{db, sessionStore}
	r.GET("/api/org", h.getOrgs, middlewares...)

	user := h.mkOrgMiddleware([]string{"admin", "user"})

	org := r.Group("/api/org/:org_uuid")
	org.Use(middlewares...)
	org.GET("/accounts", h.getOrgAccounts, user)
	return h
}

