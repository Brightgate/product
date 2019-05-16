/*
 * COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package main

import (
	"strconv"
	"strings"
)

const (
	oldURLProp = "@/cloud/svc_rpc/url"
	oldTLSProp = "@/cloud/svc_rpc/tls"
	hostProp   = "@/cloud/svc_rpc/0/host"
	hostIPProp = "@/cloud/svc_rpc/0/hostip"
	portProp   = "@/cloud/svc_rpc/0/port"
	tlsProp    = "@/cloud/svc_rpc/0/tls"
)

func upgradeV22() error {
	host := "rpc0.b10e.net"
	hostip := "34.83.242.232"
	port := "443"
	tls := "true"

	oldTLS, err := propTree.GetNode(oldTLSProp)
	if err != nil {
		slog.Infof("no old %s", oldTLSProp)
	} else {
		tls = oldTLS.Value
	}

	oldURL, err := propTree.GetNode(oldURLProp)
	if err != nil {
		slog.Infof("no old %s", oldURLProp)
	} else {
		old := oldURL.Value
		good := false

		// Any existing URL should look like 'host:port'
		f := strings.Split(old, ":")
		if len(f) == 2 {
			if _, err := strconv.Atoi(f[1]); err == nil {
				host = f[0]
				port = f[1]
				hostip = ""
				good = true
			}
		}
		if good {
			slog.Infof("converting svc url: %s", old)
		} else {
			slog.Infof("invalid svc url: %s", old)
		}
	}

	propTree.Delete(oldURLProp)
	propTree.Delete(oldTLSProp)
	propTree.Add(hostProp, host, nil)
	if hostip != "" {
		propTree.Add(hostIPProp, hostip, nil)
	}
	propTree.Add(portProp, port, nil)
	propTree.Add(tlsProp, tls, nil)

	return nil
}

func init() {
	addUpgradeHook(22, upgradeV22)
}
