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
	"time"

	"bg/cloud_rpc"

	"github.com/satori/uuid"
)

type eventManager interface {
	InsertHeartbeatIngest(context.Context, *HeartbeatIngest) error
	InsertSiteNetException(context.Context, uuid.UUID, time.Time, string, *uint64, string) error
}

// HeartbeatIngest represents a row in the heartbeat_ingest table.  In this
// case "ingest" means that we record heartbeats into this table for later
// coalescing by another process.
type HeartbeatIngest struct {
	IngestID      uint64
	ApplianceUUID uuid.UUID
	SiteUUID      uuid.UUID
	BootTS        time.Time
	RecordTS      time.Time
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
