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

	"bg/ap_common/aputil"
	"bg/ap_common/broker"
	"bg/ap_common/mcp"
	"bg/base_def"
	"bg/base_msg"

	"github.com/golang/protobuf/proto"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	addr = flag.String("listen-address", base_def.LOGD_PROMETHEUS_PORT,
		"The address to listen on for HTTP requests.")
	logDir = flag.String("logdir", "", "Log file directory")

	eventsHandled = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "events_handled",
			Help: "Number of cleaning scans completed.",
		})
)

const pname = "ap.logd"

func handlePing(event []byte) {
	// XXX pings were green
	ping := &base_msg.EventPing{}
	proto.Unmarshal(event, ping)
	log.Printf("[sys.ping] %v", ping)
	eventsHandled.Inc()
}

func handleConfig(event []byte) {
	config := &base_msg.EventConfig{}
	proto.Unmarshal(event, config)
	log.Printf("[sys.config] %v", config)
	eventsHandled.Inc()
}

func handleEntity(event []byte) {
	// XXX entities were blue
	entity := &base_msg.EventNetEntity{}
	proto.Unmarshal(event, entity)
	log.Printf("[net.entity] %v", entity)
	eventsHandled.Inc()
}

func handleResource(event []byte) {
	resource := &base_msg.EventNetResource{}
	proto.Unmarshal(event, resource)
	log.Printf("[net.resource] %v", resource)
	eventsHandled.Inc()
}

func handleRequest(event []byte) {
	// XXX requests were also blue
	request := &base_msg.EventNetRequest{}
	proto.Unmarshal(event, request)
	log.Printf("[net.request] %v", request)
	eventsHandled.Inc()
}

func handleIdentity(event []byte) {
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

func openLog(path string) (*os.File, error) {
	fp, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("couldn't get absolute path: %v", err)
	}

	if err := os.MkdirAll(fp, 0755); err != nil {
		return nil, fmt.Errorf("failed to make path: %v", err)
	}

	// slice, since might eventually want multiple types of logs
	logDirs := []string{fp}
	logRotate(logDirs)

	logfile := fp + "/events.log"
	file, err := os.OpenFile(logfile, os.O_CREATE|os.O_APPEND|os.O_WRONLY,
		0600)
	if err != nil {
		return nil, fmt.Errorf("error opening log file: %v", err)
	}
	return file, nil
}

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	flag.Parse()
	*logDir = aputil.ExpandDirPath(*logDir)

	file, err := openLog(*logDir)
	if err == nil {
		defer file.Close()
		log.SetOutput(io.MultiWriter(file, os.Stdout))
	} else {
		log.Printf("Failed to open logfile: %v\n", err)
	}

	mcpd, err := mcp.New(pname)
	if err != nil {
		log.Println("Failed to connect to mcp")
	}

	prometheus.MustRegister(eventsHandled)

	http.Handle("/metrics", promhttp.Handler())
	go http.ListenAndServe(*addr, nil)

	log.Println("prometheus client launched")

	b := broker.New(pname)
	b.Handle(base_def.TOPIC_PING, handlePing)
	b.Handle(base_def.TOPIC_CONFIG, handleConfig)
	b.Handle(base_def.TOPIC_ENTITY, handleEntity)
	b.Handle(base_def.TOPIC_RESOURCE, handleResource)
	b.Handle(base_def.TOPIC_REQUEST, handleRequest)
	b.Handle(base_def.TOPIC_IDENTITY, handleIdentity)
	defer b.Fini()

	if mcpd != nil {
		mcpd.SetState(mcp.ONLINE)
	}

	sig := make(chan os.Signal)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	s := <-sig
	log.Fatalf("Signal (%v) received, stopping\n", s)
}
