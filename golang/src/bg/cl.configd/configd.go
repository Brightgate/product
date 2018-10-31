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
	"sync"
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

type perAPState struct {
	cloudUUID  string         // cloud UUID
	cachedTree *cfgtree.PTree // in-core cache of the config tree

	cmdQueue cmdQueue

	// XXX This is used only once, in frontend.go, protecting just a
	// cachedTree.Get() call.  What's the actual locking strategy here?
	sync.Mutex
}

type configStore interface {
	get(context.Context, string) (*cfgtree.PTree, error)
}

type cmdQueue interface {
	submit(context.Context, *perAPState, *cfgmsg.ConfigQuery) (int64, error)
	fetch(context.Context, *perAPState, int64, int64) ([]*cfgmsg.ConfigQuery, error)
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

func refreshAPState(s *perAPState, jsonTree string) {
	tree, err := cfgtree.NewPTree("@", []byte(jsonTree))
	if err != nil {
		slog.Warnf("failed to refresh %s: %v", s.cloudUUID, err)
	} else {
		s.cachedTree = tree
		slog.Debugf("new tree.  hash %x", s.cachedTree.Root().Hash())
	}
}

func newAPState(cloudUUID string, tree *cfgtree.PTree) (*perAPState, error) {
	var queue cmdQueue
	if environ.PostgresConnection != "" {
		queue, _ = newDBCmdQueue(environ.PostgresConnection)
	} else {
		queue = newMemCmdQueue(cloudUUID, *cqMax)
	}

	s := &perAPState{
		cloudUUID:  cloudUUID,
		cachedTree: tree,
		cmdQueue:   queue,
	}
	return s, nil
}

// XXX: this is an interim function.  When we get an update from an unknown
// appliance, we construct a new in-core cache for it.  Eventually this probably
// needs to be instantiated as part of the device provisioning process.
func initAPState(cloudUUID string) (*perAPState, error) {
	stateLock.Lock()
	defer stateLock.Unlock()

	s, ok := state[cloudUUID]
	if ok {
		return s, nil
	}

	s, _ = newAPState(cloudUUID, nil)
	state[cloudUUID] = s

	return s, nil
}

func getAPState(ctx context.Context, cloudUUID string) (*perAPState, error) {
	stateLock.Lock()
	defer stateLock.Unlock()

	if cloudUUID == "" {
		return nil, fmt.Errorf("No UUID provided")
	}

	s, ok := state[cloudUUID]
	if ok {
		return s, nil
	}

	tree, err := store.get(ctx, cloudUUID)
	if err == nil {
		s, _ = newAPState(cloudUUID, tree)
		state[cloudUUID] = s
		// XXX: currently we assume that any config tree we load from
		// cloud storage has no real appliance connected to it, so we
		// launch a go routine to emulate the missing appliance.  When
		// we start persisting real config trees to the database, we'll
		// need some way to distinguish between real configs and
		// emulated http-dev configs.
		go emulateAppliance(context.Background(), s)
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
