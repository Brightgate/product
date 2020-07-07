/*
 * COPYRIGHT 2020 Brightgate Inc. All rights reserved.
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
	"io/ioutil"
	"sync"
	"time"

	"bg/ap_common/aputil"
	"bg/base_def"
	"bg/base_msg"
	"bg/cloud_rpc"
	"bg/common/cfgapi"
	"bg/common/wgsite"

	"github.com/golang/protobuf/proto"
	"github.com/pkg/errors"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
	"google.golang.org/grpc"
)

var vpnEscrowLock sync.Mutex

func grpcEscrowCall(ctx context.Context, conn *grpc.ClientConn, privateKey string) error {
	var err error
	ctx, err = applianceCred.MakeGRPCContext(ctx)
	if err != nil {
		return errors.WithMessage(err, "failed to make GRPC credential")
	}

	clientDeadline := time.Now().Add(*rpcDeadline)
	ctx, ctxcancel := context.WithDeadline(ctx, clientDeadline)
	defer ctxcancel()

	req := &cloud_rpc.VPNPrivateKey{Key: privateKey}

	client := cloud_rpc.NewVPNManagerClient(conn)

	_, err = client.EscrowVPNPrivateKey(ctx, req)
	if err != nil {
		return errors.WithMessage(err, "escrow gRPC call failed")
	}

	return nil
}

func vpnKeyMismatchError(txt string) {
	slog.Errorf("%s", txt)

	reason := base_msg.EventSysError_VPN_KEY_MISMATCH
	entity := &base_msg.EventSysError{
		Timestamp: aputil.NowToProtobuf(),
		Sender:    proto.String(brokerd.Name),
		Reason:    &reason,
		Message:   &txt,
	}
	err := brokerd.Publish(entity, base_def.TOPIC_ERROR)
	if err != nil {
		slog.Warnf("couldn't publish %s: %v", base_def.TOPIC_ERROR, err)
	}
}

func vpnCheckEscrow(ctx context.Context, conn *grpc.ClientConn) {
	vpnEscrowLock.Lock()
	defer vpnEscrowLock.Unlock()

	filename := plat.ExpandDirPath(wgsite.SecretDir, wgsite.PrivateFile)
	if !aputil.FileExists(filename) {
		slog.Infof("No VPN key configured")
		return
	}

	public, err := config.GetProp(wgsite.PublicProp)
	if err == cfgapi.ErrNoProp || len(public) == 0 {
		vpnKeyMismatchError("private key has an empty public key")
		return
	}
	if err != nil {
		slog.Warnf("failed to fetch %s: %v", wgsite.PublicProp, err)
		return
	}

	escrow, _ := config.GetProp(wgsite.EscrowedProp)
	if public == escrow {
		slog.Infof("current VPN private key already escrowed")
		return
	}

	slog.Infof("VPN private key needs to be escrowed")
	text, err := ioutil.ReadFile(filename)
	if err != nil {
		slog.Warnf("reading VPN private key from %s: %v",
			filename, err)
		return
	}
	private, err := wgtypes.ParseKey(string(text))
	if err != nil {
		slog.Infof("invalid private key: %v", err)
		return
	}

	verifyPublic := private.PublicKey().String()
	if public != verifyPublic {
		vpnKeyMismatchError("mismatch between the public and " +
			"private VPN keys")
		return
	}

	// This could try a few times?
	if err = grpcEscrowCall(ctx, conn, private.String()); err != nil {
		slog.Errorw("Failed to escrow VPN private key", "error", err)
		return
	}

	slog.Info("VPN private key escrowed in cloud")

	err = config.CreateProp(wgsite.EscrowedProp, public, nil)
	if err != nil {
		slog.Warnf("failed to update escrowed property: %v", err)
	}
}

func vpnInit(ctx context.Context, conn *grpc.ClientConn) {
	vpnHandleUpdate := func(path []string, val string, expires *time.Time) {
		vpnCheckEscrow(context.Background(), conn)
	}
	vpnHandleDelete := func(path []string) {
		vpnCheckEscrow(context.Background(), conn)
	}

	config.HandleChange(`^`+wgsite.PublicProp, vpnHandleUpdate)
	config.HandleChange(`^`+wgsite.EscrowedProp, vpnHandleUpdate)
	config.HandleDelExp(`^`+wgsite.PublicProp, vpnHandleDelete)
	config.HandleDelExp(`^`+wgsite.EscrowedProp, vpnHandleDelete)

	vpnCheckEscrow(ctx, conn)
}
