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
	"fmt"
	"log"
	"net/http"

	"github.com/labstack/echo"

	"github.com/markbates/goth"
	"github.com/markbates/goth/gothic"
	"github.com/markbates/goth/providers/gplus"
	"github.com/markbates/goth/providers/openidConnect"
)

type providerContextKey struct{}

func init() {
	// Replace Gothic GetProviderName routine
	gothic.GetProviderName = getProviderName
}

// getProviderName fetches the name of the auth provider.  This function
// is used as a drop-in replacement for gothic.GetProviderName, which is
// overrideable.
func getProviderName(req *http.Request) (string, error) {
	s, ok := req.Context().Value(providerContextKey{}).(string)
	if !ok {
		return "", fmt.Errorf("Couldn't get Provider name")
	}
	return s, nil
}

// Transfer the URL "provider" parameter to the Go Context, so that getProviderName can
// fetch it.  See also https://github.com/markbates/goth/issues/238.
func providerToGoContext(c echo.Context) {
	// Put the provider name into the go context so gothic can find it
	newCtx := context.WithValue(c.Request().Context(), providerContextKey{}, c.Param("provider"))
	nr := c.Request().WithContext(newCtx)
	c.SetRequest(nr)
}

func gplusProvider() {
	if environ.GPlusKey == "" && environ.GPlusSecret == "" {
		log.Printf("not enabling gplus authentication: missing B10E_CLHTTPD_GPLUS_KEY or B10E_CLHTTPD_GPLUS_SECRET")
		return
	}

	log.Printf("enabling gplus authentication")
	gplusProvider := gplus.New(environ.GPlusKey, environ.GPlusSecret,
		"https://"+environ.CertHostname+"/auth/gplus/callback")
	goth.UseProviders(gplusProvider)
}

func openidConnectProvider() {
	if environ.OpenIDConnectKey == "" || environ.OpenIDConnectSecret == "" || environ.OpenIDConnectDiscoveryURL == "" {
		log.Printf("not enabling gplus authentication: missing B10E_CLHTTPD_OPENID_CONNECT_KEY, B10E_CLHTTPD_OPENID_CONNECT_SECRET or B10E_CLHTTPD_OPENID_CONNECT_DISCOVERY_URL")
		return
	}
	log.Printf("enabling openid connect authentication via %s", environ.OpenIDConnectDiscoveryURL)
	openidConnect, err := openidConnect.New(
		environ.OpenIDConnectKey,
		environ.OpenIDConnectSecret,
		"https://"+environ.CertHostname+"/auth/openid-connect/callback",
		environ.OpenIDConnectDiscoveryURL,
		"openid", "profile", "email", "phone")
	if err != nil || openidConnect == nil {
		log.Fatalf("failed to initialized openid-connect")
	}
	goth.UseProviders(openidConnect)
}

func routeAuth(r *echo.Echo) {
	gplusProvider()
	openidConnectProvider()

	r.GET("/auth/:provider", func(c echo.Context) error {
		providerToGoContext(c)
		// try to get the user without re-authenticating
		if user, err := gothic.CompleteUserAuth(c.Response(), c.Request()); err == nil {
			return c.JSON(http.StatusOK, user)
		}

		gothic.BeginAuthHandler(c.Response(), c.Request())
		return nil
	})

	r.GET("/auth/:provider/logout", func(c echo.Context) error {
		gothic.Logout(c.Response(), c.Request())
		return c.Redirect(http.StatusTemporaryRedirect, "/")
	})

	r.GET("/auth/:provider/callback", func(c echo.Context) error {
		// Put the provider name into the go context so gothic can find it
		providerToGoContext(c)

		user, err := gothic.CompleteUserAuth(c.Response(), c.Request())
		c.Logger().Printf("user is %#v", user)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err)
		}
		if user.RawData["hd"] != "brightgate.com" {
			return echo.NewHTTPError(http.StatusUnauthorized, "🙀 bad user domain, sorry.")
		}

		// TODO: We need to reconcile the user information with our database's
		// notion of a user (which doesn't exist yet).  And we need to create
		// our own Session (cookie?) for this user; goth's session is for its
		// own use.
		return c.JSON(http.StatusOK, user)
	})
}
