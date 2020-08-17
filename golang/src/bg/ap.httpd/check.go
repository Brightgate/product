/*
 * Copyright 2019 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"bg/ap_common/platform"
)

func makeCheckRouter() *mux.Router {
	router := mux.NewRouter()
	router.HandleFunc("/diag", diagHandler).Methods("GET")
	router.HandleFunc("/speedtest", speedtestHandler).Methods("GET")
	// XXX needs auth middleware in the future-- anyone can call this
	router.HandleFunc("/crashall", crashallHandler).Methods("GET")

	logRouter := router.PathPrefix("/log").Subrouter()
	logRouter.Use(cookieAuthMiddleware)
	mcpLogRe := regexp.MustCompile(`^mcp\.log(\.[0-9]+)?(\.gz)?$`)

	logRouter.HandleFunc("/{logname}", func(w http.ResponseWriter, r *http.Request) {
		logName := mux.Vars(r)["logname"]
		// Check input for sanity
		if mcpLogRe.MatchString(logName) {
			w.Header().Set("Content-Type", "text/plain")
			if strings.HasSuffix(logName, ".gz") {
				w.Header().Set("Content-Encoding", "gzip")
			}
			http.ServeFile(w, r, plat.ExpandDirPath("__APDATA__", "mcp", logName))
		} else {
			http.Error(w, "No such log", http.StatusNotFound)
		}
	}).Methods("GET")
	return router
}

func crashallHandler(w http.ResponseWriter, r *http.Request) {
	err := mcpd.Do("all", "crash")
	if err != nil {
		m := fmt.Sprintf("Failed to crash all: %v", err)
		http.Error(w, m, http.StatusInternalServerError)
	}
}

func apbin(name string) string {
	return plat.ExpandDirPath(platform.APPackage, "bin", name)
}

type flushWriter struct {
	f http.Flusher
	w io.Writer
}

func (fw *flushWriter) Write(p []byte) (n int, err error) {
	n, err = fw.w.Write(p)
	if fw.f != nil {
		fw.f.Flush()
	}
	return
}

func diagHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")

	// Time-bound the execution
	ctx := r.Context()
	ctx, cancel := context.WithTimeout(ctx, time.Duration(time.Second)*30)
	defer cancel()

	fw := flushWriter{w: w}
	if f, ok := w.(http.Flusher); ok {
		fw.f = f
	}

	cmds := []struct {
		title   string
		cmdPath string
		args    []string
	}{
		{"ip links", plat.IPCmd, []string{"link"}},
		{"ip addrs", plat.IPCmd, []string{"addr"}},
		{"ip routes", plat.IPCmd, []string{"route"}},
		{"iw list", plat.IwCmd, []string{"list"}},
		// This is a stopgap; either these should be sourced from the
		// config tree, or, hopefully, we can get the same info from
		// metrics in the future.
		{"wan", plat.EthtoolCmd, []string{"wan"}},
		{"lan0", plat.EthtoolCmd, []string{"lan0"}},
		{"lan1", plat.EthtoolCmd, []string{"lan1"}},
		{"lan2", plat.EthtoolCmd, []string{"lan2"}},
		{"lan3", plat.EthtoolCmd, []string{"lan3"}},
		{"service status", apbin("ap-ctl"), []string{"status", "all"}},
		{"health", apbin("ap-configctl"), []string{"get", "@/metrics/health"}},
		{"client list", apbin("ap-configctl"), []string{"get", "clients", "-a"}},
		{"ping 1.1.1.1", "ping", []string{"-W", "3", "-w", "3", "-A", "-c", "3", "1.1.1.1"}},
		{"dig svc1.b10e.net", plat.DigCmd, []string{"+time=3", "+tries=3", "svc1.b10e.net"}},
		{"dig @1.1.1.1 svc1.b10e.net", plat.DigCmd, []string{"+time=3", "+tries=3", "@1.1.1.1", "svc1.b10e.net"}},
		{"https://svc1.b10e.net", plat.CurlCmd, []string{"-o", "/dev/null", "--connect-timeout", "3", "--fail", "https://svc1.b10e.net/"}},
		{"heartbeat", apbin("ap-rpc"), []string{"heartbeat"}},
		{"wlan0 neighborhood", plat.IwCmd, []string{"dev", "wlan0", "scan"}},
		{"wlan1 neighborhood", plat.IwCmd, []string{"dev", "wlan1", "scan"}},
	}
	for _, cmd := range cmds {
		fmt.Fprintf(w, "---------- begin %s\n", cmd.title)
		fmt.Fprintf(w, "$ %s %s\n", cmd.cmdPath, strings.Join(cmd.args, " "))
		c := exec.CommandContext(ctx, cmd.cmdPath, cmd.args...)
		c.Stdout = &fw
		c.Stderr = &fw
		err := c.Run()
		if err != nil {
			fmt.Fprintf(w, "%s failed: %v\n", cmd.title, err)
		}
		fmt.Fprintf(w, "---------- end %s\n\n", cmd.title)
	}
	fmt.Fprintf(w, "EOF\n")
}

func speedtestHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")

	// Time-bound the execution; the test should take just over 21s
	ctx := r.Context()
	ctx, cancel := context.WithTimeout(ctx, time.Duration(time.Second)*30)
	defer cancel()

	fw := flushWriter{w: w}
	if f, ok := w.(http.Flusher); ok {
		fw.f = f
	}
	stPath := apbin("ap-speedtest")
	c := exec.CommandContext(ctx, stPath, "-all")
	c.Stdout = &fw
	c.Stderr = &fw
	err := c.Run()
	if err != nil {
		fmt.Fprintf(w, "ap-speedtest failed: %v\n", err)
	}
}

