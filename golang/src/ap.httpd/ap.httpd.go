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
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"flag"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"ap_common/apcfg"
	"ap_common/broker"
	"ap_common/mcp"
	"ap_common/network"
	"base_def"
	"data/phishtank"

	"github.com/gorilla/mux"
	"github.com/lestrrat/go-apache-logformat"
	"github.com/urfave/negroni"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	aproot = flag.String("root", "proto.armv7l/appliance/opt/com.brightgate",
		"Root of AP installation")
	addr = flag.String("promhttp-address", base_def.HTTPD_PROMETHEUS_PORT,
		"The address to listen on for Prometheus HTTP requests.")
	templateDir = flag.String("template_dir", "golang/src/ap.httpd",
		"location of httpd templates")
	clientWebDir = flag.String("client-web_dir", "client-web",
		"location of httpd client web root")
	ports = listFlag([]string{":80", ":443"})

	captiveMap map[string]bool

	cert      string
	key       string
	certValid bool

	config      *apcfg.APConfig
	subnetMap   apcfg.SubnetMap
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

	// XXX: These should come from config, so we can update it as necessary
	profileURL    = "https://demo1.brightgate.net/cgi-bin/apple-mc.py"
	profileSecret = "Bulb-Shr1ne"
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

func handle_ping(event []byte) { pings++ }

func handle_config(event []byte) { configs++ }

func handle_entity(event []byte) { entities++ }

func handle_resource(event []byte) { resources++ }

func handle_request(event []byte) { requests++ }

// hostInMap returns a Gorilla Mux matching function that checks to see if
// the host is in the given map.
func hostInMap(hostMap map[string]bool) mux.MatcherFunc {
	return func(r *http.Request, match *mux.RouteMatch) bool {
		return hostMap[r.Host]
	}
}

func getProfileParams() (ssid, passphrase string, expiry int64, hash string) {
	props, err := config.GetProps("@/network")
	if err == nil {
		if node := props.GetChild("ssid"); node != nil {
			ssid = node.GetValue()
		}

		if node := props.GetChild("passphrase"); node != nil {
			passphrase = node.GetValue()
		}

		// How many minutes should the profile be valid for?
		lifetime := 0
		if node := props.GetChild("profile_lifetime"); node != nil {
			lifetime, err = strconv.Atoi(node.GetValue())
			if err != nil {
				log.Printf("Bad expiration period '%s': %v\n",
					node.GetValue(), err)
			}
		}

		if lifetime == 0 {
			// Default to one day
			lifetime = 24 * 60
		}
		expiry = time.Now().Unix() + int64(60*lifetime)
	}

	s := fmt.Sprintf("%s:%s:%d:%s", ssid, passphrase, expiry, profileSecret)
	h := sha256.New()
	h.Write([]byte(s))
	hash = hex.EncodeToString(h.Sum(nil))

	return
}

func appleConnect(w http.ResponseWriter, r *http.Request) {
	ssid, passphrase, expiry, hash := getProfileParams()
	if ssid == "" || passphrase == "" {
		http.Error(w, "Network not configured", 503)
		return
	}

	req, err := http.NewRequest("GET", profileURL, nil)
	if err != nil {
		log.Print(err)
		http.Error(w, "Internal server error", 501)
		return
	}

	q := req.URL.Query()
	q.Add("ssid", ssid)
	q.Add("passwd", passphrase)
	q.Add("expiry", strconv.FormatInt(expiry, 10))
	q.Add("hash", hash)
	req.URL.RawQuery = q.Encode()

	resp, err := http.Get(req.URL.String())
	if err != nil {
		http.Error(w, "Profile server unavailable", 503)
		return
	}
	for a := range resp.Header {
		w.Header().Set(a, resp.Header.Get(a))
	}

	io.Copy(w, resp.Body)
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

func appleHandler(w http.ResponseWriter, r *http.Request) {
	log.Printf("appleHandler: %v\n", *r)
	gatewayu := fmt.Sprintf("http://gateway.%s/client-web/enroll.html",
		domainname)
	http.Redirect(w, r, gatewayu, http.StatusFound)
}

func defaultHandler(w http.ResponseWriter, r *http.Request) {
	log.Printf("defaultHandler: %v\n", *r)
	gatewayu := fmt.Sprintf("http://gateway.%s/client-web/", domainname)
	http.Redirect(w, r, gatewayu, http.StatusFound)
}

func initNetwork() error {
	// init maps
	captiveMap = make(map[string]bool)

	if subnetMap = config.GetSubnets(); subnetMap == nil {
		return fmt.Errorf("Failed to get subnet addresses")
	}

	for iface, subnet := range subnetMap {
		router := network.SubnetRouter(subnet)
		if iface == "setup" {
			captiveMap[router] = true
			captiveMap[router+":80"] = true
		}
	}
	return nil
}

func listen(addr string, port string, iface string, cfg *tls.Config, certf string, keyf string, handler http.Handler) {
	if port == ":443" {
		go func() {
			srv := &http.Server{
				Addr:         addr + ":443",
				Handler:      handler,
				TLSConfig:    cfg,
				TLSNextProto: make(map[string]func(*http.Server, *tls.Conn, http.Handler), 0),
			}
			err := srv.ListenAndServeTLS(certf, keyf)
			log.Printf("TLS Listener on %s (%s) exited: %v\n", addr+port, iface, err)
		}()
	} else {
		go func() {
			err := http.ListenAndServe(addr+port, handler)
			log.Printf("Listener on %s (%s) exited: %v\n", addr+port, iface, err)
		}()
	}
}

// loadPhishtank sets the global phishScorer to score how reliable a domain is
func loadPhishtank() {
	antiphishing := *aproot + "/var/spool/antiphishing/"

	reader := phishtank.NewReader(
		phishtank.Whitelist(antiphishing+"whitelist.csv"),
		phishtank.Phishtank(antiphishing+"phishtank.csv"),
		phishtank.MDL(antiphishing+"mdl.csv"),
		phishtank.Generic(antiphishing+"example_blacklist.csv", -3, 1))
	phishScorer = reader.Scorer()
}

func init() {
	prometheus.MustRegister(latencies)
}

func main() {
	var err error

	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	flag.Var(ports, "http-ports", "The ports to listen on for HTTP requests.")
	flag.Parse()

	mcpd, err = mcp.New(pname)
	if err != nil {
		log.Printf("Failed to connect to mcp\n")
	}

	// Set up connection with the broker daemon
	b := broker.New(pname)
	b.Handle(base_def.TOPIC_PING, handle_ping)
	b.Handle(base_def.TOPIC_CONFIG, handle_config)
	b.Handle(base_def.TOPIC_ENTITY, handle_entity)
	b.Handle(base_def.TOPIC_RESOURCE, handle_resource)
	b.Handle(base_def.TOPIC_REQUEST, handle_request)
	defer b.Fini()

	http.Handle("/metrics", promhttp.Handler())
	go http.ListenAndServe(*addr, nil)

	config, err = apcfg.NewConfig(b, pname)
	if err != nil {
		log.Fatalf("cannot connect to configd: %v\n", err)
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

	err = initNetwork()
	if err != nil {
		mcpd.SetState(mcp.BROKEN)
		log.Fatalf("Failed to init network: %v\n", err)
	}

	// routing
	demoAPIRouter := mux.NewRouter()
	demoAPIRouter.HandleFunc("/alerts", demoAlertsHandler)
	demoAPIRouter.HandleFunc("/devices/{ring}", demoDevicesByRingHandler)
	demoAPIRouter.HandleFunc("/devices", demoDevicesHandler)
	demoAPIRouter.HandleFunc("/access/{devid}", demoAccessByIDHandler)
	demoAPIRouter.HandleFunc("/access", demoAccessHandler)
	demoAPIRouter.HandleFunc("/supreme", demoSupremeHandler)
	demoAPIRouter.HandleFunc("/config/{property:[a-z@/]+}", demoPropertyByNameHandler)
	demoAPIRouter.HandleFunc("/config", demoPropertyHandler)

	mainRouter := mux.NewRouter()

	phishRouter := mainRouter.MatcherFunc(
		func(r *http.Request, match *mux.RouteMatch) bool {
			log.Printf("Host: %s Score: %d\n", r.Host, phishScorer.Score(r.Host, phishtank.Dns))
			return phishScorer.Score(r.Host, phishtank.Dns) < 0
		}).Subrouter()
	phishRouter.HandleFunc("/", phishHandler)

	mainRouter.HandleFunc("/", defaultHandler)
	mainRouter.PathPrefix("/apid/").Handler(http.StripPrefix("/apid", demoAPIRouter))
	mainRouter.PathPrefix("/client-web/").Handler(http.StripPrefix("/client-web/", http.FileServer(http.Dir(*clientWebDir))))
	nMain := negroni.New(negroni.NewRecovery())
	nMain.UseHandler(apachelog.CombinedLog.Wrap(mainRouter, os.Stderr))

	captiveRouter := mux.NewRouter()
	captiveRouter.HandleFunc("/", defaultHandler)
	captiveRouter.PathPrefix("/client-web/").Handler(http.StripPrefix("/client-web/", http.FileServer(http.Dir(*clientWebDir))))
	captiveRouter.HandleFunc("/hotspot-detect.html", appleHandler)
	captiveRouter.HandleFunc("/appleConnect", appleConnect)
	nCaptive := negroni.New(negroni.NewRecovery())
	nCaptive.UseHandler(apachelog.CombinedLog.Wrap(captiveRouter, os.Stderr))

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

	for iface, subnet := range subnetMap {
		router := network.SubnetRouter(subnet)
		if iface == "setup" {
			listen(router, ":80", iface, tlsCfg, certf, keyf, nCaptive)
		} else {
			for _, port := range ports {
				listen(router, port, iface, tlsCfg, certf, keyf, nMain)
			}
		}
	}

	if mcpd != nil {
		mcpd.SetState(mcp.ONLINE)
	}

	sig := make(chan os.Signal)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	s := <-sig
	log.Fatalf("Signal (%v) received", s)
}
