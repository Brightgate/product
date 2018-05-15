/*
 * COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
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
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"bg/ap_common/apcfg"
	"bg/ap_common/aputil"
	"bg/ap_common/broker"
	"bg/ap_common/certificate"
	"bg/ap_common/data"
	"bg/ap_common/mcp"
	"bg/ap_common/network"
	"bg/base_def"
	"bg/base_msg"

	"github.com/golang/protobuf/proto"

	"github.com/gorilla/mux"
	"github.com/gorilla/securecookie"
	"github.com/lestrrat/go-apache-logformat"

	"github.com/NYTimes/gziphandler"
	"github.com/unrolled/secure"
	"github.com/urfave/negroni"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	addr = flag.String("promhttp-address", base_def.HTTPD_PROMETHEUS_PORT,
		"The address to listen on for Prometheus HTTP requests.")
	clientWebDir = flag.String("client-web_dir", "client-web",
		"location of httpd client web root")
	ports         = listFlag([]string{":80", ":443"})
	developerHTTP = flag.String("developer-http", "",
		"Developer http port (disabled by default)")

	cert      string
	key       string
	certValid bool

	cutter *securecookie.SecureCookie

	config     *apcfg.APConfig
	domainname string

	mcpd *mcp.MCP
)

var latencies = prometheus.NewSummary(prometheus.SummaryOpts{
	Name: "http_render_seconds",
	Help: "HTTP page render time",
})

var (
	pings     = 0
	configs   = 0
	entities  = 0
	resources = 0
	requests  = 0
)

const (
	pname = "ap.httpd"

	cookiehmackeyprop = "@/httpd/cookie-hmac-key"
	cookieaeskeyprop  = "@/httpd/cookie-aes-key"

	// 'unsafe-inline' is needed because current HTML pages are
	// using inline <script> tags.  'unsafe-eval' is needed by
	// vue.js's template compiler.  'img-src' relaxed to allow
	// inline SVG elements.
	contentSecurityPolicy = "default-src 'self' 'unsafe-inline' 'unsafe-eval'; img-src 'self' data: 'unsafe-inline' 'unsafe-eval'; frame-ancestors 'none'"
)

// listFlag is a flag type that turns a comma-separated input into a slice of
// strings.
type listFlag []string

func (l listFlag) String() string {
	return strings.Join(l, ",")
}

func (l listFlag) Set(value string) error {
	l = strings.Split(value, ",")
	return nil
}

func handlePing(event []byte) { pings++ }

func handleConfig(event []byte) { configs++ }

func handleEntity(event []byte) { entities++ }

func handleResource(event []byte) { resources++ }

func handleRequest(event []byte) { requests++ }

func handleError(event []byte) {
	syserror := &base_msg.EventSysError{}
	proto.Unmarshal(event, syserror)

	log.Printf("sys.error received by handler: %v", *syserror)

	// Check if event is a certificate error
	if *syserror.Reason == base_msg.EventSysError_RENEWED_SSL_CERTIFICATE {
		log.Printf("exiting due to renewed certificate")
		os.Exit(0)
	}
}

// hostInMap returns a Gorilla Mux matching function that checks to see if
// the host is in the given map.
func hostInMap(hostMap map[string]bool) mux.MatcherFunc {
	return func(r *http.Request, match *mux.RouteMatch) bool {
		return hostMap[r.Host]
	}
}

func phishHandler(w http.ResponseWriter, r *http.Request) {
	log.Printf("Phishing request: %v\n", *r)

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
				Addr:         addr + port,
				Handler:      handler,
				TLSConfig:    cfg,
				TLSNextProto: make(map[string]func(*http.Server, *tls.Conn, http.Handler), 0),
			}
			err := srv.ListenAndServeTLS(certfn, keyfn)
			log.Printf("TLS Listener on %s (%s) exited: %v\n", addr+port, ring, err)
		}()
	} else {
		go func() {
			err := http.ListenAndServe(addr+port, handler)
			log.Printf("Listener on %s (%s) exited: %v\n", addr+port, ring, err)
		}()
	}
}

func establishHttpdKeys() ([]byte, []byte) {
	var hs, as []byte

	// If @/httpd/cookie-hmac-key is already set, retrieve its value.
	hs64, err := config.GetProp(cookiehmackeyprop)
	if err != nil {
		hs = securecookie.GenerateRandomKey(base_def.HTTPD_HMAC_SIZE)
		if hs == nil {
			log.Fatalf("could not generate random key of size %d\n",
				base_def.HTTPD_HMAC_SIZE)
		}
		hs64 = base64.StdEncoding.EncodeToString(hs)

		err = config.CreateProp(cookiehmackeyprop, hs64, nil)
		if err != nil {
			log.Fatalf("could not create '%s': %v\n", cookiehmackeyprop, err)
		}
	} else {
		hs, err = base64.StdEncoding.DecodeString(hs64)
		if err != nil {
			log.Fatalf("'%s' contains invalid b64 representation: %v\n", cookiehmackeyprop, err)
		}

		if len(hs) != base_def.HTTPD_HMAC_SIZE {
			// Delete
			err = config.DeleteProp(cookiehmackeyprop)
			if err != nil {
				log.Fatalf("could not delete invalid size HMAC key: %v\n", err)
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
			log.Fatalf("could not create '%s': %v\n", cookieaeskeyprop, err)
		}
	} else {
		as, err = base64.StdEncoding.DecodeString(as64)
		if err != nil {
			log.Fatalf("'%s' contains invalid b64 representation: %v\n", cookieaeskeyprop, err)
		}

		if len(as) != base_def.HTTPD_AES_SIZE {
			// Delete
			err = config.DeleteProp(cookieaeskeyprop)
			if err != nil {
				log.Fatalf("could not delete invalid size AES key: %v\n", err)
			} else {
				return establishHttpdKeys()
			}
		}
	}

	return hs, as
}

func blocklistUpdateEvent(path []string, val string, expires *time.Time) {
	data.LoadDNSBlacklist(data.DefaultDataDir)
}

func init() {
	prometheus.MustRegister(latencies)
}

func main() {
	var err error
	var rings apcfg.RingMap

	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	flag.Var(ports, "http-ports", "The ports to listen on for HTTP requests.")
	flag.Parse()
	*clientWebDir = aputil.ExpandDirPath(*clientWebDir)

	mcpd, err = mcp.New(pname)
	if err != nil {
		log.Printf("Failed to connect to mcp\n")
	}

	// Set up connection with the broker daemon
	brokerd := broker.New(pname)
	brokerd.Handle(base_def.TOPIC_PING, handlePing)
	brokerd.Handle(base_def.TOPIC_CONFIG, handleConfig)
	brokerd.Handle(base_def.TOPIC_ENTITY, handleEntity)
	brokerd.Handle(base_def.TOPIC_RESOURCE, handleResource)
	brokerd.Handle(base_def.TOPIC_REQUEST, handleRequest)
	brokerd.Handle(base_def.TOPIC_ERROR, handleError)
	defer brokerd.Fini()

	http.Handle("/metrics", promhttp.Handler())
	go http.ListenAndServe(*addr, nil)

	if config, err = apcfg.NewConfig(brokerd, pname); err == nil {
		rings = config.GetRings()
	}

	if rings == nil {
		if mcpd != nil {
			mcpd.SetState(mcp.BROKEN)
		}
		if err != nil {
			log.Fatalf("cannot connect to configd: %v\n", err)
		} else {
			log.Fatal("can't get ring configuration\n")
		}
	}

	domainname, err = config.GetDomain()
	if err != nil {
		if mcpd != nil {
			mcpd.SetState(mcp.BROKEN)
		}
		log.Fatalf("failed to fetch gateway domain: %v\n", err)
	}
	demoHostname := fmt.Sprintf("gateway.%s", domainname)
	keyfn, _, _, fullchainfn, err := certificate.GetKeyCertPaths(brokerd, demoHostname, time.Now(), false)
	if err != nil {
		// We can still run plain HTTP ports, such as the developer port.
		log.Printf("Couldn't get SSL key/fullchain: %v", err)
	}

	data.LoadDNSBlacklist(data.DefaultDataDir)
	config.HandleChange(`^@/updates/dns_.*list$`, blocklistUpdateEvent)

	secureMW := secure.New(secure.Options{
		SSLRedirect:           true,
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

	phishRouter := mainRouter.MatcherFunc(
		func(r *http.Request, match *mux.RouteMatch) bool {
			return data.BlockedHostname(r.Host)
		}).Subrouter()
	phishRouter.HandleFunc("/", phishHandler)

	mainRouter.HandleFunc("/", defaultHandler)
	mainRouter.PathPrefix("/apid/").Handler(
		http.StripPrefix("/apid", demoAPIRouter))
	mainRouter.PathPrefix("/client-web/").Handler(
		http.StripPrefix("/client-web/",
			gziphandler.GzipHandler(
				http.FileServer(http.Dir(*clientWebDir)))))

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
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA,
			tls.TLS_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_RSA_WITH_AES_256_CBC_SHA,
		},
	}

	for ring, config := range rings {
		router := network.SubnetRouter(config.Subnet)
		// The secure middleware effectively links the ports, as
		// http/80 requests redirect to https/443.
		for _, port := range ports {
			listen(router, port, ring, tlsCfg, fullchainfn, keyfn, nMain)
		}
	}

	if *developerHTTP != "" {
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

		log.Printf("Developer Port configured at %s", *developerHTTP)
		go func() {
			err := http.ListenAndServe(*developerHTTP, nDev)
			log.Printf("Developer listener on %s exited: %v\n",
				*developerHTTP, err)
		}()
	}

	if mcpd != nil {
		mcpd.SetState(mcp.ONLINE)
	}

	sig := make(chan os.Signal, 2)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	s := <-sig
	log.Fatalf("Signal (%v) received", s)
}
