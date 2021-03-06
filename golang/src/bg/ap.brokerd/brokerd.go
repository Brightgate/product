/*
 * Copyright 2020 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


/*
 * 0MQ XPUB
 * 0MQ XSUB
 */

package main

import (
	"flag"
	"os"
	"os/signal"
	"syscall"

	"bg/ap_common/aputil"
	"bg/ap_common/mcp"
	"bg/base_def"

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

	aputil.ReportInit(slog, pname)
	mcpd.SetState(mcp.ONLINE)

	go forward(sock)

	sig := make(chan os.Signal, 2)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	s := <-sig
	slog.Infof("Signal (%v) received, stopping", s)
	os.Exit(0)
}

