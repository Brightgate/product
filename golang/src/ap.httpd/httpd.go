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
	"errors"
	"flag"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"ap_common"
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
	sslDir = flag.String("ssldir", "",
		"The directory for storing the SSL certificate and key.")
	ports = listFlag([]string{":80", ":443"})

	adminMap      = map[string]struct{}{}
	cert          string
	config        *ap_common.Config
	captiveMap    = map[string]struct{}{}
	ifaceToSubnet map[string]string
	key           string
	phishdata     = &phishtank.DataSource{}
	statsTemplate *template.Template
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
	pname       = "ap.httpd"
	captivePort = ":8000"
	// ^^ eventually should reference some common code so doesn't have to be
	//    manually changed
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

func handle_ping(event []byte) { pings++ }

func handle_config(event []byte) { configs++ }

func handle_entity(event []byte) { entities++ }

func handle_resource(event []byte) { resources++ }

func handle_request(event []byte) { requests++ }

// XXX uncomment for use once merged with nils' code!

// // templateHandler returns an http handler that uses the given template.
// // Only for templates requiring no config.
// func templateHandler(name string) {
// 	return func(w http.ResponseWriter, r *http.Request) {
// 		conf, err := openTemplate(name)
// 		if err == nil {
// 			err = conf.Execute(w, nil)
// 		}
// 		if err != nil {
// 			http.Error(w, "Internal server error", 500)
// 		}
// 	}
// }

// XXX delete this once merged with nils'
func index_handler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "<h1>Welcome to Brightgate</h1>\n")
	fmt.Fprintf(w, "<h2>Ask around for the wifi password</h2>\n")
}

// hostInMap returns a Gorilla Mux matching function that checks to see if
// the host is in the given map.
func hostInMap(hostMap map[string]struct{}) mux.MatcherFunc {
	return func(r *http.Request, match *mux.RouteMatch) bool {
		_, hostOk := hostMap[r.Host]
		return hostOk
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

	conf := &StatsContent{
		URLPath:    r.URL.Path,
		NPings:     strconv.Itoa(pings),
		NConfigs:   strconv.Itoa(configs),
		NEntities:  strconv.Itoa(entities),
		NResources: strconv.Itoa(resources),
		NRequests:  strconv.Itoa(requests),
		Host:       r.Host,
	}

	err := statsTemplate.Execute(w, conf)
	if err != nil {
		log.Fatal(err)
		os.Exit(1)
	}

	latencies.Observe(time.Since(lt).Seconds())
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

// refreshCertificate updates the certificate using the ssl directory, if provided.
func refreshCertificate() error {
	if *sslDir == "" {
		return errors.New("no ssl directory")
	}
	cmdName := "openssl"
	cert := *sslDir + "domain.crt"
	key := *sslDir + "domain.key"
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
	return nil
}

func init() {
	prometheus.MustRegister(latencies)
}

func main() {
	var err error
	var b ap_common.Broker

	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	flag.Var(ports, "http-ports", "The ports to listen on for HTTP requests.")
	flag.Parse()

	log.Printf("starting on ports %v", ports)

	mcp, err := mcp.New(pname)
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

	config = ap_common.NewConfig(pname)

	statsTemplate, err = template.ParseFiles(
		"golang/src/ap.httpd/stats.html.got")
	if err != nil {
		log.Fatal(err)
	}

	phishdata.Loader("online-valid-test.csv")
	// phishdata.AutoLoader("online-valid.csv", time.Hour)
	// ^^ uncomment to autoupdate with real phish data, also change in dns4d

	// init maps
	ifaceToSubnet = config.GetSubnets()
	if ifaceToSubnet == nil {
		log.Fatalf("Failed to get subnet addresses")
	}

	// Add gateway address to catch dns4d phishing forwarding and if no vlan.
	gatewaySubnet, err := config.GetProp("@/dhcp/config/network")
	if err != nil {
		log.Fatalf("Failed to get gateway address")
	}
	ifaceToSubnet["gateway"] = gatewaySubnet

	for iface, subnet := range ifaceToSubnet {
		router := network.SubnetRouter(subnet)
		if iface == "connect" {
			captiveMap[router+captivePort] = struct{}{}
		} else {
			adminMap[router] = struct{}{}
			for _, port := range ports {
				adminMap[router+port] = struct{}{}
			}
		}
	}

	// routing
	mainRouter := mux.NewRouter()

	adminRouter := mainRouter.MatcherFunc(hostInMap(adminMap)).Subrouter()
	adminRouter.HandleFunc("/config", cfgHandler)
	adminRouter.HandleFunc("/stats", statsHandler)

	captiveRouter := mainRouter.MatcherFunc(hostInMap(captiveMap)).Subrouter()
	captiveRouter.HandleFunc("/", index_handler)

	// XXX uncomment when merged with nils:
	// phishRouter := mainRouter.MatcherFunc(
	// 	func(r *http.Request, match *mux.RouteMatch) bool {
	// 		return phishdata.KnownToDataSource(r.Host)
	// 	}).Subrouter()
	// phishRouter.HandleFunc("/", templateHandler("nophish"))

	// mainRouter.HandleFunc("/", templateHandler("default"))
	http.Handle("/", mainRouter)

	if mcp != nil {
		mcp.SetStatus("online")
	}

	if *sslDir != "" {
		if !strings.HasSuffix(*sslDir, "/") {
			*sslDir = *sslDir + "/"
		}
		cert = *sslDir + "domain.crt"
		key = *sslDir + "domain.key"
	}

	err = refreshCertificate()
	if err != nil {
		log.Printf("Error refreshing certificate: %v", err)
	}

	for iface, subnet := range ifaceToSubnet {
		router := network.SubnetRouter(subnet)
		if iface == "connect" {
			addr := router + captivePort
			log.Printf("Listening on %s (%s)", addr, iface)
			go func() {
				log.Fatal(http.ListenAndServe(addr, captiveRouter))
			}()
		} else {
			for _, port := range ports {
				addr := router + port
				if port == ":443" {
					if err := refreshCertificate(); err == nil {
						log.Printf("Listening on %s (%s)", addr, iface)
						go func(a string) {
							log.Fatal(http.ListenAndServeTLS(a, cert, key,
								mainRouter))
						}(addr)
					} else {
						log.Printf("Failed to listen on %s (%s): %v", addr,
							iface, err)
					}
				} else {
					log.Printf("Listening on %s (%s)", addr, iface)
					go func(a string) {
						log.Fatal(http.ListenAndServe(a, mainRouter))
					}(addr)
				}
			}
		}
	}

	sig := make(chan os.Signal)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	s := <-sig
	log.Fatalf("Signal (%v) received", s)
}
