//
// COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
//
// This copyright notice is Copyright Management Information under 17 USC 1202
// and is included to protect this work and deter copyright infringement.
// Removal or alteration of this Copyright Management Information without the
// express written permission of Brightgate Inc is prohibited, and any
// such unauthorized removal or alteration will be a violation of federal law.
//

//
// cl.httpd: cloud HTTP server

// XXX Review 12 factor application.  The SSL key and certificate
// material will come from Let's Encrypt directory to start.

// Because we have to serve static files to meet ACME HTTP-01
// authentication on renewal, we need to anchor the https:///.well-known
// directory hierarchy.  In the current Debian-based deployment, this
// location defaults to "/var/www/html/.well-known".
//

//
// This daemon also supports "developer mode" which disables TLS, and runs on
// an unprivileged port.  This is not suitable for production.
//

package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"bg/cl_common/daemonutils"
	"bg/cloud_models/sessiondb"

	"github.com/tomazk/envcfg"

	// Echo
	"github.com/antonlindstrom/pgstore"
	"github.com/labstack/echo"
	"github.com/labstack/echo-contrib/session"
	"github.com/labstack/echo/middleware"
	"github.com/unrolled/secure"
)

// Cfg contains the environment variable-based configuration settings
type Cfg struct {
	// The certificate hostname is the primary hostname associated
	// with the SSL certificate, and not necessarily the nodename.
	// We use this variable to navigate the Let's Encrypt directory
	// hierarchy.
	CertHostname string `envcfg:"B10E_CERT_HOSTNAME"`
	// Disable TLS, Enable various debug stuff, relax secure MW, etc.
	Developer                 bool   `envcfg:"B10E_CLHTTPD_DEVELOPER"`
	HTTPListen                string `envcfg:"B10E_CLHTTPD_HTTP_LISTEN"`
	HTTPSListen               string `envcfg:"B10E_CLHTTPD_HTTPS_LISTEN"`
	WellKnownPath             string `envcfg:"B10E_CERTBOT_WELLKNOWN_PATH"`
	SessionSecret             string `envcfg:"B10E_CLHTTPD_SESSION_SECRET"`
	GPlusKey                  string `envcfg:"B10E_CLHTTPD_GPLUS_KEY"`
	GPlusSecret               string `envcfg:"B10E_CLHTTPD_GPLUS_SECRET"`
	Auth0Key                  string `envcfg:"B10E_CLHTTPD_AUTH0_KEY"`
	Auth0Secret               string `envcfg:"B10E_CLHTTPD_AUTH0_SECRET"`
	Auth0Domain               string `envcfg:"B10E_CLHTTPD_AUTH0_DOMAIN"`
	OpenIDConnectKey          string `envcfg:"B10E_CLHTTPD_OPENID_CONNECT_KEY"`
	OpenIDConnectSecret       string `envcfg:"B10E_CLHTTPD_OPENID_CONNECT_SECRET"`
	OpenIDConnectDiscoveryURL string `envcfg:"B10E_CLHTTPD_OPENID_CONNECT_DISCOVERY_URL"`
	PostgresConnection        string `envcfg:"B10E_CLHTTPD_POSTGRES_CONNECTION"`
	AppPath                   string `enccfg:"B10E_CLHTTPD_APP"`
}

const (
	checkMark = `✔︎ `

	// CSP matched to that of ap.httpd, anticipating web hoist.
	contentSecurityPolicy = "default-src 'self' 'unsafe-inline' 'unsafe-eval'; img-src 'self' data: 'unsafe-inline' 'unsafe-eval'; frame-ancestors 'none'"
)

var (
	environ        Cfg
	pgSessionStore *pgstore.PGStore
)

func gracefulShutdown(h *http.Server) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := h.Shutdown(ctx); err != nil {
		log.Fatalf("Shutdown failed: %v", err)
	}
}

func mkTLSConfig() *tls.Config {
	// https (typically port 443) listener.
	certf := fmt.Sprintf("/etc/letsencrypt/live/%s/fullchain.pem",
		environ.CertHostname)
	keyf := fmt.Sprintf("/etc/letsencrypt/live/%s/privkey.pem",
		environ.CertHostname)
	keyPair, err := tls.LoadX509KeyPair(certf, keyf)
	if err != nil {
		log.Fatalf("failed to load X509 Key Pair from %s, %s: %v", certf, keyf, err)
	}

	return &tls.Config{
		MinVersion:               tls.VersionTLS12,
		CurvePreferences:         []tls.CurveID{tls.CurveP521, tls.CurveP384, tls.CurveP256},
		PreferServerCipherSuites: true,
		// The ciphers below are chosen for high security and enough
		// browser compatibility for our expected user base using the
		// qualys tool at https://www.ssllabs.com/ssltest/.
		// As of Sep. 2018 we earn an A+ rating.
		CipherSuites: []uint16{
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			// Supports older Windows 7/8 and older MacOS
			tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA256,
		},
		Certificates: []tls.Certificate{keyPair},
	}
}

func mkSessionStore() *pgstore.PGStore {
	if environ.SessionSecret == "" {
		log.Fatalf("You must set B10E_CLHTTPD_SESSION_SECRET")
	}
	sessionDB, err := sessiondb.Connect(environ.PostgresConnection, false)
	if err != nil {
		log.Fatalf("failed to connect to DB: %v", err)
	}
	log.Printf(checkMark + "Connected to Session DB")
	err = sessionDB.Ping()
	if err != nil {
		log.Fatalf("failed to ping DB: %s", err)
	}
	log.Printf(checkMark + "Pinged Session DB")
	pgStore, err := pgstore.NewPGStoreFromPool(sessionDB.GetPG(), []byte(environ.SessionSecret))
	if err != nil {
		log.Fatalf("failed to start PG Store: %s", err)
	}
	return pgStore
}

func mkSecureMW() echo.MiddlewareFunc {
	secureMW := secure.New(secure.Options{
		AllowedHosts:          []string{"svc0.b10e.net", "svc1.b10e.net"},
		HostsProxyHeaders:     []string{"X-Forwarded-Host"},
		STSSeconds:            315360000,
		STSIncludeSubdomains:  true,
		STSPreload:            true,
		FrameDeny:             true,
		ContentTypeNosniff:    true,
		BrowserXssFilter:      true,
		ContentSecurityPolicy: contentSecurityPolicy,
		IsDevelopment:         environ.Developer,
	})
	return echo.WrapMiddleware(secureMW.Handler)
}

func mkRouterHTTPS() *echo.Echo {
	wellKnownPath := "/var/www/html/.well-known"
	if environ.WellKnownPath != "" {
		wellKnownPath = environ.WellKnownPath
	}
	appPath := filepath.Join(daemonutils.ClRoot(), "lib", "cl.httpd-web")
	if environ.AppPath != "" {
		appPath = environ.AppPath
	}

	htmlFormat := `<html><body>%v</body></html>`

	r := echo.New()
	r.Debug = environ.Developer
	r.HideBanner = true
	r.Use(middleware.Logger())
	r.Use(mkSecureMW())
	r.Use(middleware.Recover())
	r.Use(session.Middleware(pgSessionStore))
	r.Static("/.well-known", wellKnownPath)
	r.Static("/app", appPath)
	log.Printf("Serving %s as /app/", appPath)
	r.GET("/", func(c echo.Context) error {
		html := fmt.Sprintf(htmlFormat, `
<p><a href="/auth/auth0">Login with Auth0</a></p>
<p><a href="/auth/gplus">Login with Google</a></p>
<p><a href="/auth/openid-connect">Login with Google (OpenID Connect)</a></p>
`)

		sess, err := pgSessionStore.Get(c.Request(), "bg_login")
		if err == nil {
			var email string
			email, ok := sess.Values["email"].(string)
			if ok {
				html += fmt.Sprintf("<p>Hello there; I think you are: '%v'</p>", email)
			} else {
				html += fmt.Sprintf("<p>Hello there; Log in so I know who you are.</p>")
			}
		} else {
			html += fmt.Sprintf("<p>Error was: %v</p>", err)
		}
		return c.HTML(http.StatusOK, html)
	})
	routeAuth(r)

	//ginPrometheus.Use(r)
	return r
}

func mkRouterHTTP() *echo.Echo {
	r := echo.New()
	r.Debug = environ.Developer
	r.HideBanner = true
	r.Use(middleware.Logger())
	r.Use(mkSecureMW())
	r.Use(middleware.Recover())
	r.Use(middleware.HTTPSRedirect())
	return r
}

func main() {
	var err error

	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	err = envcfg.Unmarshal(&environ)
	if err != nil {
		log.Fatalf("Environment Error: %s", err)
	}

	log.Printf("environ %v", environ)

	if environ.HTTPListen == "" {
		if environ.Developer {
			environ.HTTPListen = ":9080"
		} else {
			environ.HTTPListen = ":80"
		}
	}
	if environ.HTTPSListen == "" {
		if environ.Developer {
			environ.HTTPSListen = ":9443"
		} else {
			environ.HTTPSListen = ":443"
		}
	}

	pgSessionStore = mkSessionStore()
	defer pgSessionStore.Close()
	defer pgSessionStore.StopCleanup(pgSessionStore.Cleanup(time.Minute * 5))

	eHTTPS := mkRouterHTTPS()
	// In developer mode, disable TLS
	var cfg *tls.Config
	if !environ.Developer {
		cfg = mkTLSConfig()
	}

	httpsSrv := &http.Server{
		Addr:         environ.HTTPSListen,
		TLSConfig:    cfg,
		TLSNextProto: make(map[string]func(*http.Server, *tls.Conn, http.Handler)),
	}

	go func() {
		if err := eHTTPS.StartServer(httpsSrv); err != nil {
			eHTTPS.Logger.Info("shutting down HTTPS service")
		}
	}()

	// http (typically port 80) listener.
	httpSrv := &http.Server{
		Addr: environ.HTTPListen,
	}
	eHTTP := mkRouterHTTP()

	go func() {
		if err := eHTTP.StartServer(httpSrv); err != nil {
			eHTTP.Logger.Info("shutting down HTTP service")
		}
	}()

	sig := make(chan os.Signal)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	s := <-sig
	log.Printf("Signal (%v) received, shutting down", s)

	gracefulShutdown(httpSrv)
	gracefulShutdown(httpsSrv)
	log.Printf("All servers shut down, goodbye.")
}
