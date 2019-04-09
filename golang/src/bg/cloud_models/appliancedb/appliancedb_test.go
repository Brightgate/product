/*
 * COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 *
 */

package appliancedb

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"sort"
	"testing"
	"time"

	"bg/common/briefpg"

	"github.com/guregu/null"
	"github.com/satori/uuid"
	"github.com/stretchr/testify/require"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

const (
	templateDBName  = "appliancedb_template"
	templateDBArg   = "TEMPLATE=" + templateDBName
	unitTestSQLFile = "unittest-data.sql"
	testProject     = "test-project"
	testRegion      = "test-region"
	testReg         = "test-registry"
	testRegID       = "test-appliance"
	testClientID1   = "projects/test-project/locations/test-region/registries/test-registry/appliances/test-appliance-1"
	app1Str         = "00000001-0001-0001-0001-000000000001"
	app2Str         = "00000002-0002-0002-0002-000000000002"
	appNStr         = "00000009-0009-0009-0009-000000000009"
	site1Str        = "10000001-0001-0001-0001-000000000001"
	site2Str        = "10000002-0002-0002-0002-000000000002"
	org1Str         = "20000001-0001-0001-0001-000000000001"
	org2Str         = "20000002-0002-0002-0002-000000000002"
	person1Str      = "30000001-0001-0001-0001-000000000001"
	person2Str      = "30000002-0002-0002-0002-000000000002"
	account1Str     = "40000001-0001-0001-0001-000000000001"
	account2Str     = "40000002-0002-0002-0002-000000000002"
	badStr          = "ffffffff-ffff-ffff-ffff-ffffffffffff"
)

var (
	databaseURI string
	bpg         *briefpg.BriefPG

	badUUID = uuid.Must(uuid.FromString(badStr))

	testOrg1 = Organization{
		UUID: uuid.Must(uuid.FromString(org1Str)),
		Name: "org1",
	}
	testOrg2 = Organization{
		UUID: uuid.Must(uuid.FromString(org2Str)),
		Name: "org2",
	}

	testSite1 = CustomerSite{
		UUID:             uuid.Must(uuid.FromString(site1Str)),
		OrganizationUUID: testOrg1.UUID,
		Name:             "site1",
	}
	testSite2 = CustomerSite{
		UUID:             uuid.Must(uuid.FromString(site2Str)),
		OrganizationUUID: testOrg2.UUID,
		Name:             "site2",
	}

	testID1 = ApplianceID{
		ApplianceUUID:  uuid.Must(uuid.FromString(app1Str)),
		SiteUUID:       uuid.Must(uuid.FromString(site1Str)),
		GCPProject:     testProject,
		GCPRegion:      testRegion,
		ApplianceReg:   testReg,
		ApplianceRegID: testRegID + "-1",
	}
	testID2 = ApplianceID{
		ApplianceUUID:  uuid.Must(uuid.FromString(app2Str)),
		SiteUUID:       uuid.Must(uuid.FromString(site2Str)),
		GCPProject:     testProject,
		GCPRegion:      testRegion,
		ApplianceReg:   testReg,
		ApplianceRegID: testRegID + "-2",
	}
	testIDN = ApplianceID{
		ApplianceUUID:  uuid.Must(uuid.FromString(appNStr)),
		SiteUUID:       NullSiteUUID, // Sentinel UUID
		GCPProject:     testProject,
		GCPRegion:      testRegion,
		ApplianceReg:   testReg,
		ApplianceRegID: testRegID + "-N",
	}
	testPerson1 = Person{
		UUID:         uuid.Must(uuid.FromString(person1Str)),
		Name:         "Foo Bar",
		PrimaryEmail: "foo@foo.net",
	}
	testPerson2 = Person{
		UUID:         uuid.Must(uuid.FromString(person2Str)),
		Name:         "Bar Baz",
		PrimaryEmail: "bar@bar.net",
	}
	testAccount1 = Account{
		UUID:             uuid.Must(uuid.FromString(account1Str)),
		Email:            "foo@foo.net",
		PhoneNumber:      "555-1212",
		PersonUUID:       testPerson1.UUID,
		OrganizationUUID: testOrg1.UUID,
	}
	testAccount2 = Account{
		UUID:             uuid.Must(uuid.FromString(account2Str)),
		Email:            "bar@bar.net",
		PhoneNumber:      "555-2222",
		PersonUUID:       testPerson2.UUID,
		OrganizationUUID: testOrg1.UUID,
	}
)

func setupLogging(t *testing.T) (*zap.Logger, *zap.SugaredLogger) {
	logger := zaptest.NewLogger(t)
	slogger := logger.Sugar()
	return logger, slogger
}

func dumpfail(ctx context.Context, t *testing.T, bpg *briefpg.BriefPG, dbName string) {
	if !t.Failed() {
		return
	}
	fname := t.Name() + ".sql.dump"
	dumpfile, err := os.Create(fname)
	if err != nil {
		return
	}
	defer dumpfile.Close()
	err = bpg.DumpDB(ctx, dbName, dumpfile)
	if err != nil {
		t.Errorf("Failing: Saved database dump to %s", fname)
	}
}

// Test serialization to JSON
func TestJSON(t *testing.T) {
	_, _ = setupLogging(t)
	assert := require.New(t)

	j, _ := json.Marshal(&testID1)
	assert.JSONEq(`{
		"appliance_uuid":"00000001-0001-0001-0001-000000000001",
		"site_uuid":"10000001-0001-0001-0001-000000000001",
		"gcp_project":"test-project",
		"gcp_region":"test-region",
		"appliance_reg":"test-registry",
		"appliance_reg_id":"test-appliance-1",
		"system_repr_hwserial":null,
		"system_repr_mac":null}`, string(j))

	ap := &AppliancePubKey{
		Expiration: null.NewTime(time.Time{}, false),
	}
	j, _ = json.Marshal(ap)
	assert.JSONEq(`{"id":0, "format":"", "key":"", "expiration":null}`, string(j))
	ap.Expiration = null.TimeFrom(time.Date(2018, 1, 1, 0, 0, 0, 0, time.UTC))
	j, _ = json.Marshal(ap)
	assert.JSONEq(`{"id":0, "format":"", "key":"", "expiration": "2018-01-01T00:00:00Z"}`, string(j))

	acs := &SiteCloudStorage{}
	j, _ = json.Marshal(acs)
	assert.JSONEq(`{"bucket":"", "provider":""}`, string(j))
}

func TestApplianceIDStruct(t *testing.T) {
	_, _ = setupLogging(t)
	assert := require.New(t)

	x := testID1
	assert.NotEmpty(x.String())
	x.SystemReprMAC = null.NewString("123", true)
	x.SystemReprHWSerial = null.NewString("123", true)
	assert.NotEmpty(x.String())
	assert.Equal(testClientID1, x.ClientID())
}

type dbTestFunc func(*testing.T, DataStore, *zap.Logger, *zap.SugaredLogger)

// mkOrgSiteApp is a help function to prep the database: if not nil, add
// org, site, and/or appliance to the DB
func mkOrgSiteApp(t *testing.T, ds DataStore, org *Organization, site *CustomerSite, app *ApplianceID) {
	ctx := context.Background()
	assert := require.New(t)

	if org != nil {
		err := ds.InsertOrganization(ctx, org)
		assert.NoError(err, "expected Insert Organization to succeed")
	}
	if site != nil {
		err := ds.InsertCustomerSite(ctx, site)
		assert.NoError(err, "expected Insert Site to succeed")
	}
	if app != nil {
		err := ds.InsertApplianceID(ctx, app)
		assert.NoError(err, "expected Insert App to succeed")
	}
}

func testPing(t *testing.T, ds DataStore, logger *zap.Logger, slogger *zap.SugaredLogger) {
	assert := require.New(t)
	err := ds.Ping()
	assert.NoError(err)
}

// Test insertion into Heartbeat ingest table.  subtest of TestDatabaseModel
func testHeartbeatIngest(t *testing.T, ds DataStore, logger *zap.Logger, slogger *zap.SugaredLogger) {
	ctx := context.Background()
	assert := require.New(t)

	hb := HeartbeatIngest{
		SiteUUID: testSite1.UUID,
		BootTS:   time.Now(),
		RecordTS: time.Now(),
	}
	err := ds.InsertHeartbeatIngest(ctx, &hb)
	// expect to fail because UUID doesn't exist
	assert.Error(err)

	mkOrgSiteApp(t, ds, &testOrg1, &testSite1, &testID1)

	err = ds.InsertHeartbeatIngest(ctx, &hb)
	// expect to succeed now
	assert.NoError(err)
}

// Test insert of registry data.  subtest of TestDatabaseModel
func testApplianceID(t *testing.T, ds DataStore, logger *zap.Logger, slogger *zap.SugaredLogger) {
	ctx := context.Background()
	assert := require.New(t)

	ids, err := ds.AllApplianceIDs(ctx)
	assert.NoError(err)
	assert.Len(ids, 0)

	_, err = ds.ApplianceIDByUUID(ctx, testID1.ApplianceUUID)
	assert.Error(err)
	assert.IsType(err, NotFoundError{})

	_, err = ds.ApplianceIDByClientID(ctx, "not-a-real-clientid")
	assert.Error(err)
	assert.IsType(err, NotFoundError{})

	mkOrgSiteApp(t, ds, &testOrg1, &testSite1, &testID1)

	// Test that a second insert of the same UUID fails
	err = ds.InsertApplianceID(ctx, &testID1)
	assert.Error(err, "expected Insert to fail")

	// Test lookup ops
	id1, err := ds.ApplianceIDByUUID(ctx, testID1.ApplianceUUID)
	assert.NoError(err)
	assert.Equal(id1.ApplianceUUID, testID1.ApplianceUUID)

	id1, err = ds.ApplianceIDByClientID(ctx, testClientID1)
	assert.NoError(err)
	assert.Equal(id1.ApplianceUUID, testID1.ApplianceUUID)

	mkOrgSiteApp(t, ds, &testOrg2, &testSite2, &testID2)

	// Test getting complete set of appliance
	ids, err = ds.AllApplianceIDs(ctx)
	assert.NoError(err)
	assert.Len(ids, 2)

	// Test null site sentinel
	err = ds.InsertApplianceID(ctx, &testIDN)
	assert.NoError(err)
}

// Test operations related to appliance public keys.  subtest of TestDatabaseModel
func testAppliancePubKey(t *testing.T, ds DataStore, logger *zap.Logger, slogger *zap.SugaredLogger) {
	ctx := context.Background()
	assert := require.New(t)

	mkOrgSiteApp(t, ds, &testOrg1, &testSite1, &testID1)

	k := &AppliancePubKey{
		Format:     "RS256_X509",
		Key:        "not a real key",
		Expiration: null.NewTime(time.Now(), true),
	}
	err := ds.InsertApplianceKeyTx(ctx, nil, testID1.ApplianceUUID, k)
	assert.NoError(err)

	keys, err := ds.KeysByUUID(ctx, testID1.ApplianceUUID)
	assert.NoError(err)
	assert.Len(keys, 1)

	keys, err = ds.KeysByUUID(ctx, testID2.ApplianceUUID)
	assert.NoError(err)
	assert.Len(keys, 0)
}

// Test Organization APIs.  subtest of TestDatabaseModel
func testOrganization(t *testing.T, ds DataStore, logger *zap.Logger, slogger *zap.SugaredLogger) {
	ctx := context.Background()
	assert := require.New(t)

	// Null organization is 1
	orgs, err := ds.AllOrganizations(ctx)
	assert.NoError(err, "expected success")
	assert.Len(orgs, 1)

	org, err := ds.OrganizationByUUID(ctx, testOrg1.UUID)
	assert.Error(err)
	assert.IsType(err, NotFoundError{})

	err = ds.InsertOrganization(ctx, &testOrg1)
	assert.NoError(err, "expected Insert to succeed")

	orgs, err = ds.AllOrganizations(ctx)
	assert.NoError(err, "expected success")
	assert.Len(orgs, 2)

	// Test that a second insert of the same UUID fails
	err = ds.InsertOrganization(ctx, &testOrg1)
	assert.Error(err, "expected Insert to fail")

	org, err = ds.OrganizationByUUID(ctx, testOrg1.UUID)
	assert.NoError(err, "expected success")
	assert.Equal(testOrg1, *org)
}

// Test insert of customer site data.  subtest of TestDatabaseModel
func testCustomerSite(t *testing.T, ds DataStore, logger *zap.Logger, slogger *zap.SugaredLogger) {
	ctx := context.Background()
	assert := require.New(t)

	ids, err := ds.AllCustomerSites(ctx)
	assert.NoError(err)
	// Sentinel UUID
	assert.Len(ids, 1)
	assert.Equal(uuid.Nil, ids[0].UUID)

	_, err = ds.CustomerSiteByUUID(ctx, testID1.SiteUUID)
	assert.Error(err)
	assert.IsType(err, NotFoundError{})

	mkOrgSiteApp(t, ds, &testOrg1, &testSite1, &testID1)

	// Test that a second insert of the same UUID fails
	err = ds.InsertCustomerSite(ctx, &testSite1)
	assert.Error(err, "expected Insert to fail")

	s1, err := ds.CustomerSiteByUUID(ctx, testID1.SiteUUID)
	assert.NoError(err)
	assert.Equal(s1.UUID, testID1.SiteUUID)

	mkOrgSiteApp(t, ds, &testOrg2, &testSite2, &testID2)
	ids, err = ds.AllCustomerSites(ctx)
	assert.NoError(err)
	assert.Len(ids, 3)

	// Lookup by Organization
	sites, err := ds.CustomerSitesByOrganization(ctx, testOrg1.UUID)
	assert.NoError(err, "expected success")
	assert.Len(sites, 1)

	// Lookup non-existent org, should get no sites
	sites, err = ds.CustomerSitesByOrganization(ctx, uuid.NewV4())
	assert.Len(sites, 0)
	assert.NoError(err)
}

// Test OAuth2OrganizationRule APIs.  subtest of TestDatabaseModel
func testOAuth2OrganizationRule(t *testing.T, ds DataStore, logger *zap.Logger, slogger *zap.SugaredLogger) {
	ctx := context.Background()
	assert := require.New(t)
	var err error

	const testDomain = "brightgate-test.net"
	const testTenant = "tenant.brightgate-test.net"
	const testEmail = "foo@brightgate-test.net"
	const testProvider = "google"

	mkOrgSiteApp(t, ds, &testOrg1, &testSite1, nil)
	// Add second customer site under same org, for later testing
	s2 := &CustomerSite{
		UUID:             uuid.NewV4(),
		OrganizationUUID: testOrg1.UUID,
		Name:             "7411",
	}
	err = ds.InsertCustomerSite(ctx, s2)
	assert.NoError(err, "expected Insert to succeed")

	doms, err := ds.AllOAuth2OrganizationRules(ctx)
	assert.NoError(err, "expected success")
	assert.Len(doms, 0)

	testCases := map[OAuth2OrgRuleType]string{
		RuleTypeTenant: testTenant,
		RuleTypeDomain: testDomain,
		RuleTypeEmail:  testEmail,
	}

	rules, err := ds.AllOAuth2OrganizationRules(ctx)
	assert.NoError(err, "expected success")
	assert.Len(rules, 0)

	for ruleType, ruleVal := range testCases {
		rTest := &OAuth2OrganizationRule{testProvider, ruleType, ruleVal, testOrg1.UUID}

		err = ds.InsertOAuth2OrganizationRule(ctx, rTest)
		assert.NoError(err, "expected Insert to succeed")
		// Test that a second insert of the same UUID fails
		err = ds.InsertOAuth2OrganizationRule(ctx, rTest)
		assert.Error(err, "expected Insert to fail")

		// Test successful rule
		rule, err := ds.OAuth2OrganizationRuleTest(ctx, testProvider, ruleType, ruleVal)
		assert.NoError(err, "expected success")
		assert.Equal(*rTest, *rule)

		// Test unsuccessful rule
		rule, err = ds.OAuth2OrganizationRuleTest(ctx, testProvider, ruleType, "foo")
		assert.Error(err, "expected error")
		assert.IsType(err, NotFoundError{})
	}

	rules, err = ds.AllOAuth2OrganizationRules(ctx)
	assert.NoError(err, "expected success")
	assert.Len(rules, 3)
}

// Test Person APIs.  subtest of TestDatabaseModel
func testPerson(t *testing.T, ds DataStore, logger *zap.Logger, slogger *zap.SugaredLogger) {
	ctx := context.Background()
	assert := require.New(t)
	var err error

	mkOrgSiteApp(t, ds, &testOrg1, &testSite1, nil)

	person, err := ds.PersonByUUID(ctx, testPerson1.UUID)
	assert.Error(err)
	assert.IsType(err, NotFoundError{})

	err = ds.InsertPerson(ctx, &testPerson1)
	assert.NoError(err, "expected success")

	// Try again
	err = ds.InsertPerson(ctx, &testPerson1)
	assert.Error(err)

	err = ds.InsertPerson(ctx, &testPerson2)
	assert.NoError(err, "expected success")

	person, err = ds.PersonByUUID(ctx, testPerson1.UUID)
	assert.NoError(err)
	assert.Equal(testPerson1, *person)
}

// Test Account APIs.  subtest of TestDatabaseModel
func testAccount(t *testing.T, ds DataStore, logger *zap.Logger, slogger *zap.SugaredLogger) {
	ctx := context.Background()
	assert := require.New(t)
	var err error

	// Setup
	mkOrgSiteApp(t, ds, &testOrg1, &testSite1, nil)
	err = ds.InsertPerson(ctx, &testPerson1)
	assert.NoError(err, "expected success")
	err = ds.InsertPerson(ctx, &testPerson2)
	assert.NoError(err, "expected success")

	accts, err := ds.AccountsByOrganization(ctx, testOrg1.UUID)
	assert.NoError(err)
	assert.Len(accts, 0)

	sites, err := ds.CustomerSitesByAccount(ctx, testAccount1.UUID)
	assert.NoError(err)
	assert.Len(sites, 0)

	err = ds.InsertAccount(ctx, &testAccount1)
	assert.NoError(err, "expected success")

	// Try again
	err = ds.InsertAccount(ctx, &testAccount1)
	assert.Error(err)

	err = ds.InsertAccount(ctx, &testAccount2)
	assert.NoError(err, "expected success")

	accts, err = ds.AccountsByOrganization(ctx, testOrg1.UUID)
	assert.NoError(err)
	assert.Len(accts, 2)

	sites, err = ds.CustomerSitesByAccount(ctx, testAccount1.UUID)
	assert.NoError(err)
	assert.Len(sites, 1)
	assert.Equal(testSite1, sites[0])

	acct, err := ds.AccountByUUID(ctx, testAccount1.UUID)
	assert.NoError(err)
	assert.Equal(testAccount1, *acct)

	acct, err = ds.AccountByUUID(ctx, badUUID)
	assert.Error(err)
	assert.IsType(err, NotFoundError{})

	ds.AccountSecretsSetPassphrase([]byte("I LIKE COCONUTS"))
	_, err = ds.AccountSecretsByUUID(ctx, testAccount1.UUID)
	assert.Error(err)
	assert.IsType(err, NotFoundError{})

	testAs := &AccountSecrets{testAccount1.UUID, "k1", "regime", time.Now(), "k2", "regime", time.Now()}
	err = ds.UpsertAccountSecrets(ctx, testAs)
	assert.NoError(err, "expected success")

	// Try again
	err = ds.UpsertAccountSecrets(ctx, testAs)
	assert.NoError(err, "expected success")

	as, err := ds.AccountSecretsByUUID(ctx, testAccount1.UUID)
	assert.NoError(err)
	assert.Equal(testAs.ApplianceUserBcrypt, as.ApplianceUserBcrypt)
	assert.Equal(testAs.ApplianceUserMSCHAPv2, as.ApplianceUserMSCHAPv2)
	assert.WithinDuration(time.Now(), as.ApplianceUserMSCHAPv2Ts, time.Second)
	assert.WithinDuration(time.Now(), as.ApplianceUserBcryptTs, time.Second)

	// Bad passphrase should be detected
	ds.AccountSecretsSetPassphrase([]byte("I DO NOT LIKE COCONUTS"))
	as, err = ds.AccountSecretsByUUID(ctx, testAccount1.UUID)
	assert.Error(err)
}

// Test AccountOrgRole APIs.  subtest of TestDatabaseModel
func testAccountOrgRole(t *testing.T, ds DataStore, logger *zap.Logger, slogger *zap.SugaredLogger) {
	ctx := context.Background()
	assert := require.New(t)
	var err error

	adminRole := AccountOrgRole{
		AccountUUID:      testAccount1.UUID,
		OrganizationUUID: testAccount1.OrganizationUUID,
		Role:             "admin",
	}
	// Not really realistic as we would not normally add both
	// admin and user roles, but we are testing the assertion
	// that a user may have more than one role.
	userRole := AccountOrgRole{
		AccountUUID:      testAccount1.UUID,
		OrganizationUUID: testAccount1.OrganizationUUID,
		Role:             "user",
	}

	// Setup
	mkOrgSiteApp(t, ds, &testOrg1, &testSite1, nil)
	err = ds.InsertPerson(ctx, &testPerson1)
	assert.NoError(err)
	err = ds.InsertPerson(ctx, &testPerson2)
	assert.NoError(err)

	accts, err := ds.AccountsByOrganization(ctx, testOrg1.UUID)
	assert.NoError(err)
	assert.Len(accts, 0)

	sites, err := ds.CustomerSitesByAccount(ctx, testAccount1.UUID)
	assert.NoError(err)
	assert.Len(sites, 0)

	err = ds.InsertAccount(ctx, &testAccount1)
	assert.NoError(err)
	err = ds.InsertAccount(ctx, &testAccount2)
	assert.NoError(err)

	// similar to the userids google uses
	const testSubj1 = "123456789012345678900"
	id1 := &OAuth2Identity{
		Subject:     testSubj1,
		Provider:    "google",
		AccountUUID: testAccount1.UUID,
	}
	err = ds.InsertOAuth2Identity(ctx, id1)
	assert.NoError(err, "expected success")

	roles, err := ds.AccountOrgRolesByAccount(ctx, testAccount1.UUID)
	assert.NoError(err)
	assert.Len(roles, 0)

	rolesStrs, err := ds.AccountOrgRolesByAccountOrg(ctx, testAccount1.UUID, testOrg1.UUID)
	assert.NoError(err)
	assert.Len(rolesStrs, 0)

	roles, err = ds.AccountOrgRolesByOrg(ctx, testAccount1.OrganizationUUID, "admin")
	assert.NoError(err)
	assert.Len(roles, 0)

	roles, err = ds.AccountOrgRolesByOrg(ctx, testAccount1.OrganizationUUID, "")
	assert.NoError(err)
	assert.Len(roles, 0)

	li, err := ds.LoginInfoByProviderAndSubject(ctx, "google", testSubj1)
	assert.NoError(err, "expected success")
	assert.Equal(&LoginInfo{
		Person:           testPerson1,
		Account:          testAccount1,
		OAuth2IdentityID: id1.ID,
		PrimaryOrgRoles:  nil,
	}, li)

	err = ds.InsertAccountOrgRole(ctx, &adminRole)
	assert.NoError(err)
	// Same again
	err = ds.InsertAccountOrgRole(ctx, &adminRole)
	assert.NoError(err)

	err = ds.InsertAccountOrgRole(ctx, &userRole)
	assert.NoError(err)

	li, err = ds.LoginInfoByProviderAndSubject(ctx, "google", testSubj1)
	assert.NoError(err, "expected success")
	sort.Strings(li.PrimaryOrgRoles)
	assert.Equal(&LoginInfo{
		Person:           testPerson1,
		Account:          testAccount1,
		OAuth2IdentityID: id1.ID,
		PrimaryOrgRoles:  []string{"admin", "user"},
	}, li)

	roles, err = ds.AccountOrgRolesByAccount(ctx, testAccount1.UUID)
	assert.NoError(err)
	assert.Len(roles, 2)
	assert.ElementsMatch([]AccountOrgRole{userRole, adminRole}, roles)

	rolesStrs, err = ds.AccountOrgRolesByAccountOrg(ctx, testAccount1.UUID, testOrg1.UUID)
	assert.NoError(err)
	assert.Len(rolesStrs, 2)
	sort.Strings(rolesStrs)
	assert.ElementsMatch([]string{"admin", "user"}, rolesStrs)

	roles, err = ds.AccountOrgRolesByOrg(ctx, testAccount1.OrganizationUUID, "")
	assert.NoError(err)
	assert.Len(roles, 2)
	assert.ElementsMatch([]AccountOrgRole{adminRole, userRole}, roles)

	roles, err = ds.AccountOrgRolesByOrg(ctx, testAccount1.OrganizationUUID, "admin")
	assert.NoError(err)
	assert.Len(roles, 1)
	assert.ElementsMatch([]AccountOrgRole{adminRole}, roles)

	roles, err = ds.AccountOrgRolesByOrg(ctx, testAccount1.OrganizationUUID, "user")
	assert.NoError(err)
	assert.Len(roles, 1)
	assert.ElementsMatch([]AccountOrgRole{userRole}, roles)

	err = ds.DeleteAccountOrgRole(ctx, &userRole)
	assert.NoError(err)

	roles, err = ds.AccountOrgRolesByAccount(ctx, testAccount1.UUID)
	assert.NoError(err)
	assert.Equal(adminRole, roles[0])

	rolesStrs, err = ds.AccountOrgRolesByAccountOrg(ctx, testAccount1.UUID, testOrg1.UUID)
	assert.NoError(err)
	assert.Equal([]string{"admin"}, rolesStrs)

	err = ds.DeleteAccountOrgRole(ctx, &adminRole)
	assert.NoError(err)

	roles, err = ds.AccountOrgRolesByAccount(ctx, testAccount1.UUID)
	assert.NoError(err)
	assert.Len(roles, 0)

	rolesStrs, err = ds.AccountOrgRolesByAccountOrg(ctx, testAccount1.UUID, testOrg1.UUID)
	assert.NoError(err)
	assert.Len(rolesStrs, 0)

	roles, err = ds.AccountOrgRolesByOrg(ctx, testAccount1.OrganizationUUID, "")
	assert.NoError(err)
	assert.Len(roles, 0)
}

func testOAuth2Identity(t *testing.T, ds DataStore, logger *zap.Logger, slogger *zap.SugaredLogger) {
	ctx := context.Background()
	assert := require.New(t)
	var err error

	// Setup
	mkOrgSiteApp(t, ds, &testOrg1, &testSite1, nil)
	err = ds.InsertPerson(ctx, &testPerson1)
	assert.NoError(err, "expected success")
	err = ds.InsertPerson(ctx, &testPerson2)
	assert.NoError(err, "expected success")
	err = ds.InsertAccount(ctx, &testAccount1)
	assert.NoError(err, "expected success")
	err = ds.InsertAccount(ctx, &testAccount2)
	assert.NoError(err, "expected success")

	// similar to the userids google uses
	const testSubj1 = "123456789012345678900"
	id1 := &OAuth2Identity{
		Subject:     testSubj1,
		Provider:    "google",
		AccountUUID: testAccount1.UUID,
	}
	const testSubj2 = "987654321098765432100"
	id2 := &OAuth2Identity{
		Subject:     testSubj2,
		Provider:    "google",
		AccountUUID: testAccount2.UUID,
	}

	err = ds.InsertOAuth2Identity(ctx, id1)
	assert.NoError(err, "expected success")

	err = ds.InsertOAuth2Identity(ctx, id2)
	assert.NoError(err, "expected success")

	assert.NotEqual(id1.ID, id2.ID)

	// Try #1 again, expect error
	err = ds.InsertOAuth2Identity(ctx, id1)
	assert.Error(err, "expected error")

	ids, err := ds.OAuth2IdentitiesByAccount(ctx, testAccount1.UUID)
	assert.NoError(err)
	assert.Len(ids, 1)
	assert.Equal(*id1, ids[0])

	li, err := ds.LoginInfoByProviderAndSubject(ctx, "google", testSubj1)
	assert.NoError(err, "expected success")
	assert.Equal(&LoginInfo{
		Person:           testPerson1,
		Account:          testAccount1,
		OAuth2IdentityID: id1.ID,
		PrimaryOrgRoles:  nil,
	}, li)

	li, err = ds.LoginInfoByProviderAndSubject(ctx, "invalid", "invalid")
	assert.Error(err)
	assert.IsType(err, NotFoundError{})

	at := &OAuth2AccessToken{
		OAuth2IdentityID: id2.ID,
		Token:            "I like coconuts",
		Expires:          time.Now(),
	}
	err = ds.InsertOAuth2AccessToken(ctx, at)
	assert.NoError(err, "expected success")

	rt := &OAuth2RefreshToken{
		OAuth2IdentityID: id1.ID,
		Token:            "I like coconuts! A lot!",
	}
	err = ds.UpsertOAuth2RefreshToken(ctx, rt)
	assert.NoError(err, "expected success")
	err = ds.UpsertOAuth2RefreshToken(ctx, rt)
	assert.NoError(err, "expected success")
}

// Test insertion into cloudstorage table.  subtest of TestDatabaseModel
func testCloudStorage(t *testing.T, ds DataStore, logger *zap.Logger, slogger *zap.SugaredLogger) {
	var err error
	ctx := context.Background()
	assert := require.New(t)

	mkOrgSiteApp(t, ds, &testOrg1, &testSite1, &testID1)

	cs1 := &SiteCloudStorage{
		Bucket:   "test-bucket",
		Provider: "gcs",
	}
	err = ds.UpsertCloudStorage(ctx, testSite1.UUID, cs1)
	assert.NoError(err)

	cs2, err := ds.CloudStorageByUUID(ctx, testSite1.UUID)
	assert.NoError(err)
	assert.Equal(*cs1, *cs2)

	cs2.Provider = "s3"
	err = ds.UpsertCloudStorage(ctx, testSite1.UUID, cs2)
	assert.NoError(err)

	cs3, err := ds.CloudStorageByUUID(ctx, testSite1.UUID)
	assert.NoError(err)
	assert.Equal(*cs2, *cs3)
}

// Test loading and using a more realistic set of registry data.  subtest of TestDatabaseModel
func testUnittestData(t *testing.T, ds DataStore, logger *zap.Logger, slogger *zap.SugaredLogger) {
	ctx := context.Background()
	assert := require.New(t)

	// Cast down to underlying struct, which embeds sql.DB; use that to
	// load the unit test data file.
	adb := ds.(*ApplianceDB)
	bytes, err := ioutil.ReadFile(unitTestSQLFile)
	assert.NoError(err)
	_, err = adb.ExecContext(ctx, string(bytes))
	assert.NoError(err)

	ids, err := ds.AllApplianceIDs(ctx)
	assert.NoError(err)
	assert.Len(ids, 2)

	// Test "appliance with keys" case
	keys, err := ds.KeysByUUID(ctx, testID1.ApplianceUUID)
	assert.NoError(err)
	assert.Len(keys, 2)

	// Test "appliance with no keys" case
	keys, err = ds.KeysByUUID(ctx, testSite2.UUID)
	assert.NoError(err)
	assert.Len(keys, 0)

	// Test "appliance with cloud storage" case
	cs, err := ds.CloudStorageByUUID(ctx, testSite1.UUID)
	assert.NoError(err)
	assert.Equal(cs.Provider, "gcs")

	// Test "appliance with no cloud storage" case
	cs, err = ds.CloudStorageByUUID(ctx, testSite2.UUID)
	assert.Error(err)
	assert.IsType(err, NotFoundError{})
	assert.Nil(cs)

	// Test "appliance with config store" case
	cfg, err := ds.ConfigStoreByUUID(ctx, testSite1.UUID)
	assert.NoError(err)
	assert.Equal([]byte{0xde, 0xad, 0xbe, 0xef}, cfg.RootHash)

	// Test "appliance with no config store" case
	cfg, err = ds.ConfigStoreByUUID(ctx, testSite2.UUID)
	assert.Error(err)
	assert.IsType(err, NotFoundError{})
	assert.Nil(cfg)

	// This testing is light for now, but we can expand it over time as
	// the DB becomes more complex.
}

// Test the configuration store.  subtest of TestDatabaseModel
func testConfigStore(t *testing.T, ds DataStore, logger *zap.Logger, slogger *zap.SugaredLogger) {
	var err error
	ctx := context.Background()
	assert := require.New(t)

	mkOrgSiteApp(t, ds, &testOrg1, &testSite1, &testID1)

	// Add appliance 1 to the appliance_config_store table
	acs := SiteConfigStore{
		RootHash:  []byte{0xca, 0xfe, 0xbe, 0xef},
		TimeStamp: time.Now(),
		Config:    []byte{0xde, 0xad, 0xbe, 0xef},
	}
	err = ds.UpsertConfigStore(ctx, testSite1.UUID, &acs)
	assert.NoError(err)

	// Make sure we can pull it back out again.
	cfg, err := ds.ConfigStoreByUUID(ctx, testSite1.UUID)
	assert.NoError(err)
	assert.Equal([]byte{0xca, 0xfe, 0xbe, 0xef}, cfg.RootHash)

	// Test that changing the config succeeds: change the config and upsert,
	// then test pulling it out again.
	acs.Config = []byte{0xfe, 0xed, 0xfa, 0xce}
	err = ds.UpsertConfigStore(ctx, testSite1.UUID, &acs)
	assert.NoError(err)

	cfg, err = ds.ConfigStoreByUUID(ctx, testSite1.UUID)
	assert.NoError(err)
	assert.Equal([]byte{0xfe, 0xed, 0xfa, 0xce}, cfg.Config)
}

func testCommandQueue(t *testing.T, ds DataStore, logger *zap.Logger, slogger *zap.SugaredLogger) {
	var err error
	ctx := context.Background()
	assert := require.New(t)

	mkOrgSiteApp(t, ds, &testOrg1, &testSite1, &testID1)

	makeCmd := func(query string) (*SiteCommand, time.Time) {
		enqTime := time.Now()
		cmd := &SiteCommand{
			EnqueuedTime: enqTime,
			Query:        []byte(query),
		}
		return cmd, enqTime
	}
	makeManyCmds := func(query string, count int) []int64 {
		cmdIDs := make([]int64, count)
		for i := 0; i < count; i++ {
			cmd, _ := makeCmd(fmt.Sprintf("%s %d", query, i))
			err := ds.CommandSubmit(ctx, testSite1.UUID, cmd)
			assert.NoError(err)
			cmdIDs[i] = cmd.ID
		}
		return cmdIDs
	}

	cmd, enqTime := makeCmd("Ask Me Anything")
	// Make sure we can submit a command and have an ID assigned
	err = ds.CommandSubmit(ctx, testSite1.UUID, cmd)
	assert.NoError(err)
	assert.Equal(int64(1), cmd.ID)

	// Make sure we get a NotFoundError when looking up a command that was
	// never submitted
	cmd, err = ds.CommandSearch(ctx, 99)
	assert.Error(err)
	assert.IsType(NotFoundError{}, err)
	assert.Nil(cmd)

	// Make sure that we get back what we put in.
	cmd, err = ds.CommandSearch(ctx, 1)
	assert.NoError(err)
	assert.Equal(int64(1), cmd.ID)
	// Some part of the round-trip is rounding the times to the nearest
	// microsecond.
	assert.WithinDuration(enqTime, cmd.EnqueuedTime, time.Microsecond)
	assert.Equal("ENQD", cmd.State)
	assert.Equal([]byte("Ask Me Anything"), cmd.Query)

	// Make sure that canceling a command returns the old state and changes
	// the state to "CNCL".
	newCmd, oldCmd, err := ds.CommandCancel(ctx, 1)
	assert.NoError(err)
	assert.Equal("ENQD", oldCmd.State)
	assert.Equal("CNCL", newCmd.State)

	// Make sure that canceling a canceled command is a no-op.
	newCmd, oldCmd, err = ds.CommandCancel(ctx, 1)
	assert.NoError(err)
	assert.Equal("CNCL", oldCmd.State)

	// Make sure that canceling a non-existent command gives us a
	// NotFoundError
	_, _, err = ds.CommandCancel(ctx, 12345)
	assert.Error(err)
	assert.IsType(NotFoundError{}, err)

	// Queue up a new command
	cmd, enqTime = makeCmd("What Me Worry")
	err = ds.CommandSubmit(ctx, testSite1.UUID, cmd)
	assert.NoError(err)
	assert.Equal(int64(2), cmd.ID)

	// Make sure fetching something for testSite2.UUID doesn't return anything.
	cmds, err := ds.CommandFetch(ctx, testSite2.UUID, 1, 1)
	assert.NoError(err)
	assert.Len(cmds, 0)

	// Make sure that fetching the command gets the one we expect, that it
	// has been moved to the correct state, and that the number-of-refetches
	// counter has not been touched.
	cmds, err = ds.CommandFetch(ctx, testSite1.UUID, 1, 1)
	assert.NoError(err)
	assert.Len(cmds, 1)
	cmd = cmds[0]
	assert.Equal(int64(2), cmd.ID)
	assert.Equal("WORK", cmd.State)
	assert.Nil(cmd.NResent.Ptr())

	// Do it again, this time making sure that the resent counter has the
	// correct non-null value.
	cmds, err = ds.CommandFetch(ctx, testSite1.UUID, 1, 1)
	assert.NoError(err)
	assert.Len(cmds, 1)
	cmd = cmds[0]
	assert.Equal(int64(2), cmd.ID)
	assert.Equal("WORK", cmd.State)
	assert.Equal(null.IntFrom(1), cmd.NResent)

	// Complete the command.
	newCmd, oldCmd, err = ds.CommandComplete(ctx, 2, []byte{})
	assert.NoError(err)
	assert.Equal("WORK", oldCmd.State)
	assert.Nil(oldCmd.DoneTime.Ptr())
	assert.Equal("DONE", newCmd.State)
	assert.NotNil(newCmd.DoneTime.Ptr())

	// Delete commands
	// Specify keep == number of commands left.
	deleted, err := ds.CommandDelete(ctx, testSite1.UUID, 2)
	assert.NoError(err)
	assert.Equal(int64(0), deleted)
	// Specify keep > number of commands left.
	deleted, err = ds.CommandDelete(ctx, testSite1.UUID, 5)
	assert.NoError(err)
	assert.Equal(int64(0), deleted)
	// Specify keep == 0.
	deleted, err = ds.CommandDelete(ctx, testSite1.UUID, 0)
	assert.NoError(err)
	assert.Equal(int64(2), deleted)
	// Make some more to play with.
	cmdIDs := makeManyCmds("Whatcha Talkin' About", 20)
	// Cancel half
	for i := 0; i < 10; i++ {
		_, _, err = ds.CommandCancel(ctx, cmdIDs[i])
	}
	// Keep 5; this shouldn't delete still-queued commands.
	deleted, err = ds.CommandDelete(ctx, testSite1.UUID, 5)
	assert.NoError(err)
	assert.Equal(int64(5), deleted)
}

// make a template database, loaded with the schema.  Subsequently
// we can knock out copies.
func mkTemplate(ctx context.Context) error {
	templateURI, err := bpg.CreateDB(ctx, templateDBName, "")
	if err != nil {
		return fmt.Errorf("failed to make templatedb: %+v", err)
	}
	templateDB, err := Connect(templateURI)
	if err != nil {
		return fmt.Errorf("failed to connect to templatedb: %+v", err)
	}
	defer templateDB.Close()
	err = templateDB.LoadSchema(ctx, "schema")
	if err != nil {
		return fmt.Errorf("failed to load schema: %+v", err)
	}
	return nil
}

func TestDatabaseModel(t *testing.T) {
	var ctx = context.Background()
	bpg = briefpg.New(nil)
	defer bpg.Fini(ctx)
	err := bpg.Start(ctx)
	if err != nil {
		t.Fatalf("failed to setup: %+v", err)
	}
	err = mkTemplate(ctx)
	if err != nil {
		t.Fatal(err)
	}

	testCases := []struct {
		name  string
		tFunc dbTestFunc
	}{
		{"testPing", testPing},
		{"testHeartbeatIngest", testHeartbeatIngest},
		{"testApplianceID", testApplianceID},
		{"testAppliancePubKey", testAppliancePubKey},

		{"testOrganization", testOrganization},
		{"testCustomerSite", testCustomerSite},
		{"testOAuth2OrganizationRule", testOAuth2OrganizationRule},
		{"testPerson", testPerson},
		{"testAccount", testAccount},
		{"testAccountOrgRole", testAccountOrgRole},
		{"testOAuth2Identity", testOAuth2Identity},

		{"testCloudStorage", testCloudStorage},
		{"testUnittestData", testUnittestData},
		{"testConfigStore", testConfigStore},

		{"testCommandQueue", testCommandQueue},
		{"testServerCerts", testServerCerts},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			logger, slogger := setupLogging(t)
			bpg.Logger = zap.NewStdLog(logger)
			// Ensure uniqueness so that things work if count > 1
			dbName := fmt.Sprintf("%s_%d", t.Name(), time.Now().Unix())

			testdb, err := bpg.CreateDB(ctx, dbName, templateDBArg)
			if err != nil {
				t.Fatalf("CreateDB Failed: %v", err)
			}
			ds, err := Connect(testdb)
			if err != nil {
				t.Fatalf("Connect failed: %v", err)
			}
			defer ds.Close()
			tc.tFunc(t, ds, logger, slogger)
		})
	}
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
