/*
 * COPYRIGHT 2018 Brightgate Inc. All rights reserved.
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
	"strings"
	"time"

	"github.com/lib/pq"
	"github.com/satori/uuid"
)

// DataStore facilitates mocking the database
// See http://www.alexedwards.net/blog/organising-database-access
type DataStore interface {
	AllApplianceIDs(context.Context) ([]ApplianceID, error)
	ApplianceIDByClientID(context.Context, string) (*ApplianceID, error)
	ApplianceIDByUUID(context.Context, uuid.UUID) (*ApplianceID, error)
	UpsertApplianceID(context.Context, *ApplianceID) error
	KeysByUUID(context.Context, uuid.UUID) ([]AppliancePubKey, error)
	InsertHeartbeatIngest(context.Context, *HeartbeatIngest) error
	Ping() error
	Close()
}

// ApplianceDB implements DataStore with the actual DB backend
type ApplianceDB struct {
	*sql.DB
}

// ApplianceID represents the identity "map" for an Appliance.  This is
// a table with the various forms of unique identity that we track for
// an appliance.
type ApplianceID struct {
	// CloudUUID is used as the primary key for tracking an appliance
	// across cloud properties
	CloudUUID uuid.UUID

	// System Identification Intrinsic to the Hardware
	SystemReprMAC      sql.NullString
	SystemReprHWSerial sql.NullString

	// Google Cloud Identification
	GCPProject string
	GCPRegion  string

	// Appliance Registry name and ID in the Registry
	ApplianceReg   string
	ApplianceRegID string
}

// AppliancePubKey represents one of the public keys for an Appliance.
type AppliancePubKey struct {
	ID         uint64
	Format     string
	Key        string
	Expiration pq.NullTime
}

// NotFoundError is returned when the requested resource is not present in the
// database.
type NotFoundError struct {
	s string
}

func (e NotFoundError) Error() string {
	return e.s
}

func (i *ApplianceID) String() string {
	var hwser = "-"
	var mac = "-"
	if i.SystemReprHWSerial.Valid {
		hwser = i.SystemReprHWSerial.String
	}
	if i.SystemReprMAC.Valid {
		mac = i.SystemReprMAC.String
	}
	return fmt.Sprintf("ApplianceID<u=%s hwser=%s mac=%s gcpID=%s>",
		i.CloudUUID, hwser, mac, i.ClientID())
}

// ClientID returns the canonical "address" of the represented appliance
// The format is borrowed from the conventions used by Google's properties.
func (i *ApplianceID) ClientID() string {
	return fmt.Sprintf("projects/%s/locations/%s/registries/%s/appliances/%s",
		i.GCPProject, i.GCPRegion, i.ApplianceReg, i.ApplianceRegID)
}

// Connect opens a new connection to the DataStore
func Connect(dataSource string) (DataStore, error) {
	sqldb, err := sql.Open("postgres", dataSource)
	if err != nil {
		return nil, err
	}
	// We found that not limiting this can cause problems as Go attempts to
	// open many many connections to the database.  (presumably the cloud
	// sql proxy can't handle massive numbers of connections)
	sqldb.SetMaxOpenConns(16)
	var ds DataStore = &ApplianceDB{sqldb}
	return ds, nil
}

// Close closes the connection to the DataStore
func (db *ApplianceDB) Close() {
	db.DB.Close()
}

var allIDColumns = []string{
	"cloud_uuid",
	"system_repr_mac",
	"system_repr_hwserial",
	"gcp_project",
	"gcp_region",
	"appliance_reg",
	"appliance_reg_id",
}

var allIDColumnsSQL = strings.Join(allIDColumns, ", ")

// AllApplianceIDs returns a complete list of the Appliance IDs in the
// database
func (db *ApplianceDB) AllApplianceIDs(ctx context.Context) ([]ApplianceID, error) {
	var ids []ApplianceID
	rows, err := db.QueryContext(ctx,
		"SELECT "+allIDColumnsSQL+" FROM appliance_id_map")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id ApplianceID
		err = rows.Scan(&id.CloudUUID,
			&id.SystemReprMAC,
			&id.SystemReprHWSerial,
			&id.GCPProject,
			&id.GCPRegion,
			&id.ApplianceReg,
			&id.ApplianceRegID)
		if err != nil {
			panic(err)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func doIDScan(label string, query string, row *sql.Row, id *ApplianceID) error {
	err := row.Scan(&id.CloudUUID,
		&id.SystemReprMAC,
		&id.SystemReprHWSerial,
		&id.GCPProject,
		&id.GCPRegion,
		&id.ApplianceReg,
		&id.ApplianceRegID)
	switch err {
	case sql.ErrNoRows:
		return NotFoundError{fmt.Sprintf("%s: Couldn't find %s",
			label, query)}
	case nil:
		break
	default:
		panic(err)
	}
	return nil
}

// ApplianceIDByUUID selects an ApplianceID using its Cloud UUID
func (db *ApplianceDB) ApplianceIDByUUID(ctx context.Context,
	u uuid.UUID) (*ApplianceID, error) {

	var id ApplianceID
	row := db.QueryRowContext(ctx,
		"SELECT "+allIDColumnsSQL+" FROM appliance_id_map WHERE cloud_uuid=$1", u)
	err := doIDScan("ApplianceIDByUUID", u.String(), row, &id)
	return &id, err
}

// ApplianceIDByClientID selects an ApplianceID using its client ID string
// which is a string of the form:
// projects/<projname>/locations/<region>/registries/<regname>/appliances/<regid>
func (db *ApplianceDB) ApplianceIDByClientID(ctx context.Context, clientID string) (*ApplianceID, error) {

	var id ApplianceID
	row := db.QueryRowContext(ctx,
		"SELECT "+allIDColumnsSQL+` FROM appliance_id_map WHERE
		    concat_ws('/',
			'projects', gcp_project,
		        'locations', gcp_region,
			'registries', appliance_reg,
			'appliances', appliance_reg_id) = $1`, clientID)
	err := doIDScan("ApplianceIDByClientID", clientID, row, &id)
	return &id, err
}

// UpsertApplianceID inserts or updates an ApplianceID.
func (db *ApplianceDB) UpsertApplianceID(ctx context.Context,
	id *ApplianceID) error {

	_, err := db.ExecContext(ctx,
		`INSERT INTO appliance_id_map
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 ON CONFLICT (cloud_uuid) DO UPDATE
		 SET (system_repr_mac,
		      system_repr_hwserial,
		      gcp_project,
		      gcp_region,
		      appliance_reg,
		      appliance_reg_id) = (
		      EXCLUDED.system_repr_mac,
		      EXCLUDED.system_repr_hwserial,
		      EXCLUDED.gcp_project,
		      EXCLUDED.gcp_region,
		      EXCLUDED.appliance_reg,
		      EXCLUDED.appliance_reg_id)`,
		id.CloudUUID,
		id.SystemReprMAC,
		id.SystemReprHWSerial,
		id.GCPProject,
		id.GCPRegion,
		id.ApplianceReg,
		id.ApplianceRegID)
	return err
}

// KeysByUUID returns the public keys associated with the Appliance cloud UUID
func (db *ApplianceDB) KeysByUUID(ctx context.Context, u uuid.UUID) ([]AppliancePubKey, error) {
	keys := make([]AppliancePubKey, 0)
	rows, err := db.QueryContext(ctx,
		"SELECT id, format, key, expiration FROM appliance_pubkey WHERE cloud_uuid=$1", u)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var key AppliancePubKey
		err := rows.Scan(&key.ID,
			&key.Format,
			&key.Key,
			&key.Expiration)
		switch err {
		case sql.ErrNoRows:
			return nil, NotFoundError{fmt.Sprintf("KeysByUUID: Couldn't find keys for %s", u)}
		case nil:
			keys = append(keys, key)
		default:
			panic(err)
		}
	}
	return keys, nil
}

// HeartbeatIngest represents a row in the heartbeat_ingest table.  In this
// case "ingest" means that we record heartbeats into this table for later
// coalescing by another process.
type HeartbeatIngest struct {
	IngestID    uint64
	ApplianceID uuid.UUID
	BootTS      time.Time
	RecordTS    time.Time
}

// InsertHeartbeatIngest adds a row to the heartbeat_ingest table.
func (db *ApplianceDB) InsertHeartbeatIngest(ctx context.Context, heartbeat *HeartbeatIngest) error {
	_, err := db.ExecContext(ctx,
		"INSERT INTO heartbeat_ingest VALUES (DEFAULT, $1, $2, $3)",
		heartbeat.ApplianceID,
		heartbeat.BootTS,
		heartbeat.RecordTS)
	return err
}
