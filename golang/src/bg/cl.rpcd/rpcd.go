/*
 * COPYRIGHT 2020 Brightgate Inc.  All rights reserved.
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
	_ "net/http/pprof"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"bg/base_def"
	"bg/cl_common/auth/m2mauth"
	"bg/cl_common/certificate"
	"bg/cl_common/clcfg"
	"bg/cl_common/daemonutils"
	"bg/cloud_models/appliancedb"
	"bg/cloud_rpc"
	"bg/common/cfgapi"

	"github.com/satori/uuid"
	"github.com/tomazk/envcfg"

	"github.com/labstack/echo"
	"github.com/labstack/echo/middleware"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"go.uber.org/zap"
	"go.uber.org/zap/zapgrpc"

	"cloud.google.com/go/compute/metadata"
	"cloud.google.com/go/pubsub"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	_ "google.golang.org/grpc/encoding/gzip"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/status"

	"github.com/grpc-ecosystem/go-grpc-middleware"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap"
	"github.com/grpc-ecosystem/go-grpc-middleware/tags"
	"github.com/grpc-ecosystem/go-grpc-middleware/util/metautils"
	"github.com/grpc-ecosystem/go-grpc-prometheus"
)

// Cfg contains the environment variable-based configuration settings
type Cfg struct {
	// The certificate hostname is the primary hostname associated
	// with the SSL certificate, and not necessarily the nodename.
	// We use this variable to navigate the Let's Encrypt directory
	// hierarchy.
	CertHostname       string `envcfg:"B10E_CERT_HOSTNAME"`
	GenerateCert       bool   `envcfg:"B10E_GENERATE_CERT"`
	DiagPort           string `envcfg:"B10E_CLRPCD_DIAG_PORT"`
	GrpcPort           string `envcfg:"B10E_CLRPCD_GRPC_PORT"`
	HTTPListen         string `envcfg:"B10E_CLRPCD_HTTP_LISTEN"`
	PubsubProject      string `envcfg:"B10E_CLRPCD_PUBSUB_PROJECT"`
	PubsubTopic        string `envcfg:"B10E_CLRPCD_PUBSUB_TOPIC"`
	PostgresConnection string `envcfg:"B10E_CLRPCD_POSTGRES_CONNECTION"`
	// Whether to disable TLS for incoming requests (danger!)
	// XXX it would be nicer if we could have this be ENABLE_TLS with
	// default=true but envcfg does not support that.
	DisableTLS bool `envcfg:"B10E_CLRPCD_DISABLE_TLS"`

	ConfigdConnection string `envcfg:"B10E_CLRPCD_CLCONFIGD_CONNECTION"`
	// Whether to disable TLS for outbound requests to cl.configd
	ConfigdDisableTLS bool   `envcfg:"B10E_CLRPCD_CLCONFIGD_DISABLE_TLS"`
	RPCTimeout        string `envcfg:"B10E_CLRPCD_CLCONFIGD_TIMEOUT"`

	KeyLogFile string `envcfg:"SSLKEYLOGFILE"`
}

type getClientHandleFunc func(uuid string) (*cfgapi.Handle, error)

const (
	checkMark = `✔︎ `
	pname     = "cl.rpcd"
)

var (
	log  *zap.Logger
	slog *zap.SugaredLogger

	environ Cfg
)

func getSiteUUID(ctx context.Context, allowNullSiteUUID bool) (uuid.UUID, error) {
	siteUUID := metautils.ExtractIncoming(ctx).Get("site_uuid")
	if siteUUID == "" {
		return uuid.Nil, status.Errorf(codes.Internal, "missing site_uuid")
	}
	u, err := uuid.FromString(siteUUID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("bad site_uuid")
	}
	if allowNullSiteUUID == false && u == appliancedb.NullSiteUUID {
		return uuid.Nil, status.Errorf(codes.PermissionDenied,
			"not permitted for null site_uuid")
	}
	return u, nil
}

func getApplianceUUID(ctx context.Context, allowNullApplianceUUID bool) (uuid.UUID, error) {
	appUUID := metautils.ExtractIncoming(ctx).Get("appliance_uuid")
	if appUUID == "" {
		return uuid.Nil, status.Errorf(codes.Internal, "missing appliance_uuid")
	}
	u, err := uuid.FromString(appUUID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("bad appliance_uuid")
	}
	if allowNullApplianceUUID == false && u == uuid.Nil {
		return uuid.Nil, status.Errorf(codes.PermissionDenied,
			"not permitted for null appliance_uuid")
	}
	return u, nil
}

// processEnv checks (and in some cases modifies) the environment-derived
// configuration.
func processEnv() {
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
		slog.Fatalf("B10E_CLRPCD_CLCONFIGD_CONNECTION must be set")
	}
	// Supply defaults where applicable
	if environ.DiagPort == "" {
		environ.DiagPort = base_def.CLRPCD_DIAG_PORT
	}
	if environ.GrpcPort == "" {
		environ.GrpcPort = base_def.CLRPCD_GRPC_PORT
	}
	if environ.HTTPListen == "" {
		environ.HTTPListen = ":80"
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

func makeGrpcServer(applianceDB appliancedb.DataStore) *grpc.Server {
	var opts []grpc.ServerOption
	var keypair tls.Certificate
	var serverCertPool *x509.CertPool

	if environ.DisableTLS {
		slog.Warnf("TLS Mode: local, NO TLS!  For developers only.")
	} else {
		slog.Infof(checkMark + "TLS Mode: Secured by TLS")
		if environ.CertHostname == "" && !environ.GenerateCert {
			slog.Fatalf("B10E_GENERATE_CERT must be defined if B10E_CERT_HOSTNAME is not")
		}

		var keyb, certb []byte
		var err error
		if environ.GenerateCert {
			// Behind an HTTPS load-balancer proxy, we need to use a
			// key/cert pair, even if they don't correspond to the
			// host being contacted.
			keyb, certb, err = certificate.CreateSSKeyCert(environ.CertHostname)
			if err != nil {
				slog.Fatalw("generate self-signed cert failed", "err", err)
			}
		} else {
			certf := fmt.Sprintf("/etc/letsencrypt/live/%s/fullchain.pem",
				environ.CertHostname)
			keyf := fmt.Sprintf("/etc/letsencrypt/live/%s/privkey.pem",
				environ.CertHostname)

			certb, err = ioutil.ReadFile(certf)
			if err != nil {
				slog.Fatalw("read cert file failed", "err", err)
			}
			keyb, err = ioutil.ReadFile(keyf)
			if err != nil {
				slog.Fatalw("read key file failed", "err", err)
			}
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

		if environ.KeyLogFile != "" {
			w, err := os.Create(environ.KeyLogFile)
			if err == nil {
				tlsc.KeyLogWriter = w
			}
		}

		opts = append(opts, grpc.Creds(credentials.NewTLS(&tlsc)))
	}

	logOpts := []grpc_zap.Option{
		grpc_zap.WithDecider(func(fullMethodName string, err error) bool {
			if err == nil && strings.HasPrefix(fullMethodName, "/cloud_rpc.ConfigBackEnd/") {
				return false
			}
			return true
		}),
	}
	streamFuncs := []grpc.StreamServerInterceptor{
		grpc_ctxtags.StreamServerInterceptor(),
		grpc_zap.StreamServerInterceptor(log),
	}
	unaryFuncs := []grpc.UnaryServerInterceptor{
		grpc_ctxtags.UnaryServerInterceptor(),
		grpc_zap.UnaryServerInterceptor(log, logOpts...),
	}

	// Insert Prometheus interceptor if enabled
	if len(environ.DiagPort) != 0 {
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

	kep := keepalive.EnforcementPolicy{
		MinTime:             30 * time.Second,
		PermitWithoutStream: true,
	}
	opts = append(opts, grpc.KeepaliveEnforcementPolicy(kep))

	grpcServer := grpc.NewServer(opts...)

	if len(environ.DiagPort) != 0 {
		// Documentation notes that this is somewhat expensive
		grpc_prometheus.EnableHandlingTimeHistogram()
		grpc_prometheus.Register(grpcServer)
	}
	return grpcServer
}

func setupGrpcLog(log *zap.Logger) {
	// Redirect grpc internal log messages to zap, at DEBUG
	glog := log.WithOptions(
		// zapgrpc adds extra frames, which need to be skipped
		zap.AddCallerSkip(3),
	)
	grpclog.SetLogger(zapgrpc.NewLogger(glog, zapgrpc.WithDebug()))
}

func getConfigClientHandle(cuuid string) (*cfgapi.Handle, error) {
	uu, err := uuid.FromString(cuuid)
	if err != nil {
		return nil, err
	}
	configd, err := clcfg.NewConfigd(pname, uu.String(),
		environ.ConfigdConnection, !environ.ConfigdDisableTLS)
	if err != nil {
		return nil, err
	}
	configHandle := cfgapi.NewHandle(configd)
	return configHandle, nil
}

func makeHTTPServer() *echo.Echo {
	r := echo.New()
	r.HideBanner = true
	r.Use(middleware.Logger())
	r.Use(middleware.Recover())

	// Setup /check endpoints
	_ = newCheckHandler(r, getConfigClientHandle)

	return r
}

func main() {
	var err error

	log, slog = daemonutils.SetupLogs()
	flag.Parse()
	log, slog = daemonutils.ResetupLogs()
	defer log.Sync()
	setupGrpcLog(log)

	err = envcfg.Unmarshal(&environ)
	if err != nil {
		slog.Fatalf("Environment Error: %s", err)
	}
	processEnv()

	slog.Infow(pname+" starting", "args", os.Args)

	applianceDB := makeApplianceDB(environ.PostgresConnection)
	grpcServer := makeGrpcServer(applianceDB)

	pubsubClient, err := pubsub.NewClient(context.Background(), environ.PubsubProject)
	if err != nil {
		slog.Fatalf("failed to make pubsub client")
	}

	eventServer, err := newEventServer(pubsubClient, environ.PubsubTopic)
	if err != nil {
		slog.Fatalf("failed to start event server: %s", err)
	}

	cloudStorageServer := defaultCloudStorageServer(applianceDB)
	certificateServer := newCertServer(applianceDB)
	relServer := newReleaseServer(applianceDB)

	cloud_rpc.RegisterEventServer(grpcServer, eventServer)
	slog.Infof(checkMark+"Ready to put event to Cloud PubSub %s", environ.PubsubTopic)
	cloud_rpc.RegisterCloudStorageServer(grpcServer, cloudStorageServer)
	slog.Infof(checkMark + "Ready to serve Cloud Storage related requests")
	cloud_rpc.RegisterCertificateManagerServer(grpcServer, certificateServer)
	slog.Infof(checkMark + "Ready to serve certificate requests")
	cloud_rpc.RegisterReleaseManagerServer(grpcServer, relServer)
	slog.Infof(checkMark + "Ready to serve release requests")

	if environ.ConfigdDisableTLS {
		slog.Warnf("Disabling TLS for connection to Configd")
	}
	configdServer := defaultConfigServer(environ.ConfigdConnection,
		environ.RPCTimeout, environ.ConfigdDisableTLS)
	cloud_rpc.RegisterConfigBackEndServer(grpcServer, configdServer)
	slog.Infof(checkMark + "Ready to relay configd requests")

	prometheusInit(environ.DiagPort)

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

	rHTTP := makeHTTPServer()
	httpSrv := &http.Server{
		Addr: environ.HTTPListen,
	}
	go func() {
		if err := rHTTP.StartServer(httpSrv); err != nil {
			rHTTP.Logger.Info("shutting down HTTP (health) service")
		}
	}()

	sig := make(chan os.Signal, 2)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	s := <-sig
	slog.Infof("Signal (%v) received, stopping", s)
	grpcServer.Stop()
}
