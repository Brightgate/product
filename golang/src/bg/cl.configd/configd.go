/*
 * COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 *
 */

package main

import (
	"flag"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"bg/base_def"
	"bg/cl_common/daemonutils"
	"bg/common/cfgtree"

	rpc "bg/cloud_rpc"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/tomazk/envcfg"
	"go.uber.org/zap"

	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap"
)

const pname = "cl.configd"

type perAPState struct {
	cachedTree *cfgtree.PTree
	submitted  []*rpc.CfgPropOps
	completed  []*rpc.CfgPropResponse
}

var environ struct {
	// The certificate hostname is the primary hostname associated
	// with the SSL certificate, and not necessarily the nodename.
	// We use this variable to navigate the Let's Encrypt directory
	// hierarchy.
	CertHostname       string `envcfg:"B10E_CERT_HOSTNAME"`
	PrometheusPort     string `envcfg:"B10E_CLCONFIGD_PROMETHEUS_PORT"`
	GrpcPort           string `envcfg:"B10E_CLCONFIGD_GRPC_PORT"`
	PostgresConnection string `envcfg:"B10E_CLCONFIGD_POSTGRES_CONNECTION"`

	// XXX it would be nicer if we could have this be ENABLE_TLS with
	// default=true but envcfg does not support that.
	DisableTLS bool `envcfg:"B10E_CLCONFIGD_DISABLE_TLS"`
}

var (
	state map[string]*perAPState

	log  *zap.Logger
	slog *zap.SugaredLogger
)

func getAPState(uuid string) (*perAPState, error) {
	var err error

	s, ok := state[uuid]
	if !ok {
		// XXX: eventually this state will be retrieved from the
		// database

		slog.Infof("Loading state for %s from file", uuid)
		if tree, lerr := configFromFile(uuid); lerr == nil {
			s = &perAPState{
				cachedTree: tree,
				submitted:  make([]*rpc.CfgPropOps, 0),
				completed:  make([]*rpc.CfgPropResponse, 0),
			}
			state[uuid] = s
		} else {
			err = lerr
		}
	}

	return s, err
}

func prometheusInit(prometheusPort string) {
	if len(prometheusPort) == 0 {
		slog.Warnf("Prometheus disabled")
		return
	}
	slog.Infof("Prometheus launching on port %v", prometheusPort)

	http.Handle("/metrics", promhttp.Handler())
	err := http.ListenAndServe(prometheusPort, nil)
	if err != nil {
		slog.Warnf("prometheus listener failed: %v\n", err)
	}
}

func main() {
	var err error

	log, slog = daemonutils.SetupLogs()
	flag.Parse()
	log, slog = daemonutils.ResetupLogs()
	defer log.Sync()
	grpc_zap.ReplaceGrpcLogger(log)

	slog.Infow(pname+" starting", "args", os.Args)

	if err = envcfg.Unmarshal(&environ); err != nil {
		slog.Fatalf("Environment Error: %s", err)
	}

	go prometheusInit(environ.PrometheusPort)

	state = make(map[string]*perAPState)

	port := environ.GrpcPort
	if port == "" {
		port = base_def.CLCONFIGD_GRPC_PORT
	}
	grpcServer := newGrpcServer(environ.CertHostname, environ.DisableTLS, port)

	rpc.RegisterConfigBackEndServer(grpcServer.Server, &backEndServer{})
	rpc.RegisterConfigFrontEndServer(grpcServer.Server, &frontEndServer{})

	grpcServer.start()

	sig := make(chan os.Signal, 2)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	s := <-sig
	slog.Infof("Signal (%v) received, stopping", s)
	grpcServer.stop()
}
