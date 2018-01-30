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
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"bg/ap_common/network"
	"bg/base_def"
	"bg/base_msg"
	"bg/cl_common/daemonutils"
	"bg/cloud_rpc"

	"github.com/golang/protobuf/proto"
	"github.com/tomazk/envcfg"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"go.uber.org/zap"

	"golang.org/x/net/context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
)

// Cfg contains the environment variable-based configuration settings
type Cfg struct {
	// The certificate hostname is the primary hostname associated
	// with the SSL certificate, and not necessarily the nodename.
	// We use this variable to navigate the Let's Encrypt directory
	// hierarchy.
	CertHostname   string `envcfg:"B10E_CERT_HOSTNAME"`
	PrometheusPort string `envcfg:"B10E_CLRPCD_PROMETHEUS_PORT"`
	LocalMode      bool   `envcfg:"LOCAL_MODE"`
}

type applianceInfo struct {
	componentVersion []string
	lastContact      time.Time
	netHostCount     int32
	uptime           int64
	wanHwaddr        []string
	wanIpv4addr      string
}

const (
	pname = "cl.rpcd"
)

var (
	clroot = flag.String("root", "proto.x86_64/cloud/opt/net.b10e",
		"Root of cloud installation")

	environ Cfg

	serverKeyPair  *tls.Certificate
	serverCertPool *x509.CertPool
	serverAddr     string

	latencies = prometheus.NewSummary(prometheus.SummaryOpts{
		Name: "upcall_seconds",
		Help: "GRPC upcall time",
	})
	invalidUpcalls = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "upcall_invalids",
		Help: "GRPC upcall invalid HMAC attempts",
	})
)

func validhmac(received []byte, data string) bool {
	year := time.Now().Year()
	rhmac := hmac.New(sha256.New, cloud_rpc.HMACKeys[year])
	rhmac.Write([]byte(data))
	expectedHMAC := rhmac.Sum(nil)
	return hmac.Equal(received, expectedHMAC)
}

type inventoryServer struct{}

func writeInfo(devInfo *base_msg.DeviceInfo, basePath string) (string, error) {
	hwaddr := network.Uint64ToHWAddr(devInfo.GetMacAddress())
	path := filepath.Join(basePath, hwaddr.String())
	if err := os.MkdirAll(path, 0755); err != nil {
		return "", grpc.Errorf(codes.FailedPrecondition, "mkdir failed")
	}

	filename := fmt.Sprintf("device_info.%d.pb", int(time.Now().Unix()))
	path = filepath.Join(path, filename)
	f, err := os.OpenFile(
		path,
		os.O_WRONLY|os.O_CREATE|os.O_TRUNC,
		0644)
	if err != nil {
		return "", grpc.Errorf(codes.FailedPrecondition, "open failed")
	}
	defer f.Close()

	out, err := proto.Marshal(devInfo)
	if err != nil {
		os.Remove(path)
		return "", grpc.Errorf(codes.FailedPrecondition, "marshal failed")
	}

	if _, err := f.Write(out); err != nil {
		os.Remove(path)
		return "", grpc.Errorf(codes.FailedPrecondition, "write failed")
	}
	return path, nil
}

func loggerFromCtx(ctx context.Context) (*zap.Logger, *zap.SugaredLogger) {
	log, _ := daemonutils.GetLogs()
	if ctx != nil {
		pr, ok := peer.FromContext(ctx)
		if ok && pr != nil {
			log = log.With(zap.String("peer", pr.Addr.String()))
		}
	}
	return log, log.Sugar()
}

func (i *inventoryServer) Upcall(ctx context.Context, req *cloud_rpc.InventoryReport) (*cloud_rpc.UpcallResponse, error) {
	lt := time.Now()
	_, slog := loggerFromCtx(ctx)

	if req.HMAC == nil || req.Uuid == nil {
		invalidUpcalls.Inc()
		return nil, grpc.Errorf(codes.InvalidArgument, "req missing needed parameters")
	}

	if !validhmac(req.GetHMAC(), req.Inventory.String()) {
		invalidUpcalls.Inc()
		return nil, grpc.Errorf(codes.Unauthenticated, "valid hmac required")
	}

	slog.Infow("incoming inventory", "uuid", req.GetUuid(), "WanHwAddr", req.GetWanHwaddr())

	// We receive only what has recently changed
	basePath := filepath.Join(*clroot, "var", "spool", req.GetUuid())
	for _, devInfo := range req.Inventory.Devices {
		path, err := writeInfo(devInfo, basePath)
		if err != nil {
			return nil, err
		}
		slog.Infow("wrote report", "path", path)
	}

	// Formulate a response.
	res := cloud_rpc.UpcallResponse{
		UpcallElapsed: proto.Int64(time.Now().Sub(lt).Nanoseconds()),
	}

	return &res, nil
}

func newInventoryServer() *inventoryServer {
	ret := &inventoryServer{}
	return ret
}

type upbeatServer struct {
	// map of MACs to UUIDs
	macs map[string]string
	// map of UUIDs to appliance info
	uuids map[string]applianceInfo
}

func (s *upbeatServer) Init() {
	s.macs = make(map[string]string)
	s.uuids = make(map[string]applianceInfo)
}

func (s *upbeatServer) Upcall(ctx context.Context, req *cloud_rpc.UpcallRequest) (*cloud_rpc.UpcallResponse, error) {
	// Prometheus metric: upcall latency.
	lt := time.Now()
	_, slog := loggerFromCtx(ctx)
	slog.Info("UpcallRequest: ", req.String())

	if req.HMAC == nil || req.WanHwaddr == nil ||
		req.UptimeElapsed == nil || req.Uuid == nil {
		invalidUpcalls.Inc()
		return nil, grpc.Errorf(codes.InvalidArgument, "req missing parameters")
	}

	slog.Infof("hwaddr %v uuid %s version %v uptime %d\n",
		req.GetWanHwaddr(), req.GetUuid(), req.GetComponentVersion(),
		req.GetUptimeElapsed())

	data := fmt.Sprintf("%x %d", req.GetWanHwaddr(), req.GetUptimeElapsed())
	if !validhmac(req.GetHMAC(), data) {
		// Discard invalid HMAC messages!
		invalidUpcalls.Inc()
		return nil, grpc.Errorf(codes.Unauthenticated, "valid hmac required")
	}

	peer, ok := peer.FromContext(ctx)
	if !ok {
		return nil, fmt.Errorf("no peer available in %v", ctx)
	}

	// What do we do with a request?
	// Turn it into an appliance info.
	ai := applianceInfo{
		componentVersion: req.ComponentVersion,
		lastContact:      time.Now(),
		netHostCount:     0,
		uptime:           req.GetUptimeElapsed(),
		wanHwaddr:        req.GetWanHwaddr(),
		wanIpv4addr:      peer.Addr.String(),
	}

	// Update our tables.
	slog.Infof("len hwaddr %v\n", len(req.GetWanHwaddr()))

	newSystem := false
	newSoftwareInstall := false

	// req.Uuid not in s.uuids[] --> new system
	if _, ok := s.uuids[req.GetUuid()]; ok {
		slog.Info("uuid is known")
	} else {
		slog.Infof("uuid %s is a new system", req.GetUuid())
		newSystem = true
	}

	// req.WanHwaddr not in s.macs[] --> new system
	for _, hwaddr := range req.GetWanHwaddr() {
		if _, ok := s.macs[hwaddr]; ok {
			// req.WanHwaddr not the same Uuid --> new
			// software install
			if s.macs[hwaddr] != req.GetUuid() {
				// New installation?
				slog.Info("WanHwaddr not equal to Uuid, new software install")
				newSoftwareInstall = true
			}
		} else {
			newSystem = true
		}
	}

	if newSystem {
		slog.Infof("recording uuid %s", req.GetUuid())

		// Record it!
		s.uuids[req.GetUuid()] = ai
	}

	if newSystem || newSoftwareInstall {
		for _, hwaddr := range req.WanHwaddr {
			slog.Infof("recording hwaddr %s", hwaddr)
			s.macs[hwaddr] = req.GetUuid()
		}
	}

	latencies.Observe(time.Since(lt).Seconds())

	// Formulate a response.
	res := cloud_rpc.UpcallResponse{
		UpcallElapsed: proto.Int64(time.Now().Sub(lt).Nanoseconds()),
	}

	return &res, nil
}

func newUpbeatServer() *upbeatServer {
	u := new(upbeatServer)
	u.Init()
	return u
}

func init() {
	prometheus.MustRegister(latencies)
	prometheus.MustRegister(invalidUpcalls)
}

func main() {
	var environ Cfg
	var opts []grpc.ServerOption
	var keypair tls.Certificate
	var serverCertPool *x509.CertPool
	var err error

	log, slog := daemonutils.SetupLogs()
	defer log.Sync()

	flag.Parse()
	err = envcfg.Unmarshal(&environ)
	if err != nil {
		slog.Fatalf("Environment Error: %s", err)
	}
	log, slog = daemonutils.ResetupLogs()

	slog.Infow(pname+" starting", "args", os.Args, "envcfg", environ)

	if len(environ.PrometheusPort) != 0 {
		http.Handle("/metrics", promhttp.Handler())
		go http.ListenAndServe(environ.PrometheusPort, nil)
		slog.Info("prometheus client launched")
	}

	// It is bad if B10E_CERT_HOSTNAME is not defined.
	// It is unfortunate if B10E_CERT_HOSTNAME is not defined.

	// Port 443 listener.
	certf := fmt.Sprintf("/etc/letsencrypt/live/%s/fullchain.pem",
		environ.CertHostname)
	keyf := fmt.Sprintf("/etc/letsencrypt/live/%s/privkey.pem",
		environ.CertHostname)

	grpcPort := base_def.CLRPCD_GRPC_PORT

	if environ.LocalMode {
		slog.Info("local mode")
	} else {
		slog.Info("secure remote mode")
		certb, err := ioutil.ReadFile(certf)
		if err != nil {
			slog.Warnw("read cert file failed", "err", err)
		}
		keyb, err := ioutil.ReadFile(keyf)
		if err != nil {
			slog.Warnw("read key file failed", "err", err)
		}

		keypair, err = tls.X509KeyPair(certb, keyb)
		if err != nil {
			slog.Warnw("generate X509 key pair failed", "err", err)
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
				tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA,
				tls.TLS_RSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_RSA_WITH_AES_256_CBC_SHA,
			},
		}

		opts = append(opts, grpc.Creds(credentials.NewTLS(&tlsc)))
	}

	// XXX RPCCompressor() and RPCDecompressor() will be deprecated in the next
	// grpc release. Use UseCompressor() instead.
	opts = append(opts,
		grpc.RPCCompressor(grpc.NewGZIPCompressor()),
		grpc.RPCDecompressor(grpc.NewGZIPDecompressor()))

	grpcServer := grpc.NewServer(opts...)

	ubServer := newUpbeatServer()
	cloud_rpc.RegisterUpbeatServer(grpcServer, ubServer)

	invServer := newInventoryServer()
	cloud_rpc.RegisterInventoryServer(grpcServer, invServer)

	grpcConn, err := net.Listen("tcp", grpcPort)
	if err != nil {
		panic(err)
	}

	go grpcServer.Serve(grpcConn)

	fleetMux := http.NewServeMux()

	fleetMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")

		fmt.Fprintf(w, "%-36v %-17v %-39v %v\n", "UUID", "MAC", "LAST", "VERSION")
		for uu, appinfo := range ubServer.uuids {
			fmt.Fprintf(w, "%36v %17v %-39v %v\n", uu,
				appinfo.wanHwaddr[0], appinfo.lastContact,
				appinfo.componentVersion[0])
		}
	})

	fleetServer := &http.Server{
		Addr:    ":7000",
		Handler: fleetMux,
	}

	go fleetServer.ListenAndServe()

	sig := make(chan os.Signal)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	s := <-sig
	slog.Infof("Signal (%v) received, stopping", s)
}
