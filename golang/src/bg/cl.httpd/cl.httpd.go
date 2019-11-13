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

	"cloud.google.com/go/storage"
	"github.com/sfreiberg/gotwilio"
	"github.com/tomazk/envcfg"

	// Echo
	"github.com/antonlindstrom/pgstore"
	"github.com/labstack/echo"
	"github.com/labstack/echo-contrib/session"
	"github.com/labstack/echo/middleware"
	gommonlog "github.com/labstack/gommon/log"
	"github.com/satori/uuid"
	"github.com/unrolled/secure"
)

const pname = "cl.httpd"

type getClientHandleFunc func(uuid string) (*cfgapi.Handle, error)

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
	AvatarBucket              string `envcfg:"B10E_CLHTTPD_AVATAR_BUCKET"`
	// Whether to Disable TLS for outbound connections to cl.configd
	ConfigdDisableTLS bool   `envcfg:"B10E_CLHTTPD_CLCONFIGD_DISABLE_TLS"`
	AppPath           string `enccfg:"B10E_CLHTTPD_APP"`
}

const (
	checkMark = `✔︎ `

	// CSP matched to that of ap.httpd, anticipating web hoist.
	contentSecurityPolicy = "default-src 'self' 'unsafe-inline' 'unsafe-eval'; img-src 'self' data: 'unsafe-inline' 'unsafe-eval'; frame-src https://brightgate.freshdesk.com/; frame-ancestors 'none'"
)

var (
	environ          Cfg
	enableConfigdTLS bool
)

func gracefulShutdown(e *echo.Echo) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := e.Shutdown(ctx); err != nil {
		e.Logger.Fatalf("Shutdown failed: %v", err)
	}
}

func mkTLSConfig() (*tls.Config, error) {
	// https (typically port 443) listener.
	certf := fmt.Sprintf("/etc/letsencrypt/live/%s/fullchain.pem",
		environ.CertHostname)
	keyf := fmt.Sprintf("/etc/letsencrypt/live/%s/privkey.pem",
		environ.CertHostname)
	keyPair, err := tls.LoadX509KeyPair(certf, keyf)
	if err != nil {
		return nil, fmt.Errorf("failed to load X509 Key Pair from %s, %s: %v", certf, keyf, err)
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
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA256,
			// Supports older Windows 7/8 and older MacOS
			tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA256,
		},
		Certificates: []tls.Certificate{keyPair},
		NextProtos:   []string{"h2"},
	}, nil
}

func mkSecureMW() echo.MiddlewareFunc {
	secureMW := secure.New(secure.Options{
		AllowedHosts:          []string{"svc0.b10e.net", "svc1.b10e.net", "build0.b10e.net:9443"},
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

type routerState struct {
	applianceDB     appliancedb.DataStore
	sessionDB       sessiondb.DataStore
	sessionStore    *pgstore.PGStore
	sessCleanupDone chan<- struct{}
	sessCleanupQuit <-chan struct{}
	echo            *echo.Echo
}

func (rs *routerState) Fini(ctx context.Context) {
	rs.applianceDB.Close()
	rs.sessionStore.StopCleanup(rs.sessCleanupDone, rs.sessCleanupQuit)
	rs.sessionStore.Close()
}

func (rs *routerState) mkSessionStore() {
	log := rs.echo.Logger
	log.Infof("Creating Session Store\n")
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
	rs.sessionDB, err = sessiondb.Connect(environ.SessionDB, false)
	if err != nil {
		log.Fatalf("failed to connect to session DB: %v", err)
	}
	log.Infof(checkMark + "Connected to Session DB")
	err = rs.sessionDB.Ping()
	if err != nil {
		log.Fatalf("failed to ping DB: %s", err)
	}
	log.Infof(checkMark + "Pinged Session DB")
	rs.sessionStore, err = pgstore.NewPGStoreFromPool(rs.sessionDB.GetPG(), []byte(environ.SessionSecret), blockSecret)
	if err != nil {
		log.Fatalf("failed to start PG Store: %s", err)
	}
	// Defaults to 4K but some providers (Azure) issue enormous tokens
	rs.sessionStore.MaxLength(32768)
	rs.sessCleanupDone, rs.sessCleanupQuit = rs.sessionStore.Cleanup(time.Minute * 5)
}

func (rs *routerState) mkApplianceDB() {
	var err error
	log := rs.echo.Logger
	// Appliancedb setup
	rs.applianceDB, err = appliancedb.Connect(environ.ApplianceDB)
	if err != nil {
		log.Fatalf("failed to connect to appliance DB: %v", err)
	}
	log.Infof(checkMark + "Created Appliance DB client")
	err = rs.applianceDB.Ping()
	if err != nil {
		log.Fatalf("failed to ping DB: %s", err)
	}
	log.Infof(checkMark + "Pinged Appliance DB")

	// Setup Account Secrets
	if environ.AccountSecret == "" {
		log.Fatalf("Must specify B10E_CLHTTPD_ACCOUNT_SECRET")
	}
	accountSecret, err := hex.DecodeString(environ.AccountSecret)
	if err != nil || len(accountSecret) != 32 {
		log.Fatalf("Failed to decode B10E_CLHTTPD_ACCOUNT_SECRET; should be hex encoded and 32 bytes long %d", len(accountSecret))
	}
	rs.applianceDB.AccountSecretsSetPassphrase(accountSecret)
	log.Infof(checkMark + "Appliance Secrets")
}

func mkRouterHTTPS() *routerState {
	var state routerState

	wellKnownPath := "/var/www/html/.well-known"
	if environ.WellKnownPath != "" {
		wellKnownPath = environ.WellKnownPath
	}
	appPath := filepath.Join(daemonutils.ClRoot(), "lib", "cl.httpd-web")
	if environ.AppPath != "" {
		appPath = environ.AppPath
	}

	// Setup Echo, Pt 1
	state.echo = echo.New()
	r := state.echo
	r.Debug = environ.Developer
	if r.Debug {
		r.Logger.SetLevel(gommonlog.DEBUG)
	} else {
		r.Logger.SetLevel(gommonlog.INFO)
	}
	r.HideBanner = true

	state.mkSessionStore()
	state.mkApplianceDB()

	// Configd setup
	enableConfigdTLS = !environ.ConfigdDisableTLS && !environ.Developer
	if !enableConfigdTLS {
		r.Logger.Warnf("Disabling TLS for connection to Configd")
	}
	hdl, err := getConfigClientHandle(appliancedb.NullSiteUUID.String())
	if err != nil {
		r.Logger.Fatalf("failed to make Config Client: %s", err)
	}
	defer hdl.Close()
	err = hdl.Ping(context.Background())
	if err != nil {
		r.Logger.Fatalf("failed to Ping Config Client: %s", err)
	}
	r.Logger.Infof(checkMark + "Pinged cl.configd")

	// Twilio setup
	var twil *gotwilio.Twilio
	if environ.TwilioSID != "" && environ.TwilioAuthToken != "" {
		twil = gotwilio.NewTwilioClient(environ.TwilioSID,
			environ.TwilioAuthToken)
		r.Logger.Infof(checkMark + "Setup Twilio Client")
	} else {
		r.Logger.Warnf("Disabling Twilio Client")
	}

	r.Use(middleware.Logger())
	r.Use(mkSecureMW())
	r.Use(middleware.Recover())
	r.Use(session.Middleware(state.sessionStore))
	r.Static("/.well-known", wellKnownPath)
	cwp := filepath.Join(appPath, "client-web")
	r.Static("/client-web", cwp)
	r.Logger.Infof("Serving %s as /client-web/", cwp)
	r.GET("/", func(c echo.Context) error {
		return c.Redirect(http.StatusTemporaryRedirect, "/client-web/")
	})

	// Avatar GCS Setup
	var avBucketName string
	if environ.AvatarBucket != "" {
		avBucketName = environ.AvatarBucket
	} else {
		if environ.Developer {
			avBucketName = "bg-appliance-dev-avatars"
		} else {
			r.Logger.Fatalf("Must specify Avatar Storage Bucket B10E_CLHTTPD_AVATAR_BUCKET")
		}
	}

	// Setup /auth endpoints
	gcs, err := storage.NewClient(context.Background())
	if err != nil {
		r.Logger.Fatalf("failed to make gcs client: %s", err)
	}
	avBucket := gcs.Bucket(avBucketName)
	avBucketAttrs, err := avBucket.Attrs(context.Background())
	if err != nil {
		r.Logger.Fatalf("failed to get bucket attrs: %s", err)
	}
	r.Logger.Infof(checkMark+"Setup Avatar Bucket '%s'", avBucketAttrs.Name)

	_ = newAuthHandler(r, state.sessionStore, state.applianceDB, avBucket)

	wares := []echo.MiddlewareFunc{
		newSessionMiddleware(state.sessionStore).Process,
	}
	_ = newSiteHandler(r, state.applianceDB, wares, getConfigClientHandle, twil)
	_ = newAccountHandler(r, state.applianceDB, wares, state.sessionStore, avBucket, getConfigClientHandle)
	_ = newOrgHandler(r, state.applianceDB, wares, state.sessionStore)
	_ = newAccessHandler(r, state.applianceDB, state.sessionStore)

	// Setup /check endpoints
	_ = newCheckHandler(r, getConfigClientHandle)

	return &state
}

func mkRouterHTTP() *echo.Echo {
	r := echo.New()
	r.Debug = environ.Developer
	if r.Debug {
		r.Logger.SetLevel(gommonlog.DEBUG)
	} else {
		r.Logger.SetLevel(gommonlog.INFO)
	}
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
	startupLog := gommonlog.New("startup")
	startupLog.Infof("start")
	err = envcfg.Unmarshal(&environ)
	if err != nil {
		startupLog.Fatalf("Environment Error: %s", err)
	}

	if environ.Developer {
		startupLog.Infof("environ %v", environ)
	}

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

	rsHTTPS := mkRouterHTTPS()
	defer rsHTTPS.Fini(context.Background())

	var cfg *tls.Config
	if !environ.DisableTLS {
		cfg, err = mkTLSConfig()
		if err != nil {
			rsHTTPS.echo.Logger.Fatalf("Failed to setup TLS: %v", err)
		}
	}

	httpsSrv := &http.Server{
		Addr:      environ.HTTPSListen,
		TLSConfig: cfg,
	}

	go func() {
		if err := rsHTTPS.echo.StartServer(httpsSrv); err != nil {
			rsHTTPS.echo.Logger.Infof("shutting down HTTPS service: %v", err)
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
	eHTTP.Logger.Infof("Signal (%v) received, shutting down", s)

	gracefulShutdown(rsHTTPS.echo)
	gracefulShutdown(eHTTP)
	eHTTP.Logger.Infof("All servers shut down, goodbye.")
}
