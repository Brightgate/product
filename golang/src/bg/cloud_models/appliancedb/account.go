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
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io/ioutil"
	"strings"
	"time"

	"github.com/lib/pq"
	"github.com/pkg/errors"
	"github.com/satori/uuid"
	"golang.org/x/crypto/openpgp"
	"golang.org/x/crypto/openpgp/armor"
)

type accountManager interface {
	PersonByUUID(context.Context, uuid.UUID) (*Person, error)
	InsertPerson(context.Context, *Person) error
	InsertPersonTx(context.Context, DBX, *Person) error

	AccountsByOrganization(context.Context, uuid.UUID) ([]Account, error)
	AccountByUUID(context.Context, uuid.UUID) (*Account, error)
	InsertAccount(context.Context, *Account) error
	InsertAccountTx(context.Context, DBX, *Account) error
	DeleteAccount(context.Context, uuid.UUID) error
	DeleteAccountTx(context.Context, DBX, uuid.UUID) error

	AccountInfosByOrganization(context.Context, uuid.UUID) ([]AccountInfo, error)
	AccountInfoByUUID(context.Context, uuid.UUID) (*AccountInfo, error)

	AccountSecretsSetPassphrase(passphrase []byte)
	AccountSecretsByUUID(context.Context, uuid.UUID) (*AccountSecrets, error)
	UpsertAccountSecrets(context.Context, *AccountSecrets) error
	UpsertAccountSecretsTx(context.Context, DBX, *AccountSecrets) error
	DeleteAccountSecrets(context.Context, uuid.UUID) error
	DeleteAccountSecretsTx(context.Context, DBX, uuid.UUID) error

	AccountOrgRolesByAccount(context.Context, uuid.UUID) ([]AccountOrgRole, error)
	AccountOrgRolesByAccountTarget(context.Context, uuid.UUID, uuid.UUID) ([]AccountOrgRole, error)
	AccountPrimaryOrgRoles(context.Context, uuid.UUID) ([]string, error)
	AccountOrgRolesByOrg(context.Context, uuid.UUID, string) ([]AccountOrgRole, error)
	AccountOrgRolesByOrgTx(context.Context, DBX, uuid.UUID, string) ([]AccountOrgRole, error)
	InsertAccountOrgRole(context.Context, *AccountOrgRole) error
	InsertAccountOrgRoleTx(context.Context, DBX, *AccountOrgRole) error
	DeleteAccountOrgRole(context.Context, *AccountOrgRole) error
	DeleteAccountOrgRoleTx(context.Context, DBX, *AccountOrgRole) error

	OrgOrgRelationshipsByOrg(context.Context, uuid.UUID) ([]OrgOrgRelationship, error)
	OrgOrgRelationshipsByOrgTx(context.Context, DBX, uuid.UUID) ([]OrgOrgRelationship, error)
	OrgOrgRelationshipsByOrgTarget(context.Context, uuid.UUID, uuid.UUID) ([]OrgOrgRelationship, error)
	OrgOrgRelationshipsByOrgTargetTx(context.Context, DBX, uuid.UUID, uuid.UUID) ([]OrgOrgRelationship, error)
	InsertOrgOrgRelationship(context.Context, *OrgOrgRelationship) error
	InsertOrgOrgRelationshipTx(context.Context, DBX, *OrgOrgRelationship) error
	DeleteOrgOrgRelationship(context.Context, uuid.UUID) error
	DeleteOrgOrgRelationshipTx(context.Context, DBX, uuid.UUID) error

	OAuth2IdentitiesByAccount(context.Context, uuid.UUID) ([]OAuth2Identity, error)
	InsertOAuth2Identity(context.Context, *OAuth2Identity) error
	InsertOAuth2IdentityTx(context.Context, DBX, *OAuth2Identity) error

	LoginInfoByProviderAndSubject(context.Context, string, string) (*LoginInfo, error)

	InsertOAuth2AccessToken(context.Context, *OAuth2AccessToken) error
	InsertOAuth2AccessTokenTx(context.Context, DBX, *OAuth2AccessToken) error
	UpsertOAuth2RefreshToken(context.Context, *OAuth2RefreshToken) error
	UpsertOAuth2RefreshTokenTx(context.Context, DBX, *OAuth2RefreshToken) error
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

// DeleteAccount deletes an Account and all related information
func (db *ApplianceDB) DeleteAccount(ctx context.Context,
	accountUUID uuid.UUID) error {
	tx, err := db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := db.DeleteAccountTx(ctx, tx, accountUUID); err != nil {
		return err
	}
	tx.Commit()
	return nil
}

// DeleteAccountTx deletes an Account and all related information, as a transaction.
func (db *ApplianceDB) DeleteAccountTx(ctx context.Context, dbx DBX,
	acctuu uuid.UUID) error {

	if dbx == nil {
		panic("dbx cannot be nil")
	}

	var acct Account
	err := dbx.GetContext(ctx, &acct,
		`SELECT * FROM account WHERE uuid=$1`, acctuu)
	switch err {
	case sql.ErrNoRows:
		return NotFoundError{fmt.Sprintf(
			"DeleteAccountTx: Couldn't find record for %s", acctuu)}
	case nil:
		break
	default:
		panic(err)
	}
	_, err = dbx.ExecContext(ctx,
		`DELETE FROM account_secrets WHERE account_uuid = $1`, acctuu)
	if err != nil {
		panic(err)
	}
	_, err = dbx.ExecContext(ctx,
		`DELETE FROM oauth2_identity WHERE account_uuid = $1`, acctuu)
	if err != nil {
		panic(err)
	}
	_, err = dbx.ExecContext(ctx,
		`DELETE FROM account_org_role WHERE account_uuid=$1`, acctuu)
	if err != nil {
		panic(err)
	}
	_, err = dbx.ExecContext(ctx,
		`DELETE FROM account WHERE uuid=$1`, acctuu)
	if err != nil {
		panic(err)
	}
	_, err = dbx.ExecContext(ctx,
		`DELETE FROM person WHERE uuid=$1`, acct.PersonUUID)
	if err != nil {
		panic(err)
	}
	return nil
}

// AccountInfo represents the join of Account and Person
type AccountInfo struct {
	UUID         uuid.UUID `db:"uuid" json:"accountUUID"`
	Email        string    `db:"email" json:"email"`
	PhoneNumber  string    `db:"phone_number" json:"phoneNumber"`
	Name         string    `db:"name" json:"name"`
	PrimaryEmail string    `db:"primary_email" json:"primaryEmail"`
}

// AccountInfosByOrganization returns a list of all AccountInfos for a given organization
func (db *ApplianceDB) AccountInfosByOrganization(ctx context.Context, org uuid.UUID) ([]AccountInfo, error) {
	var accts []AccountInfo
	err := db.SelectContext(ctx, &accts, `
		SELECT
		  a.uuid,
		  a.email,
		  a.phone_number,
		  p.name,
		  p.primary_email
		FROM account a, person p
		WHERE
		  a.organization_uuid = $1 AND
		  a.person_uuid = p.uuid`, org)
	if err != nil {
		return nil, err
	}
	return accts, nil
}

// AccountInfoByUUID returns an AccountInfo for a given account UUID
func (db *ApplianceDB) AccountInfoByUUID(ctx context.Context, acct uuid.UUID) (*AccountInfo, error) {
	var ai AccountInfo
	err := db.GetContext(ctx, &ai, `
		SELECT
		  a.uuid,
		  a.email,
		  a.phone_number,
		  p.name,
		  p.primary_email
		FROM account a, person p
		WHERE
		  a.uuid = $1 AND
		  a.person_uuid = p.uuid`, acct)
	switch err {
	case sql.ErrNoRows:
		return nil, NotFoundError{fmt.Sprintf(
			"AccountInfoByUUID: Couldn't find record for %s", acct)}
	case nil:
		return &ai, nil
	default:
		panic(err)
	}
}

func pgpSymEncrypt(plaintext string, passphrase []byte) (string, error) {
	if passphrase == nil {
		return "", errors.New("invalid empty passphrase")
	}
	b := &strings.Builder{}
	armorW, err := armor.Encode(b, "PGP MESSAGE", nil)
	if err != nil {
		return "", errors.Wrap(err, "Could not prepare message armor")
	}
	defer armorW.Close()

	plaintextW, err := openpgp.SymmetricallyEncrypt(armorW, passphrase, nil, nil)
	if err != nil {
		return "", errors.Wrap(err, "Could not prepare message encryptor")
	}

	defer plaintextW.Close()
	_, err = plaintextW.Write([]byte(plaintext))
	if err != nil {
		return "", errors.Wrap(err, "Could not write plaintext")
	}
	plaintextW.Close()
	armorW.Close()
	return b.String(), nil
}

func pgpSymDecrypt(cipherText []byte, passphrase []byte) (string, error) {
	buf := bytes.NewBuffer(cipherText)
	unarmored, err := armor.Decode(buf)
	if err != nil {
		return "", err
	}

	prompted := false
	msgDetails, err := openpgp.ReadMessage(unarmored.Body, nil,
		func(keys []openpgp.Key, _ bool) ([]byte, error) {
			if prompted {
				return nil, fmt.Errorf("incorrect decryption passphrase")
			}
			prompted = true
			return passphrase, nil
		}, nil)
	if err != nil {
		return "", err
	}

	plainBytes, err := ioutil.ReadAll(msgDetails.UnverifiedBody)
	if err != nil {
		return "", err
	}
	return string(plainBytes), nil
}

// AccountSecrets represents an entry in the account_secrets table.
// This data is encrypted (on the client-side).
type AccountSecrets struct {
	AccountUUID                 uuid.UUID `db:"account_uuid"`
	ApplianceUserBcrypt         string    `db:"appliance_user_bcrypt"`
	ApplianceUserBcryptRegime   string    `db:"appliance_user_bcrypt_regime"`
	ApplianceUserBcryptTs       time.Time `db:"appliance_user_bcrypt_ts"`
	ApplianceUserMSCHAPv2       string    `db:"appliance_user_mschapv2"`
	ApplianceUserMSCHAPv2Regime string    `db:"appliance_user_mschapv2_regime"`
	ApplianceUserMSCHAPv2Ts     time.Time `db:"appliance_user_mschapv2_ts"`
}

// AccountSecretsSetPassphrase sets the symmetric encryption passphrase used
// to encrypt certain account_secrets database columns.
func (db *ApplianceDB) AccountSecretsSetPassphrase(passphrase []byte) {
	db.accountSecretsPassphrase = passphrase
}

// AccountSecretsByUUID selects a row from account_secrets by user account
// UUID.
func (db *ApplianceDB) AccountSecretsByUUID(ctx context.Context, acctUUID uuid.UUID) (*AccountSecrets, error) {
	var as AccountSecrets
	err := db.GetContext(ctx, &as,
		`SELECT *
		    FROM account_secrets
		    WHERE account_uuid=$1`, acctUUID)
	switch err {
	case sql.ErrNoRows:
		return nil, NotFoundError{fmt.Sprintf(
			"AccountSecretsByUUID: Couldn't find record for %s", acctUUID)}
	case nil:
		break
	default:
		panic(err)
	}
	bc, err := pgpSymDecrypt([]byte(as.ApplianceUserBcrypt), db.accountSecretsPassphrase)
	if err != nil {
		return nil, errors.Wrap(err, "AccountSecretsByUUID: Couldn't decrypt UserBcrypt")
	}
	ms, err := pgpSymDecrypt([]byte(as.ApplianceUserMSCHAPv2), db.accountSecretsPassphrase)
	if err != nil {
		return nil, errors.Wrap(err, "AccountSecretsByUUID: Couldn't decrypt UserMSCHAPv2")
	}
	as.ApplianceUserBcrypt = bc
	as.ApplianceUserMSCHAPv2 = ms
	return &as, nil
}

// UpsertAccountSecrets upserts a row in account_secrets
func (db *ApplianceDB) UpsertAccountSecrets(ctx context.Context, as *AccountSecrets) error {
	return db.UpsertAccountSecretsTx(ctx, nil, as)
}

// UpsertAccountSecretsTx upserts a row in account_secrets, possibly inside a transaction.
func (db *ApplianceDB) UpsertAccountSecretsTx(ctx context.Context, dbx DBX,
	as *AccountSecrets) error {

	cryptedBcrypt, err := pgpSymEncrypt(as.ApplianceUserBcrypt, db.accountSecretsPassphrase)
	if err != nil {
		return err
	}
	cryptedMSCHAPv2, err := pgpSymEncrypt(as.ApplianceUserMSCHAPv2, db.accountSecretsPassphrase)
	if err != nil {
		return err
	}
	// Take a copy so we can modify it
	crypted := *as
	crypted.ApplianceUserBcrypt = cryptedBcrypt
	crypted.ApplianceUserMSCHAPv2 = cryptedMSCHAPv2

	if dbx == nil {
		dbx = db
	}
	_, err = dbx.NamedExecContext(ctx,
		`INSERT INTO account_secrets
		  (account_uuid,
		   appliance_user_bcrypt, appliance_user_bcrypt_regime, appliance_user_bcrypt_ts,
		   appliance_user_mschapv2, appliance_user_mschapv2_regime, appliance_user_mschapv2_ts)
		 VALUES
		  (:account_uuid,
		  :appliance_user_bcrypt, :appliance_user_bcrypt_regime, :appliance_user_bcrypt_ts,
		  :appliance_user_mschapv2, :appliance_user_mschapv2_regime, :appliance_user_mschapv2_ts)
		 ON CONFLICT (account_uuid)
		 DO UPDATE SET (
		   appliance_user_bcrypt, appliance_user_bcrypt_regime, appliance_user_bcrypt_ts,
		   appliance_user_mschapv2, appliance_user_mschapv2_regime, appliance_user_mschapv2_ts
		 ) = (
		   EXCLUDED.appliance_user_bcrypt, EXCLUDED.appliance_user_bcrypt_regime, EXCLUDED.appliance_user_bcrypt_ts,
		   EXCLUDED.appliance_user_mschapv2, EXCLUDED.appliance_user_mschapv2_regime, EXCLUDED.appliance_user_mschapv2_ts
		 )`, &crypted)
	return err
}

// DeleteAccountSecrets removes account_secrets record for an account
func (db *ApplianceDB) DeleteAccountSecrets(ctx context.Context, acct uuid.UUID) error {
	return db.DeleteAccountSecretsTx(ctx, nil, acct)
}

// DeleteAccountSecretsTx removes account_secrets record for an account, possibly
// inside a transaction
func (db *ApplianceDB) DeleteAccountSecretsTx(ctx context.Context, dbx DBX, acct uuid.UUID) error {
	if dbx == nil {
		dbx = db
	}
	_, err := dbx.ExecContext(ctx,
		`DELETE FROM account_secrets WHERE account_uuid = $1`, acct)
	if err != nil {
		panic(err)
	}
	return err
}

// ValidRole tests if the given role is acceptible.  Used for input checking.
func ValidRole(role string) bool {
	return (role == "user" || role == "admin")
}

// AccountOrgRole represents the tuple {account, organization, role, relationship}
// which is used to express which roles are assigned to users.
// This information is synthesized from the roles and relationships
// tables.
type AccountOrgRole struct {
	AccountUUID            uuid.UUID `db:"account_uuid"`
	OrganizationUUID       uuid.UUID `db:"organization_uuid"`
	TargetOrganizationUUID uuid.UUID `db:"target_organization_uuid"`
	Role                   string    `db:"role"`
	Relationship           string    `db:"relationship"`
}

// AccountOrgRolesByAccount compute the effective roles for an account; this
// query also makes sure to constrain the effective roles against the limit
// set of roles for the relationship type, for safety.
func (db *ApplianceDB) AccountOrgRolesByAccount(ctx context.Context,
	account uuid.UUID) ([]AccountOrgRole, error) {
	var roles []AccountOrgRole
	err := db.SelectContext(ctx, &roles,
		`SELECT * FROM account_org_role, relationship_roles
		WHERE
		  account_org_role.account_uuid=$1 AND
		  account_org_role.relationship = relationship_roles.relationship AND
		  account_org_role.role = relationship_roles.role`, account)
	if err != nil {
		return nil, err
	}
	return roles, nil
}

// AccountOrgRolesByAccountTarget computes the effective roles for an account with
// respect to a specific organization; this query also makes sure to constrain
// the effective roles against the limit set of roles for the relationship
// type, for safety.
func (db *ApplianceDB) AccountOrgRolesByAccountTarget(ctx context.Context,
	account uuid.UUID, org uuid.UUID) ([]AccountOrgRole, error) {
	var roles []AccountOrgRole
	err := db.SelectContext(ctx, &roles,
		`SELECT *
		FROM account_org_role
		  JOIN relationship_roles USING (relationship, role)
		WHERE
		  account_org_role.account_uuid=$1 AND
		  account_org_role.target_organization_uuid=$2`, account, org)
	if err != nil {
		return nil, err
	}
	return roles, nil
}

// AccountPrimaryOrgRoles computes the roles in effect for an account's
// primary ("home") organization.
func (db *ApplianceDB) AccountPrimaryOrgRoles(ctx context.Context,
	account uuid.UUID) ([]string, error) {
	var roles []string
	err := db.SelectContext(ctx, &roles, `
		SELECT account_org_role.role AS role
		FROM account_org_role
		  JOIN relationship_roles USING (relationship, role)
		WHERE
		  account_org_role.account_uuid=$1 AND
		  account_org_role.organization_uuid = account_org_role.target_organization_uuid`,
		account)
	if err != nil {
		return nil, err
	}
	return roles, nil
}

// AccountOrgRolesByOrg returns the set of accounts possessing the given role
// for a target organization. If role is "", select all roles.
func (db *ApplianceDB) AccountOrgRolesByOrg(ctx context.Context,
	org uuid.UUID, role string) ([]AccountOrgRole, error) {
	return db.AccountOrgRolesByOrgTx(ctx, nil, org, role)
}

// AccountOrgRolesByOrgTx returns the set of accounts possessing the given role
// for a target organization, possibly inside a transaction.  If role is "",
// select all roles.
func (db *ApplianceDB) AccountOrgRolesByOrgTx(ctx context.Context, dbx DBX,
	org uuid.UUID, role string) ([]AccountOrgRole, error) {
	var roles []AccountOrgRole
	var err error
	if dbx == nil {
		dbx = db
	}

	err = dbx.SelectContext(ctx, &roles, `
		SELECT * FROM account_org_role
		WHERE
		  account_org_role.target_organization_uuid=$1 AND
		  ($2='' OR account_org_role.role=$2)`,
		org, role)
	if err != nil {
		return nil, err
	}
	return roles, nil
}

// InsertAccountOrgRole inserts a row in account_org_role
func (db *ApplianceDB) InsertAccountOrgRole(ctx context.Context, role *AccountOrgRole) error {
	return db.InsertAccountOrgRoleTx(ctx, nil, role)
}

// InsertAccountOrgRoleTx inserts a row in account_org_role, possibly inside a transaction
func (db *ApplianceDB) InsertAccountOrgRoleTx(ctx context.Context, dbx DBX, role *AccountOrgRole) error {
	if dbx == nil {
		dbx = db
	}
	_, err := dbx.NamedExecContext(ctx,
		`INSERT INTO account_org_role
		 (account_uuid, organization_uuid, target_organization_uuid, relationship, role)
		 VALUES (:account_uuid, :organization_uuid, :target_organization_uuid, :relationship, :role)
		 ON CONFLICT DO NOTHING`,
		role)
	return err
}

// DeleteAccountOrgRole deletes a row in account_org_role
func (db *ApplianceDB) DeleteAccountOrgRole(ctx context.Context, role *AccountOrgRole) error {
	return db.DeleteAccountOrgRoleTx(ctx, nil, role)
}

// DeleteAccountOrgRoleTx deletes a row in account_org_role, possibly inside a transaction
func (db *ApplianceDB) DeleteAccountOrgRoleTx(ctx context.Context, dbx DBX, role *AccountOrgRole) error {
	if dbx == nil {
		dbx = db
	}
	_, err := dbx.NamedExecContext(ctx,
		`DELETE FROM account_org_role
		WHERE
		  account_uuid=:account_uuid AND
		  organization_uuid=:organization_uuid AND
		  target_organization_uuid=:target_organization_uuid AND
		  relationship=:relationship AND
		  role=:role`,
		role)
	return err
}

// OrgOrgRelationship represents the tuple {managing-organization, target-organization,
// relationship-type}
type OrgOrgRelationship struct {
	UUID                   uuid.UUID      `db:"uuid"`
	OrganizationUUID       uuid.UUID      `db:"organization_uuid"`
	TargetOrganizationUUID uuid.UUID      `db:"target_organization_uuid"`
	Relationship           string         `db:"relationship"`
	LimitRoles             pq.StringArray `db:"limit_roles"`
}

// OrgOrgRelationshipsByOrg returns the org/org relationships for which the given
// organization is the owner/originator of the relationship.
func (db *ApplianceDB) OrgOrgRelationshipsByOrg(ctx context.Context, org uuid.UUID) ([]OrgOrgRelationship, error) {
	return db.OrgOrgRelationshipsByOrgTx(ctx, nil, org)
}

// OrgOrgRelationshipsByOrgTx returns the org/org relationships for which the
// given organization is the owner/originator of the relationship, possibly
// inside a transaction.
func (db *ApplianceDB) OrgOrgRelationshipsByOrgTx(ctx context.Context, dbx DBX, org uuid.UUID) ([]OrgOrgRelationship, error) {
	var rels []OrgOrgRelationship
	if dbx == nil {
		dbx = db
	}
	err := dbx.SelectContext(ctx, &rels, `
		SELECT
		  oo.uuid,
		  oo.organization_uuid,
		  oo.target_organization_uuid,
		  oo.relationship,
		  array_agg(r.role) as limit_roles
		FROM
		  org_org_relationship AS oo, relationship_roles AS r
		WHERE
		  oo.relationship = r.relationship AND
		  oo.organization_uuid = $1
		GROUP BY oo.uuid`, org)
	if err != nil {
		return nil, err
	}
	return rels, nil
}

// OrgOrgRelationshipsByOrgTarget returns the org/org relationships for which the
// given organization is the owner/originator of the relationship, and the given
// target is the target of the relationship.
func (db *ApplianceDB) OrgOrgRelationshipsByOrgTarget(ctx context.Context, org uuid.UUID, tgt uuid.UUID) ([]OrgOrgRelationship, error) {
	return db.OrgOrgRelationshipsByOrgTargetTx(ctx, nil, org, tgt)
}

// OrgOrgRelationshipsByOrgTargetTx returns the org/org relationships for which the
// given organization is the owner/originator of the relationship, and the given
// target is the target of the relationship, possibly inside a transaction.
func (db *ApplianceDB) OrgOrgRelationshipsByOrgTargetTx(ctx context.Context, dbx DBX, org uuid.UUID, tgt uuid.UUID) ([]OrgOrgRelationship, error) {
	var rels []OrgOrgRelationship
	if dbx == nil {
		dbx = db
	}
	err := dbx.SelectContext(ctx, &rels, `
		SELECT
		  oo.uuid,
		  oo.organization_uuid,
		  oo.target_organization_uuid,
		  oo.relationship,
		  array_agg(r.role) as limit_roles
		FROM
		  org_org_relationship AS oo, relationship_roles AS r
		WHERE
		  oo.relationship = r.relationship AND
		  oo.organization_uuid = $1 AND
		  oo.target_organization_uuid = $2
		GROUP BY oo.uuid`, org, tgt)
	if err != nil {
		return nil, err
	}
	return rels, nil
}

// InsertOrgOrgRelationship inserts a row in org_org_relationship, establishing
// a new Org/Org relationship.
func (db *ApplianceDB) InsertOrgOrgRelationship(ctx context.Context, rel *OrgOrgRelationship) error {
	return db.InsertOrgOrgRelationshipTx(ctx, nil, rel)
}

// InsertOrgOrgRelationshipTx inserts a row in org_org_relationship, establishing
// a new Org/Org relationship, possibly inside a transaction.
func (db *ApplianceDB) InsertOrgOrgRelationshipTx(ctx context.Context, dbx DBX, rel *OrgOrgRelationship) error {
	if dbx == nil {
		dbx = db
	}
	_, err := dbx.NamedExecContext(ctx,
		`INSERT INTO org_org_relationship
		 (uuid, organization_uuid, target_organization_uuid, relationship)
		 VALUES (:uuid, :organization_uuid, :target_organization_uuid, :relationship)
		 ON CONFLICT DO NOTHING`,
		rel)
	return err
}

// DeleteOrgOrgRelationship removes a row in org_org_relationship and any associated
// account_org_roles.
func (db *ApplianceDB) DeleteOrgOrgRelationship(ctx context.Context, uu uuid.UUID) error {
	return db.DeleteOrgOrgRelationshipTx(ctx, nil, uu)
}

// DeleteOrgOrgRelationshipTx removes a row in org_org_relationship and any associated
// account_org_roles, possibly inside a transaction.
func (db *ApplianceDB) DeleteOrgOrgRelationshipTx(ctx context.Context, dbx DBX, uu uuid.UUID) error {
	if dbx == nil {
		dbx = db
	}
	// The use of a CTE here causes the delete from multiple tables to be
	// transactional; 'x' is just a placeholder name
	_, err := dbx.ExecContext(ctx, `
		WITH x AS (
		  DELETE FROM account_org_role
		  WHERE
		    ROW(organization_uuid, target_organization_uuid, relationship) = (
		      SELECT o.organization_uuid, o.target_organization_uuid, o.relationship
		      FROM org_org_relationship o
		      WHERE uuid=$1
	            )
		)
		DELETE FROM org_org_relationship WHERE uuid=$1`,
		uu)
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
	PrimaryOrgRoles  []string
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
		&li.OAuth2IdentityID,
	)
	switch err {
	case sql.ErrNoRows:
		return nil, NotFoundError{fmt.Sprintf(
			"LoginInfoByProviderAndSubject: Couldn't find info for %v,%v",
			provider, subject)}
	case nil:
		break
	default:
		panic(err)
	}

	li.PrimaryOrgRoles, err = db.AccountPrimaryOrgRoles(ctx, li.Account.UUID)
	if err != nil {
		return nil, err
	}
	return &li, nil
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
