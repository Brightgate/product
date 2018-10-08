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

/*
 * cloud gRPC server
 *
 * Follows 12 factor app design.
 */

package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"bg/base_def"
	"bg/cl_common/auth/m2mauth"
	"bg/cl_common/daemonutils"
	"bg/cloud_models/appliancedb"
	"bg/cloud_rpc"

	"github.com/tomazk/envcfg"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"cloud.google.com/go/compute/metadata"
	"cloud.google.com/go/pubsub"

	"github.com/grpc-ecosystem/go-grpc-middleware/util/metautils"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	_ "google.golang.org/grpc/encoding/gzip"
	"google.golang.org/grpc/peer"

	"github.com/grpc-ecosystem/go-grpc-middleware"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap"
	"github.com/grpc-ecosystem/go-grpc-middleware/tags"
	"github.com/grpc-ecosystem/go-grpc-prometheus"
)

const checkMark = `✔︎ `

// Cfg contains the environment variable-based configuration settings
type Cfg struct {
	// The certificate hostname is the primary hostname associated
	// with the SSL certificate, and not necessarily the nodename.
	// We use this variable to navigate the Let's Encrypt directory
	// hierarchy.
	CertHostname       string `envcfg:"B10E_CERT_HOSTNAME"`
	PrometheusPort     string `envcfg:"B10E_CLRPCD_PROMETHEUS_PORT"`
	GrpcPort           string `envcfg:"B10E_CLRPCD_GRPC_PORT"`
	PubsubProject      string `envcfg:"B10E_CLRPCD_PUBSUB_PROJECT"`
	PubsubTopic        string `envcfg:"B10E_CLRPCD_PUBSUB_TOPIC"`
	PostgresConnection string `envcfg:"B10E_CLRPCD_POSTGRES_CONNECTION"`
	// XXX it would be nicer if we could have this be ENABLE_TLS with
	// default=true but envcfg does not support that.
	DisableTLS bool `envcfg:"B10E_CLRPCD_DISABLE_TLS"`

	ConfigdConnection string `envcfg:"B10E_CLCONFIGD_CONNECTION"`
	RPCTimeout        string `envcfg:"B10E_CLCONFIGD_TIMEOUT"`
}

const (
	pname = "cl.rpcd"
)

var (
	log  *zap.Logger
	slog *zap.SugaredLogger
)

// endpointLogger is a utility routine to build a zap logger customized for
// use by an endpoint.  It attaches useful context to the logger.
func endpointLogger(ctx context.Context) (*zap.Logger, *zap.SugaredLogger) {
	// An alternative here is to attach the logger to the context and
	// get it out that way.
	// In fact, ctx_zap has already done this for us, however the grpc zap
	// child logger adds an avalanche of information to the logger, and for
	// now it seems a bit much.
	fields := make([]zapcore.Field, 0)
	uuid := metautils.ExtractIncoming(ctx).Get("clouduuid")
	if uuid != "" {
		fields = append(fields, zap.String("clouduuid", uuid))
	}
	pr, ok := peer.FromContext(ctx)
	if ok && pr != nil {
		fields = append(fields, zap.String("peer", pr.Addr.String()))
	}
	childLog := log.With(fields...)
	return childLog, childLog.Sugar()
}

// processEnv checks (and in some cases modifies) the environment-derived
// configuration.
func processEnv(environ *Cfg) {
	if environ.PostgresConnection == "" {
		slog.Fatalf("B10E_CLRPCD_POSTGRES_CONNECTION must be set")
	}
	if environ.PubsubProject == "" {
		p, err := metadata.ProjectID()
		if err != nil {
			slog.Fatalf("Couldn't determine GCE ProjectID")
		}
		environ.PubsubProject = p
		slog.Infof("B10E_CLRPCD_PUBSUB_PROJECT defaulting to %v", p)
	}
	if environ.PubsubTopic == "" {
		slog.Fatalf("B10E_CLRPCD_PUBSUB_TOPIC must be set")
	}
	if environ.ConfigdConnection == "" {
		slog.Fatalf("B10E_CLCONFIGD_CONNECTION must be set")
	}
	// Supply defaults where applicable
	if environ.PrometheusPort == "" {
		environ.PrometheusPort = base_def.CLRPCD_PROMETHEUS_PORT
	}
	if environ.GrpcPort == "" {
		environ.GrpcPort = base_def.CLRPCD_GRPC_PORT
	}
	slog.Infof(checkMark + "Environ looks good")
}

func prometheusInit(prometheusPort string) {
	if len(prometheusPort) == 0 {
		slog.Warnf("Prometheus disabled")
		return
	}
	http.Handle("/metrics", promhttp.Handler())
	go func() { slog.Fatalf("prometheus listener: %v", http.ListenAndServe(prometheusPort, nil)) }()
	slog.Infof(checkMark+"Prometheus launched on port %v", prometheusPort)
}

// makeApplianceDB handles connection setup to the appliance database
func makeApplianceDB(postgresURI string) appliancedb.DataStore {
	applianceDB, err := appliancedb.Connect(postgresURI)
	if err != nil {
		slog.Fatalf("failed to connect to DB: %v", err)
	}
	slog.Infof(checkMark + "Connected to Appliance DB")
	err = applianceDB.Ping()
	if err != nil {
		slog.Fatalf("failed to ping DB: %s", err)
	}
	slog.Infof(checkMark + "Pinged Appliance DB")
	return applianceDB
}

func makeGrpcServer(environ Cfg, applianceDB appliancedb.DataStore) *grpc.Server {
	var opts []grpc.ServerOption
	var keypair tls.Certificate
	var serverCertPool *x509.CertPool

	if environ.DisableTLS {
		slog.Warnf("TLS Mode: local, NO TLS!  For developers only.")
	} else {
		slog.Infof(checkMark + "TLS Mode: Secured by TLS")
		if environ.CertHostname == "" {
			slog.Fatalf("B10E_CERT_HOSTNAME must be defined")
		}
		// Port 443 listener.
		certf := fmt.Sprintf("/etc/letsencrypt/live/%s/fullchain.pem",
			environ.CertHostname)
		keyf := fmt.Sprintf("/etc/letsencrypt/live/%s/privkey.pem",
			environ.CertHostname)

		certb, err := ioutil.ReadFile(certf)
		if err != nil {
			slog.Fatalw("read cert file failed", "err", err)
		}
		keyb, err := ioutil.ReadFile(keyf)
		if err != nil {
			slog.Fatalw("read key file failed", "err", err)
		}

		keypair, err = tls.X509KeyPair(certb, keyb)
		if err != nil {
			slog.Fatalw("generate X509 key pair failed", "err", err)
		}

		serverCertPool = x509.NewCertPool()

		ok := serverCertPool.AppendCertsFromPEM(certb)
		if !ok {
			slog.Fatal("bad certs")
		}

		tlsc := tls.Config{
			MinVersion:   tls.VersionTLS12,
			NextProtos:   []string{"h2"},
			Certificates: []tls.Certificate{keypair},
			CurvePreferences: []tls.CurveID{tls.CurveP521,
				tls.CurveP384, tls.CurveP256},
			PreferServerCipherSuites: true,
			CipherSuites: []uint16{
				tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
				tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA,
				tls.TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA,
				tls.TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA256,
				tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
				tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			},
		}

		opts = append(opts, grpc.Creds(credentials.NewTLS(&tlsc)))
	}

	streamFuncs := []grpc.StreamServerInterceptor{
		grpc_ctxtags.StreamServerInterceptor(),
		grpc_zap.StreamServerInterceptor(log),
	}
	unaryFuncs := []grpc.UnaryServerInterceptor{
		grpc_ctxtags.UnaryServerInterceptor(),
		grpc_zap.UnaryServerInterceptor(log),
	}

	// Insert Prometheus interceptor if enabled
	if len(environ.PrometheusPort) != 0 {
		streamFuncs = append(streamFuncs, grpc_prometheus.StreamServerInterceptor)
		unaryFuncs = append(unaryFuncs, grpc_prometheus.UnaryServerInterceptor)
	}

	m2mware := m2mauth.New(applianceDB)
	streamFuncs = append(streamFuncs, m2mware.StreamServerInterceptor())
	unaryFuncs = append(unaryFuncs, m2mware.UnaryServerInterceptor())

	opts = append(opts,
		grpc_middleware.WithStreamServerChain(streamFuncs...),
		grpc_middleware.WithUnaryServerChain(unaryFuncs...),
	)

	grpcServer := grpc.NewServer(opts...)

	if len(environ.PrometheusPort) != 0 {
		// Documentation notes that this is somewhat expensive
		grpc_prometheus.EnableHandlingTimeHistogram()
		grpc_prometheus.Register(grpcServer)
	}
	return grpcServer
}

func main() {
	var environ Cfg
	var err error

	log, slog = daemonutils.SetupLogs()
	flag.Parse()
	log, slog = daemonutils.ResetupLogs()
	defer log.Sync()
	grpc_zap.ReplaceGrpcLogger(log)

	err = envcfg.Unmarshal(&environ)
	if err != nil {
		slog.Fatalf("Environment Error: %s", err)
	}
	processEnv(&environ)

	slog.Infow(pname+" starting", "args", os.Args)

	applianceDB := makeApplianceDB(environ.PostgresConnection)
	grpcServer := makeGrpcServer(environ, applianceDB)

	pubsubClient, err := pubsub.NewClient(context.Background(), environ.PubsubProject)
	if err != nil {
		slog.Fatalf("failed to make pubsub client")
	}

	eventServer, err := newEventServer(pubsubClient, environ.PubsubTopic)
	if err != nil {
		slog.Fatalf("failed to start event server: %s", err)
	}

	cloudStorageServer := defaultCloudStorageServer(applianceDB)

	cloud_rpc.RegisterEventServer(grpcServer, eventServer)
	slog.Infof(checkMark+"Ready to put event to Cloud PubSub %s", environ.PubsubTopic)
	cloud_rpc.RegisterCloudStorageServer(grpcServer, cloudStorageServer)
	slog.Infof(checkMark + "Ready to serve Cloud Storage related requests")

	configdServer := defaultConfigServer(environ.ConfigdConnection,
		environ.RPCTimeout, environ.DisableTLS)
	cloud_rpc.RegisterConfigBackEndServer(grpcServer, configdServer)
	slog.Infof(checkMark + "Ready to relay configd requests")

	prometheusInit(environ.PrometheusPort)

	grpcConn, err := net.Listen("tcp", environ.GrpcPort)
	if err != nil {
		slog.Fatalf("Could not open gRPC listen socket: %v", err)
	}
	go func() {
		serr := grpcServer.Serve(grpcConn)
		if serr == nil {
			slog.Infof("gRPC Server stopped.")
			return
		}
		slog.Fatalf("gRPC Server failed: %v", err)
	}()
	slog.Infof(checkMark+"Started gRPC service at %v", environ.GrpcPort)

	sig := make(chan os.Signal, 2)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	s := <-sig
	slog.Infof("Signal (%v) received, stopping", s)
	grpcServer.Stop()
}
