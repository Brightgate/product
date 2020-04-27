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
	"io/ioutil"
	"sync"
	"time"

	"bg/ap_common/aputil"
	"bg/base_def"
	"bg/base_msg"
	"bg/common/cfgapi"
	"bg/common/vpn"

	"github.com/golang/protobuf/proto"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

var vpnEscrowLock sync.Mutex

func grpcEscrowCall() error {
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

func vpnCheckEscrow() {
	vpnEscrowLock.Lock()
	defer vpnEscrowLock.Unlock()

	filename := plat.ExpandDirPath(vpn.SecretDir, vpn.PrivateFile)
	if !aputil.FileExists(filename) {
		slog.Infof("No VPN key configured")
		return
	}

	public, err := config.GetProp(vpn.PublicProp)
	if err == cfgapi.ErrNoProp || len(public) == 0 {
		vpnKeyMismatchError("private key has an empty public key")
		return
	}
	if err != nil {
		slog.Warnf("failed to fetch %s: %v", vpn.PublicProp, err)
		return
	}

	escrow, _ := config.GetProp(vpn.EscrowedProp)
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

	err = grpcEscrowCall()

	err = config.CreateProp(vpn.EscrowedProp, public, nil)
	if err != nil {
		slog.Warnf("failed to update escrowed property: %v", err)
	}
}

func vpnHandleUpdate(path []string, val string, expires *time.Time) {
	vpnCheckEscrow()
}

func vpnHandleDelete(path []string) {
	vpnCheckEscrow()
}

func vpnInit() {
	config.HandleChange(`^`+vpn.PublicProp, vpnHandleUpdate)
	config.HandleChange(`^`+vpn.EscrowedProp, vpnHandleUpdate)
	config.HandleDelExp(`^`+vpn.PublicProp, vpnHandleDelete)
	config.HandleDelExp(`^`+vpn.EscrowedProp, vpnHandleDelete)

	vpnCheckEscrow()
}
