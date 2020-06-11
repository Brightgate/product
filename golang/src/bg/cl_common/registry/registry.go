/*
 * COPYRIGHT 2020 Brightgate Inc.  All rights reserved.
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
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"math/big"
	"net"
	"os"
	"time"

	"github.com/guregu/null"
	vault "github.com/hashicorp/vault/api"
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

// ErrNoAccount indicates an non-existent account
var ErrNoAccount = errors.New("Account does not exist")

// SyncAccountSelfProv performs synchronization of an account to one or
// more CustomerSites.
// - getConfig is a function which serves a source of cfgapi.Handle's for
//   talking to configd.
// - If sites is nil, all sites for the account's organization are synced.
func SyncAccountSelfProv(ctx context.Context,
	db appliancedb.DataStore, getConfig GetConfigHandleFunc,
	accountUUID uuid.UUID, sites []appliancedb.CustomerSite, wait bool) error {

	account, err := db.AccountByUUID(ctx, accountUUID)
	if err != nil {
		if _, ok := err.(appliancedb.NotFoundError); ok {
			return ErrNoAccount
		}
		return err
	}

	secret, err := db.AccountSecretsByUUID(ctx, accountUUID)
	if err != nil {
		// it's ok if secret is nil as long as the reason is
		// that there is no secret; other failures require
		// a hard stop.
		if _, ok := err.(appliancedb.NotFoundError); !ok {
			log.Printf("AccountSecretsByUUID: %v", err)
			return errors.Wrap(err, "getting account secrets")
		}
		log.Printf("AccountSecretsByUUID indicated no secrets")
	}

	person, err := db.PersonByUUID(ctx, account.PersonUUID)
	if err != nil {
		return errors.Wrap(err, "getting person")
	}

	if sites == nil {
		sites, err = db.CustomerSitesByOrganization(ctx, account.OrganizationUUID)
		if err != nil {
			return errors.Wrap(err, "getting customer sites")
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
		log.Printf("syncing to %s %s", site.UUID, site.Name)
		hdl, err := getConfig(site.UUID.String())
		if err != nil {
			return errors.Wrap(err, "getConfig")
		}
		// This is a little trickier than it might seem.  We really do
		// want to defer the handle closes until the end of function
		// (as opposed to end of this loop iteration) because the
		// UserInfo structs we get back from GetUserByUUID() embed the
		// hdl.
		defer hdl.Close()

		// Fetch the old UserInfo structure.  This is also a test to
		// detect if there is a config at all for this site.
		var ui *cfgapi.UserInfo
		ui, err = hdl.GetUserByUUID(accountUUID)
		if err != nil {
			if errors.Cause(err) == cfgapi.ErrNoConfig {
				// No config for this site, keep going.
				log.Printf("no config found for %s %s", site.UUID, site.Name)
				continue
			}
			if _, ok := errors.Cause(err).(cfgapi.NoSuchUserError); ok {
				// Create the new UI and then fall out of this error logic
				ui, err = hdl.NewSelfProvisionUserInfo(account.Email, accountUUID)
				if err != nil {
					return errors.Wrap(err, "NewSelfProvisionUserInfo")
				}
			} else {
				err = errors.Wrap(err, "GetUserByUUID")
				return err
			}
		}

		ui.DisplayName = person.Name
		ui.Email = account.Email
		ui.TelephoneNumber = account.PhoneNumber
		if ui.TelephoneNumber == "" {
			ui.TelephoneNumber = "650-555-1212"
		}
		if secret != nil {
			ui.SetPasswordsByHash(secret.ApplianceUserBcrypt, secret.ApplianceUserMSCHAPv2)
		} else {
			ui.SetNoPassword()
		}
		uis = append(uis, ui)
	}

	var errs []error
	success := 0
	for _, ui := range uis {
		// More work is needed to give the user progress and/or partial
		// results.
		hdl, err := ui.Update(ctx)
		if err != nil {
			errs = append(errs, errors.Wrap(err, "Update"))
			continue
		}
		if wait {
			_, err := hdl.Wait(ctx)
			if err != nil {
				errs = append(errs, errors.Wrap(err, "Wait"))
				continue
			}
		}
		success++
	}
	if errs != nil {
		return errors.Wrapf(errs[0], "partial or total failure. #success=%d, #fail=%d.  First failure is indicated",
			success, len(errs))
	}
	return nil
}

// SyncAccountDeprovision performs password deprovisioning or full
// @/users/<uid>/ deletion of an account to all CustomerSites for the account's
// organization.
//
// - getConfig is a function which serves a source of cfgapi.Handles for
//   talking to configd.
func SyncAccountDeprovision(ctx context.Context,
	db appliancedb.DataStore, getConfig GetConfigHandleFunc,
	accountUUID uuid.UUID, fullDelete bool) error {

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

	uis := make([]*cfgapi.UserInfo, 0)
	// Try to build up all of the handles we need first, then run the
	// deletions.  We want to do our best to see that this operation
	// succeeds or fails as a whole.
	for _, site := range sites {
		var hdl *cfgapi.Handle
		hdl, err = getConfig(site.UUID.String())
		if err != nil {
			return errors.Wrap(err, "getConfig")
		}
		// This is a little trickier than it might seem.  We really do
		// want to defer the handle closes until the end of function
		// (as opposed to end of this loop iteration) because the
		// UserInfo structs we get back from GetUserByUUID() embed the
		// hdl.
		defer hdl.Close()

		ui, err := hdl.GetUserByUUID(account.UUID)
		if err != nil {
			if errors.Cause(err) == cfgapi.ErrNoConfig {
				// No config for this site, keep going.
				continue
			}
			if _, ok := errors.Cause(err).(cfgapi.NoSuchUserError); ok {
				continue
			}
			log.Printf("GetUserByUUID failed unexpectedly: %v", err)
			continue
		}
		// Setup ops to remove passwords
		ui.SetNoPassword()
		uis = append(uis, ui)
	}

	var errs []error
	success := 0
	queued := 0
	for _, ui := range uis {
		var cmdHdl cfgapi.CmdHdl
		if fullDelete {
			cmdHdl = ui.Delete(ctx)
		} else {
			// Just remove password instead of deletion
			ui.SetNoPassword()
			cmdHdl, err = ui.Update(ctx)
			if err != nil {
				errs = append(errs, errors.Wrap(err, "Update"))
				continue
			}
		}
		_, err = cmdHdl.Status(ctx)
		if err != nil {
			if err == cfgapi.ErrQueued || err == cfgapi.ErrInProgress {
				queued++
			} else if err == cfgapi.ErrNoProp {
				// Seems unlikely, but more harmless than not since
				// this is deprovision/delete
				success++
			} else {
				errs = append(errs, errors.Wrap(err, "Status"))
			}
			continue
		}
		success++
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

	err := SyncAccountDeprovision(ctx, db, getConfig, accountUUID, true)
	if err != nil {
		return err
	}
	return db.DeleteAccount(ctx, accountUUID)
}

// AccountDeprovision deprovisions the account specified and drops its
// self provisioning secret.
func AccountDeprovision(ctx context.Context, db appliancedb.DataStore,
	getConfig GetConfigHandleFunc, accountUUID uuid.UUID) error {

	err := SyncAccountDeprovision(ctx, db, getConfig, accountUUID, false)
	if err != nil {
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
	systemReprHWSerial, systemReprMAC string,
	enginePath, componentPath string,
	noEscrow bool) (uuid.UUID, []byte, []byte, []byte, string, error) {

	// Check to see if we'll be able to escrow the appliance's cloud secret
	// when the time comes, and exit early if we don't have the permissions
	// (or can't tell).  This is racy, but it's unlikely to trigger.
	var vaultClient *vault.Client
	var err error
	path, cleanPath := escrowPrivateKeyPath(enginePath, componentPath,
		project, region, regID, appID)
	if !noEscrow {
		vaultClient, err = getVaultClient()
		var caps []string
		if caps, err = vaultClient.Sys().CapabilitiesSelf(path); err != nil {
			return uuid.Nil, nil, nil, nil, "",
				errors.New("unable to determine token " +
					"capabilities; aborting")
		}
		var found bool
		for _, cap := range caps {
			if cap == "create" {
				found = true
				break
			}
		}
		if !found {
			return uuid.Nil, nil, nil, nil, "",
				errors.Errorf("escrow will fail due to "+
					"insufficient Vault permissions at %s",
					path)
		}
	}

	keyPEM, certPEM, err := genPEMKey()
	if err != nil {
		return uuid.Nil, nil, nil, nil, "", err
	}

	if appliance == uuid.Nil {
		appliance = uuid.NewV4()
	}

	reprSerial := null.NewString("", false)
	if systemReprHWSerial != "" {
		_, err = mfg.NewExtSerialFromString(systemReprHWSerial)
		if err != nil {
			return uuid.Nil, nil, nil, nil, "", err
		}
		reprSerial = null.StringFrom(systemReprHWSerial)
	}

	reprMac := null.NewString("", false)
	if systemReprMAC != "" {
		mac, err := net.ParseMAC(systemReprMAC)
		if err != nil {
			return uuid.Nil, nil, nil, nil, "", errors.Wrap(err, "Invalid systemReprMAC")
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
		return uuid.Nil, nil, nil, nil, "", err
	}
	defer tx.Rollback()

	if err = db.InsertApplianceIDTx(ctx, tx, id); err != nil {
		return uuid.Nil, nil, nil, nil, "", err
	}
	if err = db.InsertApplianceKeyTx(ctx, tx, appliance, key); err != nil {
		return uuid.Nil, nil, nil, nil, "", err
	}
	err = tx.Commit()
	if err != nil {
		return uuid.Nil, nil, nil, nil, "", err
	}

	// From here on, return the data in addition to the error, because the
	// caller may still be able to do something with it.
	jsecret, err := applianceSecret(project, region, regID, appID, keyPEM)
	if noEscrow || err != nil {
		return appliance, keyPEM, certPEM, jsecret, "", err
	}

	err = escrowPrivateKey(ctx, vaultClient, appliance, path, jsecret)

	return appliance, keyPEM, certPEM, jsecret, cleanPath, err
}

func applianceSecret(project, region, registry, id string, keyPEM []byte) ([]byte, error) {
	jmap := map[string]string{
		"project":      project,
		"region":       region,
		"registry":     registry,
		"appliance_id": id,
		"private_key":  string(keyPEM),
	}
	return json.MarshalIndent(jmap, "", "\t")
}

func escrowPrivateKeyPath(enginePath, componentPath, project, region, regID, appID string) (string, string) {
	if enginePath == "" {
		enginePath = fmt.Sprintf("secret/%s", project)
	}
	if componentPath == "" {
		componentPath = fmt.Sprintf("appliance-pubkey-escrow/%s/%s/%s",
			region, regID, appID)
	}

	path := fmt.Sprintf("%s/data/%s", enginePath, componentPath)
	// `vault kv get` needs the path without `/data`.
	cleanPath := fmt.Sprintf("%s/%s", enginePath, componentPath)

	return path, cleanPath
}

func getVaultClient() (*vault.Client, error) {
	vaultClient, err := vault.NewClient(nil)
	if err != nil {
		return nil, err
	}

	// If $VAULT_TOKEN wasn't set, then look at ~/.vault-token, like vault
	// itself does.
	if vaultClient.Token() == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, errors.Wrap(err,
				"Couldn't find .vault-token in home directory")
		}
		token, err := ioutil.ReadFile(fmt.Sprintf("%s/.vault-token", home))
		if err != nil {
			return nil, errors.Wrap(err,
				"Couldn't read .vault-token in home directory")
		}
		vaultClient.SetToken(string(token))
	}

	return vaultClient, err
}

func escrowPrivateKey(ctx context.Context, vaultClient *vault.Client, appliance uuid.UUID, path string, jsecret []byte) error {
	vcl := vaultClient.Logical()
	data := map[string]interface{}{
		"data": map[string]interface{}{
			"cloud_secret": string(jsecret),
		},
	}
	_, err := vcl.Write(path, data)
	return err
}
