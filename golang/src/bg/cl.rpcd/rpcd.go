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
	"bg/cl_common/echozap"
	"bg/cl_common/pgutils"
	"bg/cl_common/vaultdb"
	"bg/cl_common/vaultgcpauth"
	"bg/cl_common/vaulttokensource"
	"bg/cl_common/zapgommon"
	"bg/cloud_models/appliancedb"
	"bg/cloud_rpc"
	"bg/common/cfgapi"

	vault "github.com/hashicorp/vault/api"
	"github.com/satori/uuid"
	"github.com/tomazk/envcfg"

	"github.com/labstack/echo"
	"github.com/labstack/echo/middleware"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"go.uber.org/zap"
	"go.uber.org/zap/zapgrpc"

	"cloud.google.com/go/compute/metadata"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	_ "google.golang.org/grpc/encoding/gzip"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/status"

	"github.com/grpc-ecosystem/go-grpc-middleware"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap"
	grpc_ctxtags "github.com/grpc-ecosystem/go-grpc-middleware/tags"
	"github.com/grpc-ecosystem/go-grpc-middleware/util/metautils"
	"github.com/grpc-ecosystem/go-grpc-prometheus"
)

// Cfg contains the environment variable-based configuration settings
type Cfg struct {
	// The certificate hostname is the primary hostname associated
	// with the SSL certificate, and not necessarily the nodename.
	// We use this variable to navigate the Let's Encrypt directory
	// hierarchy.
	CertHostname            string `envcfg:"B10E_CERT_HOSTNAME"`
	GenerateCert            bool   `envcfg:"B10E_GENERATE_CERT"`
	DiagPort                string `envcfg:"B10E_CLRPCD_DIAG_PORT"`
	GrpcPort                string `envcfg:"B10E_CLRPCD_GRPC_PORT"`
	HTTPListen              string `envcfg:"B10E_CLRPCD_HTTP_LISTEN"`
	PubsubProject           string `envcfg:"B10E_CLRPCD_PUBSUB_PROJECT"`
	PubsubTopic             string `envcfg:"B10E_CLRPCD_PUBSUB_TOPIC"`
	PostgresConnection      string `envcfg:"B10E_CLRPCD_POSTGRES_CONNECTION"`
	VaultAuthPath           string `envcfg:"B10E_CLRPCD_VAULT_AUTH_PATH"`
	VaultDBPath             string `envcfg:"B10E_CLRPCD_VAULT_DB_PATH"`
	VaultDBRole             string `envcfg:"B10E_CLRPCD_VAULT_DB_ROLE"`
	VaultGCPPath            string `envcfg:"B10E_CLRPCD_VAULT_GCP_PATH"`
	VaultGCPRole            string `envcfg:"B10E_CLRPCD_VAULT_GCP_ROLE"`
	VaultVPNEscrowPath      string `envcfg:"B10E_CLRPCD_VAULT_VPN_ESCROW_PATH"`
	VaultVPNEscrowComponent string `envcfg:"B10E_CLRPCD_VAULT_VPN_ESCROW_COMPONENT"`
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

	environ       Cfg
	useVaultForDB bool
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
	var project string
	getProject := func() {
		if project != "" {
			return
		}
		p, err := metadata.ProjectID()
		if err != nil {
			slog.Fatalw("Can't get GCP project ID", "error", err)
		}
		project = p
	}

	if environ.PostgresConnection == "" {
		slog.Fatalf("B10E_CLRPCD_POSTGRES_CONNECTION must be set")
	}
	if environ.PubsubProject == "" {
		getProject()
		environ.PubsubProject = project
		slog.Infof("B10E_CLRPCD_PUBSUB_PROJECT defaulting to %v", project)
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

	if environ.VaultDBRole == "" {
		environ.VaultDBRole = pname
	}
	useVaultForDB = environ.VaultDBPath != ""

	if environ.VaultAuthPath == "" {
		getProject()
		environ.VaultAuthPath = "auth/gcp-" + project
		slog.Warnf("B10E_CLRPCD_VAULT_AUTH_PATH not found in "+
			"environment; setting to %s", environ.VaultAuthPath)
	}
	if environ.VaultVPNEscrowPath == "" {
		getProject()
		environ.VaultVPNEscrowPath = "secret/" + project
		slog.Warnf("B10E_CLRPCD_VAULT_VPN_ESCROW_PATH not found in "+
			"environment; setting to %s", environ.VaultVPNEscrowPath)
	}
	if environ.VaultVPNEscrowComponent == "" {
		environ.VaultVPNEscrowComponent = "appliance-vpn-escrow"
		slog.Warnf("B10E_CLRPCD_VAULT_VPN_ESCROW_COMPONENT not found "+
			"in environment; setting to %s",
			environ.VaultVPNEscrowComponent)
	}

	if environ.VaultGCPPath == "" {
		getProject()
		environ.VaultGCPPath = "gcp/" + project
		slog.Warnf("B10E_CLRPCD_VAULT_GCP_PATH not found in "+
			"environment; setting to %s", environ.VaultGCPPath)
	}
	if environ.VaultGCPRole == "" {
		environ.VaultGCPRole = pname
		slog.Warnf("B10E_CLRPCD_VAULT_GCP_ROLE not found in "+
			"environment; setting to %s", environ.VaultGCPRole)
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
func makeApplianceDB(postgresURI string, vaultClient *vault.Client, notifier *daemonutils.FanOut) (appliancedb.DataStore, *vaultdb.Connector) {
	postgresURI = pgutils.AddApplication(postgresURI, pname)

	var err error
	var applianceDB appliancedb.DataStore
	var vdbc *vaultdb.Connector
	if vaultClient != nil {
		vdbc = vaultdb.NewConnector(postgresURI, vaultClient, notifier,
			environ.VaultDBPath, environ.VaultDBRole, slog)
		applianceDB, err = appliancedb.VaultConnect(vdbc)
		if err != nil {
			slog.Fatalf("Error configuring DB from Vault: %v", err)
		}
	} else {
		applianceDB, err = appliancedb.Connect(postgresURI)
		if err != nil {
			slog.Fatalf("failed to connect to DB: %v", err)
		}
	}
	slog.Infof(checkMark + "Connected to Appliance DB")
	err = applianceDB.Ping()
	if err != nil {
		slog.Fatalf("failed to ping DB: %s", err)
	}
	slog.Infof(checkMark + "Pinged Appliance DB")
	return applianceDB, vdbc
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

func mkEchoZapLogger(zlog *zap.Logger) echo.MiddlewareFunc {
	// Mostly the default fields, but we skip time, which is already emitted
	// by zap, and id, which is always empty.
	m := []echozap.Field{
		echozap.CoreField("remote_ip"),
		echozap.CoreField("host"),
		echozap.CoreField("method"),
		echozap.CoreField("uri"),
		echozap.CoreField("user_agent"),
		echozap.CoreField("status"),
		echozap.CoreField("error"),
		echozap.CoreField("latency"),
		echozap.CoreField("latency_human"),
		echozap.CoreField("bytes_in"),
		echozap.CoreField("bytes_out"),
	}
	return echozap.Logger(zlog, m)
}

func makeHTTPServer(vdbc *vaultdb.Connector, log *zap.Logger) *echo.Echo {
	log = log.Named("http")

	r := echo.New()
	r.HideBanner = true
	r.Logger = zapgommon.ZapToGommonLog(log)
	r.Use(mkEchoZapLogger(log.Named("server")))
	r.Use(middleware.Recover())

	// Setup /check endpoints
	_ = newCheckHandler(r, getConfigClientHandle, vdbc)

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

	var vaultClient *vault.Client
	var notifier *daemonutils.FanOut
	if useVaultForDB {
		vaultClient, err = vault.NewClient(nil)
		if err != nil {
			slog.Fatalf("Vault error: %s", err)
		}
		if vaultClient.Token() == "" {
			slog.Info("Authenticating to Vault with GCP auth")
			hcLog := vaultgcpauth.ZapToHCLog(slog)
			if notifier, err = vaultgcpauth.VaultAuth(context.Background(),
				hcLog, vaultClient, environ.VaultAuthPath, pname); err != nil {
				slog.Fatalf("Vault login error: %s", err)
			}
			slog.Info(checkMark + "Authenticated to Vault")
		} else {
			slog.Info("Authenticating to Vault with existing token")
		}
	}

	applianceDB, vdbc := makeApplianceDB(environ.PostgresConnection, vaultClient, notifier)
	grpcServer := makeGrpcServer(applianceDB)

	slog.Infof("Attempting to get token source from Vault: path=%s role=%s",
		environ.VaultGCPPath, environ.VaultGCPRole)
	vts, err := vaulttokensource.NewVaultTokenSource(
		vaultClient, environ.VaultGCPPath, environ.VaultGCPRole)
	if err != nil {
		slog.Warnf("Failed to get access token from Vault; falling "+
			"back to ADC: %v", err)
		vts = nil
	}

	eventServer, err := newEventServer(vts, environ.PubsubTopic)
	if err != nil {
		slog.Fatalf("failed to start event server: %s", err)
	}

	cloudStorageServer := defaultCloudStorageServer(applianceDB, vts.Copy())
	certificateServer := newCertServer(applianceDB)
	relServer := newReleaseServer(applianceDB, vts.Copy())

	cloud_rpc.RegisterEventServer(grpcServer, eventServer)
	slog.Infof(checkMark+"Ready to put event to Cloud PubSub %s", environ.PubsubTopic)
	cloud_rpc.RegisterCloudStorageServer(grpcServer, cloudStorageServer)
	slog.Infof(checkMark + "Ready to serve Cloud Storage related requests")
	cloud_rpc.RegisterCertificateManagerServer(grpcServer, certificateServer)
	slog.Infof(checkMark + "Ready to serve certificate requests")
	cloud_rpc.RegisterReleaseManagerServer(grpcServer, relServer)
	slog.Infof(checkMark + "Ready to serve release requests")
	cloud_rpc.RegisterVPNManagerServer(grpcServer, &vpnServer{vaultClient})
	slog.Infof(checkMark + "Ready to escrow appliance VPN private keys")

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

	rHTTP := makeHTTPServer(vdbc, log)
	httpSrv := &http.Server{
		Addr: environ.HTTPListen,
	}
	go func() {
		if err := rHTTP.StartServer(httpSrv); err != nil {
			rHTTP.Logger.Info("shutting down HTTP (health) service: %v", err)
		}
	}()

	sig := make(chan os.Signal, 2)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	s := <-sig
	slog.Infof("Signal (%v) received, stopping", s)
	grpcServer.Stop()
}
