/*
 * COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package main

import (
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"bg/ap_common/platform"
)

func makeCheckRouter() *mux.Router {
	plat := platform.NewPlatform()
	router := mux.NewRouter()
	router.HandleFunc("/diag", diagHandler).Methods("GET")
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

func diagHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")

	// Time-bound the execution
	ctx := r.Context()
	ctx, cancel := context.WithTimeout(ctx, time.Duration(time.Second)*30)
	defer cancel()

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
		{"service status", plat.ExpandDirPath("__APPACKAGE__", "bin", "ap-ctl"), []string{"status", "all"}},
		{"health", plat.ExpandDirPath("__APPACKAGE__", "bin", "ap-configctl"), []string{"get", "@/metrics/health"}},
		{"client list", plat.ExpandDirPath("__APPACKAGE__", "bin", "ap-configctl"), []string{"get", "clients", "-a"}},
		{"ping 1.1.1.1", "ping", []string{"-W", "3", "-w", "3", "-A", "-c", "3", "1.1.1.1"}},
		{"dig svc1.b10e.net", plat.DigCmd, []string{"+time=3", "+tries=3", "svc1.b10e.net"}},
		{"dig @1.1.1.1 svc1.b10e.net", plat.DigCmd, []string{"+time=3", "+tries=3", "@1.1.1.1", "svc1.b10e.net"}},
		{"https://svc1.b10e.net", plat.CurlCmd, []string{"-o", "/dev/null", "--connect-timeout", "3", "--fail", "https://svc1.b10e.net/"}},
		{"heartbeat", plat.ExpandDirPath("__APPACKAGE__", "bin", "ap-rpc"), []string{"heartbeat"}},
	}
	for _, cmd := range cmds {
		fmt.Fprintf(w, "---------- begin %s\n", cmd.title)
		fmt.Fprintf(w, "$ %s %s\n", cmd.cmdPath, strings.Join(cmd.args, " "))
		c := exec.CommandContext(ctx, cmd.cmdPath, cmd.args...)
		c.Stdout = w
		c.Stderr = w
		err := c.Run()
		if err != nil {
			fmt.Fprintf(w, "%s failed: %v\n", cmd.title, err)
		}
		fmt.Fprintf(w, "---------- end %s\n\n", cmd.title)
	}
	fmt.Fprintf(w, "EOF\n")

}
