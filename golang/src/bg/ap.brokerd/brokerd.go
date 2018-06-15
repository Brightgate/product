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
 * 0MQ XPUB
 * 0MQ XSUB
 */

package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"bg/ap_common/mcp"
	"bg/base_def"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	// Ubuntu: requires libzmq3-dev, which is 0MQ 4.2.1.
	zmq "github.com/pebbe/zmq4"
)

var addr = flag.String("listen-address", base_def.BROKER_PROMETHEUS_PORT,
	"The address to listen on for HTTP requests.")

var mcpd *mcp.MCP

func main() {
	var err error

	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	log.Println("start")
	/*
	 * Establish sockets and connect via proxy.
	 *
	 * XXX Unlike other servers, we do not ping ourselves.  Not sure
	 * whether this principle has to be held strongly, but since the
	 * messaging system is non-operational if we are not running,
	 * I'm not sure of the semantic value of a missed ping from the
	 * broker...
	 */
	flag.Parse()

	mcpd, err := mcp.New("ap.brokerd")
	if err != nil {
		log.Printf("Failed to connect to mcp\n")
	}

	http.Handle("/metrics", promhttp.Handler())

	go http.ListenAndServe(*addr, nil)

	log.Println("prometheus client thread started")

	frontend, _ := zmq.NewSocket(zmq.XSUB)
	defer frontend.Close()
	port := base_def.INCOMING_ZMQ_URL + base_def.BROKER_ZMQ_PUB_PORT
	if err = frontend.Bind(port); err != nil {
		log.Fatalf("Unable to bind publish port %s: %v\n", port, err)
	}
	log.Printf("Publishing on %s\n", port)

	backend, _ := zmq.NewSocket(zmq.XPUB)
	defer backend.Close()
	port = base_def.INCOMING_ZMQ_URL + base_def.BROKER_ZMQ_SUB_PORT
	if err = backend.Bind(port); err != nil {
		log.Fatalf("Unable to bind subscribe port %s: %v\n", port, err)
	}
	log.Printf("Subscribed on %s\n", port)

	mcpd.SetState(mcp.ONLINE)

	go func() {
		for {
			start := time.Now()

			err = zmq.Proxy(frontend, backend, nil)
			log.Printf("zmq proxy interrupted: %v\n", err)
			if time.Since(start).Seconds() < 10 {
				break
			}
		}
		mcpd.SetState(mcp.BROKEN)
		log.Fatalf("Errors coming too quickly.  Giving up.\n")
	}()

	sig := make(chan os.Signal, 2)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	s := <-sig
	log.Fatalf("Signal (%v) received, stopping\n", s)
}
