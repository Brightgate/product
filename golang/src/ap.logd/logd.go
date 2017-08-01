/*
 * COPYRIGHT 2017 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 *
 */

/*
 * message logger
 */

// XXX Exception messages are not displayed.

package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"ap_common/broker"
	"ap_common/mcp"
	"base_def"
	"base_msg"

	"github.com/golang/protobuf/proto"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	addr = flag.String("listen-address", base_def.LOGD_PROMETHEUS_PORT,
		"The address to listen on for HTTP requests.")
	logDirCLI     = flag.String("logdir", "", "Log file directory")
	eventsHandled = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "events_handled",
			Help: "Number of cleaning scans completed.",
		})
)

const pname = "ap.logd"

func handle_ping(event []byte) {
	// XXX pings were green
	ping := &base_msg.EventPing{}
	proto.Unmarshal(event, ping)
	log.Printf("[sys.ping] %v", ping)
	eventsHandled.Inc()
}

func handle_config(event []byte) {
	config := &base_msg.EventConfig{}
	proto.Unmarshal(event, config)
	log.Printf("[sys.config] %v", config)
	eventsHandled.Inc()
}

func handle_entity(event []byte) {
	// XXX entities were blue
	entity := &base_msg.EventNetEntity{}
	proto.Unmarshal(event, entity)
	log.Printf("[net.entity] %v", entity)
	eventsHandled.Inc()
}

func handle_resource(event []byte) {
	resource := &base_msg.EventNetResource{}
	proto.Unmarshal(event, resource)
	log.Printf("[net.resource] %v", resource)
	eventsHandled.Inc()
}

func handle_request(event []byte) {
	// XXX requests were also blue
	request := &base_msg.EventNetRequest{}
	proto.Unmarshal(event, request)
	log.Printf("[net.request] %v", request)
	eventsHandled.Inc()
}

func handle_identity(event []byte) {
	identity := &base_msg.EventNetIdentity{}
	proto.Unmarshal(event, identity)
	log.Printf("[net.identity] %v", identity)
	eventsHandled.Inc()
}

// logRotate creates the logrotate(1) configuration file for ap.logd at
// /etc/logrotate.d/logd.
func logRotate(logDirs []string) {
	fn := "/etc/logrotate.d/logd"
	cf, err := os.OpenFile(fn, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0755)
	if err != nil {
		log.Printf("Error creating logrotate file: %v", err)
		return
	}
	defer cf.Close()

	files := strings.Join(logDirs, "/*.log\n")
	opts := []string{"monthly", "copytruncate", "rotate 10", "size 10M",
		"missingok", "notifempty"}
	optsF := strings.Join(opts, "\n")
	fmt.Fprintf(cf, "%s/*.log {\n%s\n}", files, optsF)
}

func main() {
	var b broker.Broker

	flag.Parse()
	var logDir string

	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	apRoot, rootDefined := os.LookupEnv("APROOT")

	if *logDirCLI != "" {
		logDir = *logDirCLI
	} else if rootDefined {
		// Else if APROOT defined, construct path and open that.
		logDir = fmt.Sprintf("%s/var/spool/logd", apRoot)
	} else {
		log.Println("No log folder found; use -logdir or APROOT")
	}

	if logDir != "" {
		fp, err := filepath.Abs(logDir)
		if err != nil {
			log.Printf("Couldn't get absolute path: %v", err)
		}

		if err := os.MkdirAll(fp, 0755); err != nil {
			log.Printf("failed to mkdir: %v", err)
		}
		// slice, since might eventually want multiple types of logs
		logDirs := []string{fp}
		logRotate(logDirs)

		logfile := fp + "/events.log"
		file, err := os.OpenFile(logfile,
			os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0755)
		if err != nil {
			log.Println("Error opening log file.")
		} else {
			defer file.Close()
			log.SetOutput(io.MultiWriter(file, os.Stdout))
		}
	}

	mcp, err := mcp.New(pname)
	if err != nil {
		log.Println("Failed to connect to mcp")
	}

	prometheus.MustRegister(eventsHandled)

	http.Handle("/metrics", promhttp.Handler())
	go http.ListenAndServe(*addr, nil)

	log.Println("prometheus client launched")

	b.Init(pname)
	b.Handle(base_def.TOPIC_PING, handle_ping)
	b.Handle(base_def.TOPIC_CONFIG, handle_config)
	b.Handle(base_def.TOPIC_ENTITY, handle_entity)
	b.Handle(base_def.TOPIC_RESOURCE, handle_resource)
	b.Handle(base_def.TOPIC_REQUEST, handle_request)
	b.Handle(base_def.TOPIC_IDENTITY, handle_identity)
	b.Connect()
	defer b.Disconnect()

	if mcp != nil {
		mcp.SetStatus("online")
	}

	sig := make(chan os.Signal)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	s := <-sig
	log.Fatalf("Signal (%v) received, stopping\n", s)
}
