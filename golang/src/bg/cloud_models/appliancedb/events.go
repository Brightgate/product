/*
 * Copyright 2019 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package appliancedb

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"bg/cloud_rpc"

	"github.com/satori/uuid"
)

type eventManager interface {
	InsertHeartbeatIngest(context.Context, *HeartbeatIngest) error
	LatestHeartbeatBySiteUUID(context.Context, uuid.UUID) (*HeartbeatIngest, error)
	InsertSiteNetException(context.Context, uuid.UUID, time.Time, string, *uint64, string) error
}

// HeartbeatIngest represents a row in the heartbeat_ingest table.  In this
// case "ingest" means that we record heartbeats into this table for later
// coalescing by another process.
type HeartbeatIngest struct {
	IngestID      uint64    `db:"ingest_id"`
	ApplianceUUID uuid.UUID `db:"appliance_uuid"`
	SiteUUID      uuid.UUID `db:"site_uuid"`
	BootTS        time.Time `db:"boot_ts"`
	RecordTS      time.Time `db:"record_ts"`
}

// InsertHeartbeatIngest adds a row to the heartbeat_ingest table.
func (db *ApplianceDB) InsertHeartbeatIngest(ctx context.Context, heartbeat *HeartbeatIngest) error {
	_, err := db.ExecContext(ctx,
		"INSERT INTO heartbeat_ingest VALUES (DEFAULT, $1, $2, $3, $4)",
		heartbeat.ApplianceUUID,
		heartbeat.SiteUUID,
		heartbeat.BootTS,
		heartbeat.RecordTS)
	return err
}

// LatestHeartbeatBySiteUUID returns the most recently ingested heartbeat for
// the given site.
func (db *ApplianceDB) LatestHeartbeatBySiteUUID(ctx context.Context, site uuid.UUID) (*HeartbeatIngest, error) {
	var heartbeat HeartbeatIngest
	err := db.GetContext(ctx, &heartbeat,
		"SELECT * from heartbeat_ingest WHERE site_uuid=$1 ORDER BY ingest_id DESC LIMIT 1", site)
	switch err {
	case sql.ErrNoRows:
		return nil, NotFoundError{fmt.Sprintf(
			"LatestHeartbeatBySiteUUID: No heartbeats for %v", site)}
	case nil:
		return &heartbeat, nil
	default:
		panic(err)
	}
}

// SiteNetException represents a row in the site_net_exception table.
type SiteNetException struct {
	SiteUUID  uuid.UUID
	Exception *cloud_rpc.NetException
}

// InsertSiteNetException adds a row to the site_net_exception table.
func (db *ApplianceDB) InsertSiteNetException(ctx context.Context, siteUUID uuid.UUID, ts time.Time, reason string, mac *uint64, exception string) error {
	_, err := db.ExecContext(ctx,
		"INSERT INTO site_net_exception VALUES (DEFAULT, $1, $2, $3, $4, $5)",
		siteUUID, ts, reason, mac, exception)
	return err
}

