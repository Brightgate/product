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

	"ap_common/mcp"
	"base_def"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	// Ubuntu: requires libzmq3-dev, which is 0MQ 4.2.1.
	zmq "github.com/pebbe/zmq4"
)

var addr = flag.String("listen-address", base_def.BROKER_PROMETHEUS_PORT,
	"The address to listen on for HTTP requests.")

func main() {
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

	mcp, err := mcp.New("ap.brokerd")
	if err != nil {
		log.Printf("Failed to connect to mcp\n")
	}

	http.Handle("/metrics", promhttp.Handler())

	go http.ListenAndServe(*addr, nil)

	log.Println("prometheus client thread started")

	frontend, _ := zmq.NewSocket(zmq.XSUB)
	defer frontend.Close()
	frontend.Bind(base_def.BROKER_ZMQ_PUB_URL)

	backend, _ := zmq.NewSocket(zmq.XPUB)
	defer backend.Close()
	backend.Bind(base_def.BROKER_ZMQ_SUB_URL)

	log.Println("frontend, backend ready; about to invoke proxy")

	if mcp != nil {
		mcp.SetStatus("online")
	}
	err = zmq.Proxy(frontend, backend, nil)

	log.Fatalln("zmq proxy interrupted", err)
}
