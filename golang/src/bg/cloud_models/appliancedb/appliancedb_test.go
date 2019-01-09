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
	app3Str         = "00000003-0003-0003-0003-000000000003"
	site1Str        = "10000001-0001-0001-0001-000000000001"
	site2Str        = "10000002-0002-0002-0002-000000000002"
)

var (
	databaseURI string
	bpg         *briefpg.BriefPG

	testSite1 = CustomerSite{
		UUID: uuid.Must(uuid.FromString(site1Str)),
		Name: "site1",
	}
	testSite2 = CustomerSite{
		UUID: uuid.Must(uuid.FromString(site2Str)),
		Name: "site2",
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
	testID3 = ApplianceID{
		ApplianceUUID:  uuid.Must(uuid.FromString(app3Str)),
		SiteUUID:       NullSiteUUID, // Sentinel UUID
		GCPProject:     testProject,
		GCPRegion:      testRegion,
		ApplianceReg:   testReg,
		ApplianceRegID: testRegID + "-3",
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

func mkSiteApp(t *testing.T, ds DataStore, site *CustomerSite, app *ApplianceID) {
	ctx := context.Background()
	assert := require.New(t)

	// Prep the database: add appliance to the appliance_id_map table
	err := ds.InsertCustomerSite(ctx, site)
	assert.NoError(err, "expected Insert to succeed")

	// Prep the database: add appliance to the appliance_id_map table
	err = ds.InsertApplianceID(ctx, app)
	assert.NoError(err, "expected Insert to succeed")
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

	mkSiteApp(t, ds, &testSite1, &testID1)

	err = ds.InsertHeartbeatIngest(ctx, &hb)
	// expect to succeed now
	assert.NoError(err)
}

func testPing(t *testing.T, ds DataStore, logger *zap.Logger, slogger *zap.SugaredLogger) {
	assert := require.New(t)
	err := ds.Ping()
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

	mkSiteApp(t, ds, &testSite1, &testID1)

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

	mkSiteApp(t, ds, &testSite2, &testID2)

	// Test getting complete set of appliance
	ids, err = ds.AllApplianceIDs(ctx)
	assert.NoError(err)
	assert.Len(ids, 2)

	// Test null site sentinel
	err = ds.InsertApplianceID(ctx, &testID3)
	assert.NoError(err)
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

	mkSiteApp(t, ds, &testSite1, &testID1)

	// Test that a second insert of the same UUID fails
	err = ds.InsertCustomerSite(ctx, &testSite1)
	assert.Error(err, "expected Insert to fail")

	s1, err := ds.CustomerSiteByUUID(ctx, testID1.SiteUUID)
	assert.NoError(err)
	assert.Equal(s1.UUID, testID1.SiteUUID)

	mkSiteApp(t, ds, &testSite2, &testID2)
	ids, err = ds.AllCustomerSites(ctx)
	assert.NoError(err)
	assert.Len(ids, 3)
}

// Test operations related to appliance public keys.  subtest of TestDatabaseModel
func testAppliancePubKey(t *testing.T, ds DataStore, logger *zap.Logger, slogger *zap.SugaredLogger) {
	ctx := context.Background()
	assert := require.New(t)

	mkSiteApp(t, ds, &testSite1, &testID1)

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

// Test insertion into cloudstorage table.  subtest of TestDatabaseModel
func testCloudStorage(t *testing.T, ds DataStore, logger *zap.Logger, slogger *zap.SugaredLogger) {
	var err error
	ctx := context.Background()
	assert := require.New(t)

	mkSiteApp(t, ds, &testSite1, &testID1)

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

	mkSiteApp(t, ds, &testSite1, &testID1)

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

	mkSiteApp(t, ds, &testSite1, &testID1)

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
		{"testCustomerSite", testCustomerSite},
		{"testAppliancePubKey", testAppliancePubKey},
		{"testCloudStorage", testCloudStorage},
		{"testUnittestData", testUnittestData},
		{"testConfigStore", testConfigStore},
		{"testCommandQueue", testCommandQueue},
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
