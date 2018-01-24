/*
 * COPYRIGHT 2017 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

/*
 * cl.httpd: cloud HTTP
 * XXX Review 12 factor application.  These will come from Let's
 * Encrypt directory to start.
 */

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

	apachelog "github.com/lestrrat/go-apache-logformat"
	"github.com/tomazk/envcfg"

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
}

const (
	pname = "cl.httpd"
)

var (
	environ Cfg
)

func port443Handler(w http.ResponseWriter, r *http.Request) {
	w.Header().Add(
		"Strict-Transport-Security", "max-age=63072000; includeSubDomains")
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

	// Port 443 listener.
	certf := fmt.Sprintf("/etc/letsencrypt/live/%s/fullchain.pem",
		environ.CertHostname)
	keyf := fmt.Sprintf("/etc/letsencrypt/live/%s/privkey.pem",
		environ.CertHostname)

	port443mux := http.NewServeMux()
	port443mux.HandleFunc("/", port443Handler)
	cfg := &tls.Config{
		MinVersion:               tls.VersionTLS12,
		CurvePreferences:         []tls.CurveID{tls.CurveP521, tls.CurveP384, tls.CurveP256},
		PreferServerCipherSuites: true,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA,
			tls.TLS_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_RSA_WITH_AES_256_CBC_SHA,
		},
	}

	port443srv := &http.Server{
		Addr:         ":443",
		Handler:      apachelog.CombinedLog.Wrap(port443mux, os.Stderr),
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
	port80mux := http.NewServeMux()
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
