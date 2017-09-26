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
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
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
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	addr = flag.String("promhttp-address", base_def.HTTPD_PROMETHEUS_PORT,
		"The address to listen on for Prometheus HTTP requests.")
	templateDir = flag.String("template_dir", "golang/src/ap.httpd",
		"location of httpd templates")
	sslDir = flag.String("ssldir", "",
		"The directory for storing the SSL certificate and key.")
	ports = listFlag([]string{":80", ":443"})

	adminMap   map[string]bool
	captiveMap map[string]bool

	cert      string
	key       string
	certValid bool

	config    *apcfg.APConfig
	subnetMap apcfg.SubnetMap

	phishdata     = &phishtank.DataSource{}
	statsTemplate *template.Template

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

// StatsContent contains information for filling out the stats template.
type StatsContent struct {
	URLPath string

	NPings     string
	NConfigs   string
	NEntities  string
	NResources string
	NRequests  string

	Host string
}

func statsHandler(w http.ResponseWriter, r *http.Request) {
	lt := time.Now()

	statsTemplate, err := openTemplate("stats")
	if err == nil {
		conf := &StatsContent{
			URLPath:    r.URL.Path,
			NPings:     strconv.Itoa(pings),
			NConfigs:   strconv.Itoa(configs),
			NEntities:  strconv.Itoa(entities),
			NResources: strconv.Itoa(resources),
			NRequests:  strconv.Itoa(requests),
			Host:       r.Host,
		}

		err = statsTemplate.Execute(w, conf)
	}
	if err != nil {
		http.Error(w, "Internal server error", 501)

	} else {
		latencies.Observe(time.Since(lt).Seconds())
	}
}

// XXX broken-- unexpected end of JSON
func cfgHandler(w http.ResponseWriter, r *http.Request) {
	var err error

	t := time.Now()

	if r.Method != "GET" && r.Method != "POST" {
		http.Error(w, "Invalid request method.", 405)
		return
	}

	if r.Method == "GET" {
		// Get setting from ap.configd
		//
		// From the command line:
		//     wget -q -O- http://127.0.0.1:8000/config?@/network/wlan0/ssid

		val, err := config.GetProp(r.URL.RawQuery)
		if err != nil {
			estr := fmt.Sprintf("%v", err)
			http.Error(w, estr, 400)
		} else {
			fmt.Fprintf(w, "%s", val)
		}
	} else {
		// Send property updates to ap.configd
		//
		// From the command line:
		//    wget -q --post-data '@/network/wlan0/ssid=newssid' \
		//           http://127.0.0.1:8000/config

		err = r.ParseForm()
		for key, values := range r.Form {
			if len(values) != 1 {
				http.Error(w, "Properties may only have one value", 400)
				return
			}
			err = config.SetProp(key, values[0], nil)
		}
	}

	if err == nil {
		latencies.Observe(time.Since(t).Seconds())
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
	templateHandler(w, "nophish")
}
func appleHandler(w http.ResponseWriter, r *http.Request) {
	log.Printf("apple_connect: %v\n", *r)
	templateHandler(w, "connect_apple")
}

func defaultHandler(w http.ResponseWriter, r *http.Request) {
	log.Printf("default connect: %v\n", *r)
	templateHandler(w, "connect_generic")
}

// refreshCertificate updates the certificate using the ssl directory, if provided.
func refreshCertificate() error {
	certValid = false

	if *sslDir == "" {
		return errors.New("no ssl directory")
	}
	cmdName := "openssl"
	certInfo := "-subj /C=US/ST=California/L=Burlingame/O=Brightgate" +
		"/CN=brightgate.net"

	_, err := os.Stat(cert)
	if err == nil {
		args := fmt.Sprintf("x509 -checkend 0 -in %s", cert)
		if out, err := exec.Command(
			cmdName, strings.Split(args, " ")...).Output(); err != nil {
			return fmt.Errorf("failed to check certificate expiration: %v",
				err)
		} else if string(strings.TrimSpace(string(out))) ==
			"Certificate will not expire" {
			return nil
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("failed to open certificate file: %v", err)
	}

	if _, err := os.Stat(key); err != nil {
		if os.IsNotExist(err) {
			args := fmt.Sprintf("genrsa -out %s 2048", key)
			if err := exec.Command(
				cmdName, strings.Split(args, " ")...).Run(); err != nil {
				return fmt.Errorf("failed to create ssl key: %v", err)
			}
		} else {
			return fmt.Errorf("failed to open key file: %v", err)
		}
	}

	args := fmt.Sprintf("req -key %s -new -x509 -days 365 -out %s %s",
		key, cert, certInfo)
	if err := exec.Command(
		cmdName, strings.Split(args, " ")...).Run(); err != nil {
		return fmt.Errorf("failed to sign certificate: %v", err)
	}
	certValid = true
	return nil
}

func initNetwork() error {
	// init maps
	adminMap = make(map[string]bool)
	captiveMap = make(map[string]bool)

	if subnetMap = config.GetSubnets(); subnetMap == nil {
		return fmt.Errorf("Failed to get subnet addresses")
	}

	for iface, subnet := range subnetMap {
		router := network.SubnetRouter(subnet)
		if iface == "setup" {
			captiveMap[router] = true
			captiveMap[router+":80"] = true
		} else {
			adminMap[router] = true
			for _, port := range ports {
				adminMap[router+port] = true
			}
		}
	}
	return nil
}

func listen(addr, port, iface string, handler http.Handler) {
	listening := true

	if port == ":443" {
		if certValid {
			go http.ListenAndServeTLS(addr+port, cert, key, handler)
		} else {
			listening = false
		}
	} else {
		go http.ListenAndServe(addr+port, handler)
	}
	if listening {
		log.Printf("Listening on %s (%s)", addr+port, iface)
	}
}

func init() {
	prometheus.MustRegister(latencies)
}

func main() {
	var err error
	var b broker.Broker

	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	flag.Var(ports, "http-ports", "The ports to listen on for HTTP requests.")
	flag.Parse()

	if *sslDir != "" {
		if !strings.HasSuffix(*sslDir, "/") {
			*sslDir = *sslDir + "/"
		}
		cert = *sslDir + "domain.crt"
		key = *sslDir + "domain.key"
	}

	mcpd, err = mcp.New(pname)
	if err != nil {
		log.Printf("Failed to connect to mcp\n")
	}

	// Set up connection with the broker daemon
	b.Init(pname)
	b.Handle(base_def.TOPIC_PING, handle_ping)
	b.Handle(base_def.TOPIC_CONFIG, handle_config)
	b.Handle(base_def.TOPIC_ENTITY, handle_entity)
	b.Handle(base_def.TOPIC_RESOURCE, handle_resource)
	b.Handle(base_def.TOPIC_REQUEST, handle_request)
	b.Connect()
	defer b.Disconnect()
	b.Ping()

	http.Handle("/metrics", promhttp.Handler())
	go http.ListenAndServe(*addr, nil)

	config = apcfg.NewConfig(pname)

	phishdata.Loader("online-valid-test.csv")
	// phishdata.AutoLoader("online-valid.csv", time.Hour)
	// ^^ uncomment to autoupdate with real phish data, also change in dns4d

	err = initNetwork()
	if err != nil {
		mcpd.SetState(mcp.BROKEN)
		log.Fatalf("Failed to init network: %v\n", err)
	}

	// routing
	mainRouter := mux.NewRouter()
	mainRouter.HandleFunc("/", defaultHandler)

	adminRouter := mainRouter.MatcherFunc(hostInMap(adminMap)).Subrouter()
	adminRouter.HandleFunc("/config", cfgHandler)
	adminRouter.HandleFunc("/stats", statsHandler)

	captiveRouter := mainRouter.MatcherFunc(hostInMap(captiveMap)).Subrouter()
	captiveRouter.HandleFunc("/", defaultHandler)
	captiveRouter.HandleFunc("/hotspot-detect.html", appleHandler)
	captiveRouter.HandleFunc("/appleConnect", appleConnect)

	phishRouter := mainRouter.MatcherFunc(
		func(r *http.Request, match *mux.RouteMatch) bool {
			return phishdata.KnownToDataSource(r.Host)
		}).Subrouter()
	phishRouter.HandleFunc("/", phishHandler)

	http.Handle("/", mainRouter)

	err = refreshCertificate()
	if err != nil {
		log.Printf("Error refreshing certificate: %v", err)
	}

	for iface, subnet := range subnetMap {
		router := network.SubnetRouter(subnet)
		if iface == "setup" {
			listen(router, ":80", iface, captiveRouter)
		} else {
			for _, port := range ports {
				listen(router, port, iface, mainRouter)
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
