/*
 * Copyright 2019 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package grpcutils

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"time"

	"github.com/pkg/errors"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
)

func newConn(server, agent string, opts []grpc.DialOption) (*grpc.ClientConn, error) {
	var err error

	kopts := keepalive.ClientParameters{Time: time.Minute}
	opts = append(opts, grpc.WithKeepaliveParams(kopts))
	opts = append(opts, grpc.WithUserAgent(agent))

	conn, err := grpc.Dial(server, opts...)
	if err != nil {
		return nil, errors.Wrapf(err, "grpc Dial() to '%s' failed", server)
	}
	return conn, nil
}

// NewClientTLSConn will create a new Cloud Appliance gRPC client using TLS.
func NewClientTLSConn(serverAddr, certHost, agent, keyLogFile string) (*grpc.ClientConn, error) {
	var opts []grpc.DialOption

	cp, err := x509.SystemCertPool()
	if err != nil {
		return nil, fmt.Errorf("no system certificate pool: %v", err)
	}

	tc := tls.Config{
		RootCAs: cp,
	}
	if keyLogFile == "" {
		keyLogFile = os.Getenv("SSLKEYLOGFILE")
	}
	if keyLogFile != "" {
		w, err := os.Create(keyLogFile)
		if err == nil {
			tc.KeyLogWriter = w
		}
	}

	ctls := credentials.NewTLS(&tc)
	if certHost != "" {
		ctls.OverrideServerName(certHost)
	}
	opts = append(opts, grpc.WithTransportCredentials(ctls))

	return newConn(serverAddr, agent, opts)
}

// NewClientConn will create a new insecure connection to a Cloud Appliance gRPC client.
func NewClientConn(serverAddr, agent string) (*grpc.ClientConn, error) {

	opts := []grpc.DialOption{grpc.WithInsecure()}
	return newConn(serverAddr, agent, opts)
}

