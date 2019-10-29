/*
 * COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package registry

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math"
	"math/big"
	"net"
	"time"

	"github.com/guregu/null"
	"github.com/pkg/errors"
	"github.com/satori/uuid"

	"bg/cloud_models/appliancedb"
	"bg/common/cfgapi"
	"bg/common/mfg"
)

// PubSub is a part of ApplianceRegistry, describing the publisher/subscriber
// topic that has been set up for a registry.
type PubSub struct {
	Events string `json:"events"`
}

// ApplianceRegistry is the registry configuration that is used to configure new
// appliances.
type ApplianceRegistry struct {
	Project     string `json:"project"`
	Region      string `json:"region"`
	Registry    string `json:"registry"`
	SQLInstance string `json:"cloudsql_instance"`
	DbURI       string `json:"dburi"`
	PubSub      PubSub `json:"pubsub"`
}

func genPEMKey() ([]byte, []byte, error) {
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, err
	}

	notBefore := time.Now()
	notAfter := notBefore.Add(30 * 24 * time.Hour)
	serialMax := big.NewInt(math.MaxInt64)
	serialNumber, err := rand.Int(rand.Reader, serialMax)
	if err != nil {
		return nil, nil, err
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject:      pkix.Name{CommonName: "unused"},
		NotBefore:    notBefore,
		NotAfter:     notAfter,
	}
	derBytes, err := x509.CreateCertificate(rand.Reader, &template,
		&template, &privKey.PublicKey, privKey)
	if err != nil {
		return nil, nil, err
	}

	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privKey),
	})
	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: derBytes,
	})

	return keyPEM, certPEM, nil
}

// NewOrganization registers a new organization in the registry.  It returns the organization UUID.
func NewOrganization(ctx context.Context, db appliancedb.DataStore, name string) (uuid.UUID, error) {
	u := uuid.NewV4()

	err := db.InsertOrganization(ctx, &appliancedb.Organization{
		UUID: u,
		Name: name,
	})
	if err != nil {
		return uuid.Nil, err
	}
	return u, nil
}

// NewSite registers a new site in the registry.  It returns the site UUID.
func NewSite(ctx context.Context, db appliancedb.DataStore, hostProject string, name string, orgUUID uuid.UUID) (uuid.UUID, *appliancedb.SiteCloudStorage, error) {
	u := uuid.NewV4()

	site := &appliancedb.CustomerSite{
		UUID:             u,
		OrganizationUUID: orgUUID,
		Name:             name,
	}

	tx, err := db.BeginTxx(ctx, nil)
	if err != nil {
		return uuid.Nil, nil, err
	}
	defer tx.Rollback()

	cs, err := newBucket(ctx, db, hostProject, site)
	if err != nil {
		return uuid.Nil, nil, errors.Wrap(err, "failed to make site bucket")
	}

	err = db.InsertCustomerSiteTx(ctx, tx, site)
	if err != nil {
		return uuid.Nil, nil, err
	}
	err = db.UpsertCloudStorageTx(ctx, tx, site.UUID, cs)
	if err != nil {
		return uuid.Nil, nil, errors.Wrap(err, "Failed to upsert CloudStorage record")
	}
	tx.Commit()

	return u, cs, nil
}

// NewOAuth2OrganizationRule registers a new oauth2_organization_rule in the registry.
func NewOAuth2OrganizationRule(ctx context.Context, db appliancedb.DataStore,
	provider string, ruleType appliancedb.OAuth2OrgRuleType,
	ruleValue string, organization uuid.UUID) error {

	err := db.InsertOAuth2OrganizationRule(ctx, &appliancedb.OAuth2OrganizationRule{
		Provider:         provider,
		RuleType:         ruleType,
		RuleValue:        ruleValue,
		OrganizationUUID: organization,
	})
	if err != nil {
		return err
	}
	return nil
}

// DeleteOAuth2OrganizationRule registers a new oauth2_organization_rule in the registry.
func DeleteOAuth2OrganizationRule(ctx context.Context, db appliancedb.DataStore,
	provider string, ruleType appliancedb.OAuth2OrgRuleType,
	ruleValue string) (*appliancedb.OAuth2OrganizationRule, error) {

	rule, err := db.OAuth2OrganizationRuleTest(ctx, provider, ruleType, ruleValue)
	if err != nil {
		return nil, err
	}
	err = db.DeleteOAuth2OrganizationRule(ctx, rule)
	if err != nil {
		return nil, err
	}
	return rule, nil
}

// AccountInformation is a convenience type to return detailed information
// about a single user account; it includes associated structures Person,
// Organization, and any OAuth2Identity records.
type AccountInformation struct {
	Account      *appliancedb.Account
	Person       *appliancedb.Person
	Organization *appliancedb.Organization
	OAuth2IDs    []appliancedb.OAuth2Identity
}

// GetAccountInformation returns information about the account specified.
func GetAccountInformation(ctx context.Context, db appliancedb.DataStore, acctUUID uuid.UUID) (*AccountInformation, error) {
	acct, err := db.AccountByUUID(ctx, acctUUID)
	if err != nil {
		return nil, err
	}
	person, err := db.PersonByUUID(ctx, acct.PersonUUID)
	if err != nil {
		return nil, err
	}
	org, err := db.OrganizationByUUID(ctx, acct.OrganizationUUID)
	if err != nil {
		return nil, err
	}
	ids, err := db.OAuth2IdentitiesByAccount(ctx, acctUUID)
	if err != nil {
		return nil, err
	}
	return &AccountInformation{
		Account:      acct,
		Person:       person,
		Organization: org,
		OAuth2IDs:    ids,
	}, nil
}

// GetConfigHandleFunc is a type representing a function which can create
// cfgapi Handles given a siteUUID
type GetConfigHandleFunc func(siteUUID string) (*cfgapi.Handle, error)

// ErrNotSelfProvisioned indicates an account which has not self-provisioned
var ErrNotSelfProvisioned = errors.New("Account is not self provisioned")

// ErrNoAccount indicates an non-existent account
var ErrNoAccount = errors.New("Account does not exist")

// SyncAccountSelfProv performs synchronization of an account to one or
// more CustomerSites.
// - getConfig is a function which serves a source of cfgapi.Handle's for
//   talking to configd.
// - If sites is nil, all sites for the account's organization are synced.
func SyncAccountSelfProv(ctx context.Context,
	db appliancedb.DataStore, getConfig GetConfigHandleFunc,
	accountUUID uuid.UUID, sites []appliancedb.CustomerSite) error {

	account, err := db.AccountByUUID(ctx, accountUUID)
	if err != nil {
		if _, ok := err.(appliancedb.NotFoundError); ok {
			return ErrNoAccount
		}
		return err
	}

	secret, err := db.AccountSecretsByUUID(ctx, accountUUID)
	if err != nil {
		if _, ok := err.(appliancedb.NotFoundError); ok {
			return ErrNotSelfProvisioned
		}
		return err
	}
	if secret.ApplianceUserMSCHAPv2 == "" {
		return ErrNotSelfProvisioned
	}

	person, err := db.PersonByUUID(ctx, account.PersonUUID)
	if err != nil {
		return err
	}

	if sites == nil {
		sites, err = db.CustomerSitesByOrganization(ctx, account.OrganizationUUID)
		if err != nil {
			return err
		}
	} else {
		// Check that input sites are valid
		for _, site := range sites {
			if site.OrganizationUUID != account.OrganizationUUID {
				return errors.Errorf("Site and account organization mismatch: %v / %v",
					site, account)
			}
		}
	}

	// Try to build up all of the config we want first, then run the
	// updates.  We want to do our best to see that this operation succeeds
	// or fails as a whole.
	uis := make([]*cfgapi.UserInfo, 0)
	for _, site := range sites {
		var hdl *cfgapi.Handle
		hdl, err := getConfig(site.UUID.String())
		if err != nil {
			return err
		}
		// Try to get a single property; helps us detect if there is no
		// config at all for this site.
		_, err = hdl.GetProp("@/apversion")
		if err != nil && errors.Cause(err) == cfgapi.ErrNoConfig {
			// No config for this site, keep going.
			continue
		} else if err != nil {
			return err
		}
		var ui *cfgapi.UserInfo
		ui, err = hdl.NewSelfProvisionUserInfo(account.Email, accountUUID)
		if err != nil {
			return err
		}
		ui.DisplayName = person.Name
		ui.Email = account.Email
		ui.TelephoneNumber = account.PhoneNumber
		if ui.TelephoneNumber == "" {
			ui.TelephoneNumber = "650-555-1212"
		}
		uis = append(uis, ui)
	}
	var errs []error
	success := 0
	for _, ui := range uis {
		ops := ui.PropOpsFromPasswordHashes(secret.ApplianceUserBcrypt, secret.ApplianceUserMSCHAPv2)
		_, err := ui.Update(ops...)
		if err != nil {
			errs = append(errs, err)
		} else {
			success++
		}
		// XXX for now we don't wait around to see if the update succeeds.
		// More work is needed to give the user progress and/or partial
		// results.
	}
	if errs != nil {
		return errors.Wrapf(errs[0], "partial or total failure. #success=%d, #fail=%d.  First failure is indicated",
			success, len(errs))
	}
	return nil
}

// SyncAccountDeprovision performs deletion of an account to all CustomerSites
// for the account's organization.
// - getConfig is a function which serves a source of cfgapi.Handles for
//   talking to configd.
func SyncAccountDeprovision(ctx context.Context,
	db appliancedb.DataStore, getConfig GetConfigHandleFunc,
	accountUUID uuid.UUID) error {

	account, err := db.AccountByUUID(ctx, accountUUID)
	if err != nil {
		if _, ok := err.(appliancedb.NotFoundError); ok {
			return ErrNoAccount
		}
		return err
	}
	sites, err := db.CustomerSitesByOrganization(ctx, account.OrganizationUUID)
	if err != nil {
		return err
	}

	// Try to build up all of the handles we need first, then run the
	// deletions.  We want to do our best to see that this operation
	// succeeds or fails as a whole.
	acctProp := fmt.Sprintf("@/users/%s", account.Email)
	hdls := make([]*cfgapi.Handle, 0)
	for _, site := range sites {
		var hdl *cfgapi.Handle
		hdl, err = getConfig(site.UUID.String())
		if err != nil {
			return err
		}
		_, err = hdl.GetProp("@/apversion")
		if err != nil && errors.Cause(err) == cfgapi.ErrNoConfig {
			// No config for this site, keep going.
			continue
		} else if err != nil {
			return err
		}
		// We could test for the existence of the user here, but we
		// want the greatest assurance that the user is gone, so
		// we unconditionally blow it away below.
		hdls = append(hdls, hdl)
	}

	var errs []error
	success := 0
	queued := 0
	ops := []cfgapi.PropertyOp{
		{Op: cfgapi.PropDelete, Name: acctProp},
	}
	for _, hdl := range hdls {
		cmdHdl := hdl.Execute(ctx, ops)
		_, err = cmdHdl.Status(ctx)
		if err != nil {
			if err == cfgapi.ErrQueued || err == cfgapi.ErrInProgress {
				queued++
			} else {
				errs = append(errs, err)
			}
		} else {
			success++
		}
		// XXX for now we don't wait around to see if the update succeeds.
		// More work is needed to give the user progress and/or partial
		// results.
	}
	if errs != nil {
		return errors.Wrapf(errs[0], "partial or total failure. #success=%d, #queued=%d, #fail=%d.  First failure is indicated",
			success, queued, len(errs))
	}
	return nil
}

// DeleteAccountInformation deprovisions and deletes the account specified.
func DeleteAccountInformation(ctx context.Context, db appliancedb.DataStore,
	getConfig GetConfigHandleFunc, accountUUID uuid.UUID) error {

	err := SyncAccountDeprovision(ctx, db, getConfig, accountUUID)
	if err != nil && err != ErrNotSelfProvisioned {
		return err
	}
	return db.DeleteAccount(ctx, accountUUID)
}

// AccountDeprovision deprovisions the account specified and drops its
// self provisioning secret.
func AccountDeprovision(ctx context.Context, db appliancedb.DataStore,
	getConfig GetConfigHandleFunc, accountUUID uuid.UUID) error {

	err := SyncAccountDeprovision(ctx, db, getConfig, accountUUID)
	if err != nil && err != ErrNotSelfProvisioned {
		return err
	}
	return db.DeleteAccountSecrets(ctx, accountUUID)
}

// NewAppliance registers a new appliance and associated it with
// a site (possibly the sentinal null site).
//
// If appliance is uuid.Nil, a uuid is selected.
func NewAppliance(ctx context.Context, db appliancedb.DataStore,
	appliance uuid.UUID, site uuid.UUID,
	project, region, regID, appID string,
	systemReprHWSerial, systemReprMAC string) (uuid.UUID, []byte, []byte, error) {

	keyPEM, certPEM, err := genPEMKey()
	if err != nil {
		return uuid.Nil, nil, nil, err
	}

	if appliance == uuid.Nil {
		appliance = uuid.NewV4()
	}

	reprSerial := null.NewString("", false)
	if systemReprHWSerial != "" {
		_, err = mfg.NewExtSerialFromString(systemReprHWSerial)
		if err != nil {
			return uuid.Nil, nil, nil, err
		}
		reprSerial = null.StringFrom(systemReprHWSerial)
	}

	reprMac := null.NewString("", false)
	if systemReprMAC != "" {
		mac, err := net.ParseMAC(systemReprMAC)
		if err != nil {
			return uuid.Nil, nil, nil, errors.Wrap(err, "Invalid systemReprMAC")
		}
		reprMac = null.StringFrom(mac.String())
	}

	id := &appliancedb.ApplianceID{
		ApplianceUUID:      appliance,
		SiteUUID:           site,
		GCPProject:         project,
		GCPRegion:          region,
		ApplianceReg:       regID,
		ApplianceRegID:     appID,
		SystemReprHWSerial: reprSerial,
		SystemReprMAC:      reprMac,
	}
	key := &appliancedb.AppliancePubKey{
		Format: "RS256_X509",
		Key:    string(certPEM),
	}

	tx, err := db.BeginTxx(ctx, nil)
	if err != nil {
		return uuid.Nil, nil, nil, err
	}
	defer tx.Rollback()

	if err = db.InsertApplianceIDTx(ctx, tx, id); err != nil {
		return uuid.Nil, nil, nil, err
	}
	if err = db.InsertApplianceKeyTx(ctx, tx, appliance, key); err != nil {
		return uuid.Nil, nil, nil, err
	}
	err = tx.Commit()
	if err != nil {
		return uuid.Nil, nil, nil, err
	}
	return appliance, keyPEM, certPEM, nil
}
