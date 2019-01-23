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
	"io/ioutil"
	"path/filepath"
	"strings"
	"time"

	"github.com/guregu/null"
	"github.com/jmoiron/sqlx"
	// As per pq documentation
	_ "github.com/lib/pq"
	"github.com/pkg/errors"
	"github.com/satori/uuid"
)

// DBX describes the interface common to sqlx.DB and sqlx.Tx.
type DBX interface {
	sqlx.QueryerContext
	sqlx.ExecerContext
	NamedExecContext(context.Context, string, interface{}) (sql.Result, error)
	QueryRowContext(context.Context, string, ...interface{}) *sql.Row
}

// DataStore facilitates mocking the database
// See http://www.alexedwards.net/blog/organising-database-access
type DataStore interface {
	LoadSchema(context.Context, string) error

	InsertCustomerSite(context.Context, *CustomerSite) error
	InsertCustomerSiteTx(context.Context, DBX, *CustomerSite) error
	AllCustomerSites(context.Context) ([]CustomerSite, error)
	CustomerSiteByUUID(context.Context, uuid.UUID) (*CustomerSite, error)
	CustomerSitesByOrganization(context.Context, uuid.UUID) ([]CustomerSite, error)
	CustomerSitesByAccount(context.Context, uuid.UUID) ([]CustomerSite, error)

	AllApplianceIDs(context.Context) ([]ApplianceID, error)
	ApplianceIDByClientID(context.Context, string) (*ApplianceID, error)
	ApplianceIDByUUID(context.Context, uuid.UUID) (*ApplianceID, error)
	InsertApplianceID(context.Context, *ApplianceID) error
	InsertApplianceIDTx(context.Context, DBX, *ApplianceID) error

	InsertApplianceKeyTx(context.Context, DBX, uuid.UUID, *AppliancePubKey) error
	KeysByUUID(context.Context, uuid.UUID) ([]AppliancePubKey, error)

	UpsertCloudStorage(context.Context, uuid.UUID, *SiteCloudStorage) error
	CloudStorageByUUID(context.Context, uuid.UUID) (*SiteCloudStorage, error)

	UpsertConfigStore(context.Context, uuid.UUID, *SiteConfigStore) error
	ConfigStoreByUUID(context.Context, uuid.UUID) (*SiteConfigStore, error)

	CommandSearch(context.Context, int64) (*SiteCommand, error)
	CommandSubmit(context.Context, uuid.UUID, *SiteCommand) error
	CommandFetch(context.Context, uuid.UUID, int64, uint32) ([]*SiteCommand, error)
	CommandCancel(context.Context, int64) (*SiteCommand, *SiteCommand, error)
	CommandComplete(context.Context, int64, []byte) (*SiteCommand, *SiteCommand, error)
	CommandDelete(context.Context, uuid.UUID, int64) (int64, error)

	InsertHeartbeatIngest(context.Context, *HeartbeatIngest) error

	AllOrganizations(context.Context) ([]Organization, error)
	OrganizationByUUID(context.Context, uuid.UUID) (*Organization, error)
	InsertOrganization(context.Context, *Organization) error
	InsertOrganizationTx(context.Context, DBX, *Organization) error

	AllOAuth2OrganizationRules(context.Context) ([]OAuth2OrganizationRule, error)
	OAuth2OrganizationRuleTest(context.Context, string, OAuth2OrgRuleType, string) (*OAuth2OrganizationRule, error)
	InsertOAuth2OrganizationRule(context.Context, *OAuth2OrganizationRule) error
	InsertOAuth2OrganizationRuleTx(context.Context, DBX, *OAuth2OrganizationRule) error

	PersonByUUID(context.Context, uuid.UUID) (*Person, error)
	InsertPerson(context.Context, *Person) error
	InsertPersonTx(context.Context, DBX, *Person) error

	AccountsByOrganization(context.Context, uuid.UUID) ([]Account, error)
	AccountByUUID(context.Context, uuid.UUID) (*Account, error)
	InsertAccount(context.Context, *Account) error
	InsertAccountTx(context.Context, DBX, *Account) error

	OAuth2IdentitiesByAccount(context.Context, uuid.UUID) ([]OAuth2Identity, error)
	InsertOAuth2Identity(context.Context, *OAuth2Identity) error
	InsertOAuth2IdentityTx(context.Context, DBX, *OAuth2Identity) error

	LoginInfoByProviderAndSubject(context.Context, string, string) (*LoginInfo, error)

	InsertOAuth2AccessToken(context.Context, *OAuth2AccessToken) error
	InsertOAuth2AccessTokenTx(context.Context, DBX, *OAuth2AccessToken) error
	UpsertOAuth2RefreshToken(context.Context, *OAuth2RefreshToken) error
	UpsertOAuth2RefreshTokenTx(context.Context, DBX, *OAuth2RefreshToken) error

	Ping() error
	Close() error

	BeginTxx(context.Context, *sql.TxOptions) (*sqlx.Tx, error)
}

// ApplianceDB implements DataStore with the actual DB backend
// sql.DB implements Ping() and Close()
type ApplianceDB struct {
	*sqlx.DB
}

// CustomerSite represents a customer installation of a group of
// Appliances at a single physical location.
type CustomerSite struct {
	UUID             uuid.UUID `db:"uuid"`
	OrganizationUUID uuid.UUID `db:"organization_uuid"`
	Name             string    `db:"name"`
}

// NullSiteUUID is a reserved UUID for appliances which have no associated
// site.  This is expected to be for appliances which are in a "factory"
// registry with no site.
var NullSiteUUID = uuid.Must(uuid.FromString("00000000-0000-0000-0000-000000000000"))

// ApplianceID represents the identity "map" for an Appliance.  This is
// a table with the various forms of unique identity that we track for
// an appliance.
type ApplianceID struct {
	// ApplianceUUID is used as the primary key for tracking an appliance
	// across cloud properties
	ApplianceUUID uuid.UUID `json:"appliance_uuid" db:"appliance_uuid"`

	// SiteUUID is used as the primary key for tracking a customer site
	// across cloud properties
	SiteUUID uuid.UUID `json:"site_uuid" db:"site_uuid"`

	// System Identification Intrinsic to the Hardware
	SystemReprMAC      null.String `json:"system_repr_mac" db:"system_repr_mac"`
	SystemReprHWSerial null.String `json:"system_repr_hwserial" db:"system_repr_hwserial"`

	// Google Cloud Identification
	GCPProject string `json:"gcp_project" db:"gcp_project"`
	GCPRegion  string `json:"gcp_region" db:"gcp_region"`

	// Appliance Registry name and ID in the Registry
	ApplianceReg   string `json:"appliance_reg" db:"appliance_reg"`
	ApplianceRegID string `json:"appliance_reg_id" db:"appliance_reg_id"`
}

// AppliancePubKey represents one of the public keys for an Appliance.
type AppliancePubKey struct {
	ID         uint64    `json:"id"`
	Format     string    `json:"format"`
	Key        string    `json:"key"`
	Expiration null.Time `json:"expiration"`
}

// SiteCloudStorage represents cloud storage information for an Appliance.
type SiteCloudStorage struct {
	Bucket   string `json:"bucket"`
	Provider string `json:"provider"`
}

// SiteConfigStore represents the configuration storage information for an
// Appliance.
type SiteConfigStore struct {
	RootHash  []byte    `json:"roothash"`
	TimeStamp time.Time `json:"timestamp"`
	Config    []byte    `json:"config"`
}

// SiteCommand represents an entry in the persisted command queue.
type SiteCommand struct {
	UUID         uuid.UUID `json:"site_uuid"`
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
		i.ApplianceUUID, hwser, mac, i.ClientID())
}

// ClientID returns the canonical "address" of the represented appliance
// The format is borrowed from the conventions used by Google's properties.
func (i *ApplianceID) ClientID() string {
	return fmt.Sprintf("projects/%s/locations/%s/registries/%s/appliances/%s",
		i.GCPProject, i.GCPRegion, i.ApplianceReg, i.ApplianceRegID)
}

// Connect opens a new connection to the DataStore
func Connect(dataSource string) (DataStore, error) {
	sqldb, err := sqlx.Open("postgres", dataSource)
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

// InsertCustomerSite inserts a record into the customer_site table.
func (db *ApplianceDB) InsertCustomerSite(ctx context.Context,
	cs *CustomerSite) error {
	return db.InsertCustomerSiteTx(ctx, nil, cs)
}

// InsertCustomerSiteTx inserts a record into the customer_site table,
// possibly inside a transaction.
func (db *ApplianceDB) InsertCustomerSiteTx(ctx context.Context, dbx DBX,
	cs *CustomerSite) error {

	if dbx == nil {
		dbx = db
	}
	_, err := dbx.ExecContext(ctx,
		`INSERT INTO customer_site
		 (uuid, organization_uuid, name)
		 VALUES ($1, $2, $3)`,
		cs.UUID,
		cs.OrganizationUUID,
		cs.Name)
	return err
}

// AllCustomerSites returns a complete list of the Customer Sites in the
// database
func (db *ApplianceDB) AllCustomerSites(ctx context.Context) ([]CustomerSite, error) {
	var sites []CustomerSite
	err := db.SelectContext(ctx, &sites,
		"SELECT uuid, organization_uuid, name FROM customer_site")
	if err != nil {
		return nil, err
	}
	return sites, nil
}

// CustomerSiteByUUID selects a CustomerSite using its UUID
func (db *ApplianceDB) CustomerSiteByUUID(ctx context.Context,
	u uuid.UUID) (*CustomerSite, error) {

	var site CustomerSite
	row := db.QueryRowContext(ctx,
		"SELECT uuid, name FROM customer_site WHERE uuid=$1", u)
	err := row.Scan(&site.UUID, &site.Name)
	switch err {
	case sql.ErrNoRows:
		return nil, NotFoundError{fmt.Sprintf(
			"CustomerSiteByUUID: Couldn't find site for %v", u)}
	case nil:
		return &site, nil
	default:
		panic(err)
	}
}

// CustomerSitesByOrganization returns a list of the customer_site
// records for the given organization.
func (db *ApplianceDB) CustomerSitesByOrganization(ctx context.Context,
	orgUUID uuid.UUID) ([]CustomerSite, error) {

	var sites []CustomerSite
	err := db.SelectContext(ctx, &sites,
		`SELECT * FROM customer_site WHERE organization_uuid=$1`,
		orgUUID)
	if err != nil {
		return nil, err
	}
	return sites, nil
}

// CustomerSitesByAccount returns a list of the customer_site
// records for the given Account's organization.
func (db *ApplianceDB) CustomerSitesByAccount(ctx context.Context,
	accountUUID uuid.UUID) ([]CustomerSite, error) {

	var sites []CustomerSite
	err := db.SelectContext(ctx, &sites,
		`SELECT 
		  customer_site.uuid AS uuid,
		  customer_site.organization_uuid AS organization_uuid,
		  customer_site.name AS name
		FROM
		  customer_site
		JOIN
		  account
		  ON account.organization_uuid = customer_site.organization_uuid
		WHERE account.uuid=$1`, accountUUID)
	if err != nil {
		return nil, err
	}
	return sites, nil
}

// AllApplianceIDs returns a complete list of the Appliance IDs in the
// database
func (db *ApplianceDB) AllApplianceIDs(ctx context.Context) ([]ApplianceID, error) {
	var ids []ApplianceID
	err := db.SelectContext(ctx, &ids,
		"SELECT * FROM appliance_id_map")
	if err != nil {
		return nil, err
	}
	return ids, nil
}

// ApplianceIDByUUID selects an ApplianceID using its UUID
func (db *ApplianceDB) ApplianceIDByUUID(ctx context.Context,
	u uuid.UUID) (*ApplianceID, error) {

	var id ApplianceID
	err := db.GetContext(ctx, &id,
		"SELECT * FROM appliance_id_map WHERE appliance_uuid=$1", u)
	switch err {
	case sql.ErrNoRows:
		return nil, NotFoundError{fmt.Sprintf(
			"ApplianceIDByUUID: Couldn't find %s", u)}
	case nil:
		return &id, nil
	default:
		panic(err)
	}
}

// ApplianceIDByClientID selects an ApplianceID using its client ID string,
// which is of the form:
// projects/<projname>/locations/<region>/registries/<regname>/appliances/<regid>
func (db *ApplianceDB) ApplianceIDByClientID(ctx context.Context, clientID string) (*ApplianceID, error) {
	var id ApplianceID
	err := db.GetContext(ctx, &id,
		`SELECT * FROM appliance_id_map
		 WHERE concat_ws('/',
		   'projects', gcp_project,
		   'locations', gcp_region,
		   'registries', appliance_reg,
		   'appliances', appliance_reg_id) = $1`, clientID)
	switch err {
	case sql.ErrNoRows:
		return nil, NotFoundError{fmt.Sprintf(
			"ApplianceIDByClientID: Couldn't find %s", clientID)}
	case nil:
		return &id, nil
	default:
		panic(err)
	}
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
		 (appliance_uuid,
		      site_uuid,
		      system_repr_mac,
		      system_repr_hwserial,
		      gcp_project,
		      gcp_region,
		      appliance_reg,
		      appliance_reg_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		id.ApplianceUUID,
		id.SiteUUID,
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
		 (appliance_uuid, format, key)
		 VALUES ($1, $2, $3)`,
		u, key.Format, key.Key)
	return err
}

// KeysByUUID returns the public keys (may be none) associated with the Appliance cloud UUID
func (db *ApplianceDB) KeysByUUID(ctx context.Context, u uuid.UUID) ([]AppliancePubKey, error) {
	keys := make([]AppliancePubKey, 0)
	rows, err := db.QueryContext(ctx,
		"SELECT id, format, key, expiration FROM appliance_pubkey WHERE appliance_uuid=$1", u)
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
	u uuid.UUID) (*SiteConfigStore, error) {
	var cfg SiteConfigStore

	row := db.QueryRowContext(ctx,
		"SELECT root_hash, ts, config FROM site_config_store WHERE site_uuid=$1", u)
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
	cfg *SiteConfigStore) error {
	_, err := db.ExecContext(ctx,
		`INSERT INTO site_config_store
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (site_uuid) DO UPDATE
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
// its UUID
func (db *ApplianceDB) CloudStorageByUUID(ctx context.Context,
	u uuid.UUID) (*SiteCloudStorage, error) {
	var stor SiteCloudStorage

	row := db.QueryRowContext(ctx,
		"SELECT bucket, provider FROM site_cloudstorage WHERE site_uuid=$1", u)
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
	u uuid.UUID, stor *SiteCloudStorage) error {

	_, err := db.ExecContext(ctx,
		`INSERT INTO site_cloudstorage
		 VALUES ($1, $2, $3)
		 ON CONFLICT (site_uuid) DO UPDATE
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
func (db *ApplianceDB) CommandSearch(ctx context.Context, cmdID int64) (*SiteCommand, error) {
	row := db.QueryRowContext(ctx,
		`SELECT * FROM site_commands WHERE id=$1`, cmdID)
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

// commandFinish moves the command cmdID to a "done" state -- either done or
// canceled -- and returns both the old and new commands.
func (db *ApplianceDB) commandFinish(ctx context.Context, cmdID int64, resp []byte) (*SiteCommand, *SiteCommand, error) {
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
		 SET state = $2, done_ts = now(), config_response = $3
		 FROM (SELECT * FROM site_commands WHERE id=$1 FOR UPDATE) old
		 WHERE new.id = old.id
		 RETURNING old.*, new.*`, cmdID, state, resp)
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
func (db *ApplianceDB) CommandCancel(ctx context.Context, cmdID int64) (*SiteCommand, *SiteCommand, error) {
	return db.commandFinish(ctx, cmdID, nil)
}

// CommandComplete marks the command cmdID done, setting the response column and
// returning both the old and new commands.
func (db *ApplianceDB) CommandComplete(ctx context.Context, cmdID int64, resp []byte) (*SiteCommand, *SiteCommand, error) {
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
		     DELETE FROM site_commands
		     WHERE id <= (
		         SELECT id
		         FROM (
		             SELECT id
		             FROM site_commands
		             WHERE site_uuid = $1 AND state IN ('DONE', 'CNCL')
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

// HeartbeatIngest represents a row in the site_heartbeat_ingest table.  In this
// case "ingest" means that we record heartbeats into this table for later
// coalescing by another process.
type HeartbeatIngest struct {
	IngestID uint64
	SiteUUID uuid.UUID
	BootTS   time.Time
	RecordTS time.Time
}

// InsertHeartbeatIngest adds a row to the site_heartbeat_ingest table.
func (db *ApplianceDB) InsertHeartbeatIngest(ctx context.Context, heartbeat *HeartbeatIngest) error {
	_, err := db.ExecContext(ctx,
		"INSERT INTO site_heartbeat_ingest VALUES (DEFAULT, $1, $2, $3)",
		heartbeat.SiteUUID,
		heartbeat.BootTS,
		heartbeat.RecordTS)
	return err
}

// Organization represents a group or business
type Organization struct {
	// UUID is used as the primary key for tracking a customer
	// across cloud properties
	UUID uuid.UUID `db:"uuid"`
	Name string    `db:"name"` // Familiar name of customer
}

// AllOrganizations returns a complete list of the organization records in the
// database
func (db *ApplianceDB) AllOrganizations(ctx context.Context) ([]Organization, error) {
	var orgs []Organization
	err := db.SelectContext(ctx, &orgs, "SELECT uuid, name FROM organization")
	if err != nil {
		return nil, err
	}
	return orgs, nil
}

// OrganizationByUUID returns the specified org from the organization table.
func (db *ApplianceDB) OrganizationByUUID(ctx context.Context, orgUUID uuid.UUID) (*Organization, error) {
	var org Organization
	err := db.GetContext(ctx, &org,
		`SELECT *
		    FROM organization 
		    WHERE uuid=$1`, orgUUID)
	switch err {
	case sql.ErrNoRows:
		return nil, NotFoundError{fmt.Sprintf(
			"OrganizationByUUID: Couldn't find record for %s", orgUUID)}
	case nil:
		return &org, nil
	default:
		panic(err)
	}
}

// InsertOrganization inserts an Organization.
func (db *ApplianceDB) InsertOrganization(ctx context.Context,
	org *Organization) error {
	return db.InsertOrganizationTx(ctx, nil, org)
}

// InsertOrganizationTx inserts an Organization, possibly inside a transaction.
func (db *ApplianceDB) InsertOrganizationTx(ctx context.Context, dbx DBX,
	org *Organization) error {

	if dbx == nil {
		dbx = db
	}
	_, err := dbx.NamedExecContext(ctx,
		`INSERT INTO organization (uuid, name) VALUES (:uuid,:name)`, org)
	return err
}

// OAuth2OrgRuleType represents the different kind of OAuth2 Identity to
// Organization mapping rules.
type OAuth2OrgRuleType string

// Matches SQL values
const (
	RuleTypeTenant OAuth2OrgRuleType = "tenant"
	RuleTypeDomain OAuth2OrgRuleType = "domain"
	RuleTypeEmail  OAuth2OrgRuleType = "email"
)

// OAuth2OrganizationRule represents a rule to map OAuth2 facts to Organizations.
type OAuth2OrganizationRule struct {
	Provider         string            `db:"provider"`
	RuleType         OAuth2OrgRuleType `db:"rule_type"`
	RuleValue        string            `db:"rule_value"`
	OrganizationUUID uuid.UUID         `db:"organization_uuid"`
}

// AllOAuth2OrganizationRules returns a complete list of the
// oauth2_organization_rule records in the database
func (db *ApplianceDB) AllOAuth2OrganizationRules(ctx context.Context) ([]OAuth2OrganizationRule, error) {
	var rules []OAuth2OrganizationRule
	err := db.SelectContext(ctx, &rules, "SELECT * FROM oauth2_organization_rule")
	if err != nil {
		return nil, err
	}
	return rules, nil
}

// OAuth2OrganizationRuleTest tries to find a match for the OAuth2
// provider, rule_type and rule_value.  And example would be
// (provider=google, rule_type=RuleTypeTenant, rule_value='testech.org')
func (db *ApplianceDB) OAuth2OrganizationRuleTest(ctx context.Context,
	provider string, ruleType OAuth2OrgRuleType, ruleValue string) (*OAuth2OrganizationRule, error) {

	var rule OAuth2OrganizationRule
	err := db.GetContext(ctx, &rule,
		`SELECT *
		    FROM oauth2_organization_rule
		    WHERE provider=$1 AND rule_type=$2 AND rule_value=$3`,
		provider, ruleType, ruleValue)
	switch err {
	case sql.ErrNoRows:
		return nil, NotFoundError{fmt.Sprintf(
			"OAuth2OrganizationRuleTest: Couldn't find record for (%v,%v,%v)",
			provider, ruleType, ruleValue)}
	case nil:
		return &rule, nil
	default:
		panic(err)
	}
}

// InsertOAuth2OrganizationRule inserts an OAuth2OrganizationRule.
func (db *ApplianceDB) InsertOAuth2OrganizationRule(ctx context.Context,
	rule *OAuth2OrganizationRule) error {
	return db.InsertOAuth2OrganizationRuleTx(ctx, nil, rule)
}

// InsertOAuth2OrganizationRuleTx inserts an OAuth2OrganizationRule, possibly inside a transaction.
func (db *ApplianceDB) InsertOAuth2OrganizationRuleTx(ctx context.Context, dbx DBX,
	rule *OAuth2OrganizationRule) error {

	if dbx == nil {
		dbx = db
	}
	_, err := dbx.NamedExecContext(ctx,
		`INSERT INTO oauth2_organization_rule
		 (provider, rule_type, rule_value, organization_uuid)
		 VALUES
		 (:provider, :rule_type, :rule_value, :organization_uuid)`, rule)
	return err
}

// Person represents a natural person
type Person struct {
	UUID         uuid.UUID `db:"uuid"`
	Name         string    `db:"name"`
	PrimaryEmail string    `db:"primary_email"`
}

// PersonByUUID returns a person record by primary key (UUID)
func (db *ApplianceDB) PersonByUUID(ctx context.Context, personUUID uuid.UUID) (*Person, error) {
	var person Person
	err := db.GetContext(ctx, &person,
		`SELECT *
		    FROM person
		    WHERE uuid=$1`, personUUID)
	switch err {
	case sql.ErrNoRows:
		return nil, NotFoundError{fmt.Sprintf(
			"PersonByUUID: Couldn't find record for %s", personUUID)}
	case nil:
		return &person, nil
	default:
		panic(err)
	}
}

// InsertPerson inserts a Person
func (db *ApplianceDB) InsertPerson(ctx context.Context,
	person *Person) error {
	return db.InsertPersonTx(ctx, nil, person)
}

// InsertPersonTx inserts a Person, possibly inside a transaction.
func (db *ApplianceDB) InsertPersonTx(ctx context.Context, dbx DBX,
	person *Person) error {

	if dbx == nil {
		dbx = db
	}
	_, err := dbx.NamedExecContext(ctx,
		`INSERT INTO person
		 (uuid, name, primary_email)
		 VALUES (:uuid, :name, :primary_email)`, person)
	return err
}

// Account represents a user account
type Account struct {
	UUID             uuid.UUID `db:"uuid"`
	Email            string    `db:"email"`
	PhoneNumber      string    `db:"phone_number"`
	PersonUUID       uuid.UUID `db:"person_uuid"`
	OrganizationUUID uuid.UUID `db:"organization_uuid"`
}

// AccountsByOrganization returns a list of all accounts for a given organization
func (db *ApplianceDB) AccountsByOrganization(ctx context.Context, org uuid.UUID) ([]Account, error) {
	var accts []Account
	err := db.SelectContext(ctx, &accts, `
		SELECT *
		FROM account
		WHERE account.organization_uuid = $1`, org)
	if err != nil {
		return nil, err
	}
	return accts, nil
}

// AccountByUUID returns an Account by primary key (uuid)
func (db *ApplianceDB) AccountByUUID(ctx context.Context, acctUUID uuid.UUID) (*Account, error) {
	var acct Account
	err := db.GetContext(ctx, &acct,
		`SELECT *
		    FROM account
		    WHERE uuid=$1`, acctUUID)
	switch err {
	case sql.ErrNoRows:
		return nil, NotFoundError{fmt.Sprintf(
			"AccountByUUID: Couldn't find record for %s", acctUUID)}
	case nil:
		return &acct, nil
	default:
		panic(err)
	}
}

// InsertAccount inserts a Account
func (db *ApplianceDB) InsertAccount(ctx context.Context,
	account *Account) error {
	return db.InsertAccountTx(ctx, nil, account)
}

// InsertAccountTx inserts a Account, possibly inside a transaction.
func (db *ApplianceDB) InsertAccountTx(ctx context.Context, dbx DBX,
	account *Account) error {

	if dbx == nil {
		dbx = db
	}
	_, err := dbx.NamedExecContext(ctx,
		`INSERT INTO account
		 (uuid, email, phone_number, person_uuid, organization_uuid)
		 VALUES (:uuid, :email, :phone_number, :person_uuid, :organization_uuid)`,
		account)
	return err
}

// OAuth2Identity represents an OAuth2 identity provider's record of a User.
type OAuth2Identity struct {
	ID          int       `db:"id"`
	Subject     string    `db:"subject"`
	Provider    string    `db:"provider"`
	AccountUUID uuid.UUID `db:"account_uuid"`
}

// OAuth2IdentitiesByAccount returns a list of the oauth2_identity
// records for the given Account
func (db *ApplianceDB) OAuth2IdentitiesByAccount(ctx context.Context,
	accountUUID uuid.UUID) ([]OAuth2Identity, error) {

	var ids []OAuth2Identity
	err := db.SelectContext(ctx, &ids,
		`SELECT 
		  id, provider, subject, account_uuid
		FROM
		  oauth2_identity
		WHERE account_uuid=$1`, accountUUID)
	if err != nil {
		return nil, err
	}
	return ids, nil
}

// InsertOAuth2Identity inserts a row in OAuth2Identity
func (db *ApplianceDB) InsertOAuth2Identity(ctx context.Context,
	identity *OAuth2Identity) error {
	return db.InsertOAuth2IdentityTx(ctx, nil, identity)
}

// InsertOAuth2IdentityTx inserts a row in OAuth2Identity, possibly inside a transaction.
func (db *ApplianceDB) InsertOAuth2IdentityTx(ctx context.Context, dbx DBX,
	identity *OAuth2Identity) error {

	if dbx == nil {
		dbx = db
	}
	row := dbx.QueryRowContext(ctx,
		`INSERT INTO oauth2_identity
		 (subject, provider, account_uuid)
		 VALUES ($1, $2, $3)
		 RETURNING id`, identity.Subject, identity.Provider, identity.AccountUUID)
	return row.Scan(&identity.ID)
}

// LoginInfo is a compound struct representing basic information needed
// when a user logs in.
type LoginInfo struct {
	Account          Account
	Person           Person
	OAuth2IdentityID int
}

// LoginInfoByProviderAndSubject looks up the subject for the given provider
// and returns LoginInfo for that user.
func (db *ApplianceDB) LoginInfoByProviderAndSubject(ctx context.Context,
	provider, subject string) (*LoginInfo, error) {

	var li LoginInfo
	row := db.QueryRowContext(ctx, `
		SELECT
		  a.uuid,
		  a.email,
		  a.phone_number,
		  a.person_uuid,
		  a.organization_uuid,
		  p.uuid,
		  p.name,
		  p.primary_email,
		  o.id
		FROM account a, person p, oauth2_identity o
		WHERE o.provider=$1
		  AND o.subject=$2
		  AND a.uuid=o.account_uuid
		  AND a.person_uuid=p.uuid`,
		provider, subject)
	err := row.Scan(
		&li.Account.UUID,
		&li.Account.Email,
		&li.Account.PhoneNumber,
		&li.Account.PersonUUID,
		&li.Account.OrganizationUUID,
		&li.Person.UUID,
		&li.Person.Name,
		&li.Person.PrimaryEmail,
		&li.OAuth2IdentityID)
	switch err {
	case sql.ErrNoRows:
		return nil, NotFoundError{fmt.Sprintf(
			"LoginInfoByProviderAndSubject: Couldn't find info for %v,%v",
			provider, subject)}
	case nil:
		return &li, nil
	default:
		panic(err)
	}
}

// OAuth2AccessToken represents an OAuth2 Access Token obtained from a provider
type OAuth2AccessToken struct {
	OAuth2IdentityID int       `db:"identity_id"`
	Token            string    `db:"token"`
	Expires          time.Time `db:"expires"`
}

// InsertOAuth2AccessToken inserts a row in OAuth2AccessToken
func (db *ApplianceDB) InsertOAuth2AccessToken(ctx context.Context,
	tok *OAuth2AccessToken) error {
	return db.InsertOAuth2AccessTokenTx(ctx, nil, tok)
}

// InsertOAuth2AccessTokenTx inserts a row in OAuth2AccessToken, possibly inside a transaction.
func (db *ApplianceDB) InsertOAuth2AccessTokenTx(ctx context.Context, dbx DBX,
	tok *OAuth2AccessToken) error {

	if dbx == nil {
		dbx = db
	}
	_, err := dbx.NamedExecContext(ctx,
		`INSERT INTO oauth2_access_token
		 (identity_id, token, expires)
		 VALUES (:identity_id, :token, :expires)`, tok)
	return err
}

// OAuth2RefreshToken represents an OAuth2 Refresh Token obtained from a provider
type OAuth2RefreshToken struct {
	OAuth2IdentityID int    `db:"identity_id"`
	Token            string `db:"token"`
}

// UpsertOAuth2RefreshToken upserts a row in oauth2_refresh_token
func (db *ApplianceDB) UpsertOAuth2RefreshToken(ctx context.Context,
	tok *OAuth2RefreshToken) error {
	return db.UpsertOAuth2RefreshTokenTx(ctx, nil, tok)
}

// UpsertOAuth2RefreshTokenTx upserts a row in oauth2_refresh_token, possibly inside a transaction.
func (db *ApplianceDB) UpsertOAuth2RefreshTokenTx(ctx context.Context, dbx DBX,
	tok *OAuth2RefreshToken) error {

	if dbx == nil {
		dbx = db
	}
	_, err := dbx.NamedExecContext(ctx,
		`INSERT INTO oauth2_refresh_token
		 (identity_id, token)
		 VALUES (:identity_id, :token)
		 ON CONFLICT (identity_id)
		 DO UPDATE SET (token) = (EXCLUDED.token)`, tok)
	return err
}
