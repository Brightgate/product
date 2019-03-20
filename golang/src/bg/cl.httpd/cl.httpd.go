//
// COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
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
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"bg/cl_common/clcfg"
	"bg/cl_common/daemonutils"
	"bg/cloud_models/appliancedb"
	"bg/cloud_models/sessiondb"
	"bg/common/cfgapi"

	"github.com/gorilla/sessions"
	"github.com/sfreiberg/gotwilio"
	"github.com/tomazk/envcfg"

	// Echo
	"github.com/antonlindstrom/pgstore"
	"github.com/labstack/echo"
	"github.com/labstack/echo-contrib/session"
	"github.com/labstack/echo/middleware"
	"github.com/satori/uuid"
	"github.com/unrolled/secure"
)

const pname = "cl.httpd"

// Cfg contains the environment variable-based configuration settings
type Cfg struct {
	// The certificate hostname is the primary hostname associated
	// with the SSL certificate, and not necessarily the nodename.
	// We use this variable to navigate the Let's Encrypt directory
	// hierarchy.
	CertHostname string `envcfg:"B10E_CERT_HOSTNAME"`
	// Enable various debug stuff, non-priv port #s, relax secure MW, etc.
	Developer                 bool   `envcfg:"B10E_CLHTTPD_DEVELOPER"`
	DisableTLS                bool   `envcfg:"B10E_CLHTTPD_DISABLE_TLS"`
	HTTPListen                string `envcfg:"B10E_CLHTTPD_HTTP_LISTEN"`
	HTTPSListen               string `envcfg:"B10E_CLHTTPD_HTTPS_LISTEN"`
	WellKnownPath             string `envcfg:"B10E_CERTBOT_WELLKNOWN_PATH"`
	SessionSecret             string `envcfg:"B10E_CLHTTPD_SESSION_SECRET"`
	SessionBlockSecret        string `envcfg:"B10E_CLHTTPD_SESSION_BLOCK_SECRET"`
	AccountSecret             string `envcfg:"B10E_CLHTTPD_ACCOUNT_SECRET"`
	GoogleKey                 string `envcfg:"B10E_CLHTTPD_GOOGLE_KEY"`
	GoogleSecret              string `envcfg:"B10E_CLHTTPD_GOOGLE_SECRET"`
	Auth0Key                  string `envcfg:"B10E_CLHTTPD_AUTH0_KEY"`
	Auth0Secret               string `envcfg:"B10E_CLHTTPD_AUTH0_SECRET"`
	Auth0Domain               string `envcfg:"B10E_CLHTTPD_AUTH0_DOMAIN"`
	OpenIDConnectKey          string `envcfg:"B10E_CLHTTPD_OPENID_CONNECT_KEY"`
	OpenIDConnectSecret       string `envcfg:"B10E_CLHTTPD_OPENID_CONNECT_SECRET"`
	OpenIDConnectDiscoveryURL string `envcfg:"B10E_CLHTTPD_OPENID_CONNECT_DISCOVERY_URL"`
	AzureADV2Key              string `envcfg:"B10E_CLHTTPD_AZUREADV2_KEY"`
	AzureADV2Secret           string `envcfg:"B10E_CLHTTPD_AZUREADV2_SECRET"`
	SessionDB                 string `envcfg:"B10E_CLHTTPD_POSTGRES_SESSIONDB"`
	ApplianceDB               string `envcfg:"B10E_CLHTTPD_POSTGRES_APPLIANCEDB"`
	ConfigdConnection         string `envcfg:"B10E_CLHTTPD_CLCONFIGD_CONNECTION"`
	TwilioSID                 string `envcfg:"B10E_CLHTTPD_TWILIO_SID"`
	TwilioAuthToken           string `envcfg:"B10E_CLHTTPD_TWILIO_AUTHTOKEN"`
	// Whether to Disable TLS for outbound connections to cl.configd
	ConfigdDisableTLS bool   `envcfg:"B10E_CLHTTPD_CLCONFIGD_DISABLE_TLS"`
	AppPath           string `enccfg:"B10E_CLHTTPD_APP"`
}

const (
	checkMark = `✔︎ `

	// CSP matched to that of ap.httpd, anticipating web hoist.
	contentSecurityPolicy = "default-src 'self' 'unsafe-inline' 'unsafe-eval'; img-src 'self' data: 'unsafe-inline' 'unsafe-eval'; frame-ancestors 'none'"
)

var (
	environ          Cfg
	enableConfigdTLS bool
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
	if environ.SessionBlockSecret == "" {
		log.Fatalf("You must set B10E_CLHTTPD_SESSION_BLOCK_SECRET")
	}
	blockSecret, err := hex.DecodeString(environ.SessionBlockSecret)
	if err != nil || len(blockSecret) != 32 {
		log.Fatalf("Failed to decode B10E_CLHTTPD_SESSION_BLOCK_SECRET; should be hex encoded and 32 bytes long")
	}
	sessionDB, err := sessiondb.Connect(environ.SessionDB, false)
	if err != nil {
		log.Fatalf("failed to connect to session DB: %v", err)
	}
	log.Printf(checkMark + "Connected to Session DB")
	err = sessionDB.Ping()
	if err != nil {
		log.Fatalf("failed to ping DB: %s", err)
	}
	log.Printf(checkMark + "Pinged Session DB")
	pgStore, err := pgstore.NewPGStoreFromPool(sessionDB.GetPG(), []byte(environ.SessionSecret), blockSecret)
	if err != nil {
		log.Fatalf("failed to start PG Store: %s", err)
	}
	// Defaults to 4K but some providers (Azure) issue enormous tokens
	pgStore.MaxLength(32768)
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

func mkRouterHTTPS(sessionStore sessions.Store) *echo.Echo {
	wellKnownPath := "/var/www/html/.well-known"
	if environ.WellKnownPath != "" {
		wellKnownPath = environ.WellKnownPath
	}
	appPath := filepath.Join(daemonutils.ClRoot(), "lib", "cl.httpd-web")
	if environ.AppPath != "" {
		appPath = environ.AppPath
	}

	applianceDB, err := appliancedb.Connect(environ.ApplianceDB)
	if err != nil {
		log.Fatalf("failed to connect to appliance DB: %v", err)
	}
	log.Printf(checkMark + "Connected to Appliance DB")
	err = applianceDB.Ping()
	if err != nil {
		log.Fatalf("failed to ping DB: %s", err)
	}
	log.Printf(checkMark + "Pinged Appliance DB")

	r := echo.New()
	r.Debug = environ.Developer
	r.HideBanner = true
	r.Use(middleware.Logger())
	r.Use(mkSecureMW())
	r.Use(middleware.Recover())
	r.Use(session.Middleware(sessionStore))
	r.Static("/.well-known", wellKnownPath)
	cwp := filepath.Join(appPath, "client-web")
	r.Static("/client-web", cwp)
	log.Printf("Serving %s as /client-web/", cwp)
	r.GET("/", func(c echo.Context) error {
		return c.Redirect(http.StatusTemporaryRedirect, "/client-web/")
	})
	_ = newAuthHandler(r, sessionStore, applianceDB)

	if environ.AccountSecret == "" {
		log.Fatalf("Must specify B10E_CLHTTPD_ACCOUNT_SECRET")
	}
	accountSecret, err := hex.DecodeString(environ.AccountSecret)
	if err != nil || len(accountSecret) != 32 {
		log.Fatalf("Failed to decode B10E_CLHTTPD_ACCOUNT_SECRET; should be hex encoded and 32 bytes long %d", len(accountSecret))
	}
	applianceDB.AccountSecretsSetPassphrase(accountSecret)

	enableConfigdTLS = !environ.ConfigdDisableTLS && !environ.Developer
	if !enableConfigdTLS {
		log.Printf("Disabling TLS for connection to Configd")
	}

	var twil *gotwilio.Twilio
	if environ.TwilioSID != "" && environ.TwilioAuthToken != "" {
		twil = gotwilio.NewTwilioClient(environ.TwilioSID,
			environ.TwilioAuthToken)
	} else {
		log.Printf("Disabling Twilio Client")
	}

	wares := []echo.MiddlewareFunc{
		newSessionMiddleware(sessionStore).Process,
	}
	_ = newSiteHandler(r, applianceDB, wares, getConfigClientHandle, twil)
	_ = newAccountHandler(r, applianceDB, wares, sessionStore, getConfigClientHandle)
	hdl, err := getConfigClientHandle("00000000-0000-0000-0000-000000000000")
	if err != nil {
		log.Fatalf("failed to make Config Client: %s", err)
	}
	defer hdl.Close()
	err = hdl.Ping(context.Background())
	if err != nil {
		log.Fatalf("failed to Ping Config Client: %s", err)
	}
	log.Printf(checkMark + "Can connect to cl.configd")

	_ = newAccessHandler(r, applianceDB, sessionStore)
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

func getConfigClientHandle(cuuid string) (*cfgapi.Handle, error) {
	uu, err := uuid.FromString(cuuid)
	if err != nil {
		return nil, err
	}
	configd, err := clcfg.NewConfigd(pname, uu.String(),
		environ.ConfigdConnection, enableConfigdTLS)
	if err != nil {
		return nil, err
	}
	configHandle := cfgapi.NewHandle(configd)
	return configHandle, nil
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

	pgSessionStore := mkSessionStore()
	defer pgSessionStore.Close()
	defer pgSessionStore.StopCleanup(pgSessionStore.Cleanup(time.Minute * 5))

	eHTTPS := mkRouterHTTPS(pgSessionStore)

	var cfg *tls.Config
	if !environ.DisableTLS {
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
