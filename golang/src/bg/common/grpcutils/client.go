/*
 * COPYRIGHT 2018 Brightgate Inc. All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package grpcutils

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"

	"github.com/pkg/errors"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// NewClientConn will create a new Cloud Appliance gRPC client.
func NewClientConn(serverAddr string, enableTLS bool, agent string) (*grpc.ClientConn, error) {
	var err error

	var opts []grpc.DialOption

	if enableTLS {
		cp, nocperr := x509.SystemCertPool()
		if nocperr != nil {
			return nil, fmt.Errorf("no system certificate pool: %v", nocperr)
		}

		tc := tls.Config{
			RootCAs: cp,
		}

		ctls := credentials.NewTLS(&tc)
		opts = append(opts, grpc.WithTransportCredentials(ctls))
	} else {
		opts = append(opts, grpc.WithInsecure())
	}

	opts = append(opts, grpc.WithUserAgent(agent))

	conn, err := grpc.Dial(serverAddr, opts...)
	if err != nil {
		return nil, errors.Wrapf(err, "grpc Dial() to '%s' failed", serverAddr, err)
	}
	return conn, nil
}
