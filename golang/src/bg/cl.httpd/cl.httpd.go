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

	"github.com/tomazk/envcfg"

	"github.com/labstack/echo"
	"github.com/labstack/echo/middleware"

	"github.com/unrolled/secure"
)

// Cfg contains the environment variable-based configuration settings
type Cfg struct {
	// The certificate hostname is the primary hostname associated
	// with the SSL certificate, and not necessarily the nodename.
	// We use this variable to navigate the Let's Encrypt directory
	// hierarchy.
	CertHostname              string `envcfg:"B10E_CERT_HOSTNAME"`
	Developer                 bool   `envcfg:"B10E_CLHTTPD_DEVMODE"`
	HTTPListen                string `envcfg:"B10E_CLHTTPD_HTTP_LISTEN"`
	HTTPSListen               string `envcfg:"B10E_CLHTTPD_HTTPS_LISTEN"`
	WellKnownPath             string `envcfg:"B10E_CERTBOT_WELLKNOWN_PATH"`
	GPlusKey                  string `envcfg:"B10E_CLHTTPD_GPLUS_KEY"`
	GPlusSecret               string `envcfg:"B10E_CLHTTPD_GPLUS_SECRET"`
	OpenIDConnectKey          string `envcfg:"B10E_CLHTTPD_OPENID_CONNECT_KEY"`
	OpenIDConnectSecret       string `envcfg:"B10E_CLHTTPD_OPENID_CONNECT_SECRET"`
	OpenIDConnectDiscoveryURL string `envcfg:"B10E_CLHTTPD_OPENID_CONNECT_DISCOVERY_URL"`
	AppPath                   string `enccfg:"B10E_CLHTTPD_APP"`
}

const (
	pname = "cl.httpd"

	// CSP matched to that of ap.httpd, anticipating web hoist.
	contentSecurityPolicy = "default-src 'self' 'unsafe-inline' 'unsafe-eval'; img-src 'self' data: 'unsafe-inline' 'unsafe-eval'; frame-ancestors 'none'"
)

var (
	environ Cfg
)

func gracefulShutdown(h *http.Server) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := h.Shutdown(ctx); err != nil {
		log.Fatalf("Shutdown failed: %v", err)
	}
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
	r.Static("/.well-known", wellKnownPath)
	r.Static("/app", appPath)
	log.Printf("Serving %s as /app/", appPath)
	r.GET("/", func(c echo.Context) error {
		html := fmt.Sprintf(htmlFormat, `
<p><a href="/auth/gplus">Login with Google</a></p>
<p><a href="/auth/openid-connect">Login with Google (OpenID Connect)</a></p>
`)
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
		environ.HTTPListen = ":80"
	}
	if environ.HTTPSListen == "" {
		environ.HTTPSListen = ":443"
	}

	// https (typically port 443) listener.
	certf := fmt.Sprintf("/etc/letsencrypt/live/%s/fullchain.pem",
		environ.CertHostname)
	keyf := fmt.Sprintf("/etc/letsencrypt/live/%s/privkey.pem",
		environ.CertHostname)
	keyPair, err := tls.LoadX509KeyPair(certf, keyf)
	if err != nil {
		panic("XXX keys!")
	}

	cfg := &tls.Config{
		MinVersion:               tls.VersionTLS12,
		CurvePreferences:         []tls.CurveID{tls.CurveP521, tls.CurveP384, tls.CurveP256},
		PreferServerCipherSuites: true,
		// Validate the cipher suites, and this configuration using
		// https://www.ssllabs.com/ssltest/
		CipherSuites: []uint16{
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
		},
		Certificates: []tls.Certificate{keyPair},
	}

	httpsSrv := &http.Server{
		Addr:         environ.HTTPSListen,
		TLSConfig:    cfg,
		TLSNextProto: make(map[string]func(*http.Server, *tls.Conn, http.Handler)),
	}
	eHTTPS := mkRouterHTTPS()

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
