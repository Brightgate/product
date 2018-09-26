/*
 * COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
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
	"math/rand"
	"time"

	rpc "bg/cloud_rpc"

	"github.com/golang/protobuf/ptypes"
)

const maxDelay = 3

// State of a single submitted command.
type cmdState struct {
	cmdID     int64                // monotonically increasing ID
	op        *rpc.CfgPropOps      // config operation(s)
	response  *rpc.CfgPropResponse // result of the operation(s)
	submitted *time.Time           // when added to the submission queue
	fetched   *time.Time           // last time it was pulled from the queue
	completed *time.Time           // when the completion arrived
}

// A queue of cmdState structures.  We maintain both a proper queue, to enforce
// FIFO ordering, and a cmdID-indexed map, to enable fast lookups.
type cmdQueue struct {
	uuid   string
	queue  []*cmdState
	pool   map[int64]*cmdState
	maxLen int
}

// Instantiate a new command queue
func newCmdQueue(uuid string, maxLen int) *cmdQueue {
	q := cmdQueue{
		uuid:   uuid,
		queue:  make([]*cmdState, 0),
		pool:   make(map[int64]*cmdState),
		maxLen: maxLen,
	}
	return &q
}

// Remove one item from a queue and its associated pool.  Note: this is not a
// standard "pull the next item from the head of the queue" operation.  The item
// to be removed may be anywhere in the queue and is identified by cmdID.
func (q *cmdQueue) dequeue(cmdID int64) int {
	for i, cmd := range q.queue {
		if cmd.cmdID == cmdID {
			delete(q.pool, cmdID)
			q.queue = append(q.queue[:i], q.queue[i+1:]...)
			return i
		}
	}
	slog.Warnf("%s:%d not in queue", q.uuid, cmdID)
	return -1
}

// Add one item to the tail of a queue, and add it to the map as well.
func (q *cmdQueue) enqueue(cmd *cmdState) {
	q.queue = append(q.queue, cmd)
	q.pool[cmd.cmdID] = cmd

	for q.maxLen != 0 && len(q.queue) > q.maxLen {
		cmdID := q.queue[0].cmdID
		q.dequeue(cmdID)
	}
}

// Look for a command in both the submission and completion queues.
func cmdSearch(s *perAPState, cmdID int64) *cmdState {
	if cmd, ok := s.sq.pool[cmdID]; ok {
		return cmd
	}
	if cmd, ok := s.cq.pool[cmdID]; ok {
		return cmd
	}

	return nil
}

// Add a single command to an AP's submitted queue and to its map of outstanding
// commands.
func cmdSubmit(s *perAPState, ops *rpc.CfgPropOps) int64 {
	s.Lock()
	defer s.Unlock()

	cmdID := s.lastCmdID + 1

	now := time.Now()
	cmd := cmdState{
		cmdID:     cmdID,
		op:        ops,
		submitted: &now,
	}
	ops.CmdID = cmdID
	s.lastCmdID = cmdID
	s.sq.enqueue(&cmd)

	return cmdID
}

// Fetch one or more commands from the submitted queue.  Commands are left in
// the queue until they are completed, allowing them to be refetched if the
// appliance crashes/restarts before they are executed.
func cmdFetch(s *perAPState, start, max int64) []*rpc.CfgPropOps {
	o := make([]*rpc.CfgPropOps, 0)

	s.Lock()
	defer s.Unlock()

	t := time.Now()
	for _, c := range s.sq.queue {
		if c.cmdID >= start {
			if c.fetched != nil {
				slog.Infof("%s:%d being refetched "+
					"- last fetched at %s", s.uuid, c.cmdID,
					c.fetched.Format(time.RFC3339))
			}
			c.fetched = &t

			o = append(o, c.op)
			if len(o) >= int(max) {
				break
			}
		}
	}
	return o
}

// Get the status of a submitted command
func cmdStatus(s *perAPState, cmdID int64) *rpc.CfgPropResponse {
	rval := &rpc.CfgPropResponse{
		Time:  ptypes.TimestampNow(),
		CmdID: cmdID,
	}

	s.Lock()
	defer s.Unlock()

	cmd := cmdSearch(s, cmdID)

	switch {
	case cmd == nil:
		rval.Response = rpc.CfgPropResponse_NOCMD

	case cmd.fetched == nil:
		rval.Response = rpc.CfgPropResponse_QUEUED

	case cmd.completed == nil:
		rval.Response = rpc.CfgPropResponse_INPROGRESS

	default:
		rval = cmd.response
	}
	return rval
}

// Attempt to cancel a command
func cmdCancel(s *perAPState, cmdID int64) *rpc.CfgPropResponse {
	rval := &rpc.CfgPropResponse{
		Time:  ptypes.TimestampNow(),
		CmdID: cmdID,
	}

	s.Lock()
	defer s.Unlock()

	cmd := cmdSearch(s, cmdID)

	switch {
	case cmd == nil:
		rval.Response = rpc.CfgPropResponse_NOCMD

	case cmd.fetched == nil:
		// command is still queued, so we can cancel it
		rval.Response = rpc.CfgPropResponse_OK
		s.sq.dequeue(cmdID)

	case cmd.completed == nil:
		// command has been fetched, so we can't cancel
		// it.  We'll still remove it from the queue so
		// it can't be fetched again.
		rval.Response = rpc.CfgPropResponse_INPROGRESS
		s.sq.dequeue(cmdID)

	default:
		// Too late - the command has already been executed
		rval.Response = rpc.CfgPropResponse_FAILED
		rval.Errmsg = "command has already completed"
	}
	return rval
}

// Handle a completion for an outstanding command
func cmdComplete(s *perAPState, rval *rpc.CfgPropResponse) {
	s.Lock()
	defer s.Unlock()

	cmdID := rval.CmdID
	cmd := cmdSearch(s, cmdID)

	if cmd == nil {
		slog.Warnf("%s:%d completion for unknown command", s.uuid, cmdID)
		return
	}

	// record the command result
	if cmd.completed == nil {
		cmd.response = rval

		if idx := s.sq.dequeue(cmdID); idx > 0 {
			// commands are expected to be completed in their
			// arrival order, so note when they don't.
			prior := s.sq.queue[idx-1]
			slog.Warnf("%s:%d completed before %d", s.uuid, cmdID,
				prior.cmdID)
		}
		s.cq.enqueue(cmd)
	} else {
		slog.Infof("%s:%d multiple completions - last at %s",
			s.uuid, cmd.cmdID, cmd.completed.Format(time.RFC3339))
	}

	t := time.Now()
	cmd.completed = &t
}

// Execute a single CfgPropOps command, which may include multiple property
// updates.  This mimics work that would really be done by ap.configd on the
// appliance.  The changes made to the in-core tree are not persisted, so we
// will revert to the original tree next time cl.configd launches.
// XXX: is there a need for a Reset() rpc to trigger this cleanup without
// restarting the daemon?
func execute(state *perAPState, ops *rpc.CfgPropOps) *rpc.CfgPropResponse {
	var err error
	var rval rpc.CfgPropResponse

	t := state.cachedTree
	t.ChangesetInit()

	for _, op := range ops.Ops {
		prop, val, expires, perr := getParams(op)
		if perr != nil {
			err = perr
			break
		}

		switch op.Operation {

		case rpc.CfgPropOps_CfgPropOp_SET:
			err = t.Set(prop, val, expires)

		case rpc.CfgPropOps_CfgPropOp_CREATE:
			err = t.Add(prop, val, expires)

		case rpc.CfgPropOps_CfgPropOp_DELETE:
			err = t.Delete(prop)
		}

		if err != nil {
			break
		}
	}

	if err == nil {
		rval.Response = rpc.CfgPropResponse_OK
		t.ChangesetCommit()
	} else {
		rval.Errmsg = fmt.Sprintf("%v", err)
		rval.Response = rpc.CfgPropResponse_FAILED
		t.ChangesetRevert()
	}

	rval.CmdID = ops.CmdID
	rval.Time = ptypes.TimestampNow()

	return &rval
}

func delay() {
	seconds := rand.Int() % maxDelay
	time.Sleep(time.Duration(seconds) * time.Second)
}

// Repeatedly pull commands from the queue, execute them, and post the results.
// Sleep for some number of seconds between iterations to emulate the
// asynchronous nature of interacting with a remote device.
func emulateAppliance(app *perAPState) {
	lastCmd := int64(-1)

	for {
		delay()
		ops := cmdFetch(app, lastCmd+1, 1)
		if len(ops) > 0 {
			delay()
			for _, o := range ops {
				r := execute(app, o)
				cmdComplete(app, r)
				if o.CmdID > lastCmd {
					lastCmd = o.CmdID
				}
			}
		}
	}
}
