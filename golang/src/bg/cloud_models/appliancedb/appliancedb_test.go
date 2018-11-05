/*
 * COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
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
	testClientID    = "projects/test-project/locations/test-region/registries/test-registry/appliances/test-appliance"
	testUUIDstr     = "00000001-0001-0001-0001-000000000001"
	testUUIDstr2    = "00000002-0002-0002-0002-000000000002"
)

var (
	databaseURI string
	bpg         *briefpg.BriefPG
	testUUID    uuid.UUID
	testUUID2   uuid.UUID
)

func init() {
	testUUID = uuid.Must(uuid.FromString(testUUIDstr))
	testUUID2 = uuid.Must(uuid.FromString(testUUIDstr2))
}

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

	ai := &ApplianceID{
		CloudUUID:          testUUID,
		SystemReprHWSerial: null.NewString("", false),
		SystemReprMAC:      null.NewString("", false),
	}
	j, _ := json.Marshal(ai)
	assert.JSONEq(`{
		"cloud_uuid":"00000001-0001-0001-0001-000000000001",
		"gcp_project":"",
		"gcp_region":"",
		"appliance_reg":"",
		"appliance_reg_id":"",
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

	acs := &ApplianceCloudStorage{}
	j, _ = json.Marshal(acs)
	assert.JSONEq(`{"bucket":"", "provider":""}`, string(j))
}

func TestApplianceID(t *testing.T) {
	_, _ = setupLogging(t)
	assert := require.New(t)

	x := &ApplianceID{
		CloudUUID:      testUUID,
		GCPProject:     testProject,
		GCPRegion:      testRegion,
		ApplianceReg:   testReg,
		ApplianceRegID: testRegID,
	}
	assert.NotEmpty(x.String())
	x.SystemReprMAC = null.NewString("123", true)
	x.SystemReprHWSerial = null.NewString("123", true)
	assert.NotEmpty(x.String())
	assert.Equal(testClientID, x.ClientID())
}

type dbTestFunc func(*testing.T, DataStore, *zap.Logger, *zap.SugaredLogger)

// Test conditions when tables are empty.  subtest of TestDatabaseModel
func testEmpty(t *testing.T, ds DataStore, logger *zap.Logger, slogger *zap.SugaredLogger) {
	ctx := context.Background()
	assert := require.New(t)

	// Also test Ping() while we're here
	err := ds.Ping()
	assert.NoError(err)

	ids, err := ds.AllApplianceIDs(ctx)
	assert.NoError(err)
	assert.Len(ids, 0)

	_, err = ds.ApplianceIDByUUID(ctx, testUUID)
	assert.Error(err)
	assert.IsType(err, NotFoundError{})

	_, err = ds.ApplianceIDByClientID(ctx, "not-a-real-clientid")
	assert.Error(err)
	assert.IsType(err, NotFoundError{})
}

// Test insertion into Heartbeat ingest table.  subtest of TestDatabaseModel
func testHeartbeatIngest(t *testing.T, ds DataStore, logger *zap.Logger, slogger *zap.SugaredLogger) {
	ctx := context.Background()
	assert := require.New(t)

	hb := HeartbeatIngest{
		ApplianceID: testUUID,
		BootTS:      time.Now(),
		RecordTS:    time.Now(),
	}
	err := ds.InsertHeartbeatIngest(ctx, &hb)
	// expect to fail because UUID doesn't exist
	assert.Error(err)

	a := ApplianceID{
		CloudUUID: testUUID,
	}
	err = ds.InsertApplianceID(ctx, &a)
	assert.NoError(err, "expected Insert to succeed")

	err = ds.InsertHeartbeatIngest(ctx, &hb)
	// expect to succeed now
	assert.NoError(err)
}

// Test insert of registry data.  subtest of TestDatabaseModel
func testInsertApplianceID(t *testing.T, ds DataStore, logger *zap.Logger, slogger *zap.SugaredLogger) {
	ctx := context.Background()
	assert := require.New(t)

	a := &ApplianceID{
		CloudUUID: testUUID,
	}
	err := ds.InsertApplianceID(ctx, a)
	assert.NoError(err, "expected Insert to succeed")

	a2, err := ds.ApplianceIDByUUID(ctx, testUUID)
	assert.NoError(err, "expected ApplianceIDByUUID to succeed")
	assert.Equal(a2.CloudUUID, testUUID)

	// Test that a second insert of the same UUID fails
	err = ds.InsertApplianceID(ctx, a)
	assert.Error(err, "expected Insert to fail")
}

// Test insertion into cloudstorage table.  subtest of TestDatabaseModel
func testCloudStorage(t *testing.T, ds DataStore, logger *zap.Logger, slogger *zap.SugaredLogger) {
	ctx := context.Background()
	assert := require.New(t)

	a := &ApplianceID{
		CloudUUID: testUUID,
	}
	err := ds.InsertApplianceID(ctx, a)
	assert.NoError(err, "expected Insert to succeed")

	cs1 := &ApplianceCloudStorage{
		Bucket:   "test-bucket",
		Provider: "gcs",
	}
	err = ds.UpsertCloudStorage(ctx, testUUID, cs1)
	assert.NoError(err)

	cs2, err := ds.CloudStorageByUUID(ctx, testUUID)
	assert.NoError(err)
	assert.Equal(*cs1, *cs2)

	cs2.Provider = "s3"
	err = ds.UpsertCloudStorage(ctx, testUUID, cs2)
	assert.NoError(err)

	cs3, err := ds.CloudStorageByUUID(ctx, testUUID)
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
	keys, err := ds.KeysByUUID(ctx, testUUID)
	assert.NoError(err)
	assert.Len(keys, 2)

	// Test "appliance with no keys" case
	keys, err = ds.KeysByUUID(ctx, testUUID2)
	assert.NoError(err)
	assert.Len(keys, 0)

	// Test "appliance with cloud storage" case
	cs, err := ds.CloudStorageByUUID(ctx, testUUID)
	assert.NoError(err)
	assert.Equal(cs.Provider, "gcs")

	// Test "appliance with no cloud storage" case
	cs, err = ds.CloudStorageByUUID(ctx, testUUID2)
	assert.Error(err)
	assert.IsType(err, NotFoundError{})
	assert.Nil(cs)

	// Test "appliance with config store" case
	cfg, err := ds.ConfigStoreByUUID(ctx, testUUID)
	assert.NoError(err)
	assert.Equal([]byte{0xde, 0xad, 0xbe, 0xef}, cfg.RootHash)

	// Test "appliance with no config store" case
	cfg, err = ds.ConfigStoreByUUID(ctx, testUUID2)
	assert.Error(err)
	assert.IsType(err, NotFoundError{})
	assert.Nil(cfg)

	// This testing is light for now, but we can expand it over time as
	// the DB becomes more complex.
}

// Test the configuration store.  subtest of TestDatabaseModel
func testConfigStore(t *testing.T, ds DataStore, logger *zap.Logger, slogger *zap.SugaredLogger) {
	ctx := context.Background()
	assert := require.New(t)

	// Prep the database: add appliance 1 to the appliance_id_map table
	a := &ApplianceID{
		CloudUUID: testUUID,
	}
	err := ds.InsertApplianceID(ctx, a)
	assert.NoError(err, "expected Insert to succeed")

	// Add appliance 1 to the appliance_config_store table
	acs := ApplianceConfigStore{
		RootHash:  []byte{0xca, 0xfe, 0xbe, 0xef},
		TimeStamp: time.Now(),
		Config:    []byte{0xde, 0xad, 0xbe, 0xef},
	}
	err = ds.UpsertConfigStore(ctx, testUUID, &acs)
	assert.NoError(err)

	// Make sure we can pull it back out again.
	cfg, err := ds.ConfigStoreByUUID(ctx, testUUID)
	assert.NoError(err)
	assert.Equal([]byte{0xca, 0xfe, 0xbe, 0xef}, cfg.RootHash)

	// Test that changing the config succeeds: change the config and upsert,
	// then test pulling it out again.
	acs.Config = []byte{0xfe, 0xed, 0xfa, 0xce}
	err = ds.UpsertConfigStore(ctx, testUUID, &acs)
	assert.NoError(err)

	cfg, err = ds.ConfigStoreByUUID(ctx, testUUID)
	assert.NoError(err)
	assert.Equal([]byte{0xfe, 0xed, 0xfa, 0xce}, cfg.Config)
}

func testCommandQueue(t *testing.T, ds DataStore, logger *zap.Logger, slogger *zap.SugaredLogger) {
	ctx := context.Background()
	assert := require.New(t)

	a := &ApplianceID{
		CloudUUID: testUUID,
	}
	err := ds.InsertApplianceID(ctx, a)
	assert.NoError(err, "expected Insert to succeed")

	makeCmd := func(query string) (*ApplianceCommand, time.Time) {
		enqTime := time.Now()
		cmd := &ApplianceCommand{
			EnqueuedTime: enqTime,
			Query:        []byte(query),
		}
		return cmd, enqTime
	}
	makeManyCmds := func(query string, count int) []int64 {
		cmdIDs := make([]int64, count)
		for i := 0; i < count; i++ {
			cmd, _ := makeCmd(fmt.Sprintf("%s %d", query, i))
			err := ds.CommandSubmit(ctx, testUUID, cmd)
			assert.NoError(err)
			cmdIDs[i] = cmd.ID
		}
		return cmdIDs
	}

	cmd, enqTime := makeCmd("Ask Me Anything")
	// Make sure we can submit a command and have an ID assigned
	err = ds.CommandSubmit(ctx, testUUID, cmd)
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
	err = ds.CommandSubmit(ctx, testUUID, cmd)
	assert.NoError(err)
	assert.Equal(int64(2), cmd.ID)

	// Make sure fetching something for testUUID2 doesn't return anything.
	cmds, err := ds.CommandFetch(ctx, testUUID2, 1, 1)
	assert.NoError(err)
	assert.Len(cmds, 0)

	// Make sure that fetching the command gets the one we expect, that it
	// has been moved to the correct state, and that the number-of-refetches
	// counter has not been touched.
	cmds, err = ds.CommandFetch(ctx, testUUID, 1, 1)
	assert.NoError(err)
	assert.Len(cmds, 1)
	cmd = cmds[0]
	assert.Equal(int64(2), cmd.ID)
	assert.Equal("WORK", cmd.State)
	assert.Nil(cmd.NResent.Ptr())

	// Do it again, this time making sure that the resent counter has the
	// correct non-null value.
	cmds, err = ds.CommandFetch(ctx, testUUID, 1, 1)
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
	deleted, err := ds.CommandDelete(ctx, testUUID, 2)
	assert.NoError(err)
	assert.Equal(int64(0), deleted)
	// Specify keep > number of commands left.
	deleted, err = ds.CommandDelete(ctx, testUUID, 5)
	assert.NoError(err)
	assert.Equal(int64(0), deleted)
	// Specify keep == 0.
	deleted, err = ds.CommandDelete(ctx, testUUID, 0)
	assert.NoError(err)
	assert.Equal(int64(2), deleted)
	// Make some more to play with.
	cmdIDs := makeManyCmds("Whatcha Talkin' About", 20)
	// Cancel half
	for i := 0; i < 10; i++ {
		_, _, err = ds.CommandCancel(ctx, cmdIDs[i])
	}
	// Keep 5; this shouldn't delete still-queued commands.
	deleted, err = ds.CommandDelete(ctx, testUUID, 5)
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
		{"testEmpty", testEmpty},
		{"testHeartbeatIngest", testHeartbeatIngest},
		{"testInsertApplianceID", testInsertApplianceID},
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
