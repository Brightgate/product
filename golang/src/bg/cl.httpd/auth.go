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
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/labstack/echo"

	"github.com/markbates/goth"
	"github.com/markbates/goth/gothic"
	"github.com/markbates/goth/providers/auth0"
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

func callback(provider string) string {
	var callback string
	if environ.Developer {
		hoststr, portstr, err := net.SplitHostPort(environ.HTTPSListen)
		if err != nil {
			log.Fatalf("bad HTTPSListen address")
		}
		if hoststr == "" {
			hoststr, err = os.Hostname()
			if err != nil {
				log.Fatalf("could not get hostname")
			}
		}
		port, err := net.LookupPort("tcp", portstr)
		if err != nil {
			log.Fatalf("could not parse port %s", portstr)
		}
		callback = fmt.Sprintf("http://%s.b10e.net:%d/auth/%s/callback",
			hoststr, port, provider)
	} else {
		callback = fmt.Sprintf("https://%s/auth/%s/callback",
			environ.CertHostname, provider)
	}
	return callback
}

func gplusProvider() {
	if environ.GPlusKey == "" && environ.GPlusSecret == "" {
		log.Printf("not enabling gplus authentication: missing B10E_CLHTTPD_GPLUS_KEY or B10E_CLHTTPD_GPLUS_SECRET")
		return
	}

	log.Printf("enabling gplus authentication")
	gplusProvider := gplus.New(environ.GPlusKey, environ.GPlusSecret, callback("gplus"))
	goth.UseProviders(gplusProvider)
}

func openidConnectProvider() {
	if environ.OpenIDConnectKey == "" || environ.OpenIDConnectSecret == "" || environ.OpenIDConnectDiscoveryURL == "" {
		log.Printf("not enabling openid authentication: missing B10E_CLHTTPD_OPENID_CONNECT_KEY, B10E_CLHTTPD_OPENID_CONNECT_SECRET or B10E_CLHTTPD_OPENID_CONNECT_DISCOVERY_URL")
		return
	}
	log.Printf("enabling openid connect authentication via %s", environ.OpenIDConnectDiscoveryURL)
	openidConnect, err := openidConnect.New(
		environ.OpenIDConnectKey,
		environ.OpenIDConnectSecret,
		callback("openid-connect"),
		environ.OpenIDConnectDiscoveryURL,
		"openid", "profile", "email", "phone")
	if err != nil || openidConnect == nil {
		log.Fatalf("failed to initialized openid-connect")
	}
	goth.UseProviders(openidConnect)
}

func auth0Provider() {
	if environ.Auth0Key == "" || environ.Auth0Secret == "" || environ.Auth0Domain == "" {
		log.Printf("not enabling Auth0 authentication: missing B10E_CLHTTPD_AUTH0_KEY, B10E_CLHTTPD_AUTH0_SECRET or B10E_CLHTTPD_AUTH0_DOMAIN")
		return
	}

	log.Printf("enabling Auth0 authentication")
	auth0Provider := auth0.New(environ.Auth0Key, environ.Auth0Secret, callback("auth0"),
		environ.Auth0Domain, "openid", "profile", "email", "zug.zug")
	goth.UseProviders(auth0Provider)
}

func routeAuth(r *echo.Echo) {
	gothic.Store = pgSessionStore
	auth0Provider()
	gplusProvider()
	openidConnectProvider()

	r.GET("/auth/:provider", func(c echo.Context) error {
		providerToGoContext(c)
		// try to get the user without re-authenticating
		user, err := gothic.CompleteUserAuth(c.Response(), c.Request())
		if err != nil {
			gothic.BeginAuthHandler(c.Response(), c.Request())
			return nil
		}
		return c.JSON(http.StatusOK, user)
	})

	r.GET("/auth/:provider/logout", func(c echo.Context) error {
		gothic.Logout(c.Response(), c.Request())
		session, err := pgSessionStore.Get(c.Request(), "bg_login")
		if session != nil {
			session.Options.MaxAge = -1
			session.Values = make(map[interface{}]interface{})
			if err = session.Save(c.Request(), c.Response()); err != nil {
				c.Logger().Warnf("logout: Failed to save session: %v", err)
			}
		}
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
		splitEmail := strings.SplitN(user.Email, "@", 2)
		if len(splitEmail) != 2 || splitEmail[1] != "brightgate.com" {
			return echo.NewHTTPError(http.StatusUnauthorized, "🙀 bad user domain, sorry.")
		}
		userID := c.Param("provider") + "|" + user.UserID

		// TODO: We need to reconcile the user information with our database's
		// notion of a user (which doesn't exist yet).

		// As per
		// http://www.gorillatoolkit.org/pkg/sessions#CookieStore.Get,
		// Get() 'returns a new session and an error if the session
		// exists but could not be decoded.'  For our purposes, we just
		// want to blow over top of an invalid session, so drive on in
		// that case.
		session, err := pgSessionStore.Get(c.Request(), "bg_login")
		if err != nil && session == nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err)
		}
		session.Values["email"] = user.Email
		session.Values["userid"] = userID
		session.Values["auth_time"] = time.Now().Format(time.RFC3339)

		if err = session.Save(c.Request(), c.Response()); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err)
		}
		return c.JSON(http.StatusOK, user)
	})
}
