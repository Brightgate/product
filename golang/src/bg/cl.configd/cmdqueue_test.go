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

package main

import (
	"context"
	"fmt"
	"sort"
	"testing"
	"time"

	"bg/cl_common/daemonutils"
	"bg/cloud_models/appliancedb"
	"bg/common/briefpg"
	"bg/common/cfgapi"
	"bg/common/cfgmsg"
	"bg/common/cfgtree"

	"github.com/satori/uuid"
	"github.com/stretchr/testify/require"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

const (
	templateDBName = "appliancedb_template"
	templateDBArg  = "TEMPLATE=" + templateDBName
	testUUIDstr1   = "00000001-0001-0001-0001-000000000001"
	testUUIDstr2   = "00000002-0002-0002-0002-000000000002"
)

var (
	bpg       *briefpg.BriefPG
	testUUID1 = uuid.Must(uuid.FromString(testUUIDstr1))
	testUUID2 = uuid.Must(uuid.FromString(testUUIDstr2))

	testSS1 = &siteState{
		siteUUID: testUUIDstr1,
	}
	testSS2 = &siteState{
		siteUUID: testUUIDstr2,
	}

	// Make 3 basic queries for use in various tests
	testQs = mkQueries(1, 3)
	// The test query IDs in sorted order
	testQsKeys = []int64{1, 2, 3}
)

// Fake store implementation
type testStore struct {
	ptree *cfgtree.PTree
}

func (s *testStore) get(_ context.Context, _ string) (*cfgtree.PTree, error) {
	return s.ptree, nil
}

func (s *testStore) set(_ context.Context, _ string, ptree *cfgtree.PTree) error {
	s.ptree = ptree
	return nil
}

func setupLogging(t *testing.T) (*zap.Logger, *zap.SugaredLogger) {
	// log and slog are daemon logging globals
	log = zaptest.NewLogger(t)
	slog = log.Sugar()
	return log, slog
}

type cqTestFunc func(*testing.T, cmdQueue, *zap.SugaredLogger)

func mkQueries(start int64, n int) map[int64]*cfgmsg.ConfigQuery {
	queries := make(map[int64]*cfgmsg.ConfigQuery)
	for i := int64(start); i < start+int64(n); i++ {
		query, err := cfgapi.PropOpsToQuery([]cfgapi.PropertyOp{
			{
				Op:      cfgapi.PropAdd,
				Name:    fmt.Sprintf("@/test%d", i),
				Value:   fmt.Sprintf("%d", i),
				Expires: nil,
			},
		})
		if err != nil {
			panic(err)
		}
		queries[i] = query
	}
	return queries
}

// submitQueries is a helper for submitting a map of queries to the queue.
// This presumes that you know what the IDs will be in advance; the code checks
// these assumptions.
func submitQueries(ctx context.Context, t *testing.T, cq cmdQueue, slogger *zap.SugaredLogger, qs map[int64]*cfgmsg.ConfigQuery) {
	assert := require.New(t)
	// make a sorted list of keys
	keys := make([]int64, len(qs))
	i := 0
	for k := range qs {
		keys[i] = k
		i++
	}
	sort.Slice(keys, func(i, j int) bool {
		return keys[i] < keys[j]
	})
	// submit each command to the queue.
	for i = 0; i < len(keys); i++ {
		id := keys[i]
		q := qs[id]
		sid, err := cq.submit(ctx, testSS1, q)
		assert.NoError(err)
		assert.Equal(id, sid)
		slogger.Infof("submitted %d: %v", id, q)
	}
}

// testSubmit does a basic smoke test for command submission
func testSubmit(t *testing.T, q cmdQueue, slogger *zap.SugaredLogger) {
	ctx := context.Background()

	qs := mkQueries(1, 10)
	submitQueries(ctx, t, q, slogger, qs)
}

// testStatus tests getting command status under a variety of conditions
func testStatus(t *testing.T, q cmdQueue, slogger *zap.SugaredLogger) {
	ctx := context.Background()

	assert := require.New(t)

	// Empty queue
	response, err := q.status(ctx, testSS1, 1)
	assert.NoError(err)
	assert.Equal(cfgmsg.ConfigResponse_NOCMD, response.Response)

	submitQueries(ctx, t, q, slogger, testQs)

	// Check status of queue commands
	for qid := range testQs {
		response, err = q.status(ctx, testSS1, qid)
		assert.NoError(err)
		assert.Equal(cfgmsg.ConfigResponse_QUEUED, response.Response)
		assert.Equal(qid, response.CmdID)
	}

	// Fetch commands
	queries, err := q.fetch(ctx, testSS1, 0, 100, false)
	assert.NoError(err)
	assert.Len(queries, 3)

	// See that all are now in progress
	for qid := range testQs {
		response, err = q.status(ctx, testSS1, qid)
		assert.NoError(err)
		assert.Equal(cfgmsg.ConfigResponse_INPROGRESS, response.Response)
	}

	// complete each query in order, check status
	for _, qid := range testQsKeys {
		rval := cfgapi.GenerateConfigResponse("fake response", nil)
		rval.CmdID = qid
		err = q.complete(ctx, testSS1, rval)
		assert.NoError(err)
		response, err = q.status(ctx, testSS1, qid)
		assert.NoError(err)
		assert.Equal(cfgmsg.ConfigResponse_OK, response.Response)
	}

	// add one more command, cancel it, then check the status
	query, _ := cfgapi.PropOpsToQuery([]cfgapi.PropertyOp{})
	idC, err := q.submit(ctx, testSS1, query)
	assert.NoError(err)
	assert.Equal(int64(4), idC)

	response, err = q.cancel(ctx, testSS1, idC)
	assert.NoError(err)
	assert.Equal(cfgmsg.ConfigResponse_OK, response.Response)
	response, err = q.status(ctx, testSS1, idC)
	assert.NoError(err)
	assert.Equal(cfgmsg.ConfigResponse_CANCELED, response.Response)
}

// testFetch tests command fetch
func testFetch(t *testing.T, q cmdQueue, slogger *zap.SugaredLogger) {
	ctx := context.Background()
	assert := require.New(t)

	submitQueries(ctx, t, q, slogger, testQs)

	// Test max == 0
	qs, err := q.fetch(ctx, testSS1, 0, 0, false)
	assert.NoError(err)
	assert.Len(qs, 0)

	qs, err = q.fetch(ctx, testSS1, 0, 100, false)
	assert.NoError(err)
	assert.Equal([]*cfgmsg.ConfigQuery{testQs[1], testQs[2], testQs[3]}, qs)

	qs, err = q.fetch(ctx, testSS1, 1, 100, false)
	assert.NoError(err)
	assert.Equal([]*cfgmsg.ConfigQuery{testQs[2], testQs[3]}, qs)

	qs, err = q.fetch(ctx, testSS1, 3, 100, false)
	assert.NoError(err)
	assert.Len(qs, 0)

	// complete each query in order, check status
	for _, qid := range testQsKeys {
		rval := cfgapi.GenerateConfigResponse("fake response", nil)
		rval.CmdID = qid
		err = q.complete(ctx, testSS1, rval)
		assert.NoError(err)
	}
	qs, err = q.fetch(ctx, testSS1, 0, 100, false)
	assert.NoError(err)
	assert.Len(qs, 0)
}

// testCancel tests command cancellation, and related special case behavior
func testCancel(t *testing.T, q cmdQueue, slogger *zap.SugaredLogger) {
	ctx := context.Background()
	assert := require.New(t)

	submitQueries(ctx, t, q, slogger, testQs)

	// cancel a nonexistant command (id 100)
	response, err := q.cancel(ctx, testSS1, 100)
	assert.NoError(err)
	assert.Equal(cfgmsg.ConfigResponse_NOCMD, response.Response)

	// cancel an extant command
	response, err = q.cancel(ctx, testSS1, 1)
	assert.NoError(err)
	assert.Equal(cfgmsg.ConfigResponse_OK, response.Response)

	// cancel the same command twice, should work
	response, err = q.cancel(ctx, testSS1, 1)
	assert.NoError(err)
	assert.Equal(cfgmsg.ConfigResponse_OK, response.Response)

	// Fetch remaining 2 commands-- check that cancellation is impossible
	qs, err := q.fetch(ctx, testSS1, 0, 100, false)
	assert.NoError(err)
	assert.Equal([]*cfgmsg.ConfigQuery{testQs[2], testQs[3]}, qs)
	// While this cancellation will fail, the queue logic also causes it to
	// dequeue the command, making it impossible to fetch it again.
	response, err = q.cancel(ctx, testSS1, 2)
	assert.NoError(err)
	assert.Equal(cfgmsg.ConfigResponse_INPROGRESS, response.Response)
	// So now there should only be one command
	qs, err = q.fetch(ctx, testSS1, 0, 100, false)
	assert.NoError(err)
	assert.Equal([]*cfgmsg.ConfigQuery{testQs[3]}, qs)

	// complete a command-- check cancellation impossible
	r := cfgapi.GenerateConfigResponse("fake response", nil)
	r.CmdID = 3
	err = q.complete(ctx, testSS1, r)
	assert.NoError(err)
	response, err = q.cancel(ctx, testSS1, 3)
	assert.NoError(err)
	assert.Equal(cfgmsg.ConfigResponse_FAILED, response.Response)
}

// testComplete tests command completion
func testComplete(t *testing.T, q cmdQueue, slogger *zap.SugaredLogger) {
	ctx := context.Background()
	assert := require.New(t)

	submitQueries(ctx, t, q, slogger, testQs)

	response := cfgapi.GenerateConfigResponse("response", nil)
	response.CmdID = 1
	err := q.complete(ctx, testSS1, response)
	assert.NoError(err)

	// Multiple completions yields a warning
	err = q.complete(ctx, testSS1, response)
	assert.NoError(err)

	// An invalid cmdID yields a warning but nothing else
	response = cfgapi.GenerateConfigResponse("", cfgapi.ErrNoProp)
	response.CmdID = 100
	err = q.complete(ctx, testSS1, response)
	assert.NoError(err)

	// This is not really realistic, since only unfetched operations
	// should be able to be canceled.  But, a completion for a canceled
	// cmd should not fail, it should just be ignored.
	response, err = q.cancel(ctx, testSS1, 3)
	assert.NoError(err)
	assert.Equal(cfgmsg.ConfigResponse_OK, response.Response)
	err = q.complete(ctx, testSS1, response)
	assert.NoError(err)
}

// testFullRefresh tests the special-case logic for full tree refresh
// e.g. (GET @/)
func testFullRefresh(t *testing.T, q cmdQueue, slogger *zap.SugaredLogger) {
	var err error
	ctx := context.Background()
	assert := require.New(t)

	tc := &testStore{}
	// set daemon global 'store'
	store = tc

	qs := make(map[int64]*cfgmsg.ConfigQuery)
	qs[1], err = cfgapi.PropOpsToQuery([]cfgapi.PropertyOp{
		{
			Op:   cfgapi.PropGet,
			Name: "@/",
		},
	})
	if err != nil {
		panic(err)
	}
	// Copy
	qq := *qs[1]
	qs[2] = &qq

	submitQueries(ctx, t, q, slogger, qs)

	response := cfgapi.GenerateConfigResponse("{}", nil)
	response.CmdID = 1
	err = q.complete(ctx, testSS1, response)
	assert.NoError(err)
	assert.NotNil(tc.ptree)

	// Doesn't give an error when tree is bad, but should not explode
	tc.ptree = nil
	response = cfgapi.GenerateConfigResponse("badtree", nil)
	response.CmdID = 2
	err = q.complete(ctx, testSS1, response)
	assert.NoError(err)
	assert.Nil(tc.ptree)
}

// testSiteSpoof tests that sites can't affect commands they don't own
func testSiteSpoof(t *testing.T, q cmdQueue, slogger *zap.SugaredLogger) {
	ctx := context.Background()
	assert := require.New(t)

	submitQueries(ctx, t, q, slogger, testQs)

	// Try to get status as SS1
	response, err := q.status(ctx, testSS1, 1)
	assert.NoError(err)
	assert.Equal(cfgmsg.ConfigResponse_QUEUED, response.Response)

	// Try to get status as SS2
	response, err = q.status(ctx, testSS2, 1)
	assert.NoError(err)
	assert.Equal(cfgmsg.ConfigResponse_NOCMD, response.Response)

	// Use site 2 to try to complete one of site 1's commands
	// This will not return an error, but also should not affect the
	// command.
	rval := cfgapi.GenerateConfigResponse("{}", nil)
	rval.CmdID = 1
	err = q.complete(ctx, testSS2, rval)
	assert.NoError(err)
	// Check the state of the cmd
	response, err = q.status(ctx, testSS1, 1)
	assert.NoError(err)
	assert.Equal(cfgmsg.ConfigResponse_QUEUED, response.Response)

	// Use site 2 to try to cancel one of site 1's commands
	// This will not return an error (the daemon will warn instead), but
	// also should not affect the command.
	response = cfgapi.GenerateConfigResponse("{}", nil)
	response.CmdID = 1
	_, err = q.cancel(ctx, testSS2, 1)
	assert.NoError(err)
	// Check the state of the cmd
	response, err = q.status(ctx, testSS1, 1)
	assert.NoError(err)
	assert.Equal(cfgmsg.ConfigResponse_QUEUED, response.Response)
}

// make a template database, loaded with the schema.  Subsequently
// we can knock out copies.
func mkTemplate(ctx context.Context) error {
	templateURI, err := bpg.CreateDB(ctx, templateDBName, "")
	if err != nil {
		return fmt.Errorf("failed to make templatedb: %+v", err)
	}

	templateDB, err := appliancedb.Connect(templateURI)
	if err != nil {
		return fmt.Errorf("failed to connect to templatedb: %+v", err)
	}

	defer templateDB.Close()
	err = templateDB.LoadSchema(ctx, "../cloud_models/appliancedb/schema")
	if err != nil {
		return fmt.Errorf("failed to load schema: %+v", err)
	}

	// Prep one site for testing
	err = templateDB.InsertCustomerSite(ctx, &appliancedb.CustomerSite{
		UUID:             testUUID1,
		OrganizationUUID: uuid.Nil,
		Name:             "1",
	})
	if err != nil {
		return fmt.Errorf("failed to load customer site: %+v", err)
	}
	// Prep one site for testing
	err = templateDB.InsertCustomerSite(ctx, &appliancedb.CustomerSite{
		UUID:             testUUID2,
		OrganizationUUID: uuid.Nil,
		Name:             "2",
	})
	if err != nil {
		return fmt.Errorf("failed to load customer site: %+v", err)
	}

	return nil
}

// TestCmdQueue invokes subtests for each of the command queue implementations
func TestCmdQueue(t *testing.T) {
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

	type tc struct {
		name  string
		tFunc cqTestFunc
	}

	testCases := []tc{
		{"Submit", testSubmit},
		{"Fetch", testFetch},
		{"Cancel", testCancel},
		{"Status", testStatus},
		{"Complete", testComplete},
		{"FullRefresh", testFullRefresh},
	}

	for _, tc := range testCases {
		name := "Mem" + tc.name
		t.Run(name, func(t *testing.T) {
			logger, slogger := setupLogging(t)
			daemonutils.SetGlobalLogTest(logger, slogger)
			mq := newMemCmdQueue(testUUIDstr1, 3)
			// set the initial CmdID to 0 instead of unix time
			mq.lastCmdID = 0
			tc.tFunc(t, mq, slogger)
		})
	}

	// We only run SiteSpoof for the database command queue, because the
	// two queues are stylistically different: the dbCmdQueue mixes all
	// sites in the same queue, whereas memCmdQueue is inherently per-site.
	testCases = append(testCases,
		tc{"SiteSpoof", testSiteSpoof})

	for _, tc := range testCases {
		name := "DB" + tc.name
		t.Run(name, func(t *testing.T) {
			logger, slogger := setupLogging(t)
			bpg.Logger = zap.NewStdLog(logger)
			daemonutils.SetGlobalLogTest(logger, slogger)
			// Ensure uniqueness so that things work if count > 1
			dbName := fmt.Sprintf("%s_%d", t.Name(), time.Now().Unix())
			testdb, err := bpg.CreateDB(ctx, dbName, templateDBArg)
			if err != nil {
				t.Fatalf("CreateDB Failed: %v", err)
			}
			ds, err := appliancedb.Connect(testdb)
			if err != nil {
				t.Fatalf("Connect failed: %v", err)
			}
			defer ds.Close()

			q := &dbCmdQueue{connInfo: testdb, handle: ds}
			tc.tFunc(t, q, slogger)
		})
	}

}
