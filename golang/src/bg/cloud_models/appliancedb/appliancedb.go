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
	"io/ioutil"
	"path/filepath"
	"strings"
	"time"

	"github.com/guregu/null"
	// As per pq documentation
	_ "github.com/lib/pq"
	"github.com/pkg/errors"
	"github.com/satori/uuid"
)

// DataStore facilitates mocking the database
// See http://www.alexedwards.net/blog/organising-database-access
type DataStore interface {
	LoadSchema(context.Context, string) error
	AllApplianceIDs(context.Context) ([]ApplianceID, error)
	ApplianceIDByClientID(context.Context, string) (*ApplianceID, error)
	ApplianceIDByUUID(context.Context, uuid.UUID) (*ApplianceID, error)
	UpsertApplianceID(context.Context, *ApplianceID) error
	KeysByUUID(context.Context, uuid.UUID) ([]AppliancePubKey, error)
	InsertHeartbeatIngest(context.Context, *HeartbeatIngest) error
	CloudStorageByUUID(context.Context, uuid.UUID) (*ApplianceCloudStorage, error)
	UpsertCloudStorage(context.Context, uuid.UUID, *ApplianceCloudStorage) error
	Ping() error
	Close() error
}

// ApplianceDB implements DataStore with the actual DB backend
// sql.DB implements Ping() and Close()
type ApplianceDB struct {
	*sql.DB
}

// ApplianceID represents the identity "map" for an Appliance.  This is
// a table with the various forms of unique identity that we track for
// an appliance.
type ApplianceID struct {
	// CloudUUID is used as the primary key for tracking an appliance
	// across cloud properties
	CloudUUID uuid.UUID `json:"cloud_uuid"`

	// System Identification Intrinsic to the Hardware
	SystemReprMAC      null.String `json:"system_repr_mac"`
	SystemReprHWSerial null.String `json:"system_repr_hwserial"`

	// Google Cloud Identification
	GCPProject string `json:"gcp_project"`
	GCPRegion  string `json:"gcp_region"`

	// Appliance Registry name and ID in the Registry
	ApplianceReg   string `json:"appliance_reg"`
	ApplianceRegID string `json:"appliance_reg_id"`
}

// AppliancePubKey represents one of the public keys for an Appliance.
type AppliancePubKey struct {
	ID         uint64    `json:"id"`
	Format     string    `json:"format"`
	Key        string    `json:"key"`
	Expiration null.Time `json:"expiration"`
}

// ApplianceCloudStorage represents cloud storage information for an Appliance.
type ApplianceCloudStorage struct {
	Bucket   string `json:"bucket"`
	Provider string `json:"provider"`
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

// LoadSchema loads the SQL schema files from a directory.  ioutil.ReadDir sorts
// the input, ensuring the schema is loaded in the right sequence.
// XXX: Not sure this is the right interface in the right place.  Possibly an
// array of io.Readers would be better?
func (db *ApplianceDB) LoadSchema(ctx context.Context, schemaDir string) error {
	files, err := ioutil.ReadDir(schemaDir)
	if err != nil {
		return errors.Wrap(err, "could not scan schema dir")
	}

	for _, file := range files {
		bytes, err := ioutil.ReadFile(filepath.Join(schemaDir, file.Name()))
		if err != nil {
			return errors.Wrap(err, "failed to read sql")
		}
		_, err = db.ExecContext(ctx, string(bytes))
		if err != nil {
			return errors.Wrap(err, "failed to exec sql")
		}
	}
	return nil
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
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
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

// KeysByUUID returns the public keys (may be none) associated with the Appliance cloud UUID
func (db *ApplianceDB) KeysByUUID(ctx context.Context, u uuid.UUID) ([]AppliancePubKey, error) {
	keys := make([]AppliancePubKey, 0)
	rows, err := db.QueryContext(ctx,
		"SELECT id, format, key, expiration FROM appliance_pubkey WHERE cloud_uuid=$1", u)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var key AppliancePubKey
		err = rows.Scan(&key.ID,
			&key.Format,
			&key.Key,
			&key.Expiration)
		if err != nil {
			panic(err)
		}
		keys = append(keys, key)
	}
	return keys, nil
}

// CloudStorageByUUID selects Cloud Storage Information for an Appliance using
// its Cloud UUID
func (db *ApplianceDB) CloudStorageByUUID(ctx context.Context,
	u uuid.UUID) (*ApplianceCloudStorage, error) {
	var stor ApplianceCloudStorage

	row := db.QueryRowContext(ctx,
		"SELECT bucket, provider FROM appliance_cloudstorage WHERE cloud_uuid=$1", u)
	err := row.Scan(&stor.Bucket, &stor.Provider)
	switch err {
	case sql.ErrNoRows:
		return nil, NotFoundError{fmt.Sprintf("CloudStorageByUUID: Couldn't find bucket for %v", u)}
	case nil:
		return &stor, nil
	default:
		panic(err)
	}
}

// UpsertCloudStorage inserts or updates a CloudStorage Record
func (db *ApplianceDB) UpsertCloudStorage(ctx context.Context,
	u uuid.UUID, stor *ApplianceCloudStorage) error {

	_, err := db.ExecContext(ctx,
		`INSERT INTO appliance_cloudstorage
		 VALUES ($1, $2, $3)
		 ON CONFLICT (cloud_uuid) DO UPDATE
		 SET (bucket,
		      provider) = (
		      EXCLUDED.bucket,
		      EXCLUDED.provider)`,
		u.String(),
		stor.Bucket,
		stor.Provider)
	return err
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
