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
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"bg/common/briefpg"

	"github.com/guregu/null"
	"github.com/satori/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tatsushid/go-prettytable"

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
	testHWSerial1   = "001-201901BB-000011"
	testHWSerial2   = "001-201901BB-000012"
	app1Str         = "10000000-1000-1000-1000-000000000001"
	app2Str         = "10000000-1000-1000-1000-000000000002"
	appNStr         = "10000000-1000-1000-1000-000000000009"
	site1Str        = "20000000-2000-2000-2000-000000000001"
	site2Str        = "20000000-2000-2000-2000-000000000002"
	org1Str         = "30000000-3000-3000-3000-000000000001"
	org2Str         = "30000000-3000-3000-3000-000000000002"
	orgMSP1Str      = "30000000-3000-3000-3000-000000000003"
	person1Str      = "40000000-4000-4000-4000-000000000001"
	person2Str      = "40000000-4000-4000-4000-000000000002"
	personMSP1Str   = "40000000-4000-4000-4000-000000000003"
	personMSP2Str   = "40000000-4000-4000-4000-000000000004"
	account1Str     = "50000000-5000-5000-5000-000000000001"
	account2Str     = "50000000-5000-5000-5000-000000000002"
	accountMSP1Str  = "50000000-5000-5000-5000-000000000003"
	accountMSP2Str  = "50000000-5000-5000-5000-000000000004"
	orgOrgRel1Str   = "60000000-6000-6000-6000-000000000001"
	orgOrgRel2Str   = "60000000-6000-6000-6000-000000000002"
	badStr          = "ffffffff-ffff-ffff-ffff-ffffffffffff"
)

var (
	bpg *briefpg.BriefPG

	badUUID = uuid.Must(uuid.FromString(badStr))

	testOrg1 = Organization{
		UUID: uuid.Must(uuid.FromString(org1Str)),
		Name: "org1",
	}
	testOrg2 = Organization{
		UUID: uuid.Must(uuid.FromString(org2Str)),
		Name: "org2",
	}
	testMSPOrg1 = Organization{
		UUID: uuid.Must(uuid.FromString(orgMSP1Str)),
		Name: "MSP org1",
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
		ApplianceUUID:      uuid.Must(uuid.FromString(app1Str)),
		SiteUUID:           uuid.Must(uuid.FromString(site1Str)),
		SystemReprHWSerial: null.StringFrom(testHWSerial1),
		GCPProject:         testProject,
		GCPRegion:          testRegion,
		ApplianceReg:       testReg,
		ApplianceRegID:     testRegID + "-1",
	}
	testID2 = ApplianceID{
		ApplianceUUID:      uuid.Must(uuid.FromString(app2Str)),
		SiteUUID:           uuid.Must(uuid.FromString(site2Str)),
		SystemReprHWSerial: null.StringFrom(testHWSerial2),
		GCPProject:         testProject,
		GCPRegion:          testRegion,
		ApplianceReg:       testReg,
		ApplianceRegID:     testRegID + "-2",
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
	testMSPPerson1 = Person{
		UUID:         uuid.Must(uuid.FromString(personMSP1Str)),
		Name:         "msp manager",
		PrimaryEmail: "manager@msp.net",
	}
	testMSPPerson2 = Person{
		UUID:         uuid.Must(uuid.FromString(personMSP2Str)),
		Name:         "msp employee",
		PrimaryEmail: "employee@msp.net",
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
	testMSPAccount1 = Account{
		UUID:             uuid.Must(uuid.FromString(accountMSP1Str)),
		Email:            "manager@msp.net",
		PhoneNumber:      "555-1212",
		PersonUUID:       testMSPPerson1.UUID,
		OrganizationUUID: testMSPOrg1.UUID,
	}
	testMSPAccount2 = Account{
		UUID:             uuid.Must(uuid.FromString(accountMSP2Str)),
		Email:            "employee@msp.net",
		PhoneNumber:      "555-1212",
		PersonUUID:       testMSPPerson2.UUID,
		OrganizationUUID: testMSPOrg1.UUID,
	}
	testOrgOrgRel1 = OrgOrgRelationship{
		UUID:                   uuid.Must(uuid.FromString(orgOrgRel1Str)),
		OrganizationUUID:       testMSPOrg1.UUID,
		TargetOrganizationUUID: testOrg1.UUID,
		Relationship:           "msp",
	}
	testOrgOrgRel2 = OrgOrgRelationship{
		UUID:                   uuid.Must(uuid.FromString(orgOrgRel2Str)),
		OrganizationUUID:       testMSPOrg1.UUID,
		TargetOrganizationUUID: testOrg2.UUID,
		Relationship:           "msp",
	}

	allLimitRoles = []string{"admin", "user"}
)

func setupLogging(t *testing.T) (*zap.Logger, *zap.SugaredLogger) {
	logger := zaptest.NewLogger(t)
	slogger := logger.Sugar()
	return logger, slogger
}

// dumpfail is normally unused, but can be enabled manually to aid in debugging
// specific tests.
func dumpfail(ctx context.Context, t *testing.T, bpg *briefpg.BriefPG, dbName string, force bool) {
	if !force && !t.Failed() {
		return
	}
	fname := strings.Replace(t.Name()+".sql.dump", "/", "_", -1)
	t.Logf("dumpfail: Dumping database to %s", fname)
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

func dumpTable(ctx context.Context, t *testing.T, db *ApplianceDB, tableName string, limit int) {
	words := strings.Fields(tableName)
	var q string
	if len(words) == 1 {
		q = "TABLE " + tableName
	} else {
		q = tableName
	}
	if limit > 0 {
		q = fmt.Sprintf("%s LIMIT %d", q, limit)
	}

	rows, err := db.QueryContext(ctx, q)
	if err != nil {
		t.Errorf("Couldn't query DB: %v", err)
		return
	}
	defer rows.Close()

	colTypes, err := rows.ColumnTypes()
	if err != nil {
		t.Errorf("Couldn't retrieve column types: %v", err)
		return
	}

	tableColumns := make([]prettytable.Column, len(colTypes))
	for i, c := range colTypes {
		tableColumns[i] = prettytable.Column{Header: c.Name()}
	}
	table, _ := prettytable.NewTable(tableColumns...)

	values := make([]interface{}, len(colTypes))
	valuePtrs := make([]interface{}, len(colTypes))
	for rows.Next() {
		for i := range values {
			valuePtrs[i] = &values[i]
		}
		err = rows.Scan(valuePtrs...)
		if err != nil {
			t.Errorf("Couldn't scan row: %v", err)
			return
		}
		table.AddRow(values...)
	}
	table.Print()
}

// hexDecode is used to decode a hex-encdoded string into a []byte, rather than
// typing out byte literals; use only for literals, since it panics on error.
func hexDecode(s string) []byte {
	bytes, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return bytes
}

// Test serialization to JSON
func TestJSON(t *testing.T) {
	_, _ = setupLogging(t)
	assert := require.New(t)

	j, _ := json.Marshal(&testID1)
	assert.JSONEq(`{
		"appliance_uuid":"10000000-1000-1000-1000-000000000001",
		"site_uuid":"20000000-2000-2000-2000-000000000001",
		"gcp_project":"test-project",
		"gcp_region":"test-region",
		"appliance_reg":"test-registry",
		"appliance_reg_id":"test-appliance-1",
		"system_repr_hwserial":"001-201901BB-000011",
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

// similar to the userids google uses, although a bit shorter
var subj int64 = 1234567890

// Makes a test account; returns oauth2 subject id
func mkAccount(t *testing.T, ds DataStore, person *Person, account *Account, roles []string) *OAuth2Identity {
	var err error
	ctx := context.Background()

	assert := require.New(t)
	err = ds.InsertPerson(ctx, person)
	assert.NoError(err)
	err = ds.InsertAccount(ctx, account)
	assert.NoError(err)
	for _, r := range roles {
		role := &AccountOrgRole{
			AccountUUID:            account.UUID,
			OrganizationUUID:       account.OrganizationUUID,
			TargetOrganizationUUID: account.OrganizationUUID,
			Relationship:           "self",
			Role:                   r,
		}
		err = ds.InsertAccountOrgRole(ctx, role)
		assert.NoError(err)
	}

	subj++
	subject := fmt.Sprintf("%d", subj)
	id := &OAuth2Identity{
		Subject:     subject,
		Provider:    "google",
		AccountUUID: account.UUID,
	}
	err = ds.InsertOAuth2Identity(ctx, id)
	assert.NoError(err)
	return id
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
		ApplianceUUID: testID1.ApplianceUUID,
		SiteUUID:      testID1.SiteUUID,
		BootTS:        time.Now(),
		RecordTS:      time.Now(),
	}
	err := ds.InsertHeartbeatIngest(ctx, &hb)
	// expect to fail because UUID doesn't exist
	assert.Error(err)

	_, err = ds.LatestHeartbeatBySiteUUID(ctx, testID1.SiteUUID)
	assert.Error(err)

	mkOrgSiteApp(t, ds, &testOrg1, &testSite1, &testID1)

	err = ds.InsertHeartbeatIngest(ctx, &hb)
	// expect to succeed now
	assert.NoError(err)

	hbLatest, err := ds.LatestHeartbeatBySiteUUID(ctx, testID1.SiteUUID)
	assert.NoError(err)
	assert.Equal(hb.ApplianceUUID, hbLatest.ApplianceUUID)
	assert.Equal(hb.SiteUUID, hbLatest.SiteUUID)
}

// Test insertion into site_net_exception table.  subtest of TestDatabaseModel
func testSiteNetException(t *testing.T, ds DataStore, logger *zap.Logger, slogger *zap.SugaredLogger) {
	ctx := context.Background()
	assert := require.New(t)

	exc := `{"timestamp":{"seconds":1557443396,"nanos":318927852},"reason":"BAD_RING","mac_address":44668396003773,"details":["client from standard ring requested address on brvlan5('devices' ring)"]}`
	mkOrgSiteApp(t, ds, &testOrg1, &testSite1, &testID1)

	err := ds.InsertSiteNetException(ctx, testID1.SiteUUID, time.Now(), "foo", nil, exc)
	assert.NoError(err)

	mac := uint64(0x1122334455)
	err = ds.InsertSiteNetException(ctx, testID1.SiteUUID, time.Now(), "foo", &mac, exc)
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
	assert.IsType(NotFoundError{}, err)

	_, err = ds.ApplianceIDByClientID(ctx, "not-a-real-clientid")
	assert.Error(err)
	assert.IsType(NotFoundError{}, err)

	_, err = ds.ApplianceIDByHWSerial(ctx, "not-a-real-serial")
	assert.Error(err)
	assert.IsType(NotFoundError{}, err)

	mkOrgSiteApp(t, ds, &testOrg1, &testSite1, &testID1)

	// Test that a second insert of the same UUID fails
	err = ds.InsertApplianceID(ctx, &testID1)
	assert.Error(err, "expected Insert to fail")

	// Test lookup ops
	id1, err := ds.ApplianceIDByUUID(ctx, testID1.ApplianceUUID)
	assert.NoError(err)
	assert.Equal(testID1.ApplianceUUID, id1.ApplianceUUID)

	id1, err = ds.ApplianceIDByClientID(ctx, testClientID1)
	assert.NoError(err)
	assert.Equal(testID1.ApplianceUUID, id1.ApplianceUUID)

	_, err = ds.ApplianceIDByHWSerial(ctx, testHWSerial1)
	assert.NoError(err)
	assert.Equal(testID1.ApplianceUUID, id1.ApplianceUUID)

	mkOrgSiteApp(t, ds, &testOrg2, &testSite2, &testID2)

	// Test getting complete set of appliance
	ids, err = ds.AllApplianceIDs(ctx)
	assert.NoError(err)
	assert.Len(ids, 2)

	// Test null site sentinel
	err = ds.InsertApplianceID(ctx, &testIDN)
	assert.NoError(err)

	chg := testIDN
	chg.SiteUUID = testSite1.UUID
	err = ds.UpdateApplianceID(ctx, &chg)
	assert.NoError(err)

	idn, err := ds.ApplianceIDByUUID(ctx, testIDN.ApplianceUUID)
	assert.NoError(err)
	assert.Equal(chg, *idn)
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

	_, err = ds.OrganizationByUUID(ctx, testOrg1.UUID)
	assert.Error(err)
	assert.IsType(NotFoundError{}, err)

	err = ds.InsertOrganization(ctx, &testOrg1)
	assert.NoError(err, "expected Insert to succeed")

	orgs, err = ds.AllOrganizations(ctx)
	assert.NoError(err, "expected success")
	assert.Len(orgs, 2)

	// Test that a second insert of the same UUID fails
	err = ds.InsertOrganization(ctx, &testOrg1)
	assert.Error(err, "expected Insert to fail")

	org, err := ds.OrganizationByUUID(ctx, testOrg1.UUID)
	assert.NoError(err, "expected success")
	assert.Equal(testOrg1, *org)

	chg := testOrg1
	chg.Name = "foobarbaz"
	err = ds.UpdateOrganization(ctx, &chg)
	assert.NoError(err, "expected success")

	org, err = ds.OrganizationByUUID(ctx, testOrg1.UUID)
	assert.NoError(err, "expected success")
	assert.Equal(chg, *org)
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
	assert.IsType(NotFoundError{}, err)

	mkOrgSiteApp(t, ds, &testOrg1, &testSite1, &testID1)

	// Test that a second insert of the same UUID fails
	err = ds.InsertCustomerSite(ctx, &testSite1)
	assert.Error(err, "expected Insert to fail")

	s1, err := ds.CustomerSiteByUUID(ctx, testID1.SiteUUID)
	assert.NoError(err)
	assert.Equal(testID1.SiteUUID, s1.UUID)

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

	chg := testSite1
	chg.Name = "foobarbaz"
	err = ds.UpdateCustomerSite(ctx, &chg)
	assert.NoError(err, "expected success")

	schg, err := ds.CustomerSiteByUUID(ctx, chg.UUID)
	assert.NoError(err, "expected success")
	assert.Equal(chg, *schg)
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

	mkOrgSiteApp(t, ds, &testOrg1, nil, nil)

	testCases := map[OAuth2OrgRuleType]string{
		RuleTypeTenant: testTenant,
		RuleTypeDomain: testDomain,
		RuleTypeEmail:  testEmail,
	}

	for ruleType, ruleVal := range testCases {
		rTest := &OAuth2OrganizationRule{testProvider, ruleType, ruleVal, testOrg1.UUID}

		rules, err := ds.AllOAuth2OrganizationRules(ctx)
		assert.NoError(err)
		assert.Len(rules, 0)

		err = ds.InsertOAuth2OrganizationRule(ctx, rTest)
		assert.NoError(err, "expected first insert to succeed")
		err = ds.InsertOAuth2OrganizationRule(ctx, rTest)
		assert.Error(err, "expected duplicate insert to fail")

		// Test successful rule
		rule, err := ds.OAuth2OrganizationRuleTest(ctx, testProvider, ruleType, ruleVal)
		assert.NoError(err)
		assert.Equal(*rTest, *rule)

		// Test unsuccessful rule
		_, err = ds.OAuth2OrganizationRuleTest(ctx, testProvider, ruleType, "foo")
		assert.Error(err)
		assert.IsType(NotFoundError{}, err)

		rules, err = ds.AllOAuth2OrganizationRules(ctx)
		assert.NoError(err)
		assert.Len(rules, 1)

		err = ds.DeleteOAuth2OrganizationRule(ctx, rule)
		assert.NoError(err)
	}

	// Test case insensitivity for email rules
	eTest := "upANDdown@domain.com"
	err = ds.InsertOAuth2OrganizationRule(ctx, &OAuth2OrganizationRule{testProvider, "email", eTest, testOrg1.UUID})
	assert.NoError(err)
	_, err = ds.OAuth2OrganizationRuleTest(ctx, testProvider, "email", eTest)
	assert.NoError(err)
	_, err = ds.OAuth2OrganizationRuleTest(ctx, testProvider, "email", strings.ToLower(eTest))
	assert.NoError(err)
	rEmail, err := ds.OAuth2OrganizationRuleTest(ctx, testProvider, "email", strings.ToUpper(eTest))
	assert.NoError(err)

	// Test case insensitivity for domain rules
	eTest = "upANDdown.com"
	err = ds.InsertOAuth2OrganizationRule(ctx, &OAuth2OrganizationRule{testProvider, "domain", eTest, testOrg1.UUID})
	assert.NoError(err)
	_, err = ds.OAuth2OrganizationRuleTest(ctx, testProvider, "domain", eTest)
	assert.NoError(err)
	_, err = ds.OAuth2OrganizationRuleTest(ctx, testProvider, "domain", strings.ToLower(eTest))
	assert.NoError(err)
	rDomain, err := ds.OAuth2OrganizationRuleTest(ctx, testProvider, "domain", strings.ToUpper(eTest))
	assert.NoError(err)

	rules, err := ds.AllOAuth2OrganizationRules(ctx)
	assert.NoError(err)
	assert.Len(rules, 2)

	err = ds.DeleteOAuth2OrganizationRule(ctx, rEmail)
	assert.NoError(err)
	err = ds.DeleteOAuth2OrganizationRule(ctx, rDomain)
	assert.NoError(err)
}

// Test Person APIs.  subtest of TestDatabaseModel
func testPerson(t *testing.T, ds DataStore, logger *zap.Logger, slogger *zap.SugaredLogger) {
	ctx := context.Background()
	assert := require.New(t)
	var err error

	mkOrgSiteApp(t, ds, &testOrg1, &testSite1, nil)

	_, err = ds.PersonByUUID(ctx, testPerson1.UUID)
	assert.Error(err)
	assert.IsType(NotFoundError{}, err)

	err = ds.InsertPerson(ctx, &testPerson1)
	assert.NoError(err, "expected success")

	// Try again
	err = ds.InsertPerson(ctx, &testPerson1)
	assert.Error(err)

	err = ds.InsertPerson(ctx, &testPerson2)
	assert.NoError(err, "expected success")

	person, err := ds.PersonByUUID(ctx, testPerson1.UUID)
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

	accts, err := ds.AccountsByOrganization(ctx, testOrg1.UUID)
	assert.NoError(err)
	assert.Len(accts, 0)

	sites, err := ds.CustomerSitesByAccount(ctx, testAccount1.UUID)
	assert.NoError(err)
	assert.Len(sites, 0)

	_ = mkAccount(t, ds, &testPerson1, &testAccount1, []string{"admin", "user"})
	_ = mkAccount(t, ds, &testPerson2, &testAccount2, []string{"user"})

	// Try again
	err = ds.InsertAccount(ctx, &testAccount1)
	assert.Error(err)

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

	expAccountInfo := AccountInfo{
		UUID:         testAccount1.UUID,
		Email:        testAccount1.Email,
		PhoneNumber:  testAccount1.PhoneNumber,
		Name:         testPerson1.Name,
		PrimaryEmail: testPerson1.PrimaryEmail,
	}

	acctInfo, err := ds.AccountInfoByUUID(ctx, testAccount1.UUID)
	assert.NoError(err)
	assert.Equal(expAccountInfo, *acctInfo)

	_, err = ds.AccountByUUID(ctx, badUUID)
	assert.Error(err)
	assert.IsType(NotFoundError{}, err)

	_, err = ds.AccountInfoByUUID(ctx, badUUID)
	assert.Error(err)
	assert.IsType(NotFoundError{}, err)

	ds.AccountSecretsSetPassphrase([]byte("I LIKE COCONUTS"))
	_, err = ds.AccountSecretsByUUID(ctx, testAccount1.UUID)
	assert.Error(err)
	assert.IsType(NotFoundError{}, err)

	testAs := &AccountSecrets{testAccount1.UUID, "k1", "regime", time.Now(), "k2", "regime", time.Now()}
	err = ds.UpsertAccountSecrets(ctx, testAs)
	assert.NoError(err, "expected success")

	// Try again
	err = ds.UpsertAccountSecrets(ctx, testAs)
	assert.NoError(err, "expected success")

	// Delete, then add again
	err = ds.DeleteAccountSecrets(ctx, testAs.AccountUUID)
	assert.NoError(err, "expected success")
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
	_, err = ds.AccountSecretsByUUID(ctx, testAccount1.UUID)
	assert.Error(err)

	// Reset to good passphrase
	ds.AccountSecretsSetPassphrase([]byte("I LIKE COCONUTS"))

	// Delete testAccount1
	err = ds.DeleteAccount(ctx, testAccount1.UUID)
	assert.NoError(err)

	err = ds.DeleteAccount(ctx, testAccount1.UUID)
	assert.Error(err)
	assert.IsType(NotFoundError{}, err)

	// Delete testAccount2
	err = ds.DeleteAccount(ctx, testAccount2.UUID)
	assert.NoError(err)

	// See if there is anything left
	_, err = ds.AccountByUUID(ctx, testAccount1.UUID)
	assert.Error(err)
	assert.IsType(NotFoundError{}, err)

	accts, err = ds.AccountsByOrganization(ctx, testOrg1.UUID)
	assert.NoError(err)
	assert.Len(accts, 0)

	_, err = ds.AccountSecretsByUUID(ctx, testAccount1.UUID)
	assert.Error(err)
	assert.IsType(NotFoundError{}, err)

	ids, err := ds.OAuth2IdentitiesByAccount(ctx, testAccount1.UUID)
	assert.NoError(err)
	assert.Len(ids, 0)

	_, err = ds.PersonByUUID(ctx, testPerson1.UUID)
	assert.Error(err)
	assert.IsType(NotFoundError{}, err)
}

func assertRolesMatch(t *testing.T, aoroles []AccountOrgRoles, account *Account,
	targetOrg uuid.UUID, relationship string, limitRoles []string, roles []string) {
	assert := require.New(t)

	var found *AccountOrgRoles
	for _, aorole := range aoroles {
		if aorole.AccountUUID == account.UUID &&
			aorole.OrganizationUUID == account.OrganizationUUID &&
			aorole.TargetOrganizationUUID == targetOrg &&
			aorole.Relationship == relationship {
			found = &aorole
			break
		}
	}
	if found == nil {
		t.Errorf("assertRolesMatch: Couldn't find an entry matching <%v, %v %v, %v> in\n\t%v",
			account.UUID, account.OrganizationUUID, targetOrg, relationship, aoroles)
	}
	assert.ElementsMatch(limitRoles, found.LimitRoles,
		"assertRolesMatch: Limit Role mismatch for %v: exp %v != act %v",
		found, limitRoles, found.LimitRoles)
	assert.ElementsMatch(roles, found.Roles,
		"assertRolesMatch: Role mismatch for %v: exp %v != act %v",
		found, roles, found.Roles)
}

// Test AccountOrgRole APIs.  subtest of TestDatabaseModel
func testAccountOrgRole(t *testing.T, ds DataStore, logger *zap.Logger, slogger *zap.SugaredLogger) {
	ctx := context.Background()
	assert := require.New(t)
	var err error

	adminRole := AccountOrgRole{
		AccountUUID:            testAccount1.UUID,
		OrganizationUUID:       testAccount1.OrganizationUUID,
		TargetOrganizationUUID: testAccount1.OrganizationUUID,
		Relationship:           "self",
		Role:                   "admin",
	}
	// Not really realistic as we would not normally add both
	// admin and user roles, but we are testing the assertion
	// that a user may have more than one role.
	userRole := AccountOrgRole{
		AccountUUID:            testAccount1.UUID,
		OrganizationUUID:       testAccount1.OrganizationUUID,
		TargetOrganizationUUID: testAccount1.OrganizationUUID,
		Relationship:           "self",
		Role:                   "user",
	}

	// Setup Customer
	mkOrgSiteApp(t, ds, &testOrg1, nil, nil)
	oauth2ID1 := mkAccount(t, ds, &testPerson1, &testAccount1, nil)
	_ = mkAccount(t, ds, &testPerson2, &testAccount2, nil)

	// Client has role for self, so there should be only one aoroles entry
	aoroles, err := ds.AccountOrgRolesByAccount(ctx, testAccount1.UUID)
	assert.NoError(err)
	assert.Len(aoroles, 1)
	assertRolesMatch(t, aoroles, &testAccount1, testAccount1.OrganizationUUID, "self", allLimitRoles, []string{})

	rolesStrs, err := ds.AccountPrimaryOrgRoles(ctx, testAccount1.UUID)
	assert.NoError(err)
	assert.Len(rolesStrs, 0)

	roles, err := ds.AccountOrgRolesByOrg(ctx, testAccount1.OrganizationUUID, "admin")
	assert.NoError(err)
	assert.Len(roles, 0)

	roles, err = ds.AccountOrgRolesByOrg(ctx, testAccount1.OrganizationUUID, "")
	assert.NoError(err)
	assert.Len(roles, 0)

	li, err := ds.LoginInfoByProviderAndSubject(ctx, oauth2ID1.Provider, oauth2ID1.Subject)
	assert.NoError(err)
	assert.Equal(&LoginInfo{
		Person:           testPerson1,
		Account:          testAccount1,
		OAuth2IdentityID: oauth2ID1.ID,
		PrimaryOrgRoles:  nil,
	}, li)

	err = ds.InsertAccountOrgRole(ctx, &adminRole)
	assert.NoError(err)
	// Same again
	err = ds.InsertAccountOrgRole(ctx, &adminRole)
	assert.NoError(err)

	aoroles, err = ds.AccountOrgRolesByAccount(ctx, testAccount1.UUID)
	assert.NoError(err)
	assert.Len(aoroles, 1)
	assertRolesMatch(t, aoroles, &testAccount1, testAccount1.OrganizationUUID, "self", allLimitRoles, []string{"admin"})

	err = ds.InsertAccountOrgRole(ctx, &userRole)
	assert.NoError(err)

	aoroles, err = ds.AccountOrgRolesByAccount(ctx, testAccount1.UUID)
	assert.NoError(err)
	assert.Len(aoroles, 1)
	assertRolesMatch(t, aoroles, &testAccount1, testAccount1.OrganizationUUID, "self", allLimitRoles, []string{"admin", "user"})

	li, err = ds.LoginInfoByProviderAndSubject(ctx, oauth2ID1.Provider, oauth2ID1.Subject)
	assert.NoError(err)
	sort.Strings(li.PrimaryOrgRoles)
	assert.Equal(&LoginInfo{
		Person:           testPerson1,
		Account:          testAccount1,
		OAuth2IdentityID: oauth2ID1.ID,
		PrimaryOrgRoles:  []string{"admin", "user"},
	}, li)

	rolesStrs, err = ds.AccountPrimaryOrgRoles(ctx, testAccount1.UUID)
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

	aoroles, err = ds.AccountOrgRolesByAccount(ctx, testAccount1.UUID)
	assert.NoError(err)
	assert.Len(aoroles, 1)
	assertRolesMatch(t, aoroles, &testAccount1, testAccount1.OrganizationUUID, "self", allLimitRoles, []string{"admin"})

	rolesStrs, err = ds.AccountPrimaryOrgRoles(ctx, testAccount1.UUID)
	assert.NoError(err)
	assert.Equal([]string{"admin"}, rolesStrs)

	err = ds.DeleteAccountOrgRole(ctx, &adminRole)
	assert.NoError(err)

	aoroles, err = ds.AccountOrgRolesByAccount(ctx, testAccount1.UUID)
	assert.NoError(err)
	assert.Len(aoroles, 1)
	assertRolesMatch(t, aoroles, &testAccount1, testAccount1.OrganizationUUID, "self", allLimitRoles, []string{})

	rolesStrs, err = ds.AccountPrimaryOrgRoles(ctx, testAccount1.UUID)
	assert.NoError(err)
	assert.Len(rolesStrs, 0)

	roles, err = ds.AccountOrgRolesByOrg(ctx, testAccount1.OrganizationUUID, "")
	assert.NoError(err)
	assert.Len(roles, 0)
}

// Test AccountOrgRole APIs for MSP use cases.  subtest of TestDatabaseModel
func testAccountOrgRoleMSP(t *testing.T, ds DataStore, logger *zap.Logger, slogger *zap.SugaredLogger) {
	ctx := context.Background()
	assert := require.New(t)
	var err error

	// Setup
	mkOrgSiteApp(t, ds, &testMSPOrg1, nil, nil)
	mkOrgSiteApp(t, ds, &testOrg1, &testSite1, nil)

	mkAccount(t, ds, &testMSPPerson1, &testMSPAccount1, []string{"admin", "user"})
	mkAccount(t, ds, &testMSPPerson2, &testMSPAccount2, nil)
	mkAccount(t, ds, &testPerson1, &testAccount1, []string{"admin", "user"})
	mkAccount(t, ds, &testPerson2, &testAccount2, []string{"user"})

	// Setup org/org relationship and add a role
	err = ds.InsertOrgOrgRelationship(ctx, &testOrgOrgRel1)
	assert.NoError(err)

	adminRoleMSP := AccountOrgRole{
		AccountUUID:            testMSPAccount1.UUID,
		OrganizationUUID:       testMSPOrg1.UUID,
		TargetOrganizationUUID: testOrg1.UUID,
		Relationship:           "msp",
		Role:                   "admin",
	}
	err = ds.InsertAccountOrgRole(ctx, &adminRoleMSP)
	assert.NoError(err)

	aoroles, err := ds.AccountOrgRolesByAccount(ctx, testMSPAccount1.UUID)
	assert.NoError(err)
	assert.Len(aoroles, 2)
	assertRolesMatch(t, aoroles, &testMSPAccount1, testMSPAccount1.OrganizationUUID, "self", allLimitRoles, []string{"admin", "user"})
	assertRolesMatch(t, aoroles, &testMSPAccount1, testOrg1.UUID, "msp", allLimitRoles, []string{"admin"})

	aoroles, err = ds.AccountOrgRolesByAccount(ctx, testMSPAccount2.UUID)
	assert.NoError(err)
	assert.Len(aoroles, 2)
	assertRolesMatch(t, aoroles, &testMSPAccount2, testMSPAccount2.OrganizationUUID, "self", allLimitRoles, []string{})
	assertRolesMatch(t, aoroles, &testMSPAccount2, testOrg1.UUID, "msp", allLimitRoles, []string{})

	aoroles, err = ds.AccountOrgRolesByAccount(ctx, testAccount1.UUID)
	assert.NoError(err)
	assert.Len(aoroles, 1)
	assertRolesMatch(t, aoroles, &testAccount1, testAccount1.OrganizationUUID, "self", allLimitRoles, []string{"admin", "user"})

	aoroles, err = ds.AccountOrgRolesByAccount(ctx, testAccount2.UUID)
	assert.NoError(err)
	assert.Len(aoroles, 1)
	assertRolesMatch(t, aoroles, &testAccount2, testAccount2.OrganizationUUID, "self", allLimitRoles, []string{"user"})
}

func testOAuth2Identity(t *testing.T, ds DataStore, logger *zap.Logger, slogger *zap.SugaredLogger) {
	ctx := context.Background()
	assert := require.New(t)
	var err error

	// Setup
	mkOrgSiteApp(t, ds, &testOrg1, &testSite1, nil)
	oauth2ID1 := mkAccount(t, ds, &testPerson1, &testAccount1, nil)
	oauth2ID2 := mkAccount(t, ds, &testPerson2, &testAccount2, nil)
	assert.NotEqual(oauth2ID1.ID, oauth2ID2.ID)

	// Try #1 again, expect error
	err = ds.InsertOAuth2Identity(ctx, oauth2ID1)
	assert.Error(err, "should fail; already inserted")

	ids, err := ds.OAuth2IdentitiesByAccount(ctx, testAccount1.UUID)
	assert.NoError(err)
	assert.Equal([]OAuth2Identity{*oauth2ID1}, ids)

	li, err := ds.LoginInfoByProviderAndSubject(ctx, oauth2ID1.Provider, oauth2ID1.Subject)
	assert.NoError(err)
	assert.Equal(&LoginInfo{
		Person:           testPerson1,
		Account:          testAccount1,
		OAuth2IdentityID: oauth2ID1.ID,
		PrimaryOrgRoles:  nil,
	}, li)

	_, err = ds.LoginInfoByProviderAndSubject(ctx, "invalid", "invalid")
	assert.Error(err)
	assert.IsType(NotFoundError{}, err)

	at := &OAuth2AccessToken{
		OAuth2IdentityID: oauth2ID2.ID,
		Token:            "I like coconuts",
		Expires:          time.Now(),
	}
	err = ds.InsertOAuth2AccessToken(ctx, at)
	assert.NoError(err, "expected success")

	rt := &OAuth2RefreshToken{
		OAuth2IdentityID: oauth2ID1.ID,
		Token:            "I like coconuts! A lot!",
	}
	err = ds.UpsertOAuth2RefreshToken(ctx, rt)
	assert.NoError(err, "expected success")
	err = ds.UpsertOAuth2RefreshToken(ctx, rt)
	assert.NoError(err, "expected success")
}

// Test Org/Org relationships
func testOrgOrg(t *testing.T, ds DataStore, logger *zap.Logger, slogger *zap.SugaredLogger) {
	var err error
	ctx := context.Background()
	assert := require.New(t)

	// Setup
	mkOrgSiteApp(t, ds, &testMSPOrg1, nil, nil)
	mkOrgSiteApp(t, ds, &testOrg1, &testSite1, nil)
	mkAccount(t, ds, &testPerson1, &testAccount1, []string{"user"})
	mkAccount(t, ds, &testMSPPerson1, &testMSPAccount1, nil)

	// Test that a byproduct of adding an org is the 'self' relationship
	rels, err := ds.OrgOrgRelationshipsByOrg(ctx, testOrg1.UUID)
	assert.NoError(err)
	assert.Len(rels, 1)
	expRel := OrgOrgRelationship{
		UUID:                   testOrg1.UUID, // See InsertOrganizationTx
		OrganizationUUID:       testOrg1.UUID,
		TargetOrganizationUUID: testOrg1.UUID,
		Relationship:           "self",
	}
	assert.Equal(expRel.UUID, rels[0].UUID)
	assert.Equal(expRel.OrganizationUUID, rels[0].OrganizationUUID)
	assert.Equal(expRel.TargetOrganizationUUID, rels[0].TargetOrganizationUUID)
	assert.Equal(expRel.Relationship, rels[0].Relationship)
	assert.ElementsMatch([]string{"admin", "user"}, []string(rels[0].LimitRoles))

	rels, err = ds.OrgOrgRelationshipsByOrgTarget(ctx, testOrg1.UUID, testOrg1.UUID)
	assert.NoError(err)
	assert.Len(rels, 1)
	assert.Equal(expRel.UUID, rels[0].UUID)
	assert.Equal(expRel.OrganizationUUID, rels[0].OrganizationUUID)
	assert.Equal(expRel.TargetOrganizationUUID, rels[0].TargetOrganizationUUID)
	assert.Equal(expRel.Relationship, rels[0].Relationship)
	assert.ElementsMatch([]string{"admin", "user"}, []string(rels[0].LimitRoles))

	// Test insertion of invalid relationship
	badRel := testOrgOrgRel1
	badRel.Relationship = "perfect strangers"
	err = ds.InsertOrgOrgRelationship(ctx, &badRel)
	assert.Error(err)

	// Test insertion of msp->org relationship of type self, should fail
	badRel.Relationship = "self"
	err = ds.InsertOrgOrgRelationship(ctx, &badRel)
	assert.Error(err)

	// Try to grant Admin role to testMSPAccount1 without Org/Org relationship
	adminRoleMSP := AccountOrgRole{
		AccountUUID:            testMSPAccount1.UUID,
		OrganizationUUID:       testOrgOrgRel1.OrganizationUUID,
		TargetOrganizationUUID: testOrgOrgRel1.TargetOrganizationUUID,
		Relationship:           testOrgOrgRel1.Relationship,
		Role:                   "admin",
	}
	assert.Error(ds.InsertAccountOrgRole(ctx, &adminRoleMSP))

	// Test insertion of MSP relationship
	err = ds.InsertOrgOrgRelationship(ctx, &testOrgOrgRel1)
	assert.NoError(err)
	rels, err = ds.OrgOrgRelationshipsByOrgTarget(ctx, testMSPOrg1.UUID, testOrg1.UUID)
	assert.NoError(err)
	assert.Len(rels, 1)
	assert.Equal(testOrgOrgRel1.OrganizationUUID, rels[0].OrganizationUUID)
	assert.ElementsMatch([]string{"admin", "user"}, []string(rels[0].LimitRoles))

	// Successfully grant Admin role to testMSPAccount1
	assert.NoError(ds.InsertAccountOrgRole(ctx, &adminRoleMSP))

	aoroles, err := ds.AccountOrgRolesByAccount(ctx, testMSPAccount1.UUID)
	assert.NoError(err)
	// One for self relationship, one for MSP relationship.
	assert.Len(aoroles, 2)
	assertRolesMatch(t, aoroles, &testMSPAccount1, testMSPAccount1.OrganizationUUID, "self", allLimitRoles, []string{})
	assertRolesMatch(t, aoroles, &testMSPAccount1, testOrgOrgRel1.TargetOrganizationUUID, "msp", allLimitRoles, []string{"admin"})

	// Test deletion of MSP relationship; should cleanup roles
	err = ds.DeleteOrgOrgRelationship(ctx, testOrgOrgRel1.UUID)
	assert.NoError(err)

	rels, err = ds.OrgOrgRelationshipsByOrgTarget(ctx, testMSPOrg1.UUID, testOrg1.UUID)
	assert.NoError(err)
	assert.Len(rels, 0)

	aoroles, err = ds.AccountOrgRolesByAccount(ctx, testMSPAccount1.UUID)
	assert.NoError(err)
	assert.Len(aoroles, 1)
	assertRolesMatch(t, aoroles, &testMSPAccount1, testMSPAccount1.OrganizationUUID, "self", allLimitRoles, []string{})
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
	assert.Equal("gcs", cs.Provider)

	// Test "appliance with no cloud storage" case
	cs, err = ds.CloudStorageByUUID(ctx, testSite2.UUID)
	assert.Error(err)
	assert.IsType(NotFoundError{}, err)
	assert.Nil(cs)

	// Test "appliance with config store" case
	cfg, err := ds.ConfigStoreByUUID(ctx, testSite1.UUID)
	assert.NoError(err)
	assert.Equal(hexDecode("deadbeef"), cfg.RootHash)

	// Test "appliance with no config store" case
	cfg, err = ds.ConfigStoreByUUID(ctx, testSite2.UUID)
	assert.Error(err)
	assert.IsType(NotFoundError{}, err)
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
		RootHash:  hexDecode("cafebeef"),
		TimeStamp: time.Now(),
		Config:    hexDecode("deadbeef"),
	}
	err = ds.UpsertConfigStore(ctx, testSite1.UUID, &acs)
	assert.NoError(err)

	// Make sure we can pull it back out again.
	cfg, err := ds.ConfigStoreByUUID(ctx, testSite1.UUID)
	assert.NoError(err)
	assert.Equal(hexDecode("cafebeef"), cfg.RootHash)

	// Test that changing the config succeeds: change the config and upsert,
	// then test pulling it out again.
	acs.Config = hexDecode("feedface")
	err = ds.UpsertConfigStore(ctx, testSite1.UUID, &acs)
	assert.NoError(err)

	cfg, err = ds.ConfigStoreByUUID(ctx, testSite1.UUID)
	assert.NoError(err)
	assert.Equal(hexDecode("feedface"), cfg.Config)
}

func testCommandQueue(t *testing.T, ds DataStore, logger *zap.Logger, slogger *zap.SugaredLogger) {
	var err error
	ctx := context.Background()
	assert := require.New(t)

	mkOrgSiteApp(t, ds, &testOrg1, &testSite1, &testID1)
	mkOrgSiteApp(t, ds, &testOrg2, &testSite2, &testID2)

	makeCmd := func(query string) (*SiteCommand, time.Time) {
		enqTime := time.Now()
		cmd := &SiteCommand{
			EnqueuedTime: enqTime,
			Query:        []byte(query),
		}
		return cmd, enqTime
	}
	makeManyCmds := func(query string, u uuid.UUID, count int) []int64 {
		cmdIDs := make([]int64, count)
		for i := 0; i < count; i++ {
			cmd, _ := makeCmd(fmt.Sprintf("%s %d", query, i))
			err := ds.CommandSubmit(ctx, u, cmd)
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
	cmd, err = ds.CommandSearch(ctx, testSite1.UUID, 99)
	assert.Error(err)
	assert.IsType(NotFoundError{}, err)
	assert.Nil(cmd)

	// Make sure that we get back what we put in.
	cmd, err = ds.CommandSearch(ctx, testSite1.UUID, 1)
	assert.NoError(err)
	assert.Equal(int64(1), cmd.ID)
	// Some part of the round-trip is rounding the times to the nearest
	// microsecond.
	assert.WithinDuration(enqTime, cmd.EnqueuedTime, time.Microsecond)
	assert.Equal("ENQD", cmd.State)
	assert.Equal([]byte("Ask Me Anything"), cmd.Query)

	// Make sure that canceling a command returns the old state and changes
	// the state to "CNCL".
	newCmd, oldCmd, err := ds.CommandCancel(ctx, testSite1.UUID, 1)
	assert.NoError(err)
	assert.Equal("ENQD", oldCmd.State)
	assert.Equal("CNCL", newCmd.State)

	// Make sure that canceling a canceled command is a no-op.
	newCmd, oldCmd, err = ds.CommandCancel(ctx, testSite1.UUID, 1)
	assert.NoError(err)
	assert.Equal("CNCL", oldCmd.State)
	assert.Equal("CNCL", newCmd.State)

	// Make sure that canceling a non-existent command gives us a
	// NotFoundError
	_, _, err = ds.CommandCancel(ctx, testSite1.UUID, 12345)
	assert.Error(err)
	assert.IsType(NotFoundError{}, err)

	// Queue up a new command
	cmd, _ = makeCmd("What Me Worry")
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
	newCmd, oldCmd, err = ds.CommandComplete(ctx, testSite1.UUID, 2, []byte{})
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
	cmdIDs := makeManyCmds("Whatcha Talkin' About", testSite1.UUID, 20)
	// Cancel half
	for i := 0; i < 10; i++ {
		_, _, err = ds.CommandCancel(ctx, testSite1.UUID, cmdIDs[i])
		assert.NoError(err)
	}
	// Keep 5; this shouldn't delete still-queued commands.
	deleted, err = ds.CommandDelete(ctx, testSite1.UUID, 5)
	assert.NoError(err)
	assert.Equal(int64(5), deleted)

	// Queue up a new command, then try to have a different site mess with it
	cmd, _ = makeCmd("Spoof Testing")
	err = ds.CommandSubmit(ctx, testSite1.UUID, cmd)
	assert.NoError(err)

	_, _, err = ds.CommandCancel(ctx, testSite2.UUID, cmd.ID)
	assert.Error(err)
	assert.IsType(NotFoundError{}, err)

	_, _, err = ds.CommandComplete(ctx, testSite2.UUID, cmd.ID, []byte("Spoofed you"))
	assert.Error(err)
	assert.IsType(NotFoundError{}, err)

	_, _, err = ds.CommandComplete(ctx, testSite1.UUID, cmd.ID, []byte("allowed to complete"))
	assert.NoError(err)

	// Test audit
	su1 := uuid.NullUUID{
		UUID:  testSite1.UUID,
		Valid: true,
	}
	su2 := uuid.NullUUID{
		UUID:  testSite2.UUID,
		Valid: true,
	}
	sNull := uuid.NullUUID{}

	cmds, err = ds.CommandAudit(ctx, su1, 0, 0)
	assert.NoError(err)
	assert.Len(cmds, 0)

	cmds, err = ds.CommandAudit(ctx, su1, 0, 1)
	assert.NoError(err)
	assert.Len(cmds, 1)

	cmds, err = ds.CommandAudit(ctx, su1, 0, 10)
	assert.NoError(err)
	assert.Len(cmds, 10)

	cmds, err = ds.CommandAudit(ctx, sNull, 0, 10)
	assert.NoError(err)
	assert.Len(cmds, 10)

	// Audit from different site
	cmds, err = ds.CommandAudit(ctx, su2, 0, 10)
	assert.NoError(err)
	assert.Len(cmds, 0)

	cmds, err = ds.CommandAuditHealth(ctx, su1, time.Now())
	assert.NoError(err)
	assert.Len(cmds, 10)

	cmds, err = ds.CommandAuditHealth(ctx, su1, time.Now().Add(-1*time.Minute))
	assert.NoError(err)
	assert.Len(cmds, 0)
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
		{"testSiteNetException", testSiteNetException},
		{"testApplianceID", testApplianceID},
		{"testAppliancePubKey", testAppliancePubKey},

		{"testOrganization", testOrganization},
		{"testCustomerSite", testCustomerSite},
		{"testOAuth2OrganizationRule", testOAuth2OrganizationRule},
		{"testPerson", testPerson},
		{"testAccount", testAccount},
		{"testAccountOrgRole", testAccountOrgRole},
		{"testAccountOrgRoleMSP", testAccountOrgRoleMSP},
		{"testOAuth2Identity", testOAuth2Identity},
		{"testOrgOrg", testOrgOrg},

		{"testCloudStorage", testCloudStorage},
		{"testUnittestData", testUnittestData},
		{"testConfigStore", testConfigStore},

		{"testCommandQueue", testCommandQueue},
		{"testServerCerts", testServerCerts},
		{"testServerCertsDelete", testServerCertsDelete},

		{"testReleaseArtifacts", testReleaseArtifacts},
		{"testReleaseStatus", testReleaseStatus},
		{"testReleases", testReleases},
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
