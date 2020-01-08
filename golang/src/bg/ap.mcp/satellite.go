/*
 * COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
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
	"fmt"
	"net"
	"time"

	"bg/ap_common/mcp"
	"bg/base_def"
)

var (
	remoteUpdatePeriod = time.Second
)

func updateGatewayAddr() error {
	var err error

	_, lease := findWan()
	if lease == nil {
		err = fmt.Errorf("can't get DHCP info")

	} else if lease.Mode == base_def.MODE_GATEWAY {
		logWarn("node now appears to be a gateway")
		shutdown(1)
	} else {
		gatewayAddr = net.ParseIP(lease.Route)
		if gatewayAddr == nil {
			err = fmt.Errorf("invalid gateway address: %s",
				lease.Route)
		}
	}
	return err
}

func connectToGateway() *mcp.MCP {
	var mcpd *mcp.MCP
	var err error

	warnAt := time.Now()
	warnWait := time.Second
	for {
		if err = updateGatewayAddr(); err == nil {
			mcpd, err = mcp.NewPeer(pname, gatewayAddr)
		}

		if mcpd != nil {
			logInfo("connected to gateway at %v", gatewayAddr)
			return mcpd
		}

		now := time.Now()
		if now.After(warnAt) {
			logWarn("failed to connect to mcp on gateway: %v", err)
			if warnWait < time.Hour {
				warnWait *= 2
			}
			warnAt = now.Add(warnWait)
		}
		time.Sleep(time.Second)
	}
}

func satelliteLoop() {
	var mcpd *mcp.MCP
	var restartList daemonSet

	// If we intend to update every second, guarantee the upstream gateway
	// that we will check in every 5 seconds.  This gives us plenty of
	// wiggle room to allow for high system load or network congestion.
	lifeDuration := remoteUpdatePeriod * 5
	ticker := time.NewTicker(remoteUpdatePeriod)
	defer ticker.Stop()

	for {
		var err error

		if mcpd == nil {
			mcpd = connectToGateway()
		}

		daemons.Lock()
		state := getCurrentState(daemons.local, true)
		daemons.Unlock()

		state, err = mcpd.PeerUpdate(lifeDuration, state)
		if err != nil {
			logWarn("Lost connection to gateway")
			mcpd.Close()
			mcpd = nil

			x := daemonStopAll()
			if len(restartList) == 0 {
				restartList = x
			}
		} else {
			eol := time.Now().Add(lifeDuration)
			daemons.Lock()
			daemons.remote["gateway"] = remoteState{
				eol:   eol,
				state: state,
			}

			// Now that we are successfully communicating with
			// ap.mcp on the gateway node, restart any daemons we
			// may have shut down earlier.
			if len(restartList) > 0 {
				handleStart(restartList)
				restartList = make(daemonSet)
			}

			daemons.Unlock()
		}

		<-ticker.C
	}
}
