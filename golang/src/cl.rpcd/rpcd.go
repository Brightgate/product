/*
 * COPYRIGHT 2017 Brightgate Inc.  All rights reserved.
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
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"cloud_rpc"

	"github.com/golang/protobuf/proto"
	"github.com/tomazk/envcfg"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
)

type Cfg struct {
	// The certificate hostname is the primary hostname associated
	// with the SSL certificate, and not necessarily the nodename.
	// We use this variable to navigate the Let's Encrypt directory
	// hierarchy.
	B10E_CERT_HOSTNAME          string
	B10E_CLRPCD_PROMETHEUS_PORT string
	B10E_LOCAL_MODE             bool
}

type applianceInfo struct {
	component_version []string
	last_contact      time.Time
	net_host_count    int32
	uptime            int64
	wan_hwaddr        []string
	wan_ipv4addr      string
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
	invalid_upcalls = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "upcall_invalids",
		Help: "GRPC upcall invalid HMAC attempts",
	})
)

type inventoryServer struct{}

func (i *inventoryServer) Upcall(ctx context.Context, req *cloud_rpc.InventoryReport) (*cloud_rpc.UpcallResponse, error) {
	// HMAC
	lt := time.Now()
	year := lt.Year()

	rhmac := hmac.New(sha256.New, cloud_rpc.HMACKeys[year])
	rhmac.Write([]byte(req.Devices.String()))
	expectedHMAC := rhmac.Sum(nil)

	if !hmac.Equal(req.HMAC, expectedHMAC) {
		invalid_upcalls.Inc()
		return nil, grpc.Errorf(codes.Unauthenticated, "valid hmac required")
	}

	log.Printf("received inventory from %v (%v)\n", req.Uuid, req.WanHwaddr)
	dirPath := fmt.Sprintf("%s/var/spool/%s/", *clroot, req.Uuid)
	if err := os.MkdirAll(dirPath, 0755); err != nil {
		return nil, grpc.Errorf(codes.FailedPrecondition, "mkdir failed")
	}

	path := fmt.Sprintf("%s/observations.%d.pb", dirPath, int(time.Now().Unix()))
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return nil, grpc.Errorf(codes.FailedPrecondition, "open failed")
	}
	defer f.Close()

	out, err := proto.Marshal(req.Devices)
	if err != nil {
		return nil, grpc.Errorf(codes.FailedPrecondition, "marshal failed")
	}

	if _, err := f.Write(out); err != nil {
		return nil, grpc.Errorf(codes.FailedPrecondition, "write failed")
	}

	// Formulate a response.
	res := cloud_rpc.UpcallResponse{
		UpcallElapsed:   -1,
		DowncallElapsed: -1,
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
	year := lt.Year()

	log.Println(ctx, req)

	rhmac := hmac.New(sha256.New, cloud_rpc.HMACKeys[year])
	data := fmt.Sprintf("%v %v", req.WanHwaddr, req.UptimeElapsed)
	rhmac.Write([]byte(data))
	expectedHMAC := rhmac.Sum(nil)

	if !hmac.Equal(req.HMAC, expectedHMAC) {
		// Discard invalid HMAC messages!
		invalid_upcalls.Inc()
		return nil, grpc.Errorf(codes.Unauthenticated, "valid hmac required")
	}

	log.Printf("hwaddr %v uuid %v version %v uptime %v\n",
		req.WanHwaddr, req.Uuid, req.ComponentVersion, req.UptimeElapsed)

	peer, ok := peer.FromContext(ctx)
	if !ok {
		log.Printf("no peer available in %v\n", ctx)
	} else {
		log.Printf("peer %v\n", peer.Addr)
	}

	// What do we do with a request?
	// Turn it into an appliance info.
	ai := applianceInfo{
		component_version: req.ComponentVersion,
		last_contact:      time.Now(),
		net_host_count:    0,
		uptime:            req.UptimeElapsed,
		wan_hwaddr:        req.WanHwaddr,
		wan_ipv4addr:      peer.Addr.String(),
	}

	// Update our tables.
	log.Printf("len hwaddr %v\n", len(req.WanHwaddr))

	new_system := false
	new_software_install := false

	// req.Uuid not in s.uuids[] --> new system
	if _, ok := s.uuids[req.Uuid]; ok {
		log.Printf("uuid is known\n")
	} else {
		log.Printf("uuid %v is a new system\n", req.Uuid)
		new_system = true
	}

	// req.WanHwaddr not in s.macs[] --> new system
	for _, hwaddr := range req.WanHwaddr {
		if _, ok := s.macs[hwaddr]; ok {
			// req.WanHwaddr not the same Uuid --> new
			// software install
			if s.macs[hwaddr] != req.Uuid {
				// New installation?
				log.Printf("WanHwaddr not equal to Uuid, new software install")
				new_software_install = true
			}
		} else {
			new_system = true
		}
	}

	if new_system {
		log.Printf("recording uuid %v\n", req.Uuid)

		// Record it!
		s.uuids[req.Uuid] = ai
	}

	if new_system || new_software_install {
		for _, hwaddr := range req.WanHwaddr {
			log.Printf("recording hwaddr %v\n", hwaddr)
			s.macs[hwaddr] = req.Uuid
		}
	}

	latencies.Observe(time.Since(lt).Seconds())

	// Formulate a response.
	res := cloud_rpc.UpcallResponse{
		UpcallElapsed:   -1,
		DowncallElapsed: -1,
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
	prometheus.MustRegister(invalid_upcalls)
}

func main() {
	var environ Cfg
	var opts []grpc.ServerOption
	var keypair tls.Certificate
	var serverCertPool *x509.CertPool

	flag.Parse()
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	envcfg.Unmarshal(&environ)

	if len(environ.B10E_CLRPCD_PROMETHEUS_PORT) != 0 {
		http.Handle("/metrics", promhttp.Handler())
		go http.ListenAndServe(environ.B10E_CLRPCD_PROMETHEUS_PORT, nil)
		log.Println("prometheus client launched")
	}

	// It is bad if B10E_CERT_HOSTNAME is not defined.
	// It is unfortunate if B10E_CERT_HOSTNAME is not defined.
	log.Printf("environ %v", environ)

	// Port 443 listener.
	certf := fmt.Sprintf("/etc/letsencrypt/live/%s/fullchain.pem",
		environ.B10E_CERT_HOSTNAME)
	keyf := fmt.Sprintf("/etc/letsencrypt/live/%s/privkey.pem",
		environ.B10E_CERT_HOSTNAME)

	grpc_port := ":4430"

	log.Println("local mode", environ.B10E_LOCAL_MODE)

	if !environ.B10E_LOCAL_MODE {
		certb, err := ioutil.ReadFile(certf)
		if err != nil {
			log.Printf("read cert file failed: %v\n", err)
		}
		keyb, err := ioutil.ReadFile(keyf)
		if err != nil {
			log.Printf("read key file failed: %v\n", err)
		}

		keypair, err = tls.X509KeyPair(certb, keyb)
		if err != nil {
			log.Printf("generate X509 key pair failed: %v\n", err)
		}

		serverCertPool = x509.NewCertPool()

		ok := serverCertPool.AppendCertsFromPEM(certb)
		if !ok {
			panic("bad certs")
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

	grpcServer := grpc.NewServer(opts...)

	ubServer := newUpbeatServer()
	cloud_rpc.RegisterUpbeatServer(grpcServer, ubServer)

	invServer := newInventoryServer()
	cloud_rpc.RegisterInventoryServer(grpcServer, invServer)

	grpc_conn, err := net.Listen("tcp", grpc_port)
	if err != nil {
		panic(err)
	}

	go grpcServer.Serve(grpc_conn)

	fleetMux := http.NewServeMux()

	fleetMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")

		fmt.Fprintf(w, "%-36v %-17v %-39v %v\n", "UUID", "MAC", "LAST", "VERSION")
		for uu, appinfo := range ubServer.uuids {
			fmt.Fprintf(w, "%36v %17v %-39v %v\n", uu,
				appinfo.wan_hwaddr[0], appinfo.last_contact,
				appinfo.component_version[0])
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
	log.Fatalf("Signal (%v) received, stopping\n", s)
}
