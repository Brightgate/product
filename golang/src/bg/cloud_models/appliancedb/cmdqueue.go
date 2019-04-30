/*
 * COPYRIGHT 2019 Brightgate Inc. All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package appliancedb

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/guregu/null"
	"github.com/satori/uuid"
)

type commandQueue interface {
	CommandSearch(context.Context, uuid.UUID, int64) (*SiteCommand, error)
	CommandSubmit(context.Context, uuid.UUID, *SiteCommand) error
	CommandFetch(context.Context, uuid.UUID, int64, uint32) ([]*SiteCommand, error)
	CommandAudit(context.Context, uuid.NullUUID, int64, uint32) ([]*SiteCommand, error)
	CommandCancel(context.Context, uuid.UUID, int64) (*SiteCommand, *SiteCommand, error)
	CommandComplete(context.Context, uuid.UUID, int64, []byte) (*SiteCommand, *SiteCommand, error)
	CommandDelete(context.Context, uuid.UUID, int64) (int64, error)
}

// SiteCommand represents an entry in the persisted command queue.
type SiteCommand struct {
	UUID         uuid.UUID `json:"site_uuid" db:"site_uuid"`
	ID           int64     `json:"id" db:"id"`
	EnqueuedTime time.Time `json:"enq_ts" db:"enq_ts"`
	SentTime     null.Time `json:"sent_ts" db:"sent_ts"`
	NResent      null.Int  `json:"resent_n" db:"resent_n"`
	DoneTime     null.Time `json:"done_ts" db:"done_ts"`
	State        string    `json:"state" db:"state"`
	Query        []byte    `json:"config_query" db:"config_query"`
	Response     []byte    `json:"config_response" db:"config_response"`
}

// CommandSearch returns the SiteCommand, if any, in the command queue for the
// given command ID and site UUID.
func (db *ApplianceDB) CommandSearch(ctx context.Context, u uuid.UUID, cmdID int64) (*SiteCommand, error) {
	row := db.QueryRowContext(ctx,
		`SELECT * FROM site_commands WHERE site_uuid=$1 AND id=$2`, u, cmdID)
	var cmd SiteCommand
	var query, response []byte
	err := row.Scan(&cmd.ID, &cmd.UUID, &cmd.EnqueuedTime, &cmd.SentTime,
		&cmd.NResent, &cmd.DoneTime, &cmd.State, &query, &response)
	switch err {
	case sql.ErrNoRows:
		return nil, NotFoundError{"command not found"}
	case nil:
		cmd.Query, cmd.Response = copyQueryResponse(query, response)
		return &cmd, nil
	default:
		panic(err)
	}
}

// CommandSubmit adds a command to the command queue, and returns its ID.
func (db *ApplianceDB) CommandSubmit(ctx context.Context, u uuid.UUID, cmd *SiteCommand) error {
	rows, err := db.QueryContext(ctx,
		`INSERT INTO site_commands
		 (site_uuid, enq_ts, config_query)
		 VALUES ($1, $2, $3)
		 RETURNING id`,
		u,
		cmd.EnqueuedTime,
		cmd.Query)
	if err != nil {
		return err
	}
	defer rows.Close()
	if !rows.Next() {
		if err = rows.Err(); err != nil {
			return err
		}
		// This is probably impossible.
		return fmt.Errorf("INSERT INTO site_commands didn't insert?")
	}
	var id int64
	if err = rows.Scan(&id); err != nil {
		return err
	}
	cmd.ID = id
	if rows.Next() {
		// This should be impossible.
		return fmt.Errorf("EnqueueCommand inserted more than one row?")
	}
	return nil
}

func copyQueryResponse(query, response []byte) ([]byte, []byte) {
	query2 := make([]byte, len(query))
	copy(query2, query)
	response2 := make([]byte, len(response))
	copy(response2, response)
	return query2, response2
}

// CommandFetch returns from the command queue up to max commands, sorted by ID,
// for the appliance referenced by the UUID u, with a minimum ID of start.
func (db *ApplianceDB) CommandFetch(ctx context.Context, u uuid.UUID, start int64, max uint32) ([]*SiteCommand, error) {
	cmds := make([]*SiteCommand, 0)
	// In order that two concurrent fetch requests don't grab the same
	// command, we need to use SKIP LOCKED per the advice in
	// https://blog.2ndquadrant.com/what-is-select-skip-locked-for-in-postgresql-9-5/
	// https://dba.stackexchange.com/questions/69471/postgres-update-limit-1
	// We can't use a correlated subquery as suggested in the latter because
	// we're not always limiting the result of the subquery to one row.  We
	// can't use a plain subquery because the LIMIT might not be honored.
	rows, err := db.QueryContext(ctx,
		`WITH old AS (
		     SELECT id, sent_ts, state
		     FROM site_commands
		     WHERE site_uuid = $1 AND
		           state IN ('ENQD', 'WORK') AND
		           id > $2
		     ORDER BY id
		     LIMIT $3
		     FOR UPDATE SKIP LOCKED
		 )
		 UPDATE site_commands new
		 SET state = 'WORK',
		     sent_ts = now(),
		     resent_n = CASE
		         WHEN old.state = 'WORK' THEN COALESCE(resent_n, 0) + 1
		     END
		 FROM old
		 WHERE new.id = old.id
		 RETURNING new.id, new.enq_ts, new.sent_ts, new.resent_n,
		           new.done_ts, new.state, new.config_query,
		           new.config_response`,
		u, start, max)
	if err != nil {
		return cmds, err
	}
	defer rows.Close()
	var query, response []byte
	for rows.Next() {
		cmd := &SiteCommand{}
		if err = rows.Scan(&cmd.ID, &cmd.EnqueuedTime, &cmd.SentTime,
			&cmd.NResent, &cmd.DoneTime, &cmd.State, &query,
			&response); err != nil {
			// We might be returning a partial result here, but
			// we can't assume that further calls to Scan() might
			// succeed, and don't return non-contiguous commands.
			return cmds, err
		}
		cmd.Query, cmd.Response = copyQueryResponse(query, response)
		cmds = append(cmds, cmd)
	}
	err = rows.Err()
	// This might also be a partial result.
	return cmds, err
}

// CommandAudit returns all commands for a UUID, regardless of state; it is for
// auditing the appliance command queue.  The argument `u` is a NullUUID so that
// you can pass in a NullUUID with the `.Valid` member set to false and get back
// commands not specific to any site.
//
// Care must be used to be sure that public consumers of this interface are not
// allowed to pass a nulled UUID, which would allow access to all commands.
func (db *ApplianceDB) CommandAudit(ctx context.Context, u uuid.NullUUID, start int64, max uint32) ([]*SiteCommand, error) {
	cmds := make([]*SiteCommand, 0)

	err := db.SelectContext(ctx, &cmds,
		`SELECT *
		     FROM site_commands
		     WHERE ($1::uuid IS NULL OR site_uuid = $1) AND id > $2
		     ORDER BY id
		     LIMIT $3`,
		u, start, max)
	return cmds, err
}

// commandFinish moves the command cmdID to a "done" state -- either done or
// canceled -- and returns both the old and new commands.
func (db *ApplianceDB) commandFinish(ctx context.Context, siteUUID uuid.UUID, cmdID int64, resp []byte) (*SiteCommand, *SiteCommand, error) {
	// We need to move the state to DONE.  In addition, we need to retrieve
	// the old state and return that so the caller can understand what
	// transition (if any) actually happened.  This operation is slightly
	// different from completion because config_response remains empty.
	//
	// https://stackoverflow.com/questions/11532550/atomic-update-select-in-postgres
	// https://stackoverflow.com/questions/7923237/return-pre-update-column-values-using-sql-only-postgresql-version
	state := "DONE"
	if resp == nil {
		state = "CNCL"
	}
	row := db.QueryRowContext(ctx,
		`UPDATE site_commands new
		 SET state = $3, done_ts = now(), config_response = $4
		 FROM (SELECT * FROM site_commands WHERE site_uuid=$1 AND id=$2 FOR UPDATE) old
		 WHERE new.id = old.id
		 RETURNING old.*, new.*`, siteUUID, cmdID, state, resp)
	var newCmd, oldCmd SiteCommand
	var oquery, nquery, oresponse, nresponse []byte
	if err := row.Scan(&oldCmd.ID, &oldCmd.UUID, &oldCmd.EnqueuedTime,
		&oldCmd.SentTime, &oldCmd.NResent, &oldCmd.DoneTime, &oldCmd.State,
		&oquery, &oresponse, &newCmd.ID, &newCmd.UUID,
		&newCmd.EnqueuedTime, &newCmd.SentTime, &newCmd.NResent,
		&newCmd.DoneTime, &newCmd.State, &nquery, &nresponse); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil, NotFoundError{fmt.Sprintf("Could not find command ID %d", cmdID)}
		}
		return nil, nil, err
	}
	oldCmd.Query, oldCmd.Response = copyQueryResponse(oquery, oresponse)
	newCmd.Query, newCmd.Response = copyQueryResponse(nquery, nresponse)
	return &newCmd, &oldCmd, nil
}

// CommandCancel cancels the command cmdID, returning both the old and new
// commands.
func (db *ApplianceDB) CommandCancel(ctx context.Context, siteUUID uuid.UUID, cmdID int64) (*SiteCommand, *SiteCommand, error) {
	return db.commandFinish(ctx, siteUUID, cmdID, nil)
}

// CommandComplete marks the command cmdID done, setting the response column and
// returning both the old and new commands.
func (db *ApplianceDB) CommandComplete(ctx context.Context, siteUUID uuid.UUID, cmdID int64, resp []byte) (*SiteCommand, *SiteCommand, error) {
	return db.commandFinish(ctx, siteUUID, cmdID, resp)
}

// CommandDelete removes completed and canceled commands from an appliance's
// queue, keeping only the `keep` newest.  It returns the number of commands
// deleted.
func (db *ApplianceDB) CommandDelete(ctx context.Context, u uuid.UUID, keep int64) (int64, error) {
	// https://stackoverflow.com/questions/578867/sql-query-delete-all-records-from-the-table-except-latest-n/8303440
	// https://stackoverflow.com/questions/2251567/how-to-get-the-number-of-deleted-rows-in-postgresql/22546994
	row := db.QueryRowContext(ctx,
		`WITH deleted AS (
		     DELETE FROM site_commands
		     WHERE id <= (
		         SELECT id
		         FROM (
		             SELECT id
		             FROM site_commands
		             WHERE site_uuid = $1 AND state IN ('DONE', 'CNCL')
		             ORDER BY id DESC
		             LIMIT 1 OFFSET $2
		         ) AS junk -- subqueries in FROM must have an alias
		     )
		     RETURNING id
		 )
		 SELECT count(id)
		 FROM deleted`, u, keep)
	var numDeleted int64
	err := row.Scan(&numDeleted)
	return numDeleted, err
}
