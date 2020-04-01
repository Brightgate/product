/*
 * COPYRIGHT 2020 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

// appliance HTTPD front end
// no fishing picture: https://pixabay.com/p-1191938/?no_redirect

package main

import (
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"bg/ap_common/apcfg"
	"bg/ap_common/aputil"
	"bg/ap_common/bgmetrics"
	"bg/ap_common/broker"
	"bg/ap_common/certificate"
	"bg/ap_common/data"
	"bg/ap_common/mcp"
	"bg/ap_common/platform"
	"bg/base_def"
	"bg/common/cfgapi"
	"bg/common/network"

	"go.uber.org/zap"

	"github.com/gorilla/mux"
	"github.com/gorilla/securecookie"
	"github.com/lestrrat-go/apache-logformat"

	"github.com/NYTimes/gziphandler"
	"github.com/unrolled/secure"
	"github.com/urfave/negroni"
)

var (
	plat         *platform.Platform
	clientWebDir = apcfg.String("client-web_dir", "/var/www/client-web",
		false, nil)
	portList       = apcfg.String("ports", "80,443", false, nil)
	developerHTTP  = apcfg.String("developer-http", "", false, nil)
	developerHTTPS = apcfg.String("developer-https", "", false, nil)
	_              = apcfg.String("log_level", "info", true, aputil.LogSetLevel)

	cutter *securecookie.SecureCookie

	mcpd       *mcp.MCP
	config     *cfgapi.Handle
	domainname string
	slog       *zap.SugaredLogger

	certInstalled = make(chan bool, 1)

	bgm     *bgmetrics.Metrics
	metrics struct {
		latencies *bgmetrics.Summary
	}
)

const (
	pname = "ap.httpd"

	cookiehmackeyprop = "@/httpd/cookie_hmac_key"
	cookieaeskeyprop  = "@/httpd/cookie_aes_key"

	// img-src relaxed to allow inline SVG elements.
	// style-src unsafe-inline is needed due to dom7 implementation details
	// font-src data: is needed due to framework7's css-defined core icons
	contentSecurityPolicy = "default-src 'self'; script-src 'self'; img-src 'self' data:; font-src 'self' data:; frame-src https://brightgate.freshdesk.com/; style-src 'self' 'unsafe-inline'; frame-ancestors 'none'"
)

func certStateChange(path []string, val string, expires *time.Time) {
	if val == "installed" {
		certInstalled <- true
	}
}

func phishHandler(w http.ResponseWriter, r *http.Request) {
	slog.Infof("Phishing request: %v\n", *r)

	scheme := r.URL.Scheme
	if scheme == "" {
		if r.TLS != nil {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}

	phishu := fmt.Sprintf("%s://phishing.%s/client-web/malwareWarn.html?host=%s",
		scheme, domainname, r.Host)
	http.Redirect(w, r, phishu, http.StatusSeeOther)
}

func defaultHandler(w http.ResponseWriter, r *http.Request) {
	var gatewayu string

	scheme := r.URL.Scheme
	if scheme == "" {
		if r.TLS != nil {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}

	if r.Host == "localhost" || r.Host == "127.0.0.1" {
		gatewayu = fmt.Sprintf("%s://%s/client-web/",
			scheme, r.Host)
	} else {
		gatewayu = fmt.Sprintf("%s://gateway.%s/client-web/",
			scheme, domainname)
	}
	http.Redirect(w, r, gatewayu, http.StatusFound)
}

func listen(addr string, port string, ring string, cfg *tls.Config,
	certfn string, keyfn string, handler http.Handler) {
	if port == ":443" {
		go func() {
			srv := &http.Server{
				Addr:      addr + port,
				Handler:   handler,
				TLSConfig: cfg,
			}
			err := srv.ListenAndServeTLS(certfn, keyfn)
			slog.Infof("TLS Listener on %s (%s) exited: %v", addr+port, ring, err)
		}()
	} else {
		go func() {
			err := http.ListenAndServe(addr+port, handler)
			slog.Infof("Listener on %s (%s) exited: %v", addr+port, ring, err)
		}()
	}
}

func establishHttpdKeys() ([]byte, []byte) {
	var hs, as []byte

	// If @/httpd/cookie_hmac_key is already set, retrieve its value.
	hs64, err := config.GetProp(cookiehmackeyprop)
	if err != nil {
		hs = securecookie.GenerateRandomKey(base_def.HTTPD_HMAC_SIZE)
		if hs == nil {
			slog.Fatalf("could not generate random key of size %d\n",
				base_def.HTTPD_HMAC_SIZE)
		}
		hs64 = base64.StdEncoding.EncodeToString(hs)

		err = config.CreateProp(cookiehmackeyprop, hs64, nil)
		if err != nil {
			slog.Fatalf("could not create '%s': %v\n", cookiehmackeyprop, err)
		}
	} else {
		hs, err = base64.StdEncoding.DecodeString(hs64)
		if err != nil {
			slog.Fatalf("'%s' contains invalid b64 representation: %v\n", cookiehmackeyprop, err)
		}

		if len(hs) != base_def.HTTPD_HMAC_SIZE {
			// Delete
			err = config.DeleteProp(cookiehmackeyprop)
			if err != nil {
				slog.Fatalf("could not delete invalid size HMAC key: %v\n", err)
			} else {
				return establishHttpdKeys()
			}
		}
	}

	as64, err := config.GetProp(cookieaeskeyprop)
	if err != nil {
		as = securecookie.GenerateRandomKey(base_def.HTTPD_AES_SIZE)
		as64 = base64.StdEncoding.EncodeToString(as)

		err = config.CreateProp(cookieaeskeyprop, as64, nil)
		if err != nil {
			slog.Fatalf("could not create '%s': %v\n", cookieaeskeyprop, err)
		}
	} else {
		as, err = base64.StdEncoding.DecodeString(as64)
		if err != nil {
			slog.Fatalf("'%s' contains invalid b64 representation: %v\n", cookieaeskeyprop, err)
		}

		if len(as) != base_def.HTTPD_AES_SIZE {
			// Delete
			err = config.DeleteProp(cookieaeskeyprop)
			if err != nil {
				slog.Fatalf("could not delete invalid size AES key: %v\n", err)
			} else {
				return establishHttpdKeys()
			}
		}
	}

	return hs, as
}

func blocklistUpdateEvent(path []string, val string, expires *time.Time) {
	data.LoadDNSBlocklist(data.DefaultDataDir)
}

// From a comma-separated list of ports, return a slice of ":<portNum>"
// strings.
func getPortList() []string {
	ports := make([]string, 0)
	s := strings.Split(*portList, ",")
	for _, p := range s {
		// Be flexible about allowing spaces and leading colons.  Strip
		// off any that are present, and add in the single colon we want
		clean := strings.TrimSpace(p)
		clean = strings.TrimLeft(clean, ":")
		if _, err := strconv.Atoi(clean); err != nil {
			slog.Fatalf("Invalid port: %s", p)
		}
		port := ":" + clean
		ports = append(ports, port)
	}
	return ports
}

func metricsInit() {
	bgm = bgmetrics.NewMetrics(pname, config)
	metrics.latencies = bgm.NewSummary("render_seconds")
}

func main() {
	var err error
	var rings cfgapi.RingMap
	var certPaths *certificate.CertPaths

	slog = aputil.NewLogger(pname)
	defer slog.Sync()
	slog.Infof("starting")

	mcpd, err = mcp.New(pname)
	if err != nil {
		slog.Warnf("Failed to connect to mcp\n")
	}

	metricsInit()
	go http.ListenAndServe(base_def.HTTPD_DIAG_PORT, nil)

	// Set up connection with the broker daemon
	brokerd, err := broker.NewBroker(slog, pname)
	if err != nil {
		slog.Fatal(err)
	}
	defer brokerd.Fini()

	config, err = apcfg.NewConfigd(brokerd, pname, cfgapi.AccessInternal)
	if err != nil {
		slog.Fatalf("cannot connect to configd: %v", err)
	}
	go apcfg.HealthMonitor(config, mcpd)
	aputil.ReportInit(slog, pname)

	rings = config.GetRings()
	if rings == nil {
		mcpd.SetState(mcp.BROKEN)
		slog.Fatalf("can't get ring configuration")
	}

	plat = platform.NewPlatform()

	domainname, err = config.GetDomain()
	if err != nil {
		mcpd.SetState(mcp.BROKEN)
		slog.Fatalf("failed to fetch gateway domain: %v\n", err)
	}

	// We expect that a change to the domain will precede a new certificate,
	// so the restart for the latter will handle the former.  Find the TLS
	// key and certificate we should be using.  If there isn't one, sleep
	// until ap.rpcd notifies us one is available, and then restart.
	err = config.HandleChange(`^@/certs/.*/state`, certStateChange)
	if err != nil {
		slog.Fatalf("failed to setup certStateChange: %v", err)
	}

	for certPaths == nil {
		certPaths = certificate.GetKeyCertPaths(domainname)
		if certPaths == nil {
			mcpd.SetState(mcp.FAILSAFE)
			slog.Warn("Sleeping until a cert is presented")
			<-certInstalled
			slog.Infof("New cert available")
		}
	}

	data.LoadDNSBlocklist(data.DefaultDataDir)
	err = config.HandleChange(`^@/updates/dns_.*list$`, blocklistUpdateEvent)
	if err != nil {
		slog.Fatalf("failed to setup blocklistUpdateEvent: %v", err)
	}

	secureMW := secure.New(secure.Options{
		SSLRedirect:           true,
		SSLHost:               "gateway." + domainname,
		HostsProxyHeaders:     []string{"X-Forwarded-Host"},
		STSSeconds:            315360000,
		STSIncludeSubdomains:  true,
		STSPreload:            true,
		FrameDeny:             true,
		ContentTypeNosniff:    true,
		BrowserXssFilter:      true,
		ContentSecurityPolicy: contentSecurityPolicy,
	})

	// routing
	mainRouter := mux.NewRouter()

	demoAPIRouter := makeDemoAPIRouter()
	applianceAuthRouter := makeApplianceAuthRouter()

	checkRouter := makeCheckRouter()

	phishRouter := mainRouter.MatcherFunc(
		func(r *http.Request, match *mux.RouteMatch) bool {
			return data.BlockedHostname(r.Host)
		}).Subrouter()
	phishRouter.HandleFunc("/", phishHandler)

	*clientWebDir = plat.ExpandDirPath("__APPACKAGE__", *clientWebDir)
	mainRouter.HandleFunc("/", defaultHandler)
	mainRouter.PathPrefix("/api/").Handler(
		http.StripPrefix("/api", demoAPIRouter))
	mainRouter.PathPrefix("/auth/").Handler(
		http.StripPrefix("/auth", applianceAuthRouter))
	mainRouter.PathPrefix("/client-web/").Handler(
		http.StripPrefix("/client-web/",
			gziphandler.GzipHandler(
				http.FileServer(http.Dir(*clientWebDir)))))
	mainRouter.PathPrefix("/check/").Handler(
		http.StripPrefix("/check", checkRouter))

	hashKey, blockKey := establishHttpdKeys()

	cutter = securecookie.New(hashKey, blockKey)

	nMain := negroni.New(negroni.NewRecovery())
	nMain.Use(negroni.HandlerFunc(secureMW.HandlerFuncWithNext))
	nMain.UseHandler(apachelog.CombinedLog.Wrap(mainRouter, os.Stderr))

	tlsCfg := &tls.Config{
		MinVersion:               tls.VersionTLS12,
		CurvePreferences:         []tls.CurveID{tls.CurveP521, tls.CurveP384, tls.CurveP256},
		PreferServerCipherSuites: true,
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
		NextProtos: []string{"h2"},
	}

	ports := getPortList()
	for ring, config := range rings {
		if cfgapi.SystemRings[ring] {
			continue
		}

		router := network.SubnetRouter(config.Subnet)
		// The secure middleware effectively links the ports, as
		// http/80 requests redirect to https/443.
		for _, port := range ports {
			listen(router, port, ring, tlsCfg, certPaths.FullChain,
				certPaths.Key, nMain)
		}
	}

	developerMW := secure.New(secure.Options{
		HostsProxyHeaders:     []string{"X-Forwarded-Host"},
		STSSeconds:            315360000,
		STSIncludeSubdomains:  true,
		STSPreload:            true,
		FrameDeny:             true,
		ContentTypeNosniff:    true,
		BrowserXssFilter:      true,
		ContentSecurityPolicy: contentSecurityPolicy,
		IsDevelopment:         true,
	})
	nDev := negroni.New(negroni.NewRecovery())
	nDev.Use(negroni.HandlerFunc(developerMW.HandlerFuncWithNext))
	nDev.UseHandler(apachelog.CombinedLog.Wrap(mainRouter, os.Stderr))

	if *developerHTTP != "" {
		slog.Infof("Developer HTTP Port configured at %s", *developerHTTP)
		go func() {
			err := http.ListenAndServe(*developerHTTP, nDev)
			slog.Infof("Developer listener on %s exited: %v\n",
				*developerHTTP, err)
		}()
	} else {
		slog.Infof("Developer HTTP disabled")
	}

	if *developerHTTPS != "" {
		slog.Infof("Developer HTTPS Port configured at %s", *developerHTTPS)
		go func() {
			srv := &http.Server{
				Addr:      *developerHTTPS,
				Handler:   nDev,
				TLSConfig: tlsCfg,
			}
			err := srv.ListenAndServeTLS(certPaths.FullChain, certPaths.Key)
			slog.Infof("TLS Listener on %s exited: %v", *developerHTTPS, err)
		}()
	} else {
		slog.Infof("Developer HTTPS disabled")
	}

	mcpd.SetState(mcp.ONLINE)

	sig := make(chan os.Signal, 2)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	select {
	case s := <-sig:
		slog.Infof("Signal (%v) received", s)
	case <-certInstalled:
		slog.Infof("restarting due to renewed certificate")
	}

	os.Exit(0)
}
