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
	"context"
	"fmt"
	"sync"
	"time"

	"bg/common/cfgmsg"
	"bg/common/cfgtree"

	"github.com/golang/protobuf/ptypes"
)

// State of a single submitted command.
type cmdState struct {
	cloudUUID *string
	cmdID     int64                  // monotonically increasing ID
	cmd       *cfgmsg.ConfigQuery    // config operation(s)
	response  *cfgmsg.ConfigResponse // result of the operation(s)
	submitted *time.Time             // when added to the submission queue
	fetched   *time.Time             // last time it was pulled from the queue
	completed *time.Time             // when the completion arrived
}

// A queue of cmdState structures.  We maintain both a proper queue, to enforce
// FIFO ordering, and a cmdID-indexed map, to enable fast lookups.
type simpleQueue struct {
	cloudUUID string
	queue     []*cmdState
	pool      map[int64]*cmdState
	maxLen    int
}

func (c *cmdState) String() string {
	return fmt.Sprintf("%s:%d", *c.cloudUUID, c.cmdID)
}

// Instantiate a new command queue
func newSimpleQueue(cloudUUID string, maxLen int) *simpleQueue {
	q := simpleQueue{
		cloudUUID: cloudUUID,
		queue:     make([]*cmdState, 0),
		pool:      make(map[int64]*cmdState),
		maxLen:    maxLen,
	}
	return &q
}

// Remove one item from a queue and its associated pool.  Note: this is not a
// standard "pull the next item from the head of the queue" operation.  The item
// to be removed may be anywhere in the queue and is identified by cmdID.
func (q *simpleQueue) dequeue(cmdID int64) int {
	for i, cmd := range q.queue {
		if cmd.cmdID == cmdID {
			delete(q.pool, cmdID)
			q.queue = append(q.queue[:i], q.queue[i+1:]...)
			return i
		}
	}
	slog.Warnf("%s:%d not in queue", q.cloudUUID, cmdID)
	return -1
}

// Add one item to the tail of a queue, and add it to the map as well.
func (q *simpleQueue) enqueue(cmd *cmdState) {
	q.queue = append(q.queue, cmd)
	q.pool[cmd.cmdID] = cmd

	for q.maxLen != 0 && len(q.queue) > q.maxLen {
		cmdID := q.queue[0].cmdID
		q.dequeue(cmdID)
	}
}

type memCmdQueue struct {
	lastCmdID int64        // last ID assigned to a cmd
	sq        *simpleQueue // submitted, but not completed ops
	cq        *simpleQueue // completed ops

	sqBlocked bool      // go routine blocked on empty submission queue
	sqUpdated chan bool // new commands enqueued

	sync.Mutex
}

func newMemCmdQueue(uuid string, cqMax int) *memCmdQueue {
	memq := &memCmdQueue{
		lastCmdID: time.Now().Unix(),
		sq:        newSimpleQueue(uuid, 0),
		cq:        newSimpleQueue(uuid, cqMax),
		sqUpdated: make(chan bool, 10),
	}
	return memq
}

// block until one or more items are added to the submission queue.  The queue
// must be locked when this routine is called.
func (memq *memCmdQueue) block(ctx context.Context) error {
	var err error

	slog.Debugf("blocking on empty queue")
	memq.sqBlocked = true
	memq.Unlock()
	select {
	case <-memq.sqUpdated:
		slog.Debugf("woke on queue update")
	case <-ctx.Done():
		// Likely means that we lost the connection from cl.rpcd
		slog.Debugf("woke on context done")
		err = ctx.Err()
	}
	memq.Lock()
	memq.sqBlocked = false

	return err
}

// Look for a command in both the submission and completion queues.
func (memq *memCmdQueue) search(ctx context.Context, cmdID int64) *cmdState {
	// There's no need to take the lock because the only time we're ever
	// called is by other memCmdQueue methods which have already taken it.
	if cmd, ok := memq.sq.pool[cmdID]; ok {
		return cmd
	}
	if cmd, ok := memq.cq.pool[cmdID]; ok {
		return cmd
	}

	return nil
}

// Add a single command to an AP's submitted queue and to its map of outstanding
// commands.
func (memq *memCmdQueue) submit(ctx context.Context, s *perAPState, q *cfgmsg.ConfigQuery) (int64, error) {
	memq.Lock()
	defer memq.Unlock()

	cmdID := memq.lastCmdID + 1
	q.CmdID = cmdID

	now := time.Now()
	cmd := cmdState{
		cloudUUID: &s.cloudUUID,
		cmdID:     cmdID,
		cmd:       q,
		submitted: &now,
	}
	memq.lastCmdID = cmdID
	memq.sq.enqueue(&cmd)

	// If any goroutines are blocked waiting for new commands, wake them up
	if memq.sqBlocked {
		// We could enqueue multiple commands before the blocked thread
		// wakes up.  To avoid blocking ourselves on a clogged channel,
		// we drain it before pushing a new message.
		cnt := 0
		drained := false
		for !drained {
			select {
			case <-memq.sqUpdated:
				cnt++
			default:
				drained = true
			}
		}
		if cnt > 0 {
			slog.Debugf("drained %d channel messages")
		}
		memq.sqUpdated <- true
	}

	return cmdID, nil
}

// Fetch one or more commands from the submitted queue.  Commands are left in
// the queue until they are completed, allowing them to be refetched if the
// appliance crashes/restarts before they are executed.
func (memq *memCmdQueue) fetch(ctx context.Context, s *perAPState, start int64, max uint32,
	block bool) ([]*cfgmsg.ConfigQuery, error) {

	var err error

	o := make([]*cfgmsg.ConfigQuery, 0)
	if max == 0 {
		panic("invalid max of 0")
	}

	memq.Lock()
	for err == nil {
		t := time.Now()
		for _, c := range memq.sq.queue {
			if c.cmdID > start {
				if c.fetched != nil {
					slog.Infof("%v refetched - last fetched at %s",
						c, c.fetched.Format(time.RFC3339))
				}
				c.fetched = &t

				o = append(o, c.cmd)
				if uint32(len(o)) == max {
					break
				}
			}
		}
		if len(o) > 0 || !block {
			break
		}
		err = memq.block(ctx)
	}
	memq.Unlock()
	if len(o) > 1 {
		slog.Debugw("fetch", "commands", &o)
	}
	return o, err
}

// Get the status of a submitted command
func (memq *memCmdQueue) status(ctx context.Context, s *perAPState, cmdID int64) (*cfgmsg.ConfigResponse, error) {
	rval := &cfgmsg.ConfigResponse{
		Timestamp: ptypes.TimestampNow(),
		CmdID:     cmdID,
	}

	memq.Lock()
	defer memq.Unlock()

	cmd := memq.search(ctx, cmdID)

	switch {
	case cmd == nil:
		slog.Debugf("%s:%d: no such cmd\n", s.cloudUUID, cmdID)
		rval.Response = cfgmsg.ConfigResponse_NOCMD

	case cmd.fetched == nil:
		slog.Debugf("%v: queued\n", cmd)
		rval.Response = cfgmsg.ConfigResponse_QUEUED

	case cmd.completed == nil:
		slog.Debugf("%v: in-progress\n", cmd)
		rval.Response = cfgmsg.ConfigResponse_INPROGRESS

	default:
		slog.Debugf("%v: done\n", cmd)
		rval = cmd.response
	}
	return rval, nil
}

// Attempt to cancel a command
func (memq *memCmdQueue) cancel(ctx context.Context, s *perAPState, cmdID int64) (*cfgmsg.ConfigResponse, error) {
	rval := &cfgmsg.ConfigResponse{
		Timestamp: ptypes.TimestampNow(),
		CmdID:     cmdID,
	}

	memq.Lock()
	defer memq.Unlock()

	cmd := memq.search(ctx, cmdID)

	switch {
	case cmd == nil:
		rval.Response = cfgmsg.ConfigResponse_NOCMD

	case cmd.fetched == nil:
		// command is still queued, so we can cancel it
		rval.Response = cfgmsg.ConfigResponse_OK
		memq.sq.dequeue(cmdID)

	case cmd.completed == nil:
		// command has been fetched, so we can't cancel
		// it.  We'll still remove it from the queue so
		// it can't be fetched again.
		rval.Response = cfgmsg.ConfigResponse_INPROGRESS
		memq.sq.dequeue(cmdID)

	default:
		// Too late - the command has already been executed
		rval.Response = cfgmsg.ConfigResponse_FAILED
		rval.Errmsg = "command has already completed"
	}
	return rval, nil
}

// Is this command meant to be a refresh of the entire tree?
func isRefresh(cmd *cfgmsg.ConfigQuery) bool {
	if len(cmd.Ops) != 1 {
		return false
	}
	if cmd.Ops[0].Operation != cfgmsg.ConfigOp_GET {
		return false
	}

	if cmd.Ops[0].Property != "@/" {
		return false
	}

	return true
}

// Handle a completion for an outstanding command
func (memq *memCmdQueue) complete(ctx context.Context, s *perAPState, rval *cfgmsg.ConfigResponse) error {
	memq.Lock()
	defer memq.Unlock()

	cmdID := rval.CmdID
	cmd := memq.search(ctx, cmdID)
	if cmd == nil {
		slog.Warnf("%s:%d completion for unknown command",
			s.cloudUUID, cmdID)
		return nil
	}
	slog.Debugf("complete(%v)", cmd)

	// record the command result
	if cmd.completed == nil {
		cmd.response = rval

		if idx := memq.sq.dequeue(cmdID); idx > 0 {
			// commands are expected to be completed in their
			// arrival order, so note when they don't.
			prior := memq.sq.queue[idx-1]
			slog.Warnf("%v completed before %d", cmd, prior.cmdID)
		}
		memq.cq.enqueue(cmd)

		// Special-case handling for refetching a full tree.
		if rval.Response == cfgmsg.ConfigResponse_OK && isRefresh(cmd.cmd) &&
			len(rval.Value) > 0 {

			tree, err := cfgtree.NewPTree("@", []byte(rval.Value))
			if err != nil {
				slog.Warnf("failed to refresh %s: %v", s.cloudUUID, err)
			} else {
				s.setCachedTree(tree)
			}
		}
	} else {
		slog.Infof("%v multiple completions - last at %s", cmd,
			cmd.completed.Format(time.RFC3339))
	}

	t := time.Now()
	cmd.completed = &t
	return nil
}
