/*
 * COPYRIGHT 2019 Brightgate Inc. All rights reserved.
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

	"bg/cl_common/daemonutils"
	rpc "bg/cloud_rpc"

	"github.com/pkg/errors"
	"github.com/satori/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
)

// grpc endpoint for site-scoped config calls coming in from appliances
type siteEndpoint struct {
	configdURL string
	timeout    time.Duration
	tls        bool
}

type configdEndpoint struct {
	siteUUID uuid.UUID
	url      string
	tls      bool
	conn     *grpc.ClientConn
	client   rpc.ConfigBackEndClient
}

var (
	configdEndpoints = make(map[uuid.UUID]*configdEndpoint)
)

func defaultConfigServer(url, timeout string, disableTLS bool) *siteEndpoint {
	t := 5 * time.Second
	if timeout != "" {
		opt, err := time.ParseDuration(timeout)
		if err == nil {
			t = opt
		} else {
			slog.Errorf("bad rpc timeout '%s': %v", timeout, err)
		}
	}

	return &siteEndpoint{
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

// Create an outgoing context with the appliance and site UUIDs based on those
// values from the incoming context.
func relayContext(ctx context.Context) (context.Context, error) {
	siteUUID, err := getSiteUUID(ctx, false)
	if err != nil {
		return nil, err
	}
	applianceUUID, err := getApplianceUUID(ctx, false)
	if err != nil {
		return nil, err
	}

	ctx = metadata.AppendToOutgoingContext(ctx,
		"appliance_uuid", applianceUUID.String(),
		"site_uuid", siteUUID.String())
	return ctx, nil
}

// Find or establish a gRPC connection to the cl.configd supporting the
// appliance's site.
func (s *siteEndpoint) getConfigdConn(ctx context.Context) (*configdEndpoint, error) {
	siteUUID, err := getSiteUUID(ctx, false)
	if err != nil {
		return nil, err
	}

	if conn := configdEndpoints[siteUUID]; conn != nil {
		return conn, nil
	}

	conn := &configdEndpoint{
		// XXX: use uuid to lookup configd URL in the database
		siteUUID: siteUUID,
		url:      s.configdURL,
		tls:      s.tls,
	}

	if err := conn.Connect(); err != nil {
		return nil, fmt.Errorf("connecting to configd: %v", err)
	}

	configdEndpoints[siteUUID] = conn
	return conn, nil
}

// Relay a backend gRPC to the correct cl.configd for the site that sent it
func (s *siteEndpoint) relay(ctx context.Context, cmd interface{}) (*rpc.CfgBackEndResponse, error) {
	_, slog := daemonutils.EndpointLogger(ctx)

	slog.Debugw("incoming configd relay request", "cmd", cmd, "type", fmt.Sprintf("%T", cmd))

	ctx, err := relayContext(ctx)
	if err != nil {
		return nil, err
	}

	var rval *rpc.CfgBackEndResponse

	conn, err := s.getConfigdConn(ctx)
	if err == nil {
		deadline := time.Now().Add(s.timeout)
		ctx, ctxcancel := context.WithDeadline(ctx, deadline)
		defer ctxcancel()

		switch req := cmd.(type) {
		case *rpc.CfgBackEndHello:
			req.SiteUUID = conn.siteUUID.String()
			rval, err = conn.client.Hello(ctx, req)
		case *rpc.CfgBackEndDownload:
			req.SiteUUID = conn.siteUUID.String()
			rval, err = conn.client.Download(ctx, req)
		case *rpc.CfgBackEndUpdate:
			req.SiteUUID = conn.siteUUID.String()
			rval, err = conn.client.Update(ctx, req)
		case *rpc.CfgBackEndFetchCmds:
			req.SiteUUID = conn.siteUUID.String()
			rval, err = conn.client.FetchCmds(ctx, req)
		case *rpc.CfgBackEndCompletions:
			req.SiteUUID = conn.siteUUID.String()
			rval, err = conn.client.CompleteCmds(ctx, req)
		default:
			err = fmt.Errorf("unrecognized configd command")
			slog.Warnw(err.Error(),
				"cmd", cmd, "type", fmt.Sprintf("%T", cmd))
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
	return rval, nil
}

func (s *siteEndpoint) Hello(ctx context.Context,
	req *rpc.CfgBackEndHello) (*rpc.CfgBackEndResponse, error) {

	return s.relay(ctx, req)
}

func (s *siteEndpoint) Download(ctx context.Context,
	req *rpc.CfgBackEndDownload) (*rpc.CfgBackEndResponse, error) {

	return s.relay(ctx, req)
}

func (s *siteEndpoint) Update(ctx context.Context,
	req *rpc.CfgBackEndUpdate) (*rpc.CfgBackEndResponse, error) {

	return s.relay(ctx, req)
}

func (s *siteEndpoint) FetchCmds(ctx context.Context,
	req *rpc.CfgBackEndFetchCmds) (*rpc.CfgBackEndResponse, error) {

	return s.relay(ctx, req)
}

func (s *siteEndpoint) CompleteCmds(ctx context.Context,
	req *rpc.CfgBackEndCompletions) (*rpc.CfgBackEndResponse, error) {

	return s.relay(ctx, req)
}

func (s *siteEndpoint) FetchStream(req *rpc.CfgBackEndFetchCmds,
	stream rpc.ConfigBackEnd_FetchStreamServer) error {

	ctx := stream.Context()
	_, slog := daemonutils.EndpointLogger(ctx)

	ctx, err := relayContext(ctx)
	if err != nil {
		return err
	}

	slog.Infof("starting stream relay")

	conn, err := s.getConfigdConn(ctx)
	req.SiteUUID = conn.siteUUID.String()

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
					conn.siteUUID)
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
