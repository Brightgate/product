/*
 * Copyright 2019 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package main

import (
	"encoding/json"
	"log"
	"os"
	"os/exec"
	"strconv"
	"time"

	"bg/ap_common/aputil"
	"bg/ap_common/comms"
	"bg/ap_common/mcp"
	"bg/base_def"
	"bg/base_msg"

	"github.com/golang/protobuf/proto"
)

var stateReverseMap map[string]int

func handleGetState(set daemonSet, includeRemote bool) *string {
	var rval *string

	// If any node's data has expired, mark everything offline
	now := time.Now()
	for _, list := range daemons.remote {
		if list.eol.Before(now) {
			list.eol = now.AddDate(1, 0, 0)
			for _, d := range list.state {
				if d.State > mcp.OFFLINE {
					d.State = mcp.OFFLINE
					d.Pid = -1
					d.Since = now
				}
			}
		}
	}

	list := getCurrentState(set, len(set) > 1)
	if includeRemote {
		for _, remoteList := range daemons.remote {
			list = append(list, remoteList.state...)
		}
	}

	b, err := json.MarshalIndent(list, "", "  ")
	if err == nil {
		s := string(b)
		rval = &s
	}
	return rval
}

func handleSetState(set daemonSet, state *string) base_msg.MCPResponse_OpResponse {
	// A daemon can only update its own state, so we should never have more
	// than one in the set.
	if state != nil && len(set) == 1 {
		if s, ok := stateReverseMap[*state]; ok {
			for _, d := range set {
				d.Lock()
				d.setState(s)
				d.Unlock()
			}
			return mcp.OK
		}
	}
	return mcp.INVALID
}

func handlePeerUpdate(node, in *string, lifetime int32) (*string,
	base_msg.MCPResponse_OpResponse) {
	var (
		state mcp.DaemonList
		rval  *string
		code  base_msg.MCPResponse_OpResponse
	)

	b := []byte(*in)
	if err := json.Unmarshal(b, &state); err != nil {
		logWarn("failed to unmarshal state from %s: %v", *node, err)
		code = mcp.INVALID
	} else {
		// The remote node tells us how long we should consider this
		// data to be valid.
		lifeDuration := time.Duration(lifetime) * time.Second
		daemons.remote[*node] = remoteState{
			eol:   time.Now().Add(lifeDuration),
			state: state,
		}
		rval = handleGetState(daemons.local, false)
		code = mcp.OK
	}
	return rval, code
}

func handleStart(set daemonSet) {
	for _, d := range set {
		d.Lock()
		if d.state == mcp.BROKEN {
			d.failures = 0
			d.setState(mcp.OFFLINE)
		}
		d.Unlock()
		logInfo("Tell %s to come online", d.Name)
		d.goal <- mcp.ONLINE
	}
}

func handleStop(set daemonSet) {
	for _, d := range set {
		if !d.offline() {
			logInfo("Tell %s to shut down", d.Name)
			d.goal <- mcp.OFFLINE
		}
	}
}

func handleCrash(set daemonSet) {
	// If B depends on A, we don't want to crash A until after B.
	// Otherwise, the normal dependency handling code could start a clean
	// shutdown of B in response to A crashing.
	ordered := make([]*daemon, 0)
	for len(set) > 0 {
		for n, d := range set {
			liveDependents := false
			for _, dep := range d.dependents {
				if _, ok := set[dep.Name]; ok {
					liveDependents = true
				}
			}
			if !liveDependents {
				ordered = append(ordered, d)
				delete(set, n)
			}
		}
	}

	// Crash the daemons
	crashed := make([]*daemon, 0)
	for _, d := range ordered {
		if !d.offline() {
			d.crash()
			crashed = append(crashed, d)
			time.Sleep(10 * time.Millisecond)
		}
	}

	// Now try to get them back online
	for _, d := range crashed {
		d.goal <- mcp.ONLINE
	}
}

func handleDoCmd(set daemonSet, cmd string) base_msg.MCPResponse_OpResponse {
	code := mcp.OK

	switch cmd {
	case "start":
		handleStart(set)

	case "stop":
		handleStop(set)

	case "crash":
		handleCrash(set)

	default:
		code = mcp.INVALID
	}

	return code
}

//
// Given a name, select the daemons that will be affected.  Currently the
// choices are all, one, or none.  Eventually, this could be expanded to
// identify daemons that should be acted on together.
//
func selectTargets(name *string) daemonSet {
	set := make(daemonSet)

	for _, d := range daemons.local {
		if *name == "all" || *name == d.Name {
			set[d.Name] = d
		}
	}

	return set
}

func getDaemonSet(req *base_msg.MCPRequest) (daemonSet,
	base_msg.MCPResponse_OpResponse) {
	if req.Daemon == nil {
		return nil, mcp.INVALID
	}

	set := selectTargets(req.Daemon)
	if len(set) == 0 {
		return nil, mcp.NODAEMON
	}

	return set, mcp.OK
}

//
// Parse and execute a single client request
//
func handleRequest(req *base_msg.MCPRequest) (*string,
	base_msg.MCPResponse_OpResponse) {
	var (
		set  daemonSet
		rval *string
		code base_msg.MCPResponse_OpResponse
	)

	daemons.Lock()
	defer daemons.Unlock()

	ver := req.GetVersion()
	if ver == nil || *ver.Major != mcp.Version {
		return nil, mcp.BADVER
	}

	switch req.GetOperation() {
	case mcp.PING:

	case mcp.GET:
		all := (req.Daemon) != nil && (*req.Daemon == "all")

		if set, code = getDaemonSet(req); code == mcp.OK {
			if rval = handleGetState(set, all); rval == nil {
				code = mcp.INVALID
			}
		}

	case mcp.SET:
		if req.State == nil {
			code = mcp.INVALID
		} else if set, code = getDaemonSet(req); code == mcp.OK {
			code = handleSetState(set, req.State)
		}

	case mcp.DO:
		if req.Command == nil {
			code = mcp.INVALID
		} else if set, code = getDaemonSet(req); code == mcp.OK {
			code = handleDoCmd(set, req.GetCommand())
		}

	case mcp.UPDATE:
		if req.State == nil || req.Node == nil {
			code = mcp.INVALID
		} else {
			rval, code = handlePeerUpdate(req.Node, req.State,
				req.GetLifetime())
		}

	case mcp.REBOOT:
		from := req.GetSender()
		if from == "" {
			from = "unknown"
		}
		reboot(from)

	case mcp.GATEWAY:
		ip := gatewayAddr.String()
		rval = &ip
		code = mcp.OK

	default:
		code = mcp.INVALID
	}

	return rval, code
}

func apiHandle(msg []byte) []byte {
	var rval *string
	var rc base_msg.MCPResponse_OpResponse

	me := "mcp." + strconv.Itoa(os.Getpid()) + ")"

	req := &base_msg.MCPRequest{}
	if err := proto.Unmarshal(msg, req); err != nil {
		logWarn("unmarshaling request: %v", err)
		rc = mcp.INVALID
	} else {
		rval, rc = handleRequest(req)
	}

	version := base_msg.Version{Major: proto.Int32(mcp.Version)}
	response := &base_msg.MCPResponse{
		Timestamp: aputil.NowToProtobuf(),
		Sender:    proto.String(me),
		Version:   &version,
		Debug:     proto.String("-"),
		Response:  &rc,
	}
	if rval != nil {
		response.State = proto.String(*rval)
	}

	data, err := proto.Marshal(response)
	if err != nil {
		logWarn("Failed to marshal response: %v", err)
	}
	return data
}

func apiInit() {
	stateReverseMap = make(map[string]int)
	for i, s := range mcp.States {
		stateReverseMap[s] = i
	}

	err := exec.Command(plat.IPCmd, "link", "set", "up", "lo").Run()
	if err != nil {
		logWarn("Failed to enable loopback: %v", err)
	}

	url := base_def.INCOMING_COMM_URL + base_def.MCP_COMM_REP_PORT
	server, err := comms.NewAPServer(pname, url)
	if err != nil {
		log.Fatalf("failed to get open server port")
	}

	go server.Serve(apiHandle)
}

