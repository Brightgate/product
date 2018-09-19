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
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// GrpcServer encapsulates the logging, encryption, and network state needed to
// maintain a grpc endpoint
type GrpcServer struct {
	Server       *grpc.Server
	port         string
	certHostName string
	disableTLS   bool
}

func loadGrpcCerts(s *GrpcServer) grpc.ServerOption {
	slog.Infof("TLS Mode: Secured by TLS")
	if s.certHostName == "" {
		slog.Fatalf("B10E_CERT_HOSTNAME must be defined")
	}
	// Port 443 listener.
	certf := fmt.Sprintf("/etc/letsencrypt/live/%s/fullchain.pem",
		s.certHostName)
	keyf := fmt.Sprintf("/etc/letsencrypt/live/%s/privkey.pem",
		s.certHostName)

	certb, err := ioutil.ReadFile(certf)
	if err != nil {
		slog.Fatalw("read cert file failed", "err", err)
	}
	keyb, err := ioutil.ReadFile(keyf)
	if err != nil {
		slog.Fatalw("read key file failed", "err", err)
	}

	keypair, err := tls.X509KeyPair(certb, keyb)
	if err != nil {
		slog.Fatalw("generate X509 key pair failed", "err", err)
	}

	serverCertPool := x509.NewCertPool()
	if ok := serverCertPool.AppendCertsFromPEM(certb); !ok {
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

	return grpc.Creds(credentials.NewTLS(&tlsc))
}

// newGrpcServer establishes the state needed to start a grpc server.
func newGrpcServer(certHostName string, disableTLS bool, port string) *GrpcServer {

	var opts []grpc.ServerOption

	s := &GrpcServer{
		port:         port,
		certHostName: certHostName,
		disableTLS:   disableTLS,
	}

	if disableTLS {
		slog.Warnf("TLS Mode: local, NO TLS!  For developers only.")
	} else {
		opts = append(opts, loadGrpcCerts(s))
	}

	s.Server = grpc.NewServer(opts...)

	return s
}

// start will open an incoming connection on the server's TCP port, and launch a
// go routine to consume and handle messages arriving on that port
func (s *GrpcServer) start() {
	grpcConn, err := net.Listen("tcp", s.port)
	if err != nil {
		slog.Fatalf("Could not open gRPC listen socket: %v", err)
	}
	go func() {
		serr := s.Server.Serve(grpcConn)
		if serr == nil {
			slog.Infof("gRPC Server stopped.")
			return
		}
		slog.Fatalf("gRPC Server failed: %v", err)
	}()
	slog.Infof("Started gRPC service at %v", s.port)
}

// stop will shut down a running server
func (s *GrpcServer) stop() {
	s.Server.Stop()
}
