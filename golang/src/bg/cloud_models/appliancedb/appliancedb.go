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
	"strconv"
	"strings"
	"time"

	"github.com/guregu/null"
	"github.com/jmoiron/sqlx"

	"github.com/lib/pq"
	"github.com/pkg/errors"
	"github.com/satori/uuid"
)

// DBX describes the interface common to sqlx.DB and sqlx.Tx.
type DBX interface {
	sqlx.QueryerContext
	sqlx.ExecerContext
	GetContext(context.Context, interface{}, string, ...interface{}) error
	NamedExecContext(context.Context, string, interface{}) (sql.Result, error)
	QueryRowContext(context.Context, string, ...interface{}) *sql.Row
	SelectContext(context.Context, interface{}, string, ...interface{}) error
}

// DataStore facilitates mocking the database
// See http://www.alexedwards.net/blog/organising-database-access
type DataStore interface {
	LoadSchema(context.Context, string) error

	AllCustomerSites(context.Context) ([]CustomerSite, error)
	CustomerSiteByUUID(context.Context, uuid.UUID) (*CustomerSite, error)
	CustomerSitesByAccount(context.Context, uuid.UUID) ([]CustomerSite, error)
	CustomerSitesByOrganization(context.Context, uuid.UUID) ([]CustomerSite, error)
	InsertCustomerSite(context.Context, *CustomerSite) error
	InsertCustomerSiteTx(context.Context, DBX, *CustomerSite) error
	UpdateCustomerSite(context.Context, *CustomerSite) error
	UpdateCustomerSiteTx(context.Context, DBX, *CustomerSite) error

	AllApplianceIDs(context.Context) ([]ApplianceID, error)
	ApplianceIDByClientID(context.Context, string) (*ApplianceID, error)
	ApplianceIDByUUID(context.Context, uuid.UUID) (*ApplianceID, error)
	InsertApplianceID(context.Context, *ApplianceID) error
	InsertApplianceIDTx(context.Context, DBX, *ApplianceID) error
	UpdateApplianceID(context.Context, *ApplianceID) error
	UpdateApplianceIDTx(context.Context, DBX, *ApplianceID) error

	InsertApplianceKeyTx(context.Context, DBX, uuid.UUID, *AppliancePubKey) error
	KeysByUUID(context.Context, uuid.UUID) ([]AppliancePubKey, error)

	UpsertCloudStorage(context.Context, uuid.UUID, *SiteCloudStorage) error
	UpsertCloudStorageTx(context.Context, DBX, uuid.UUID, *SiteCloudStorage) error
	CloudStorageByUUID(context.Context, uuid.UUID) (*SiteCloudStorage, error)

	UpsertConfigStore(context.Context, uuid.UUID, *SiteConfigStore) error
	ConfigStoreByUUID(context.Context, uuid.UUID) (*SiteConfigStore, error)

	AllOrganizations(context.Context) ([]Organization, error)
	OrganizationByUUID(context.Context, uuid.UUID) (*Organization, error)
	InsertOrganization(context.Context, *Organization) error
	UpdateOrganization(context.Context, *Organization) error
	UpdateOrganizationTx(context.Context, DBX, *Organization) error

	AllOAuth2OrganizationRules(context.Context) ([]OAuth2OrganizationRule, error)
	OAuth2OrganizationRuleTest(context.Context, string, OAuth2OrgRuleType, string) (*OAuth2OrganizationRule, error)
	InsertOAuth2OrganizationRule(context.Context, *OAuth2OrganizationRule) error
	InsertOAuth2OrganizationRuleTx(context.Context, DBX, *OAuth2OrganizationRule) error

	// Methods related to accounts, persons, identity
	accountManager

	// Methods related to TLS certificates
	certManager

	// Methods related to the command queue
	commandQueue

	// Methods related to heartbeats, exceptions, and other events
	eventManager

	Ping() error
	PingContext(context.Context) error
	Close() error

	BeginTxx(context.Context, *sql.TxOptions) (*sqlx.Tx, error)
}

// ApplianceDB implements DataStore with the actual DB backend
// sql.DB implements Ping() and Close()
type ApplianceDB struct {
	*sqlx.DB
	accountSecretsPassphrase []byte
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

// NotFoundError is returned when the requested resource is not present in the
// database.
type NotFoundError struct {
	s string
}

func (e NotFoundError) Error() string {
	return e.s
}

// SyntaxError may be returned when there is a syntax error in the SQL query.
type SyntaxError struct {
	err   *pq.Error
	query string
}

func (e SyntaxError) Error() string {
	qLines := strings.Split(e.query, "\n")
	errPos, err := strconv.Atoi(e.err.Position)
	if err != nil {
		if e.err.Position == "" {
			return e.err.Error()
		}
		return fmt.Sprintf("%s (at byte %s)", e.err, e.err.Position)
	}

	var pos, i, col int
	var line string
	for i, line = range qLines {
		if errPos >= pos && errPos <= pos+len(line) {
			col = errPos - pos
			break
		}
		pos += len(line) + 1 // +1 for newline
	}

	return fmt.Sprintf("%s (at byte %d: line %d, col %d):\n%s",
		e.err, errPos, i+1, col, line)
}

func mkSyntaxError(e error, query string) error {
	pqErr, ok := e.(*pq.Error)
	if !ok || pqErr.Code.Name() != "syntax_error" {
		return e
	}
	return SyntaxError{pqErr, query}
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
	// Force all sessions to operate in UTC, so we don't rely on whatever
	// weird timezone is configured on the server, like GMT.
	sqldb, err := sqlx.Open("postgres", dataSource+"&timezone=UTC")
	if err != nil {
		return nil, err
	}
	// We found that not limiting this can cause problems as Go attempts to
	// open many many connections to the database.  (presumably the cloud
	// sql proxy can't handle massive numbers of connections)
	sqldb.SetMaxOpenConns(16)
	var ds DataStore = &ApplianceDB{
		DB: sqldb,
	}
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
		err = mkSyntaxError(err, string(bytes))
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

// UpdateCustomerSite updates a record into the customer_site table.
func (db *ApplianceDB) UpdateCustomerSite(ctx context.Context,
	cs *CustomerSite) error {
	return db.UpdateCustomerSiteTx(ctx, nil, cs)
}

// UpdateCustomerSiteTx updates a record into the customer_site table,
// possibly inside a transaction.
func (db *ApplianceDB) UpdateCustomerSiteTx(ctx context.Context, dbx DBX,
	cs *CustomerSite) error {

	if dbx == nil {
		dbx = db
	}
	_, err := dbx.NamedExecContext(ctx,
		`UPDATE customer_site
		 SET
		   name=:name,
		   organization_uuid=:organization_uuid
		 WHERE uuid=:uuid`, cs)
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
	err := db.GetContext(ctx, &site,
		"SELECT * FROM customer_site WHERE uuid=$1", u)
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
// records for the given Account's set of roles.
func (db *ApplianceDB) CustomerSitesByAccount(ctx context.Context,
	accountUUID uuid.UUID) ([]CustomerSite, error) {

	var sites []CustomerSite
	err := db.SelectContext(ctx, &sites,
		`SELECT
		  DISTINCT customer_site.uuid,
		  customer_site.organization_uuid AS organization_uuid,
		  customer_site.name AS name
		FROM
		  customer_site, account_org_role
		WHERE
		  account_org_role.account_uuid=$1 AND
		  account_org_role.target_organization_uuid = customer_site.organization_uuid
		`, accountUUID)
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

// UpdateApplianceID inserts an ApplianceID.
func (db *ApplianceDB) UpdateApplianceID(ctx context.Context,
	id *ApplianceID) error {
	return db.UpdateApplianceIDTx(ctx, nil, id)
}

// UpdateApplianceIDTx updates an ApplianceID, possibly inside a transaction.
// Note that only the Site ID is expected to be updated after creation,
// so that is all that is supported here.
func (db *ApplianceDB) UpdateApplianceIDTx(ctx context.Context, dbx DBX,
	id *ApplianceID) error {

	if dbx == nil {
		dbx = db
	}
	_, err := dbx.NamedExecContext(ctx,
		`UPDATE appliance_id_map
		 SET
		   site_uuid=:site_uuid
		 WHERE appliance_uuid=:appliance_uuid`, id)
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
	return db.UpsertCloudStorageTx(ctx, nil, u, stor)
}

// UpsertCloudStorageTx inserts or updates a CloudStorage Record, possibly
// inside a transaction.
func (db *ApplianceDB) UpsertCloudStorageTx(ctx context.Context,
	dbx DBX, u uuid.UUID, stor *SiteCloudStorage) error {

	if dbx == nil {
		dbx = db
	}
	_, err := dbx.ExecContext(ctx,
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

// NullOrganizationUUID is a reserved UUID for users which have no associated
// organization.  This is not expected to be a common case.
var NullOrganizationUUID = uuid.Must(uuid.FromString("00000000-0000-0000-0000-000000000000"))

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
	dbx, err := db.Beginx()
	if err != nil {
		return err
	}
	defer dbx.Rollback()
	_, err = dbx.NamedExecContext(ctx,
		`INSERT INTO organization (uuid, name) VALUES (:uuid,:name)`, org)
	if err != nil {
		return err
	}
	// To keep things deterministic for testing purposes, we always set the
	// UUID for the 'self' org/org relationship to the organization's UUID.
	// We don't really expect the self relationship UUID to ever be used
	// outside of the bounds of the core system.
	err = db.InsertOrgOrgRelationshipTx(ctx, dbx, &OrgOrgRelationship{
		UUID:                   org.UUID,
		OrganizationUUID:       org.UUID,
		TargetOrganizationUUID: org.UUID,
		Relationship:           "self",
	})
	if err != nil {
		return err
	}
	dbx.Commit()
	return err
}

// UpdateOrganization updates an Organization.
func (db *ApplianceDB) UpdateOrganization(ctx context.Context,
	org *Organization) error {
	return db.UpdateOrganizationTx(ctx, nil, org)
}

// UpdateOrganizationTx updates an Organization, possibly inside a transaction.
func (db *ApplianceDB) UpdateOrganizationTx(ctx context.Context, dbx DBX,
	org *Organization) error {

	if dbx == nil {
		dbx = db
	}
	_, err := dbx.NamedExecContext(ctx,
		`UPDATE organization SET name=:name WHERE uuid=:uuid`, org)
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
