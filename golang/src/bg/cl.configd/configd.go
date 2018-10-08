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
	"context"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"sync"
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
	uuid       string         // cloud UUID
	cachedTree *cfgtree.PTree // in-core cache of the config tree

	// XXX: the per-appliance command queue will eventually live in a
	// database - not in-core.
	lastCmdID int64     // last ID assigned to a cmd
	sq        *cmdQueue // submitted, but not completed ops
	cq        *cmdQueue // completed ops

	sync.Mutex
}

type configStore interface {
	get(context.Context, string) (*cfgtree.PTree, error)
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
	cqMax = flag.Int("cq", 1000, "max number of completions to retain")

	state     map[string]*perAPState
	stateLock sync.Mutex

	log  *zap.Logger
	slog *zap.SugaredLogger

	store multiStore
)

func getAPState(uuid string) (*perAPState, error) {
	stateLock.Lock()
	defer stateLock.Unlock()

	s, ok := state[uuid]
	if ok {
		return s, nil
	}

	tree, err := store.get(context.Background(), uuid)
	if err == nil {
		s = &perAPState{
			uuid:       uuid,
			cachedTree: tree,
			sq:         newCmdQueue(uuid, 0),
			cq:         newCmdQueue(uuid, *cqMax),
		}
		state[uuid] = s
		go emulateAppliance(s)
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

	fileStore, _ := newFileStore(daemonutils.ClRoot() + "/etc/configs")
	store.add(fileStore)
	if environ.PostgresConnection != "" {
		dbStore, err := newDBStore(environ.PostgresConnection)
		if err != nil {
			slog.Fatalf("Failed to connect to config store database: %v", err)
		}
		store.add(dbStore)
	}

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
