/*
 * COPYRIGHT 2017 Brightgate Inc.  All rights reserved.
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
	"html/template"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"bg/ap_common/apcfg"
	"bg/ap_common/aputil"
	"bg/ap_common/broker"
	"bg/ap_common/mcp"
	"bg/ap_common/network"
	"bg/base_def"
	"bg/data/phishtank"

	"github.com/gorilla/mux"
	"github.com/gorilla/securecookie"
	"github.com/lestrrat/go-apache-logformat"

	"github.com/urfave/negroni"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	addr = flag.String("promhttp-address", base_def.HTTPD_PROMETHEUS_PORT,
		"The address to listen on for Prometheus HTTP requests.")
	templateDir = flag.String("template_dir", "golang/src/ap.httpd",
		"location of httpd templates")
	clientWebDir = flag.String("client-web_dir", "client-web",
		"location of httpd client web root")
	ports         = listFlag([]string{":80", ":443"})
	developerHTTP = flag.String("developer-http", "",
		"Developer http port (disabled by default)")

	cert      string
	key       string
	certValid bool

	cutter *securecookie.SecureCookie

	config      *apcfg.APConfig
	phishScorer phishtank.Scorer
	domainname  string

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
)

func openTemplate(name string) (*template.Template, error) {
	file := name + ".html.got"
	path := *templateDir + "/" + file

	t, err := template.ParseFiles(path)
	if err != nil {
		log.Printf("Failed to parse template %s: %v\n", file, err)
	}

	return t, err
}

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

// hostInMap returns a Gorilla Mux matching function that checks to see if
// the host is in the given map.
func hostInMap(hostMap map[string]bool) mux.MatcherFunc {
	return func(r *http.Request, match *mux.RouteMatch) bool {
		return hostMap[r.Host]
	}
}

func templateHandler(w http.ResponseWriter, template string) {
	conf, err := openTemplate(template)
	if err == nil {
		err = conf.Execute(w, nil)
	}
	if err != nil {
		http.Error(w, "Internal server error", 500)
	}
}

func phishHandler(w http.ResponseWriter, r *http.Request) {
	log.Printf("Phishing request: %v\n", *r)
	phishu := fmt.Sprintf("http://phishing.%s/client-web/malwareWarn.html?host=%s", domainname, r.Host)
	http.Redirect(w, r, phishu, http.StatusSeeOther)
}

func defaultHandler(w http.ResponseWriter, r *http.Request) {
	var gatewayu string

	if r.Host == "localhost" {
		gatewayu = fmt.Sprintf("http://localhost/client-web/")
	} else {
		gatewayu = fmt.Sprintf("http://gateway.%s/client-web/",
			domainname)
	}
	http.Redirect(w, r, gatewayu, http.StatusFound)
}

func listen(addr string, port string, ring string, cfg *tls.Config,
	certf string, keyf string, handler http.Handler) {
	if port == ":443" {
		go func() {
			srv := &http.Server{
				Addr:         addr + ":443",
				Handler:      handler,
				TLSConfig:    cfg,
				TLSNextProto: make(map[string]func(*http.Server, *tls.Conn, http.Handler), 0),
			}
			err := srv.ListenAndServeTLS(certf, keyf)
			log.Printf("TLS Listener on %s (%s) exited: %v\n", addr+port, ring, err)
		}()
	} else {
		go func() {
			err := http.ListenAndServe(addr+port, handler)
			log.Printf("Listener on %s (%s) exited: %v\n", addr+port, ring, err)
		}()
	}
}

// loadPhishtank sets the global phishScorer to score how reliable a domain is
func loadPhishtank() {
	antiphishing := aputil.ExpandDirPath("/var/spool/antiphishing/")

	reader := phishtank.NewReader(
		phishtank.Whitelist(antiphishing+"whitelist.csv"),
		phishtank.Phishtank(antiphishing+"phishtank.csv"),
		phishtank.MDL(antiphishing+"mdl.csv"),
		phishtank.Generic(antiphishing+"example_blacklist.csv", -3, 1))
	phishScorer = reader.Scorer()
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

func init() {
	prometheus.MustRegister(latencies)
}

func main() {
	var err error
	var rings apcfg.RingMap

	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	flag.Var(ports, "http-ports", "The ports to listen on for HTTP requests.")
	flag.Parse()
	*templateDir = aputil.ExpandDirPath(*templateDir)
	*clientWebDir = aputil.ExpandDirPath(*clientWebDir)

	mcpd, err = mcp.New(pname)
	if err != nil {
		log.Printf("Failed to connect to mcp\n")
	}

	// Set up connection with the broker daemon
	b := broker.New(pname)
	b.Handle(base_def.TOPIC_PING, handlePing)
	b.Handle(base_def.TOPIC_CONFIG, handleConfig)
	b.Handle(base_def.TOPIC_ENTITY, handleEntity)
	b.Handle(base_def.TOPIC_RESOURCE, handleResource)
	b.Handle(base_def.TOPIC_REQUEST, handleRequest)
	defer b.Fini()

	http.Handle("/metrics", promhttp.Handler())
	go http.ListenAndServe(*addr, nil)

	if config, err = apcfg.NewConfig(b, pname); err == nil {
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

	siteid, err := config.GetProp("@/siteid")
	if err != nil {
		log.Printf("@/siteid not defined: %v\n", err)
		siteid = "0000"
	}
	domainname = fmt.Sprintf("%s.brightgate.net", siteid)
	demoHostname := fmt.Sprintf("gateway.%s", domainname)
	certf := fmt.Sprintf("/etc/letsencrypt/live/%s/fullchain.pem",
		demoHostname)
	keyf := fmt.Sprintf("/etc/letsencrypt/live/%s/privkey.pem",
		demoHostname)

	loadPhishtank()

	// routing
	mainRouter := mux.NewRouter()

	demoAPIRouter := makeDemoAPIRouter()

	phishRouter := mainRouter.MatcherFunc(
		func(r *http.Request, match *mux.RouteMatch) bool {
			log.Printf("Host: %s Score: %d\n", r.Host,
				phishScorer.Score(r.Host, phishtank.Dns))
			return phishScorer.Score(r.Host, phishtank.Dns) < 0
		}).Subrouter()
	phishRouter.HandleFunc("/", phishHandler)

	mainRouter.HandleFunc("/", defaultHandler)
	mainRouter.PathPrefix("/apid/").Handler(
		http.StripPrefix("/apid", demoAPIRouter))
	mainRouter.PathPrefix("/client-web/").Handler(
		http.StripPrefix("/client-web/",
			http.FileServer(http.Dir(*clientWebDir))))

	hashKey, blockKey := establishHttpdKeys()

	cutter = securecookie.New(hashKey, blockKey)

	nMain := negroni.New(negroni.NewRecovery())
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
		for _, port := range ports {
			listen(router, port, ring, tlsCfg, certf, keyf, nMain)
		}
	}

	if *developerHTTP != "" {
		log.Printf("Developer Port configured at %s", *developerHTTP)
		go func() {
			err := http.ListenAndServe(*developerHTTP, nMain)
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
