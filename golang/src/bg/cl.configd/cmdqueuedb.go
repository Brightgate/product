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
	"sync"
	"time"

	"github.com/golang/protobuf/ptypes"
	"github.com/satori/uuid"

	"bg/cl_common/daemonutils"
	"bg/cl_common/pgutils"
	"bg/cloud_models/appliancedb"
	"bg/common/cfgmsg"
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

func (dbq *dbCmdQueue) submit(ctx context.Context, s *siteState, q *cfgmsg.ConfigQuery) (int64, error) {
	jsonQuery, err := json.Marshal(q)
	if err != nil {
		return -1, fmt.Errorf("Failed to marshal commands to JSON: %v", err)
	}
	cmd := &appliancedb.SiteCommand{
		EnqueuedTime: time.Now(),
		Query:        jsonQuery,
	}
	u, err := uuid.FromString(s.siteUUID)
	if err != nil {
		return -1, fmt.Errorf("Failed to convert %q to UUID: %v", s.siteUUID, err)
	}
	err = dbq.handle.CommandSubmit(ctx, u, cmd)
	if err != nil {
		return -1, fmt.Errorf("Failed to submit commands to DB queue: %v", err)
	}
	return cmd.ID, nil
}

func (dbq *dbCmdQueue) fetch(ctx context.Context, s *siteState, start int64,
	max uint32, block bool) ([]*cfgmsg.ConfigQuery, error) {

	var cmds []*appliancedb.SiteCommand
	if max == 0 {
		return make([]*cfgmsg.ConfigQuery, 0), nil
	}

	u, err := uuid.FromString(s.siteUUID)
	if err != nil {
		return nil, fmt.Errorf("Failed to convert %q to UUID: %v",
			s.siteUUID, err)
	}

	_, slog := daemonutils.EndpointLogger(ctx)

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

func (dbq *dbCmdQueue) status(ctx context.Context, s *siteState, cmdID int64) (*cfgmsg.ConfigResponse, error) {
	rval := &cfgmsg.ConfigResponse{
		Timestamp: ptypes.TimestampNow(),
		CmdID:     cmdID,
	}

	_, slog := daemonutils.EndpointLogger(ctx)

	u, err := uuid.FromString(s.siteUUID)
	if err != nil {
		return nil, fmt.Errorf("Failed to convert %q to UUID: %v",
			s.siteUUID, err)
	}
	dbCmd, err := dbq.handle.CommandSearch(ctx, u, cmdID)
	if err != nil {
		if _, ok := err.(appliancedb.NotFoundError); ok {
			slog.Debugf("%s:%d: no such command", s.siteUUID, cmdID)
			rval.Response = cfgmsg.ConfigResponse_NOCMD
			return rval, nil
		}
		return nil, err
	}

	switch {
	case dbCmd.State == "ENQD":
		slog.Debugf("%s:%d: queued", s.siteUUID, cmdID)
		rval.Response = cfgmsg.ConfigResponse_QUEUED

	case dbCmd.State == "WORK":
		slog.Debugf("%s:%d: in-progress", s.siteUUID, cmdID)
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
		slog.Debugf("%s:%d: %s", s.siteUUID, cmdID, state)

	case dbCmd.State == "CNCL":
		slog.Debugf("%s:%d: canceled", s.siteUUID, cmdID)
		rval.Response = cfgmsg.ConfigResponse_CANCELED

	default:
		slog.Debugf("%s:%d: unknown state %q", s.siteUUID, cmdID, dbCmd.State)
	}

	return rval, nil
}

func (dbq *dbCmdQueue) cancel(ctx context.Context, s *siteState, cmdID int64) (*cfgmsg.ConfigResponse, error) {
	rval := &cfgmsg.ConfigResponse{
		Timestamp: ptypes.TimestampNow(),
		CmdID:     cmdID,
	}

	_, slog := daemonutils.EndpointLogger(ctx)

	u, err := uuid.FromString(s.siteUUID)
	if err != nil {
		return nil, fmt.Errorf("Failed to convert %q to UUID: %v",
			s.siteUUID, err)
	}
	newCmd, oldCmd, err := dbq.handle.CommandCancel(ctx, u, cmdID)
	if err != nil {
		if _, ok := err.(appliancedb.NotFoundError); ok {
			slog.Warnf("%s:%d cancellation for unknown command",
				s.siteUUID, cmdID)
			rval.Response = cfgmsg.ConfigResponse_NOCMD
			return rval, nil
		}
		return nil, err
	}
	slog.Debugf("cancel(%s:%d)", newCmd.UUID, newCmd.ID)
	dbq.cleanup(ctx, s)

	switch {
	case oldCmd.State == "ENQD" || oldCmd.State == "CNCL":
		// command is still queued or already canceled, so we can
		// cancel it
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

func (dbq *dbCmdQueue) complete(ctx context.Context, s *siteState, rval *cfgmsg.ConfigResponse) error {
	_, slog := daemonutils.EndpointLogger(ctx)
	cmdID := rval.CmdID
	jsonResp, err := json.Marshal(rval)
	if err != nil {
		slog.Errorf("Unable to marshal config response for command %d: %v",
			cmdID, err)
		return err
	}
	u, err := uuid.FromString(s.siteUUID)
	if err != nil {
		return fmt.Errorf("Failed to convert %q to UUID: %v",
			s.siteUUID, err)
	}
	newCmd, oldCmd, err := dbq.handle.CommandComplete(ctx, u, cmdID, jsonResp)
	if err != nil {
		if _, ok := err.(appliancedb.NotFoundError); ok {
			slog.Warnf("%s:%d completion for unknown command",
				s.siteUUID, cmdID)
			return nil
		}
		return err
	}
	slog.Debugf("complete(%s:%d)", newCmd.UUID, newCmd.ID)
	dbq.cleanup(ctx, s)

	if !oldCmd.DoneTime.Valid {
		var cfgQuery cfgmsg.ConfigQuery
		err = json.Unmarshal(newCmd.Query, &cfgQuery)
		if err != nil {
			// This is basically a "can't happen" since the db should never
			// be populated with an invalid cmd.  Don't return this
			// as an error; there's nothing the client can do to correct.
			slog.Errorf("Unable to unmarshal query for command %s:%d: %v",
				newCmd.UUID, newCmd.ID, err)
			return nil
		}

		// Responses to GET operations are used to maintain cloud caches
		// of appliance state.
		if rval.Response == cfgmsg.ConfigResponse_OK && isGetCommand(&cfgQuery) {
			s.updateCaches(ctx, cfgQuery.Ops[0].Property, rval.Value)
		}
	} else {
		slog.Infof("%s:%d multiple completions - last at %s",
			s.siteUUID, cmdID, oldCmd.DoneTime.Time.Format(time.RFC3339))
	}
	return nil
}

func (dbq *dbCmdQueue) cleanup(ctx context.Context, s *siteState) {
	u, err := uuid.FromString(s.siteUUID)
	if err != nil {
		return
	}

	_, slog := daemonutils.EndpointLogger(ctx)

	go func() {
		del, err := dbq.handle.CommandDelete(context.Background(), u, int64(*cqMax))
		if err != nil {
			slog.Errorf("Failed to delete commands from queue for %s: %v",
				s.siteUUID, err)
		} else {
			slog.Debugf("Deleted %d commands from queue for %s",
				del, s.siteUUID)
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

