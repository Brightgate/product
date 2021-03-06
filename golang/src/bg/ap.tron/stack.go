/*
 * Copyright 2019 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	"bg/ap_common/apcfg"
	"bg/ap_common/mcp"
	"bg/common/cfgapi"
)

var (
	mcpd        *mcp.MCP
	gatewayAddr net.IP

	config          *cfgapi.Handle
	configConnected bool

	// daemons mcp believes to be online
	daemonsOnline = make(map[string]time.Time)

	stackTests = []*hTest{selfTest, mcpTest, configTest, rpcdTest}

	// These tests attempt to determine the health of the brightgate
	// software stack.  In particular, we want to know whether enough of our
	// stack is working to support the creation of service tunnels from the
	// cloud.  This is communicated through the blink pattern on LED 4.
	selfTest = &hTest{
		name:     "self",
		testFn:   selfCheck,
		period:   time.Second,
		ledValue: 10,
	}
	mcpTest = &hTest{
		name:     "mcp",
		testFn:   mcpCheck,
		period:   5 * time.Second,
		triggers: []*hTest{configTest},
		ledValue: 50,
	}
	configTest = &hTest{
		name:     "configd",
		testFn:   configCheck,
		period:   5 * time.Second,
		source:   "@/apversion",
		triggers: []*hTest{wanTest, rpcdTest},
		ledValue: 90,
	}
	rpcdTest = &hTest{
		name:     "cloud_rpc",
		testFn:   rpcCheck,
		period:   5 * time.Second,
		source:   "", // initialized at runtime
		ledValue: 100,
	}
)

// Attempt to connect to configd if we aren't already.
func configConnect() error {
	var err error

	if _, ok := daemonsOnline["configd"]; !ok {
		return fmt.Errorf("daemon offline")
	}

	configMtx.Lock()
	defer configMtx.Unlock()

	if config == nil && gatewayAddr != nil {
		os.Setenv("APGATEWAY", gatewayAddr.String())
		logDebug("connecting to config on %s", gatewayAddr.String())
		config, err = apcfg.NewConfigdHdl(nil, pname,
			cfgapi.AccessInternal)
		if err != nil {
			logWarn("NewConfigd failed: %v", err)
			return err
		}

		if c := apcfg.GetComm(config); c != nil {
			c.SetRecvTimeout(time.Second)
			c.SetSendTimeout(10 * time.Millisecond)
			c.SetOpenTimeout(time.Second)
		}
	}

	err = config.Ping(context.TODO())
	configConnected = (err == nil)

	return err
}

// If any of the components' health states change, try to push the new state
// into the config tree
func configUpdater(wg *sync.WaitGroup) {
	defer wg.Done()

	propBase := "@/metrics/health/" + nodeUUID + "/"
	for running {
		// Block until a state is updated, then drain any accumulated
		// update signals.
		<-states.updated
		for len(states.updated) > 0 {
			<-states.updated
		}

		if !configConnected {
			continue
		}

		states.Lock()
		for prop, current := range states.current {
			if old := states.old[prop]; old != current {
				full := propBase + prop
				err := config.CreateProp(full, current, nil)
				if err == nil {
					states.old[prop] = current
				} else {
					break
				}
			}
		}
		states.Unlock()
	}
}

func selfCheck(t *hTest) bool {
	return true
}

// Try to connect with ap.mcp
func mcpCheck(t *hTest) bool {
	var daemons mcp.DaemonList
	var states string
	var err error

	newOnline := make(map[string]time.Time)

	defer func() {
		daemonsOnline = newOnline
		if err != nil && mcpd != nil {
			mcpd.Close()
			mcpd = nil
		}
	}()

	if mcpd == nil {
		if mcpd, err = mcp.New(pname); err != nil {
			return false
		}

		if c := mcpd.GetComm(); c != nil {
			c.SetRecvTimeout(time.Second)
			c.SetSendTimeout(10 * time.Millisecond)
			c.SetOpenTimeout(time.Second)
		}

		gatewayAddr, err = mcpd.Gateway()
		if err != nil {
			logWarn("failed to get gateway address: %v", err)
		}
	}

	if states, err = mcpd.GetState("all"); err != nil {
		return false
	}

	if err = json.Unmarshal([]byte(states), &daemons); err != nil {
		return false
	}

	// Use the state info from mcp to update our list of live
	// daemons
	for _, s := range daemons {
		// A daemon is relevant to this node's health if it's running on
		// this node, or if it's configd, which runs on the gateway but
		// services all the nodes.
		if (s.State == mcp.ONLINE) &&
			((s.Node == nodeName) || (s.Name == "configd")) {
			newOnline[s.Name] = s.Since
		}
	}

	return true
}

// Try to fetch data from the config tree
func configCheck(t *hTest) bool {
	var err error

	t.data = nil
	if err = configConnect(); err != nil {
		return false
	}

	// As long as we're connected, collect any data other tests may
	// require
	for _, x := range allTests {
		if x.source != "" {
			x.data, err = config.GetProps(x.source)
			if err != nil {
				logDebug("failed to fetch %s: %v",
					x.source, err)
			}
		}
	}

	// We don't care what we got for @/apversion.  If we got any answer, we
	// know configd is working.
	return (t.data != nil)
}

func getTimeValue(root *cfgapi.PropertyNode, key string) time.Time {
	var rval time.Time

	if root != nil && root.Children != nil {
		if child, ok := root.Children[key]; ok {
			rval, _ = time.Parse(time.RFC3339, child.Value)
		}
	}

	return rval
}

// See if the cloud_rpc status in the config tree has changed
func rpcCheck(t *hTest) bool {
	onlineAt, ok := daemonsOnline["rpcd"]
	if !ok {
		return false
	}

	if t.data == nil {
		return false
	}

	// If the last success came after the current incarnation of the
	// daemon came on line, and if that success is more recent than
	// the last failure, assume we have a live rpc connection.
	good := getTimeValue(t.data, "success")
	bad := getTimeValue(t.data, "fail")

	return good.After(bad) && good.After(onlineAt)
}

