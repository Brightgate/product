/*
 * COPYRIGHT 2019 Brightgate Inc. All rights reserved.
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
	"net"
	"strconv"
	"strings"
	"time"

	"bg/cloud_rpc"
	"bg/common/grpcutils"

	"google.golang.org/grpc"
)

const (
	defaultHost = "rpc0.b10e.net"
	defaultPort = "443"
	dnsTimeout  = 5 * time.Second
	maxBackoff  = 30 * time.Minute
)

func getPort(id int) string {
	var port string

	// If the user specified a port on the command line, that overrides
	// anything in the config tree
	if *connectPort != 0 {
		port = strconv.Itoa(*connectPort)

	} else if config != nil {
		path := fmt.Sprintf("@/cloud/svc_rpc/%d/port", id)
		if val, err := config.GetProp(path); err == nil {
			if _, err := strconv.Atoi(val); err == nil {
				port = val
			} else {
				slog.Warnf("invalid port at %s: %s", path, val)
			}
		}
	}

	return port
}

func getHost(id int) (string, string) {
	var host string

	// If the user specified a host on the command line, that overrides
	// anything in the config tree
	if *connectHost != "" {
		return *connectHost, ""
	}

	if config == nil {
		return "", ""
	}

	path := fmt.Sprintf("@/cloud/svc_rpc/%d/host", id)
	host, err := config.GetProp(path)

	if host == "" {
		slog.Warnf("no host property at %s: %v", path, err)
	} else {
		// Verify that a DNS lookup of this hostname succeeds
		ctx, cancel := context.WithTimeout(context.Background(),
			dnsTimeout)
		defer cancel()

		// We are using r.LookupIPAddr rather than net.LookupIPAddr, so
		// we can be sure that it doesn't hang indefinitely.
		r := net.DefaultResolver
		if _, err := r.LookupIPAddr(ctx, host); err == nil {
			return host, ""
		}
	}

	path = fmt.Sprintf("@/cloud/svc_rpc/%d/hostip", id)
	ipaddr, err := config.GetProp(path)
	if ipaddr == "" {
		slog.Warnf("no ipaddr at %s: %v", path, err)
	}

	return ipaddr, host
}

func getTLS(id int) bool {
	if config != nil {
		path := fmt.Sprintf("@/cloud/svc_rpc/%d/tls", id)
		if tlsStr, err := config.GetProp(path); err == nil {
			switch strings.ToLower(tlsStr) {
			case "true":
				return true
			case "false":
				return false
			default:
				slog.Warnf("Invalid value for %s: %s", path, tlsStr)
			}
		}
	}

	return *enableTLSFlag
}

func getSvcURL(id int) (string, string, bool) {
	var port, addr, certHost string

	if port = getPort(id); port == "" {
		port = defaultPort
	}

	if addr, certHost = getHost(id); addr == "" {
		addr = defaultHost
		certHost = ""
	}

	tls := getTLS(id)

	return addr + ":" + port, certHost, tls
}

func grpcConnect(ctx context.Context) *grpc.ClientConn {
	var conn *grpc.ClientConn
	var err error

	wait := time.Second
	for {
		url, certHost, tls := getSvcURL(0)
		if tls {
			as := ""
			if certHost != "" {
				as = " as " + certHost
			}

			slog.Debugf("Connecting to '%s'%s", url, as)
			conn, err = grpcutils.NewClientTLSConn(url, certHost, pname)
		} else {
			slog.Debugf("Connecting to '%s'", url)
			conn, err = grpcutils.NewClientConn(url, pname)
		}

		if err == nil {
			// Verify that we can actually communicate with the
			// cloud server
			tclient := cloud_rpc.NewEventClient(conn)
			tctx, tcancel := context.WithCancel(ctx)
			err = publishHeartbeat(tctx, tclient)
			tcancel()
			if err == nil {
				return conn
			}
			conn.Close()
		}
		slog.Infof("Failed to connect to %s: %v", url, err)

		if wait *= 2; wait > maxBackoff {
			wait = maxBackoff
		}
		slog.Warnf("Unable to connect to cl.rpcd.  Waiting %v.", wait)
		time.Sleep(wait)
	}
}
