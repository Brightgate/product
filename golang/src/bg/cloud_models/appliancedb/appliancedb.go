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

	// As advised by pq documentation
	_ "github.com/lib/pq"
	"github.com/satori/uuid"
)

// DataStore facilitates mocking the database
// See http://www.alexedwards.net/blog/organising-database-access
type DataStore interface {
	AllApplianceIDs(context.Context) ([]ApplianceID, error)
	ApplianceIDByUUID(context.Context, uuid.UUID) (*ApplianceID, error)
	ApplianceIDByDeviceNumID(context.Context, uint64) (*ApplianceID, error)
	UpsertApplianceID(context.Context, *ApplianceID) error
	InsertUpbeatIngest(context.Context, *UpbeatIngest) error
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
	// CloudUUID is used as the primary key for tracking a system across
	// cloud properties; in the IoT registry this is
	// "net_b10e_iot_cloud_uuid"
	CloudUUID uuid.UUID

	// System Identification Intrinsic to the Hardware
	SystemReprMAC      sql.NullString
	SystemReprHWSerial sql.NullString

	// Google Cloud Device Identification
	GCPIoTProject     string
	GCPIoTRegion      string
	GCPIoTRegistry    string
	GCPIoTDeviceID    string
	GCPIoTDeviceNumID uint64
}

// NotFoundError represents
type NotFoundError struct {
	s string
}

func (e NotFoundError) Error() string {
	return e.s
}

func (i ApplianceID) String() string {
	var hwser = "-"
	var mac = "-"
	if i.SystemReprHWSerial.Valid {
		hwser = i.SystemReprHWSerial.String
	}
	if i.SystemReprMAC.Valid {
		mac = i.SystemReprMAC.String
	}
	gcpID := fmt.Sprintf("%s/%s/%s/{%s,%d}", i.GCPIoTProject, i.GCPIoTRegion,
		i.GCPIoTRegistry, i.GCPIoTDeviceID, i.GCPIoTDeviceNumID)
	return fmt.Sprintf("ApplianceID<u=%s hwser=%s mac=%s gcpID=%s>", i.CloudUUID, hwser, mac, gcpID)
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
	"gcp_iot_project",
	"gcp_iot_region",
	"gcp_iot_registry",
	"gcp_iot_device_id",
	"gcp_iot_device_num_id",
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
		err := rows.Scan(&id.CloudUUID,
			&id.SystemReprMAC,
			&id.SystemReprHWSerial,
			&id.GCPIoTProject,
			&id.GCPIoTRegion,
			&id.GCPIoTRegistry,
			&id.GCPIoTDeviceID,
			&id.GCPIoTDeviceNumID)
		if err != nil {
			panic(err)
		} else {
			ids = append(ids, id)
		}
	}
	return ids, err
}

func doIDScan(label string, query string, row *sql.Row, id *ApplianceID) error {
	err := row.Scan(&id.CloudUUID,
		&id.SystemReprMAC,
		&id.SystemReprHWSerial,
		&id.GCPIoTProject,
		&id.GCPIoTRegion,
		&id.GCPIoTRegistry,
		&id.GCPIoTDeviceID,
		&id.GCPIoTDeviceNumID)
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

// ApplianceIDByDeviceNumID selects an ApplianceID using its Google-assigned
// globally unique numeric ID.
func (db *ApplianceDB) ApplianceIDByDeviceNumID(ctx context.Context,
	numID uint64) (*ApplianceID, error) {

	var id ApplianceID
	row := db.QueryRowContext(ctx,
		"SELECT "+allIDColumnsSQL+" FROM appliance_id_map WHERE gcp_iot_device_num_id=$1", numID)
	err := doIDScan("ApplianceIDByDeviceNumID", string(numID), row, &id)
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
                      gcp_iot_project,
		      gcp_iot_region,
                      gcp_iot_registry,
                      gcp_iot_device_id,
		      gcp_iot_device_num_id) = (
                      EXCLUDED.system_repr_mac,
		      EXCLUDED.system_repr_hwserial,
		      EXCLUDED.gcp_iot_project,
                      EXCLUDED.gcp_iot_region,
                      EXCLUDED.gcp_iot_registry,
                      EXCLUDED.gcp_iot_device_id,
                      EXCLUDED.gcp_iot_device_num_id)`,
		id.CloudUUID,
		id.SystemReprMAC,
		id.SystemReprHWSerial,
		id.GCPIoTProject,
		id.GCPIoTRegion,
		id.GCPIoTRegistry,
		id.GCPIoTDeviceID,
		id.GCPIoTDeviceNumID)
	return err
}

// UpbeatIngest a row in the upbeat_ingest table.  In this case "ingest" means
// that we record upbeats into this table for later coalescing by another
// process.
type UpbeatIngest struct {
	IngestID    uint64
	ApplianceID uuid.UUID
	BootTS      time.Time
	RecordTS    time.Time
}

// InsertUpbeatIngest adds a row to the upbeat_ingest table.
func (db *ApplianceDB) InsertUpbeatIngest(ctx context.Context, upbeat *UpbeatIngest) error {
	_, err := db.ExecContext(ctx,
		"INSERT INTO upbeat_ingest VALUES (DEFAULT, $1, $2, $3)",
		upbeat.ApplianceID,
		upbeat.BootTS,
		upbeat.RecordTS)
	return err
}
