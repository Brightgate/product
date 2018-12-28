/*
 * COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
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
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/golang/protobuf/ptypes"
	"github.com/satori/uuid"

	"bg/cl_common/pgutils"
	"bg/cloud_models/appliancedb"
	"bg/common/cfgmsg"
	"bg/common/cfgtree"
)

var (
	cachedDBHandle        appliancedb.DataStore
	cachedDBCmdQueue      *dbCmdQueue
	onceHandle, onceQueue sync.Once
)

type dbCmdQueue struct {
	connInfo string
	handle   appliancedb.DataStore
}

func (dbq *dbCmdQueue) String() string {
	return pgutils.CensorPassword(dbq.connInfo)
}

func (dbq *dbCmdQueue) search(ctx context.Context, s *perAPState, cmdID int64) (*cmdState, error) {
	dbCmd, err := dbq.handle.CommandSearch(ctx, cmdID)
	if err != nil {
		return nil, err
	}

	var cfgQuery *cfgmsg.ConfigQuery
	err = json.Unmarshal(dbCmd.Query, cfgQuery)
	if err != nil {
		return nil, err
	}

	var cfgResponse *cfgmsg.ConfigResponse
	if len(dbCmd.Response) != 0 {
		err = json.Unmarshal(dbCmd.Response, cfgResponse)
		if err != nil {
			return nil, err
		}
	}

	// XXX NResent isn't represented, and neither is state
	uuidStr := dbCmd.UUID.String()
	cmd := &cmdState{
		cloudUUID: &uuidStr,
		cmdID:     dbCmd.ID,
		submitted: &dbCmd.EnqueuedTime,
		fetched:   dbCmd.SentTime.Ptr(),
		completed: dbCmd.DoneTime.Ptr(),
		cmd:       cfgQuery,
		response:  cfgResponse,
	}
	return cmd, nil
}

func (dbq *dbCmdQueue) submit(ctx context.Context, s *perAPState, q *cfgmsg.ConfigQuery) (int64, error) {
	jsonQuery, err := json.Marshal(q)
	if err != nil {
		return -1, fmt.Errorf("Failed to marshal commands to JSON: %v", err)
	}
	cmd := &appliancedb.ApplianceCommand{
		EnqueuedTime: time.Now(),
		Query:        jsonQuery,
	}
	u, err := uuid.FromString(s.cloudUUID)
	if err != nil {
		return -1, fmt.Errorf("Failed to convert %q to UUID: %v", s.cloudUUID, err)
	}
	err = dbq.handle.CommandSubmit(ctx, u, cmd)
	if err != nil {
		return -1, fmt.Errorf("Failed to submit commands to DB queue: %v", err)
	}
	return cmd.ID, nil
}

func (dbq *dbCmdQueue) fetch(ctx context.Context, s *perAPState, start int64,
	max uint32, block bool) ([]*cfgmsg.ConfigQuery, error) {

	var cmds []*appliancedb.ApplianceCommand
	if max == 0 {
		panic("invalid max of 0")
	}

	u, err := uuid.FromString(s.cloudUUID)
	if err != nil {
		return nil, fmt.Errorf("Failed to convert %q to UUID: %v",
			s.cloudUUID, err)
	}

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		cmds, err = dbq.handle.CommandFetch(ctx, u, start, max)
		if len(cmds) > 0 {
			slog.Debugf("Fetched %d commands from %q", len(cmds), u)
		}
		if err != nil {
			slog.Warnf("Failure fetching commands from %q: %v", u, err)
			if len(cmds) == 0 {
				// Complete error: SQL error or first scan failed
				return nil, err
			}
		}
		if len(cmds) > 0 || !block {
			break
		}

		select {
		case <-ticker.C:
			// XXX: is there a way to get a notification from the DB
			// when a table is updated, so we don't have to actively
			// poll each second?
		case <-ctx.Done():
			// Likely means that we lost the connection from cl.rpcd
			return nil, ctx.Err()
		}
	}

	// It's possible, if unlikely, that some (even all, but that's handled
	// above) commands were marked as fetched in the database but weren't
	// returned due to an intermediate error.
	o := make([]*cfgmsg.ConfigQuery, 0)
	for _, cmd := range cmds {
		var cfgQuery cfgmsg.ConfigQuery
		jerr := json.Unmarshal(cmd.Query, &cfgQuery)
		if jerr != nil {
			// The unmarshaling error is likely specific to the one
			// command, and we could go on to the next, but we want
			// to complete the commands in order, so we can't
			// intentionally leave holes in the command stream.  The
			// caller will need to cancel this command. XXX Which
			// means we need to return it, since it might not be the
			// caller's idea of lastCmd+1.
			return o, jerr
		}
		// We store the query without a command ID, so it defaults to
		// zero.
		cfgQuery.CmdID = cmd.ID
		o = append(o, &cfgQuery)
	}

	// err comes from CommandFetch(), and if not nil, indicates that the
	// return value is incomplete.
	return o, err
}

func (dbq *dbCmdQueue) status(ctx context.Context, s *perAPState, cmdID int64) (*cfgmsg.ConfigResponse, error) {
	rval := &cfgmsg.ConfigResponse{
		Timestamp: ptypes.TimestampNow(),
		CmdID:     cmdID,
	}

	dbCmd, err := dbq.handle.CommandSearch(ctx, cmdID)
	if err != nil {
		if _, ok := err.(appliancedb.NotFoundError); ok {
			slog.Debugf("%s:%d: no such command", s.cloudUUID, cmdID)
			rval.Response = cfgmsg.ConfigResponse_NOCMD
			return rval, nil
		}
		return nil, err
	}

	switch {
	case dbCmd.State == "ENQD":
		slog.Debugf("%s:%d: queued", s.cloudUUID, cmdID)
		rval.Response = cfgmsg.ConfigResponse_QUEUED

	case dbCmd.State == "WORK":
		slog.Debugf("%s:%d: in-progress", s.cloudUUID, cmdID)
		rval.Response = cfgmsg.ConfigResponse_INPROGRESS

	case dbCmd.State == "DONE":
		// An empty response isn't an error--it simply means that the
		// command was canceled--but we have to be careful not to
		// unmarshal it.
		state := "done"
		if len(dbCmd.Response) == 0 {
			state = "canceled"
			rval.Response = cfgmsg.ConfigResponse_OK
		} else {
			err = json.Unmarshal(dbCmd.Response, rval)
			if err != nil {
				return nil, err
			}
			xstate := fmt.Sprintf(" (%s)",
				cfgmsg.ConfigResponse_OpResponse_name[int32(rval.Response)])
			if rval.Errmsg != "" {
				xstate += ": " + rval.Errmsg
			}
			state += xstate
		}
		slog.Debugf("%s:%d: %s", s.cloudUUID, cmdID, state)

	default:
		slog.Debugf("%s:%d: unknown state %q", s.cloudUUID, cmdID, dbCmd.State)
	}

	return rval, nil
}

func (dbq *dbCmdQueue) cancel(ctx context.Context, s *perAPState, cmdID int64) (*cfgmsg.ConfigResponse, error) {
	rval := &cfgmsg.ConfigResponse{
		Timestamp: ptypes.TimestampNow(),
		CmdID:     cmdID,
	}

	newCmd, oldCmd, err := dbq.handle.CommandCancel(ctx, cmdID)
	if err != nil {
		if _, ok := err.(appliancedb.NotFoundError); ok {
			slog.Warnf("%s:%d cancellation for unknown command",
				s.cloudUUID, cmdID)
			rval.Response = cfgmsg.ConfigResponse_NOCMD
			return rval, nil
		}
		return nil, err
	}
	slog.Debugf("cancel(%s:%d)", newCmd.UUID, newCmd.ID)
	dbq.cleanup(s)

	switch {
	case oldCmd.State == "ENQD":
		// command is still queued, so we can cancel it
		rval.Response = cfgmsg.ConfigResponse_OK

	case oldCmd.State == "WORK":
		// command has been fetched, so we can't cancel
		// it.  We'll still remove it from the queue so
		// it can't be fetched again.
		rval.Response = cfgmsg.ConfigResponse_INPROGRESS

	default:
		// Too late - the command has already been executed
		rval.Response = cfgmsg.ConfigResponse_FAILED
		rval.Errmsg = "command has already completed"
	}

	return rval, nil
}

func (dbq *dbCmdQueue) complete(ctx context.Context, s *perAPState, rval *cfgmsg.ConfigResponse) error {
	cmdID := rval.CmdID
	jsonResp, err := json.Marshal(rval)
	newCmd, oldCmd, err := dbq.handle.CommandComplete(ctx, cmdID, jsonResp)
	if err != nil {
		if _, ok := err.(appliancedb.NotFoundError); ok {
			slog.Warnf("%s:%d completion for unknown command",
				s.cloudUUID, cmdID)
			return nil
		}
		return err
	}
	slog.Debugf("complete(%s:%d)", newCmd.UUID, newCmd.ID)
	dbq.cleanup(s)

	if !oldCmd.DoneTime.Valid {
		// Special-case handling for refetching a full tree.
		var cfgQuery cfgmsg.ConfigQuery
		err = json.Unmarshal(newCmd.Query, &cfgQuery)
		if err != nil {
			slog.Errorf("Unable to unmarshal query for command %s:%d: %v",
				newCmd.UUID, newCmd.ID, err)
			return err
		}
		if rval.Response == cfgmsg.ConfigResponse_OK && isRefresh(&cfgQuery) &&
			len(rval.Value) > 0 {
			var tree *cfgtree.PTree
			tree, err = cfgtree.NewPTree("@", []byte(rval.Value))
			if err != nil {
				slog.Warnf("failed to refresh %s: %v", s.cloudUUID, err)
				return err
			}
			s.setCachedTree(tree)
		}
	} else {
		slog.Infof("%s:%d multiple completions - last at %s",
			s.cloudUUID, cmdID, oldCmd.DoneTime.Time.Format(time.RFC3339))
	}
	return err
}

func (dbq *dbCmdQueue) cleanup(s *perAPState) {
	u, err := uuid.FromString(s.cloudUUID)
	if err != nil {
		return
	}

	go func() {
		del, err := dbq.handle.CommandDelete(context.Background(), u, int64(*cqMax))
		if err != nil {
			slog.Errorf("Failed to delete commands from queue for %s: %v",
				s.cloudUUID, err)
		} else {
			slog.Debugf("Deleted %d commands from queue for %s",
				del, s.cloudUUID)
		}
	}()
}

func (dbq *dbCmdQueue) connect() error {
	err := dbConnect(dbq.connInfo)
	dbq.handle = cachedDBHandle
	return err
}

func dbConnect(connInfo string) error {
	if cachedDBHandle != nil {
		return nil
	}

	var err error
	onceHandle.Do(func() {
		cachedDBHandle, err = appliancedb.Connect(connInfo)
	})
	if err != nil {
		slog.Warnf("failed to connect to appliance DB: %v", err)
		return err
	}

	return nil
}

// This only creates a new queue object the first time it's called; it caches
// the value and returns that thereafter.
func newDBCmdQueue(connInfo string) cmdQueue {
	if cachedDBCmdQueue != nil {
		return cachedDBCmdQueue
	}
	onceQueue.Do(func() {
		cachedDBCmdQueue = &dbCmdQueue{connInfo: connInfo}
	})
	err := cachedDBCmdQueue.connect()
	if err != nil {
		slog.Warn(err)
	} else {
		slog.Info("Connected to appliance DB for command queue")
	}

	var queue cmdQueue = cachedDBCmdQueue
	return queue
}
