//
// Copyright 2020 Brightgate Inc.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
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
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"bg/cl_common/certificate"
	"bg/cl_common/clcfg"
	"bg/cl_common/daemonutils"
	"bg/cl_common/echozap"
	"bg/cl_common/pgutils"
	"bg/cl_common/vaultdb"
	"bg/cl_common/vaultgcpauth"
	"bg/cl_common/vaulttags"
	"bg/cl_common/vaulttokensource"
	"bg/cl_common/zapgommon"
	"bg/cloud_models/appliancedb"
	"bg/cloud_models/sessiondb"
	"bg/common/cfgapi"

	"cloud.google.com/go/compute/metadata"
	"cloud.google.com/go/storage"
	"golang.org/x/oauth2"
	"google.golang.org/api/option"

	vault "github.com/hashicorp/vault/api"
	"github.com/sfreiberg/gotwilio"
	"github.com/tomazk/envcfg"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	// Echo
	"github.com/antonlindstrom/pgstore"
	"github.com/labstack/echo"
	"github.com/labstack/echo-contrib/session"
	"github.com/labstack/echo/middleware"
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
	GenerateCert bool   `envcfg:"B10E_GENERATE_CERT"`
	// Enable various debug stuff, non-priv port #s, relax secure MW, etc.
	Developer                 bool   `envcfg:"B10E_CLHTTPD_DEVELOPER"`
	DisableTLS                bool   `envcfg:"B10E_CLHTTPD_DISABLE_TLS"`
	LoadBalanced              bool   `envcfg:"B10E_CLHTTPD_LOAD_BALANCED"`
	AllowedHosts              string `envcfg:"B10E_CLHTTPD_ALLOWED_HOSTS"`
	HTTPListen                string `envcfg:"B10E_CLHTTPD_HTTP_LISTEN"`
	HTTPSListen               string `envcfg:"B10E_CLHTTPD_HTTPS_LISTEN"`
	WellKnownPath             string `envcfg:"B10E_CERTBOT_WELLKNOWN_PATH"`
	VaultAuthPath             string `envcfg:"B10E_CLHTTPD_VAULT_AUTH_PATH"`
	VaultKVPath               string `envcfg:"B10E_CLHTTPD_VAULT_KV_PATH"`
	VaultKVComponent          string `envcfg:"B10E_CLHTTPD_VAULT_KV_COMPONENT"`
	VaultDBPath               string `envcfg:"B10E_CLHTTPD_VAULT_DB_PATH"`
	VaultDBRole               string `envcfg:"B10E_CLHTTPD_VAULT_DB_ROLE"`
	VaultGCPPath              string `envcfg:"B10E_CLHTTPD_VAULT_GCP_PATH"`
	VaultGCPRole              string `envcfg:"B10E_CLHTTPD_VAULT_GCP_ROLE"`
	Auth0Domain               string `envcfg:"B10E_CLHTTPD_AUTH0_DOMAIN"`
	OpenIDConnectDiscoveryURL string `envcfg:"B10E_CLHTTPD_OPENID_CONNECT_DISCOVERY_URL"`
	// These should have no credential information in them; instead, set the
	// VaultDB* variables appropriately and pull from Vault.
	SessionDB         string `envcfg:"B10E_CLHTTPD_POSTGRES_SESSIONDB"`
	ApplianceDB       string `envcfg:"B10E_CLHTTPD_POSTGRES_APPLIANCEDB"`
	ConfigdConnection string `envcfg:"B10E_CLHTTPD_CLCONFIGD_CONNECTION"`
	AvatarBucket      string `envcfg:"B10E_CLHTTPD_AVATAR_BUCKET"`
	// Whether to Disable TLS for outbound connections to cl.configd
	ConfigdDisableTLS bool   `envcfg:"B10E_CLHTTPD_CLCONFIGD_DISABLE_TLS"`
	AppPath           string `enccfg:"B10E_CLHTTPD_APP"`
}

type kvSecrets struct {
	SessionSecret       string `envcfg:"B10E_CLHTTPD_SESSION_SECRET" vault:"session/secret"`
	SessionBlockSecret  string `envcfg:"B10E_CLHTTPD_SESSION_BLOCK_SECRET" vault:"session/block_secret"`
	AccountSecret       string `envcfg:"B10E_CLHTTPD_ACCOUNT_SECRET" vault:"account/secret"`
	GoogleKey           string `envcfg:"B10E_CLHTTPD_GOOGLE_KEY" vault:"google/key"`
	GoogleSecret        string `envcfg:"B10E_CLHTTPD_GOOGLE_SECRET" vault:"google/secret"`
	Auth0Key            string `envcfg:"B10E_CLHTTPD_AUTH0_KEY" vault:"auth0/key"`
	Auth0Secret         string `envcfg:"B10E_CLHTTPD_AUTH0_SECRET" vault:"auth0/secret"`
	OpenIDConnectKey    string `envcfg:"B10E_CLHTTPD_OPENID_CONNECT_KEY" vault:"openid/key"`
	OpenIDConnectSecret string `envcfg:"B10E_CLHTTPD_OPENID_CONNECT_SECRET" vault:"openid/secret"`
	AzureADV2Key        string `envcfg:"B10E_CLHTTPD_AZUREADV2_KEY" vault:"azureadv2/key"`
	AzureADV2Secret     string `envcfg:"B10E_CLHTTPD_AZUREADV2_SECRET" vault:"azureadv2/secret"`
	TwilioSID           string `envcfg:"B10E_CLHTTPD_TWILIO_SID" vault:"twilio/sid"`
	TwilioAuthToken     string `envcfg:"B10E_CLHTTPD_TWILIO_AUTHTOKEN" vault:"twilio/authtoken"`
}

const (
	checkMark = `✔︎ `

	// CSP matched to that of ap.httpd.
	contentSecurityPolicy = "default-src 'self'; script-src 'self'; img-src 'self' data:; font-src 'self' data:; frame-src https://brightgate.freshdesk.com/; style-src 'self' 'unsafe-inline'; frame-ancestors 'none'"

	defaultHTTPListen  = ":80"
	defaultHTTPSListen = ":443"
)

var (
	environ          Cfg
	secrets          kvSecrets
	enableConfigdTLS bool
	lbName           string
	useVaultForDB    bool
	useVaultForKV    bool
)

func gracefulShutdown(e *echo.Echo) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := e.Shutdown(ctx); err != nil {
		e.Logger.Fatalf("Shutdown failed: %v", err)
	}
}

func mkTLSConfig() (*tls.Config, error) {
	var keyPair tls.Certificate
	var err error
	if environ.GenerateCert {
		// Behind an HTTPS load-balancer proxy, we need to use a
		// key/cert pair, even if they don't correspond to the
		// host being contacted.
		keyb, certb, err := certificate.CreateSSKeyCert(environ.CertHostname)
		if err != nil {
			return nil, fmt.Errorf("generate self-signed cert failed: %v", err)
		}
		keyPair, err = tls.X509KeyPair(certb, keyb)
		if err != nil {
			return nil, fmt.Errorf("failed to generate X509 Key Pair: %v", err)
		}
	} else {
		certf := fmt.Sprintf("/etc/letsencrypt/live/%s/fullchain.pem",
			environ.CertHostname)
		keyf := fmt.Sprintf("/etc/letsencrypt/live/%s/privkey.pem",
			environ.CertHostname)

		keyPair, err = tls.LoadX509KeyPair(certf, keyf)
		if err != nil {
			return nil, fmt.Errorf("failed to load X509 Key Pair from %s, %s: %v", certf, keyf, err)
		}
	}

	return &tls.Config{
		MinVersion:               tls.VersionTLS12,
		CurvePreferences:         []tls.CurveID{tls.CurveP521, tls.CurveP384, tls.CurveP256},
		PreferServerCipherSuites: true,
		// The ciphers below are chosen for high security and enough
		// browser compatibility for our expected user base using the
		// qualys tool at https://www.ssllabs.com/ssltest/.
		// As of Dec. 2019 we earn an A+ rating.
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

func mkSecureMW(log *zap.Logger) echo.MiddlewareFunc {
	slog := log.Sugar()

	allowedHosts := strings.Split(environ.AllowedHosts, ",")
	// Work around weird Split() behavior
	if len(allowedHosts) == 1 && allowedHosts[0] == "" {
		allowedHosts = []string{}
	}

	if len(allowedHosts) == 0 {
		// This just gets the first one.
		if ip, err := metadata.InternalIP(); err == nil && ip != "" {
			if environ.HTTPSListen != defaultHTTPSListen {
				ip += environ.HTTPSListen
			}
			allowedHosts = append(allowedHosts, ip)
		} else if err != nil {
			slog.Warnf("Unable to retrieve internal IP address: %v", err)
		}

		if ip, err := metadata.ExternalIP(); err == nil && ip != "" {
			allowedHosts = append(allowedHosts, ip)
		} else if err != nil {
			slog.Warnf("Unable to retrieve external IP address: %v", err)
		}

		// IP address assigned to the load balancer
		if ip, err := metadata.InstanceAttributeValue("lb-ip"); err == nil && ip != "" {
			allowedHosts = append(allowedHosts, ip)
		} else if err != nil {
			slog.Warnf("Unable to retrieve load balancer IP address: %v", err)
		}

		// Name assigned to the load balancer
		if lbName != "" {
			allowedHosts = append(allowedHosts, lbName)
		}
	}

	if len(allowedHosts) == 0 {
		slog.Fatalf("Unable to determine allowed hosts; set " +
			"$B10E_CLHTTPD_ALLOWED_HOSTS to override discovery")
	}
	slog.Infof("Accepting requests for %s", strings.Join(allowedHosts, ", "))

	secureMW := secure.New(secure.Options{
		AllowedHosts:          allowedHosts,
		HostsProxyHeaders:     []string{"X-Forwarded-Host"},
		SSLProxyHeaders:       map[string]string{"X-Forwarded-Proto": "https"},
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
	applianceVDBC   *vaultdb.Connector
	sessionDB       sessiondb.DataStore
	sessionVDBC     *vaultdb.Connector
	sessionStore    *pgstore.PGStore
	sessCleanupDone chan<- struct{}
	sessCleanupQuit <-chan struct{}
	echo            *echo.Echo
	logger          *zap.SugaredLogger
}

func (rs *routerState) Fini(ctx context.Context) {
	rs.applianceDB.Close()
	rs.sessionStore.StopCleanup(rs.sessCleanupDone, rs.sessCleanupQuit)
	rs.sessionStore.Close()
}

func (rs *routerState) mkSessionStore(vaultClient *vault.Client, notifier *daemonutils.FanOut) {
	log := rs.logger
	log.Info("Creating Session Store")
	if secrets.SessionSecret == "" {
		log.Fatalf("You must set B10E_CLHTTPD_SESSION_SECRET")
	}
	if secrets.SessionBlockSecret == "" {
		log.Fatalf("You must set B10E_CLHTTPD_SESSION_BLOCK_SECRET")
	}
	blockSecret, err := hex.DecodeString(secrets.SessionBlockSecret)
	if err != nil || len(blockSecret) != 32 {
		log.Fatalf("Failed to decode B10E_CLHTTPD_SESSION_BLOCK_SECRET; should be hex encoded and 32 bytes long")
	}

	dbURI := pgutils.AddApplication(environ.SessionDB, pname)

	if useVaultForDB {
		subLog := log.Named("vaultdb.sessiondb")
		vdbc := vaultdb.NewConnector(dbURI, vaultClient, notifier,
			environ.VaultDBPath, environ.VaultDBRole, subLog)
		rs.sessionDB, err = sessiondb.VaultConnect(vdbc, true)
		rs.sessionVDBC = vdbc
		if err != nil {
			log.Fatalf("failed configuring session DB from Vault: %v", err)
		}
	} else {
		rs.sessionDB, err = sessiondb.Connect(dbURI, false)
		if err != nil {
			log.Fatalf("failed to connect to session DB: %v", err)
		}
	}
	log.Infof(checkMark + "Connected to Session DB")
	err = rs.sessionDB.Ping()
	if err != nil {
		log.Fatalf("failed to ping DB: %s", err)
	}
	log.Infof(checkMark + "Pinged Session DB")

	rs.sessionStore, err = pgstore.NewPGStoreFromPool(rs.sessionDB.GetPG(), []byte(secrets.SessionSecret), blockSecret)
	if err != nil {
		log.Fatalf("failed to start PG Store: %s", err)
	}
	// Defaults to 4K but some providers (Azure) issue enormous tokens
	rs.sessionStore.MaxLength(32768)
	// One week
	rs.sessionStore.MaxAge(86400 * 7)
	rs.sessCleanupDone, rs.sessCleanupQuit = rs.sessionStore.Cleanup(time.Minute * 5)
}

func (rs *routerState) mkApplianceDB(vaultClient *vault.Client, notifier *daemonutils.FanOut) {
	var err error
	log := rs.logger

	dbURI := pgutils.AddApplication(environ.ApplianceDB, pname)

	// Appliancedb setup.  Use Vault if we successfully connected to it;
	// otherwise, assume that the environment has the necessary credentials.
	if useVaultForDB {
		subLog := log.Named("vaultdb.appliancedb")
		vdbc := vaultdb.NewConnector(dbURI, vaultClient, notifier,
			environ.VaultDBPath, environ.VaultDBRole, subLog)
		rs.applianceDB, err = appliancedb.VaultConnect(vdbc)
		rs.applianceVDBC = vdbc
		if err != nil {
			log.Fatalf("Error configuring DB from Vault: %v", err)
		}
	} else {
		rs.applianceDB, err = appliancedb.Connect(dbURI)
		if err != nil {
			log.Fatalf("failed to connect to appliance DB: %v", err)
		}
	}
	log.Infof(checkMark + "Created Appliance DB client")
	err = rs.applianceDB.Ping()
	if err != nil {
		log.Fatalf("failed to ping DB: %s", err)
	}
	log.Infof(checkMark + "Pinged Appliance DB")

	// Setup Account Secrets
	if secrets.AccountSecret == "" {
		log.Fatalf("Must specify B10E_CLHTTPD_ACCOUNT_SECRET")
	}
	accountSecret, err := hex.DecodeString(secrets.AccountSecret)
	if err != nil || len(accountSecret) != 32 {
		log.Fatalf("Failed to decode B10E_CLHTTPD_ACCOUNT_SECRET; should be hex encoded and 32 bytes long %d", len(accountSecret))
	}
	rs.applianceDB.AccountSecretsSetPassphrase(accountSecret)
	log.Infof(checkMark + "Appliance Secrets")
}

func mkEchoZapLogger(zlog *zap.Logger) echo.MiddlewareFunc {
	// Mostly the default fields, but we skip time, which is already emitted
	// by zap, and id, which is always empty.  We add the GCLB cookie, which
	// is how the load-balancing works.
	m := []echozap.Field{
		echozap.CookieField("GCLB"),
		echozap.CoreField("remote_ip"),
		echozap.CoreField("host"),
		echozap.CoreField("method"),
		echozap.CoreField("uri"),
		echozap.CoreField("user_agent"),
		echozap.CoreField("status"),
		echozap.CoreField("error"),
		echozap.CoreField("latency"),
		echozap.CoreField("latency_human"),
		echozap.CoreField("bytes_in"),
		echozap.CoreField("bytes_out"),
	}
	return echozap.Logger(zlog, m)
}

func mkRouterHTTPS(log *zap.Logger, vaultClient *vault.Client, notifier *daemonutils.FanOut) *routerState {
	var state routerState
	log = log.Named("https")
	slog := log.Sugar()

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
	r.Logger = zapgommon.ZapToGommonLog(log)
	r.HideBanner = true

	state.logger = slog
	state.mkSessionStore(vaultClient, notifier)
	state.mkApplianceDB(vaultClient, notifier)

	// Configd setup
	enableConfigdTLS = !environ.ConfigdDisableTLS && !environ.Developer
	if !enableConfigdTLS {
		slog.Warnf("Disabling TLS for connection to Configd")
	}
	hdl, err := getConfigClientHandle(appliancedb.NullSiteUUID.String())
	if err != nil {
		slog.Fatalf("failed to make Config Client: %s", err)
	}
	defer hdl.Close()
	err = hdl.Ping(context.Background())
	if err != nil {
		slog.Fatalf("failed to Ping Config Client: %s", err)
	}
	slog.Infof(checkMark + "Pinged cl.configd")

	// Twilio setup
	var twil *gotwilio.Twilio
	if secrets.TwilioSID != "" && secrets.TwilioAuthToken != "" {
		twil = gotwilio.NewTwilioClient(secrets.TwilioSID,
			secrets.TwilioAuthToken)
		slog.Infof(checkMark + "Setup Twilio Client")
	} else {
		slog.Warnf("Disabling Twilio Client")
	}

	r.Use(mkEchoZapLogger(log.Named("server")))
	r.Use(mkSecureMW(log))
	r.Use(middleware.Recover())
	r.Use(session.Middleware(state.sessionStore))
	r.Use(middleware.Gzip())
	r.Static("/.well-known", wellKnownPath)
	cwp := filepath.Join(appPath, "client-web")
	r.Static("/client-web", cwp)
	slog.Infof("Serving %s as /client-web/", cwp)
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
			slog.Fatalf("Must specify Avatar Storage Bucket B10E_CLHTTPD_AVATAR_BUCKET")
		}
	}

	// Setup /auth endpoints
	var opts []option.ClientOption
	slog.Infof("Attempting to get token source from Vault: path=%s role=%s",
		environ.VaultGCPPath, environ.VaultGCPRole)
	vts, err := vaulttokensource.NewVaultTokenSource(
		vaultClient, environ.VaultGCPPath, environ.VaultGCPRole)
	if err == nil {
		ts := oauth2.ReuseTokenSource(nil, vts)
		if _, err = ts.Token(); err == nil {
			opts = append(opts, option.WithTokenSource(ts))
		}
	}
	if opts == nil {
		slog.Warnf("Failed to get access token from Vault; falling "+
			"back to ADC: %v", err)
	}
	gcs, err := storage.NewClient(context.Background(), opts...)
	if err != nil {
		slog.Fatalf("failed to make gcs client: %s", err)
	}
	avBucket := gcs.Bucket(avBucketName)
	avBucketAttrs, err := avBucket.Attrs(context.Background())
	if err != nil {
		slog.Fatalf("failed to get bucket attrs: %s", err)
	}
	slog.Infof(checkMark+"Setup Avatar Bucket '%s'", avBucketAttrs.Name)

	_ = newAuthHandler(r, state.sessionStore, state.applianceDB, avBucket)

	wares := []echo.MiddlewareFunc{
		newSessionMiddleware(state.sessionStore).Process,
	}
	_ = newSiteHandler(r, state.applianceDB, wares, getConfigClientHandle, twil)
	_ = newAccountHandler(r, state.applianceDB, wares, state.sessionStore, avBucket, getConfigClientHandle)
	_ = newOrgHandler(r, state.applianceDB, wares, state.sessionStore)
	_ = newAccessHandler(r, state.applianceDB, state.sessionStore)

	// Setup /check endpoints
	_ = newCheckHandler(&state, getConfigClientHandle)

	return &state
}

func mkRouterHTTP(log *zap.Logger) *echo.Echo {
	log = log.Named("http")

	r := echo.New()
	r.Debug = environ.Developer
	r.Logger = zapgommon.ZapToGommonLog(log)
	r.HideBanner = true
	r.Use(mkEchoZapLogger(log.Named("server")))
	r.Use(mkSecureMW(log))
	r.Use(middleware.Recover())

	// Redirect HTTP requests to HTTPS, with the exception of health checks,
	// which must target a specific URL.  Note that the middleware redirects
	// only if the scheme is "https", based on whether the connection is TLS
	// or whether one of the standard X-Forwarded- headers suggests it is.
	//
	// We could also restrict it to come from a private IP (for k8s/GKE) or
	// 35.191.0.0/16 and 130.211.0.0/22, as documented at
	// https://cloud.google.com/load-balancing/docs/health-checks#fw-rule
	// as well as the Stackdriver Monitoring IPs, as documented at
	// https://cloud.google.com/monitoring/uptime-checks/using-uptime-checks#monitoring_uptime_check_list_ips-console
	r.Use(middleware.HTTPSRedirectWithConfig(
		middleware.RedirectConfig{
			Skipper: func(c echo.Context) bool {
				return strings.HasPrefix(
					c.Request().RequestURI, "/check/")
			},
		},
	))

	// We don't bother with something like getCheckProduction() for the HTTP
	// router, because it doesn't do anything other than the redirection.
	r.GET("/check/pulse", func(c echo.Context) error {
		return c.String(http.StatusOK, "healthy\n")
	})

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

func processEnv(logger *zap.SugaredLogger) {
	if environ.Developer && environ.LoadBalanced {
		logger.Fatalf("dev mode and LB mode can't be set simultaneously")
	}

	if environ.Developer {
		daemonutils.SetLogLevel(zapcore.DebugLevel)
		logger.With(daemonutils.SkipField).Debugf("environ %+v", environ)
	}

	if environ.HTTPListen == "" {
		if environ.Developer {
			environ.HTTPListen = ":9080"
		} else {
			environ.HTTPListen = defaultHTTPListen
		}
	}
	if environ.HTTPSListen == "" {
		if environ.Developer {
			environ.HTTPSListen = ":9443"
		} else {
			environ.HTTPSListen = defaultHTTPSListen
		}
	}

	// Name assigned to the load balancer
	if name, err := metadata.InstanceAttributeValue("lb-name"); err == nil && name != "" {
		lbName = name
	} else if err != nil {
		logger.Warnf("Unable to retrieve load balancer name: %v", err)
	}

	if environ.CertHostname == "" {
		environ.CertHostname = lbName
	}

	// DBRole and KVComponent both default to pname, and the path variables
	// determine whether we look at Vault at all.
	if environ.VaultDBRole == "" {
		environ.VaultDBRole = pname
	}
	useVaultForDB = environ.VaultDBPath != ""

	if environ.VaultKVComponent == "" {
		environ.VaultKVComponent = pname
	}
	useVaultForKV = environ.VaultKVPath != ""

	var project string
	getProject := func() {
		if project != "" {
			return
		}
		p, err := metadata.ProjectID()
		if err != nil {
			logger.Fatalf("Can't get GCP project ID: %v", err)
		}
		project = p
	}

	if (useVaultForDB || useVaultForKV) && environ.VaultAuthPath == "" {
		getProject()
		environ.VaultAuthPath = "auth/gcp-" + project
		logger.Warnf("B10E_CLHTTPD_VAULT_AUTH_PATH not found in "+
			"environment; setting to %s", environ.VaultAuthPath)
	}

	if environ.VaultGCPPath == "" {
		getProject()
		environ.VaultGCPPath = "gcp/" + project
		logger.Warnf("B10E_CLHTTPD_VAULT_GCP_PATH not found in "+
			"environment; setting to %s", environ.VaultGCPPath)
	}
	if environ.VaultGCPRole == "" {
		environ.VaultGCPRole = pname
		logger.Warnf("B10E_CLHTTPD_VAULT_GCP_ROLE not found in "+
			"environment; setting to %s", environ.VaultGCPRole)
	}
}

func getVaultSecrets(glog *zap.SugaredLogger, vc *vault.Client) {
	mount := environ.VaultKVPath
	if mount == "" {
		mount = "secret"
	}
	component := environ.VaultKVComponent
	if component == "" {
		component = pname
	}

	vcl := vc.Logical()
	err := vaulttags.Unmarshal(mount, component, vcl, glog, &secrets)
	if err != nil {
		glog.Fatalf("Error retrieving secrets from Vault: %s", err)
	}
}

func main() {
	var err error

	flag.Parse()
	log, slog := daemonutils.SetupLogs()

	startupLog := slog.Named("startup")
	startupLog.Infof("starting %s", pname)
	err = envcfg.Unmarshal(&environ)
	if err != nil {
		startupLog.Fatalf("Environment Error: %s", err)
	}
	processEnv(startupLog)

	// First, fetch secrets from Vault, if we can.
	vaultClient, err := vault.NewClient(nil)
	var notifier *daemonutils.FanOut
	if useVaultForDB || useVaultForKV {
		if err != nil {
			startupLog.Fatalf("Vault error: %s", err)
		} else {
			if vaultClient.Token() == "" || !environ.Developer {
				startupLog.Info("Authenticating to Vault with GCP auth")
				if vaultClient.Token() != "" && !environ.Developer {
					startupLog.Warnf("Vault token found in environment; will override")
				}
				hcLog := vaultgcpauth.ZapToHCLog(slog)
				if notifier, err = vaultgcpauth.VaultAuth(context.Background(),
					hcLog, vaultClient, environ.VaultAuthPath,
					pname); err != nil {
					startupLog.Fatalf("Vault login error: %s", err)
				}
				if environ.Developer {
					startupLog.Debugf("Initial GCP login token %s", vaultClient.Token())
				}
				startupLog.Info(checkMark + "Authenticated to Vault")
			} else {
				startupLog.Info("Authenticating to Vault with existing token")
			}
			getVaultSecrets(startupLog, vaultClient)
		}
	}

	// Next, fetch secrets from the environment.  If a secret is in both,
	// the environment will take precedence.
	err = envcfg.Unmarshal(&secrets)
	if err != nil {
		startupLog.Fatalf("Environment error: %s", err)
	}

	if environ.Developer {
		startupLog.With(daemonutils.SkipField).Debugf("secrets: %+v", secrets)
	}

	rsHTTPS := mkRouterHTTPS(log, vaultClient, notifier)
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
		if err := rsHTTPS.echo.StartServer(httpsSrv); err != nil && err != http.ErrServerClosed {
			rsHTTPS.echo.Logger.Fatalf("failed to start HTTPS service: %v", err)
		}
		// A bug in our current implementation (for which we don't have
		// root-cause) means that we never see StartServer return.
		rsHTTPS.echo.Logger.Info("finished serving HTTPS")
	}()

	// http (typically port 80) listener.
	httpSrv := &http.Server{
		Addr: environ.HTTPListen,
	}
	eHTTP := mkRouterHTTP(log)

	go func() {
		if err := eHTTP.StartServer(httpSrv); err != nil && err != http.ErrServerClosed {
			eHTTP.Logger.Fatalf("failed to start HTTP service: %v", err)
		}
		// A bug in our current implementation (for which we don't have
		// root-cause) means that we never see StartServer return.
		eHTTP.Logger.Info("finished serving HTTP")
	}()

	sig := make(chan os.Signal)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	s := <-sig
	startupLog.Infof("Signal (%v) received, shutting down", s)

	gracefulShutdown(rsHTTPS.echo)
	gracefulShutdown(eHTTP)
	startupLog.Infof("All servers shut down, goodbye.")
}

