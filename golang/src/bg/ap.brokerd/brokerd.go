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
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"bg/ap_common/aputil"
	"bg/ap_common/mcp"
	"bg/base_def"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"

	// Ubuntu: requires libzmq3-dev, which is 0MQ 4.2.1.
	zmq "github.com/pebbe/zmq4"
)

const pname = "ap.brokerd"

var (
	addr = flag.String("listen-address", base_def.BROKERD_DIAG_PORT,
		"The address to listen on for HTTP requests.")

	mcpd *mcp.MCP
	slog *zap.SugaredLogger
)

func main() {
	var err error

	flag.Parse()
	slog = aputil.NewLogger(pname)
	defer slog.Sync()
	slog.Infof("starting")

	mcpd, err := mcp.New(pname)
	if err != nil {
		slog.Warnf("Failed to connect to mcp")
	}

	http.Handle("/metrics", promhttp.Handler())

	go http.ListenAndServe(*addr, nil)

	frontend, _ := zmq.NewSocket(zmq.XSUB)
	defer frontend.Close()
	port := base_def.INCOMING_ZMQ_URL + base_def.BROKER_ZMQ_PUB_PORT
	if err = frontend.Bind(port); err != nil {
		slog.Fatalf("Unable to bind publish port %s: %v", port, err)
	}
	slog.Debugf("Publishing on %s", port)

	backend, _ := zmq.NewSocket(zmq.XPUB)
	defer backend.Close()
	port = base_def.INCOMING_ZMQ_URL + base_def.BROKER_ZMQ_SUB_PORT
	if err = backend.Bind(port); err != nil {
		slog.Fatalf("Unable to bind subscribe port %s: %v", port, err)
	}
	slog.Debugf("Subscribed on %s", port)

	mcpd.SetState(mcp.ONLINE)

	go func() {
		for {
			start := time.Now()

			err = zmq.Proxy(frontend, backend, nil)
			slog.Warnf("zmq proxy interrupted: %v", err)
			if time.Since(start).Seconds() < 10 {
				break
			}
		}
		mcpd.SetState(mcp.BROKEN)
		slog.Fatalf("Errors coming too quickly.  Giving up.")
	}()

	sig := make(chan os.Signal, 2)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	s := <-sig
	slog.Infof("Signal (%v) received, stopping", s)
	os.Exit(0)
}
