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

// DBX describes the interface common to sql.DB and sql.Tx.
type DBX interface {
	ExecContext(context.Context, string, ...interface{}) (sql.Result, error)
	QueryRowContext(context.Context, string, ...interface{}) *sql.Row
	QueryContext(context.Context, string, ...interface{}) (*sql.Rows, error)
}

// DataStore facilitates mocking the database
// See http://www.alexedwards.net/blog/organising-database-access
type DataStore interface {
	LoadSchema(context.Context, string) error
	AllApplianceIDs(context.Context) ([]ApplianceID, error)
	ApplianceIDByClientID(context.Context, string) (*ApplianceID, error)
	ApplianceIDByUUID(context.Context, uuid.UUID) (*ApplianceID, error)
	InsertApplianceID(context.Context, *ApplianceID) error
	InsertApplianceIDTx(context.Context, DBX, *ApplianceID) error
	KeysByUUID(context.Context, uuid.UUID) ([]AppliancePubKey, error)
	InsertApplianceKeyTx(context.Context, DBX, uuid.UUID, *AppliancePubKey) error
	InsertHeartbeatIngest(context.Context, *HeartbeatIngest) error
	CloudStorageByUUID(context.Context, uuid.UUID) (*ApplianceCloudStorage, error)
	UpsertCloudStorage(context.Context, uuid.UUID, *ApplianceCloudStorage) error
	ConfigStoreByUUID(context.Context, uuid.UUID) (*ApplianceConfigStore, error)
	UpsertConfigStore(context.Context, uuid.UUID, *ApplianceConfigStore) error

	CommandSearch(context.Context, int64) (*ApplianceCommand, error)
	CommandSubmit(context.Context, uuid.UUID, *ApplianceCommand) error
	CommandFetch(context.Context, uuid.UUID, int64, uint32) ([]*ApplianceCommand, error)
	CommandCancel(context.Context, int64) (*ApplianceCommand, *ApplianceCommand, error)
	CommandComplete(context.Context, int64, []byte) (*ApplianceCommand, *ApplianceCommand, error)
	CommandDelete(context.Context, uuid.UUID, int64) (int64, error)

	Ping() error
	Close() error

	BeginTx(context.Context) (*sql.Tx, error)
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

// ApplianceConfigStore represents the configuration storage information for an
// Appliance.
type ApplianceConfigStore struct {
	RootHash  []byte    `json:"roothash"`
	TimeStamp time.Time `json:"timestamp"`
	Config    []byte    `json:"config"`
}

// ApplianceCommand represents an entry in the persisted command queue.
type ApplianceCommand struct {
	UUID         uuid.UUID `json:"cloud_uuid"`
	ID           int64     `json:"id"`
	EnqueuedTime time.Time `json:"enq_ts"`
	SentTime     null.Time `json:"sent_ts"`
	NResent      null.Int  `json:"resent_n"`
	DoneTime     null.Time `json:"done_ts"`
	State        string    `json:"state"`
	Query        []byte    `json:"config_query"`
	Response     []byte    `json:"config_response"`
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
		// Mostly to not load vim's .swp files.
		if !strings.HasSuffix(file.Name(), ".sql") {
			continue
		}
		path := filepath.Join(schemaDir, file.Name())
		bytes, err := ioutil.ReadFile(path)
		if err != nil {
			return errors.Wrapf(err, "failed to read sql in file %s", path)
		}
		_, err = db.ExecContext(ctx, string(bytes))
		if err != nil {
			return errors.Wrapf(err, "failed to exec sql in file %s", path)
		}
	}
	return nil
}

// BeginTx creates a transaction in the database.
func (db *ApplianceDB) BeginTx(ctx context.Context) (*sql.Tx, error) {
	tx, err := db.DB.BeginTx(ctx, nil)
	return tx, err
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

// InsertApplianceID inserts an ApplianceID.
func (db *ApplianceDB) InsertApplianceID(ctx context.Context,
	id *ApplianceID) error {
	return db.InsertApplianceIDTx(ctx, nil, id)
}

// InsertApplianceIDTx inserts an ApplianceID, possibly inside a transaction.
func (db *ApplianceDB) InsertApplianceIDTx(ctx context.Context, dbx DBX,
	id *ApplianceID) error {

	if dbx == nil {
		dbx = db
	}
	_, err := dbx.ExecContext(ctx,
		`INSERT INTO appliance_id_map
		 (cloud_uuid,
		      system_repr_mac,
		      system_repr_hwserial,
		      gcp_project,
		      gcp_region,
		      appliance_reg,
		      appliance_reg_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		id.CloudUUID,
		id.SystemReprMAC,
		id.SystemReprHWSerial,
		id.GCPProject,
		id.GCPRegion,
		id.ApplianceReg,
		id.ApplianceRegID)
	return err
}

// InsertApplianceKeyTx adds an appliance's public key to the registry.
func (db *ApplianceDB) InsertApplianceKeyTx(ctx context.Context, dbx DBX, u uuid.UUID, key *AppliancePubKey) error {
	if dbx == nil {
		dbx = db
	}
	_, err := dbx.ExecContext(ctx,
		`INSERT INTO appliance_pubkey
		 (cloud_uuid, format, key)
		 VALUES ($1, $2, $3)`,
		u, key.Format, key.Key)
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

// ConfigStoreByUUID returns the configuration of the appliance referred to by
// the UUID.
func (db *ApplianceDB) ConfigStoreByUUID(ctx context.Context,
	u uuid.UUID) (*ApplianceConfigStore, error) {
	var cfg ApplianceConfigStore

	row := db.QueryRowContext(ctx,
		"SELECT root_hash, ts, config FROM appliance_config_store WHERE cloud_uuid=$1", u)
	var hash, config []byte
	err := row.Scan(&hash, &cfg.TimeStamp, &config)
	// The memory for reference types is owned by the driver, so we have to
	// copy the data explicitly: https://golang.org/pkg/database/sql/#Scanner
	cfg.RootHash = make([]byte, len(hash))
	copy(cfg.RootHash, hash)
	cfg.Config = make([]byte, len(config))
	copy(cfg.Config, config)
	switch err {
	case sql.ErrNoRows:
		return nil, NotFoundError{fmt.Sprintf(
			"ConfigStoreByUUID: Couldn't find config for %v", u)}
	case nil:
		return &cfg, nil
	default:
		panic(err)
	}
}

// UpsertConfigStore inserts or updates a configuration store record.
func (db *ApplianceDB) UpsertConfigStore(ctx context.Context, u uuid.UUID,
	cfg *ApplianceConfigStore) error {
	_, err := db.ExecContext(ctx,
		`INSERT INTO appliance_config_store
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (cloud_uuid) DO UPDATE
		 SET (root_hash,
		      ts,
		      config) = (
		      EXCLUDED.root_hash,
		      EXCLUDED.ts,
		      EXCLUDED.config)`,
		u.String(),
		cfg.RootHash,
		cfg.TimeStamp,
		cfg.Config)
	return err
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

// CommandSearch returns the ApplianceCommand, if any, in the command queue for
// the given command ID.
func (db *ApplianceDB) CommandSearch(ctx context.Context, cmdID int64) (*ApplianceCommand, error) {
	row := db.QueryRowContext(ctx,
		`SELECT * FROM appliance_commands WHERE id=$1`, cmdID)
	var cmd ApplianceCommand
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
func (db *ApplianceDB) CommandSubmit(ctx context.Context, u uuid.UUID, cmd *ApplianceCommand) error {
	rows, err := db.QueryContext(ctx,
		`INSERT INTO appliance_commands
		 (cloud_uuid, enq_ts, config_query)
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
		return fmt.Errorf("INSERT INTO appliance_commands didn't insert?")
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
func (db *ApplianceDB) CommandFetch(ctx context.Context, u uuid.UUID, start int64, max uint32) ([]*ApplianceCommand, error) {
	cmds := make([]*ApplianceCommand, 0)
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
		     FROM appliance_commands
		     WHERE cloud_uuid = $1 AND
		           state IN ('ENQD', 'WORK') AND
		           id > $2
		     ORDER BY id
		     LIMIT $3
		     FOR UPDATE SKIP LOCKED
		 )
		 UPDATE appliance_commands new
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
		cmd := &ApplianceCommand{}
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

// commandFinish moves the command cmdID to a "done" state -- either done or
// canceled -- and returns both the old and new commands.
func (db *ApplianceDB) commandFinish(ctx context.Context, cmdID int64, resp []byte) (*ApplianceCommand, *ApplianceCommand, error) {
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
		`UPDATE appliance_commands new
		 SET state = $2, done_ts = now(), config_response = $3
		 FROM (SELECT * FROM appliance_commands WHERE id=$1 FOR UPDATE) old
		 WHERE new.id = old.id
		 RETURNING old.*, new.*`, cmdID, state, resp)
	var newCmd, oldCmd ApplianceCommand
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
func (db *ApplianceDB) CommandCancel(ctx context.Context, cmdID int64) (*ApplianceCommand, *ApplianceCommand, error) {
	return db.commandFinish(ctx, cmdID, nil)
}

// CommandComplete marks the command cmdID done, setting the response column and
// returning both the old and new commands.
func (db *ApplianceDB) CommandComplete(ctx context.Context, cmdID int64, resp []byte) (*ApplianceCommand, *ApplianceCommand, error) {
	return db.commandFinish(ctx, cmdID, resp)
}

// CommandDelete removes completed and canceled commands from an appliance's
// queue, keeping only the `keep` newest.  It returns the number of commands
// deleted.
func (db *ApplianceDB) CommandDelete(ctx context.Context, u uuid.UUID, keep int64) (int64, error) {
	// https://stackoverflow.com/questions/578867/sql-query-delete-all-records-from-the-table-except-latest-n/8303440
	// https://stackoverflow.com/questions/2251567/how-to-get-the-number-of-deleted-rows-in-postgresql/22546994
	row := db.QueryRowContext(ctx,
		`WITH deleted AS (
		     DELETE FROM appliance_commands
		     WHERE id <= (
		         SELECT id
		         FROM (
		             SELECT id
		             FROM appliance_commands
		             WHERE cloud_uuid = $1 AND state IN ('DONE', 'CNCL')
		             ORDER BY id DESC
		             LIMIT 1 OFFSET $2
		         ) foo -- yes, this is necessary
		     )
		     RETURNING id
		 )
		 SELECT count(id)
		 FROM deleted`, u, keep)
	var numDeleted int64
	err := row.Scan(&numDeleted)
	return numDeleted, err
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
