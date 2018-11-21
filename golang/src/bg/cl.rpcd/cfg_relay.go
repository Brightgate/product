/*
 * COPYRIGHT 2018 Brightgate Inc. All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"time"

	rpc "bg/cloud_rpc"

	"github.com/grpc-ecosystem/go-grpc-middleware/util/metautils"
	"github.com/pkg/errors"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// grpc endpoint for calls coming in from appliances
type applianceEndpoint struct {
	configdURL string
	timeout    time.Duration
	tls        bool
}

type configdEndpoint struct {
	cloudUUID string
	url       string
	tls       bool
	conn      *grpc.ClientConn
	client    rpc.ConfigBackEndClient
}

var (
	configdEndpoints = make(map[string]*configdEndpoint)
)

func defaultConfigServer(url, timeout string, disableTLS bool) *applianceEndpoint {
	t := 5 * time.Second
	if timeout != "" {
		opt, err := time.ParseDuration(timeout)
		if err == nil {
			t = opt
		} else {
			slog.Errorf("bad rpc timeout '%s': %v", timeout, err)
		}
	}

	return &applianceEndpoint{
		configdURL: url,
		timeout:    t,
		tls:        !disableTLS,
	}
}

func (c *configdEndpoint) Connect() error {
	var opts []grpc.DialOption

	slog.Debugf("Connecting to '%s'", c.url)
	if c.tls {
		cp, nocperr := x509.SystemCertPool()
		if nocperr != nil {
			return fmt.Errorf("no system certificate pool: %v",
				nocperr)
		}

		tc := tls.Config{
			RootCAs: cp,
		}

		ctls := credentials.NewTLS(&tc)
		opts = append(opts, grpc.WithTransportCredentials(ctls))
	} else {
		opts = append(opts, grpc.WithInsecure())
	}

	opts = append(opts, grpc.WithUserAgent(pname))

	conn, err := grpc.Dial(c.url, opts...)
	if err != nil {
		return errors.Wrapf(err, "grpc Dial() to '%s' failed", c.url)
	}

	c.conn = conn
	c.client = rpc.NewConfigBackEndClient(conn)
	return nil
}

func (c *configdEndpoint) Disconnect() {
	if c.conn != nil {
		c.conn.Close()
	}
	c.conn = nil
	c.client = nil
}

// Find or establish a gRPC connection to the cl.configd supporting this
// appliance
func (s *applianceEndpoint) getConfigdConn(ctx context.Context) (*configdEndpoint, error) {

	uuid := metautils.ExtractIncoming(ctx).Get("clouduuid")
	if uuid == "" {
		return nil, fmt.Errorf("missing cloud UUID")
	}

	if conn := configdEndpoints[uuid]; conn != nil {
		return conn, nil
	}

	conn := &configdEndpoint{
		// XXX: use uuid to lookup configd URL in the database
		cloudUUID: uuid,
		url:       s.configdURL,
		tls:       s.tls,
	}

	if err := conn.Connect(); err != nil {
		return nil, fmt.Errorf("connecting to configd: %v", err)
	}

	configdEndpoints[uuid] = conn
	return conn, nil
}

// Relay a backend gRPC to the correct cl.configd for the appliance that sent it
func (s *applianceEndpoint) relay(ctx context.Context, cmd interface{}) *rpc.CfgBackEndResponse {

	var rval *rpc.CfgBackEndResponse

	conn, err := s.getConfigdConn(ctx)
	if err == nil {
		deadline := time.Now().Add(s.timeout)
		ctx, ctxcancel := context.WithDeadline(ctx, deadline)
		defer ctxcancel()

		switch req := cmd.(type) {
		case *rpc.CfgBackEndHello:
			req.CloudUuid = conn.cloudUUID
			rval, err = conn.client.Hello(ctx, req)
		case *rpc.CfgBackEndDownload:
			req.CloudUuid = conn.cloudUUID
			rval, err = conn.client.Download(ctx, req)
		case *rpc.CfgBackEndUpdate:
			req.CloudUuid = conn.cloudUUID
			rval, err = conn.client.Update(ctx, req)
		case *rpc.CfgBackEndFetchCmds:
			req.CloudUuid = conn.cloudUUID
			rval, err = conn.client.FetchCmds(ctx, req)
		case *rpc.CfgBackEndCompletions:
			req.CloudUuid = conn.cloudUUID
			rval, err = conn.client.CompleteCmds(ctx, req)
		default:
			err = fmt.Errorf("unrecognized configd command")
		}
	}

	if err != nil {
		// We want to be sure the error message conveys how far the rpc
		// got before failing.  This tag lets us know that the failure
		// happened sometime after being successfully received, parsed,
		// and handled by cl.rpcd.
		rval = &rpc.CfgBackEndResponse{
			Response: rpc.CfgBackEndResponse_ERROR,
			Errmsg:   fmt.Sprintf("relay failed: %v", err),
		}
	}
	return rval
}

func (s *applianceEndpoint) Hello(ctx context.Context,
	req *rpc.CfgBackEndHello) (*rpc.CfgBackEndResponse, error) {

	return s.relay(ctx, req), nil
}

func (s *applianceEndpoint) Download(ctx context.Context,
	req *rpc.CfgBackEndDownload) (*rpc.CfgBackEndResponse, error) {

	return s.relay(ctx, req), nil
}

func (s *applianceEndpoint) Update(ctx context.Context,
	req *rpc.CfgBackEndUpdate) (*rpc.CfgBackEndResponse, error) {

	return s.relay(ctx, req), nil
}

func (s *applianceEndpoint) FetchCmds(ctx context.Context,
	req *rpc.CfgBackEndFetchCmds) (*rpc.CfgBackEndResponse, error) {

	return s.relay(ctx, req), nil
}

func (s *applianceEndpoint) CompleteCmds(ctx context.Context,
	req *rpc.CfgBackEndCompletions) (*rpc.CfgBackEndResponse, error) {

	return s.relay(ctx, req), nil
}

func (s *applianceEndpoint) FetchStream(req *rpc.CfgBackEndFetchCmds,
	stream rpc.ConfigBackEnd_FetchStreamServer) error {

	slog.Infof("starting stream relay")
	ctx := stream.Context()
	conn, err := s.getConfigdConn(ctx)
	req.CloudUuid = conn.cloudUUID

	relayStream, err := conn.client.FetchStream(ctx, req)
	if err != nil {
		slog.Errorf("failed to establish FetchStream to cl.config: %v",
			err)
		return err
	}

	for err == nil {
		var resp *rpc.CfgBackEndResponse

		slog.Debugf("waiting for commands from cl.config")

		if resp, err = relayStream.Recv(); err != nil {
			if ctx.Err() == context.Canceled {
				slog.Infof("client %s disconnected",
					conn.cloudUUID)
				err = nil
				break
			}

			slog.Errorf("FetchStream.Recv() failed: %v", err)
			resp = &rpc.CfgBackEndResponse{
				Response: rpc.CfgBackEndResponse_ERROR,
				Errmsg:   fmt.Sprintf("%v", err),
			}
		}

		if serr := stream.Send(resp); serr != nil {
			if serr != io.EOF {
				slog.Errorf("FetchStream.Send() failed: %v", err)
			}
			err = serr
		}
	}

	return err
}
