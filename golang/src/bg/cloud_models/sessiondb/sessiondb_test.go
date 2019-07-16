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

package sessiondb

import (
	"context"
	"log"
	"os"
	"testing"

	"bg/common/briefpg"

	"github.com/stretchr/testify/require"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

var (
	bpg *briefpg.BriefPG
)

func TestTableMissing(t *testing.T) {
	var ctx = context.Background()
	bpg = briefpg.New(nil)
	defer bpg.Fini(ctx)
	err := bpg.Start(ctx)
	if err != nil {
		log.Fatalf("failed to setup: %+v", err)
	}
	assert := require.New(t)

	logger := zaptest.NewLogger(t)
	bpg.Logger = zap.NewStdLog(logger)

	testdb, err := bpg.CreateDB(ctx, "TestTableMissing", "")
	if err != nil {
		t.Fatalf("CreateDB Failed: %v", err)
	}

	ds1, err := Connect(testdb, false)
	assert.Error(err, "Connect(..., false) should fail")
	assert.Nil(ds1)

	ds2, err := Connect(testdb, true)
	assert.NoError(err, "Connect(..., true) should succeed")
	ds2.Close()

	ds2.LoadSchema(ctx, "./schema")
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
