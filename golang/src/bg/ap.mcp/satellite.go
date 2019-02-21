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
	"time"

	"bg/ap_common/mcp"
)

var (
	remoteUpdatePeriod = time.Second
)

func connectToGateway() *mcp.MCP {
	warnAt := time.Now()
	warnWait := time.Second
	for {
		if mcpd, err := mcp.NewPeer(pname); err == nil {
			return mcpd
		}

		now := time.Now()
		if now.After(warnAt) {
			logWarn("failed to connect to mcp on gateway")
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

	// If we intend to update every second, guarantee the upstream gateway
	// that we will check in every 5 seconds.  This gives us plenty of
	// wiggle room to allow for high system load or network congestion.
	lifeDuration := remoteUpdatePeriod * 5
	ticker := time.NewTicker(remoteUpdatePeriod)
	defer ticker.Stop()

	for {
		if mcpd == nil {
			mcpd = connectToGateway()
			logInfo("Connected to gateway")

			// Any daemon currently running should be restarted, so
			// it will pull the freshest state from the gateway.
			list := make(daemonSet)
			daemons.Lock()
			for n, d := range daemons.local {
				if !d.offline() {
					list[n] = d
				}
			}
			handleRestart(list)
			daemons.Unlock()
		}

		daemons.Lock()
		state := getCurrentState(daemons.local)
		daemons.Unlock()

		state, err := mcpd.PeerUpdate(lifeDuration, state)
		if err != nil {
			logWarn("Lost connection to gateway")
			mcpd.Close()
			mcpd = nil
		} else {
			eol := time.Now().Add(lifeDuration)
			daemons.Lock()
			daemons.remote["gateway"] = remoteState{
				eol:   eol,
				state: state,
			}
			daemons.Unlock()
		}

		<-ticker.C
	}
}
