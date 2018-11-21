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
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"bg/base_def"
	"bg/cl_common/daemonutils"
	"bg/common/cfgmsg"
	"bg/common/cfgtree"

	rpc "bg/cloud_rpc"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/tomazk/envcfg"
	"go.uber.org/zap"
	"go.uber.org/zap/zapgrpc"
	"google.golang.org/grpc/grpclog"
)

const pname = "cl.configd"

type configStore interface {
	get(context.Context, string) (*cfgtree.PTree, error)
	set(context.Context, string, *cfgtree.PTree) error
}

type cmdQueue interface {
	submit(context.Context, *perAPState, *cfgmsg.ConfigQuery) (int64, error)
	fetch(context.Context, *perAPState, int64, uint32, bool) ([]*cfgmsg.ConfigQuery, error)
	status(context.Context, *perAPState, int64) (*cfgmsg.ConfigResponse, error)
	cancel(context.Context, *perAPState, int64) (*cfgmsg.ConfigResponse, error)
	complete(context.Context, *perAPState, *cfgmsg.ConfigResponse) error
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
	Store              string `envcfg:"B10E_CLCONFIGD_STORE"`
	Emulate            bool   `envcfg:"B10E_CLCONFIGD_EMULATE"`
	MemCmdQueue        bool   `envcfg:"B10E_CLCONFIGD_MEMCMDQUEUE"`

	// XXX it would be nicer if we could have this be ENABLE_TLS with
	// default=true but envcfg does not support that.
	DisableTLS bool `envcfg:"B10E_CLCONFIGD_DISABLE_TLS"`
}

var (
	cqMax = flag.Int("cq", 1000, "max number of completions to retain")

	log  *zap.Logger
	slog *zap.SugaredLogger

	store configStore
)

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

func mkStore() configStore {
	var err error
	var store configStore

	switch environ.Store {
	case "file":
		store, err = newFileStore(
			filepath.Join(daemonutils.ClRoot(), "/etc/configs"))
	case "", "db":
		if environ.PostgresConnection == "" {
			err = fmt.Errorf("B10E_CLCONFIGD_POSTGRES_CONNECTION must be set")
		} else {
			store, err = newDBStore(environ.PostgresConnection)
		}
		environ.Store = "db"
	default:
		err = fmt.Errorf("Unrecognized store type")
	}
	if err != nil {
		slog.Fatalf("Failed to configure '%s' store: %v", environ.Store, err)
	}
	slog.Infof("Using '%s' store %s", environ.Store, store)
	return store
}

func main() {
	var err error

	log, slog = daemonutils.SetupLogs()
	flag.Parse()
	log, slog = daemonutils.ResetupLogs()
	defer log.Sync()

	// Redirect grpc internal log messages to zap, at DEBUG
	glog := log.WithOptions(
		// zapgrpc adds extra frames, which need to be skipped
		zap.AddCallerSkip(3),
	)
	grpclog.SetLogger(zapgrpc.NewLogger(glog, zapgrpc.WithDebug()))

	slog.Infow(pname+" starting", "args", os.Args)

	if err = envcfg.Unmarshal(&environ); err != nil {
		slog.Fatalf("Environment Error: %s", err)
	}

	go prometheusInit(environ.PrometheusPort)

	store = mkStore()

	if environ.Emulate {
		slog.Infof("Appliance emulator enabled")
	}

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
