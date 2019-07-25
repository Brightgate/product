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

	"bg/ap_common/aputil"
	"bg/ap_common/mcp"
	"bg/base_def"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"

	"nanomsg.org/go/mangos/v2"
	"nanomsg.org/go/mangos/v2/protocol/bus"
	// Importing the TCP transport
	_ "nanomsg.org/go/mangos/v2/transport/tcp"
)

const pname = "ap.brokerd"

var (
	addr = flag.String("listen-address", base_def.BROKERD_DIAG_PORT,
		"The address to listen on for HTTP requests.")

	mcpd *mcp.MCP
	slog *zap.SugaredLogger
)

func forward(sock mangos.Socket) {
	for {
		phase := "receive"
		msg, err := sock.Recv()

		if err == nil {
			phase = "send"
			err = sock.Send(msg)
		}

		if err != nil {
			slog.Warnf("%s failed: %v", phase, err)
		}
	}
}

func main() {
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

	sock, err := bus.NewSocket()
	if err != nil {
		slog.Fatalf("allocating BUS socket: %v", err)
	}
	defer sock.Close()

	port := base_def.INCOMING_COMM_URL + base_def.BROKER_COMM_BUS_PORT
	if err = sock.Listen(port); err != nil {
		slog.Fatalf("Listen() on SUB port %s: %v", port, err)
	}
	slog.Debugf("Listening on %s", port)

	mcpd.SetState(mcp.ONLINE)

	go forward(sock)

	sig := make(chan os.Signal, 2)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	s := <-sig
	slog.Infof("Signal (%v) received, stopping", s)
	os.Exit(0)
}
