//
// COPYRIGHT 2017 Brightgate Inc.  All rights reserved.
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
// material  will come from Let's Encrypt directory to start.

// Because we have to serve static files to meet ACME HTTP-01
// authentication on renewal, we need to anchor the http:///.well-known
// directory hierarchy.  In the current Debian-based deployment, this
// location is "/var/www/html/.well-known".
//

package main

import (
	//	"crypto/sha256"
	"crypto/tls"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	//	"time"

	// "base_def"
	"github.com/tomazk/envcfg"

	"github.com/gorilla/mux"
	apachelog "github.com/lestrrat/go-apache-logformat"

	"github.com/unrolled/secure"
	// "github.com/urfave/negroni"

	//	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Cfg contains the environment variable-based configuration settings
type Cfg struct {
	// The certificate hostname is the primary hostname associated
	// with the SSL certificate, and not necessarily the nodename.
	// We use this variable to navigate the Let's Encrypt directory
	// hierarchy.
	CertHostname   string `envcfg:"B10E_CERT_HOSTNAME"`
	PrometheusPort string `envcfg:"B10E_CLHTTPD_PROMETHEUS_PORT"`
	WellknownPath  string `envcfg:"B10E_CERTBOT_WELLKNOWN_PATH"`
}

const (
	pname = "cl.httpd"

	// CSP matched to that of ap.httpd, anticipating web hoist.
	contentSecurityPolicy = "default-src 'self' 'unsafe-inline' 'unsafe-eval'; img-src 'self' data: 'unsafe-inline' 'unsafe-eval'; frame-ancestors 'none'"
)

var (
	environ Cfg
)

func port443Handler(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "https://brightgate.com", 307)
}

func port80Handler(w http.ResponseWriter, r *http.Request) {
	redirectURL := "https://" + r.Host + r.URL.Path
	if len(r.URL.RawQuery) > 0 {
		redirectURL += "?" + r.URL.RawQuery
	}
	http.Redirect(w, r, redirectURL, 301)
}

// XXX Restore init() if we are registering Prometheus metrics.

func main() {
	var err error
	var wellknown string

	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	err = envcfg.Unmarshal(&environ)
	if err != nil {
		log.Fatalf("Environment Error: %s", err)
	}

	log.Printf("environ %v", environ)

	if len(environ.PrometheusPort) != 0 {
		http.Handle("/metrics", promhttp.Handler())
		go http.ListenAndServe(environ.PrometheusPort, nil)
	}

	if len(environ.WellknownPath) == 0 {
		wellknown = "/var/www/html/.well-known"
	} else {
		wellknown = environ.WellknownPath
	}

	// Port 443 listener.
	certf := fmt.Sprintf("/etc/letsencrypt/live/%s/fullchain.pem",
		environ.CertHostname)
	keyf := fmt.Sprintf("/etc/letsencrypt/live/%s/privkey.pem",
		environ.CertHostname)

	secureMW := secure.New(secure.Options{
		SSLRedirect:           true,
		AllowedHosts:          []string{"svc0.b10e.net"},
		HostsProxyHeaders:     []string{"X-Forwarded-Host"},
		STSSeconds:            315360000,
		STSIncludeSubdomains:  true,
		STSPreload:            true,
		FrameDeny:             true,
		ContentTypeNosniff:    true,
		BrowserXssFilter:      true,
		ContentSecurityPolicy: contentSecurityPolicy,
	})

	port443mux := mux.NewRouter()
	port443mux.PathPrefix("/.well-known/").Handler(
		http.StripPrefix("/.well-known/",
			http.FileServer(http.Dir(wellknown))))
	port443mux.HandleFunc("/", port443Handler)

	port443app := secureMW.Handler(port443mux)

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
	}

	port443srv := &http.Server{
		Addr:         ":443",
		Handler:      apachelog.CombinedLog.Wrap(port443app, os.Stderr),
		TLSConfig:    cfg,
		TLSNextProto: make(map[string]func(*http.Server, *tls.Conn, http.Handler), 0),
	}

	go func() {
		err := port443srv.ListenAndServeTLS(certf, keyf)
		if err != nil {
			log.Printf("port 443 server failed: %v\n", err)
		}
	}()

	// Port 80 listener.
	port80mux := mux.NewRouter()
	port80mux.PathPrefix("/.well-known/").Handler(
		http.StripPrefix("/.well-known/",
			http.FileServer(http.Dir(wellknown))))
	port80mux.HandleFunc("/", port80Handler)

	port80srv := &http.Server{
		Addr:    ":80",
		Handler: apachelog.CombinedLog.Wrap(port80mux, os.Stderr),
	}

	go func() {
		err := port80srv.ListenAndServe()
		if err != nil {
			log.Printf("port 80 server failed: %v\n", err)
		}
	}()

	sig := make(chan os.Signal)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	s := <-sig
	log.Fatalf("Signal (%v) received", s)
}
