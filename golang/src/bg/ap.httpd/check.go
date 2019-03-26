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
	"strings"
	"time"

	"github.com/gorilla/mux"
)

func makeCheckRouter() *mux.Router {
	router := mux.NewRouter()
	router.HandleFunc("/diag", diagHandler).Methods("GET")
	return router
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
		{"service status", plat.ExpandDirPath("__APPACKAGE__", "bin", "ap-ctl"), []string{"status", "all"}},
		{"client list", plat.ExpandDirPath("__APPACKAGE__", "bin", "ap-configctl"), []string{"get", "clients", "-a"}},
		{"ping 1.1.1.1", "ping", []string{"-W", "3", "-w", "3", "-A", "-c", "3", "1.1.1.1"}},
		{"nslookup svc1.b10e.net", "nslookup", []string{"svc1.b10e.net"}},
		{"https://svc1.b10e.net", "curl", []string{"-o", "/dev/null", "--fail", "https://svc1.b10e.net/"}},
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
