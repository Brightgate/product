/*
 * Copyright 2020 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package main

import (
	"context"
	"fmt"

	"bg/cl_common/daemonutils"
	"bg/cloud_rpc"

	vault "github.com/hashicorp/vault/api"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type vpnServer struct {
	vaultClient *vault.Client
}

func (vs *vpnServer) EscrowVPNPrivateKey(ctx context.Context, req *cloud_rpc.VPNPrivateKey) (*cloud_rpc.VPNEscrowResponse, error) {
	_, slog := daemonutils.EndpointLogger(ctx)

	siteUU, err := getSiteUUID(ctx, false)
	if err != nil {
		slog.Errorw("Failed to process VPN server private key escrow request",
			"error", err)
		return nil, err
	}

	vcl := vs.vaultClient.Logical()
	data := map[string]interface{}{
		"data": map[string]interface{}{
			"private_key": req.Key,
		},
	}
	path := fmt.Sprintf("%s/data/%s/%s", environ.VaultVPNEscrowPath,
		environ.VaultVPNEscrowComponent, siteUU.String())
	_, err = vcl.Write(path, data)
	if err != nil {
		slog.Errorw("Vault write error", "error", err)
		return nil, status.Errorf(codes.Internal, "Vault write error: %s", err)
	}

	slog.Infow("Escrowed appliance VPN private key", "path", path)
	return &cloud_rpc.VPNEscrowResponse{}, nil
}

