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
	"flag"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"ap_common"
	"base_def"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	addr = flag.String("promhttp-address", base_def.HTTPD_PROMETHEUS_PORT,
		"The address to listen on for Prometheus HTTP requests.")
	port = flag.String("http-port", ":8000",
		"The port to listen on for HTTP requests.")

	config *ap_common.Config
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

func handle_ping(event []byte) { pings++ }

func handle_config(event []byte) { configs++ }

func handle_entity(event []byte) { entities++ }

func handle_resource(event []byte) { resources++ }

func handle_request(event []byte) { requests++ }

func cfg_handler(w http.ResponseWriter, r *http.Request) {
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

type IndexContent struct {
	URLPath string

	NPings     string
	NConfigs   string
	NEntities  string
	NResources string
	NRequests  string

	Host string
}

var index_template *template.Template

func index_handler(w http.ResponseWriter, r *http.Request) {
	lt := time.Now()

	conf := &IndexContent{
		URLPath:    r.URL.Path,
		NPings:     strconv.Itoa(pings),
		NConfigs:   strconv.Itoa(configs),
		NEntities:  strconv.Itoa(entities),
		NResources: strconv.Itoa(resources),
		NRequests:  strconv.Itoa(requests),
		Host:       r.Host,
	}

	err := index_template.Execute(w, conf)
	if err != nil {
		log.Fatal(err)
		os.Exit(1)
	}

	latencies.Observe(time.Since(lt).Seconds())
}

func init() {
	prometheus.MustRegister(latencies)
}

func main() {
	var err error
	var b ap_common.Broker

	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	flag.Parse()

	log.Printf("start on port %v", *port)
	log.Println("cli flags parsed")

	time.Sleep(time.Second)

	// Set up connection with the broker daemon
	b.Init("ap.httpd")
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

	log.Println("prometheus client launched")
	index_template, err = template.ParseFiles("golang/src/ap.httpd/index.html.got")
	if err != nil {
		log.Fatal(err)
		os.Exit(2)
	}

	// Interface to configd
	config = ap_common.NewConfig("ap.httpd")

	//
	// HTTPD_INDEX_RENDER = promc.Summary("httpd_index_render_seconds",
	//                                    "HTTP index page render time")
	//
	// def timestamp_iso8601(ts):
	//     logging.info("timestamp_iso8601 %s %s", type(ts), ts)
	//
	//     if type(ts) != type(1.):
	//         ts = ts.seconds + ts.nanos / 1.e9
	//
	//     return arrow.Arrow.fromtimestamp(ts)
	//
	//
	// @app.route("/")
	// @HTTPD_INDEX_RENDER.time()
	// def index() -> str:
	//     return flask.render_template("index.html", entity_events=entity_events,
	//                                  request_events=request_events,
	//                                  other_events=other_events, now=arrow.utcnow())
	//
	//
	// XXX Statically bound to port 8000 on the public interfaces at the
	// moment.

	http.HandleFunc("/config", cfg_handler)
	http.HandleFunc("/", index_handler)

	log.Fatal(http.ListenAndServe(*port, nil))
}
