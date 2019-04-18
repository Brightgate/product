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

	AccountSecretsSetPassphrase(passphrase []byte)
	AccountSecretsByUUID(context.Context, uuid.UUID) (*AccountSecrets, error)
	UpsertAccountSecrets(context.Context, *AccountSecrets) error
	UpsertAccountSecretsTx(context.Context, DBX, *AccountSecrets) error

	AccountOrgRolesByAccount(context.Context, uuid.UUID) ([]AccountOrgRole, error)
	AccountOrgRolesByAccountOrg(context.Context, uuid.UUID, uuid.UUID) ([]string, error)
	AccountOrgRolesByOrg(context.Context, uuid.UUID, string) ([]AccountOrgRole, error)
	AccountOrgRolesByOrgTx(context.Context, DBX, uuid.UUID, string) ([]AccountOrgRole, error)
	InsertAccountOrgRole(context.Context, *AccountOrgRole) error
	InsertAccountOrgRoleTx(context.Context, DBX, *AccountOrgRole) error
	DeleteAccountOrgRole(context.Context, *AccountOrgRole) error
	DeleteAccountOrgRoleTx(context.Context, DBX, *AccountOrgRole) error

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

// AccountOrgRole represents the tuple {account, organization, role}
// which is used to express which roles are assigned to users.
type AccountOrgRole struct {
	AccountUUID      uuid.UUID `db:"account_uuid"`
	OrganizationUUID uuid.UUID `db:"organization_uuid"`
	Role             string    `db:"role"`
}

// AccountOrgRolesByAccount selects rows from account_org_role by user account
// UUID.
func (db *ApplianceDB) AccountOrgRolesByAccount(ctx context.Context,
	account uuid.UUID) ([]AccountOrgRole, error) {
	var roles []AccountOrgRole
	err := db.SelectContext(ctx, &roles,
		`SELECT
		  account_uuid, organization_uuid, role
		FROM
		  account_org_role
		WHERE account_uuid=$1`, account)
	if err != nil {
		return nil, err
	}
	return roles, nil
}

// AccountOrgRolesByAccountOrg selects rows from account_org_role by user
// account UUID and Organization UUID.
func (db *ApplianceDB) AccountOrgRolesByAccountOrg(ctx context.Context,
	account uuid.UUID, org uuid.UUID) ([]string, error) {
	var roles []string
	err := db.SelectContext(ctx, &roles,
		`SELECT role FROM account_org_role
		WHERE account_uuid=$1 AND organization_uuid=$2`,
		account, org)
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

	if role == "" {
		err = dbx.SelectContext(ctx, &roles,
			`SELECT * FROM account_org_role
			WHERE organization_uuid=$1`, org)
	} else {
		err = dbx.SelectContext(ctx, &roles,
			`SELECT * FROM account_org_role
			WHERE organization_uuid=$1
			AND role=$2`, org, role)
	}
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
		 (account_uuid, organization_uuid, role)
		 VALUES (:account_uuid, :organization_uuid, :role)
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
		WHERE account_uuid=:account_uuid AND organization_uuid=:organization_uuid AND role=:role`,
		role)
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

	li.PrimaryOrgRoles, err = db.AccountOrgRolesByAccountOrg(ctx,
		li.Account.UUID, li.Account.OrganizationUUID)
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
