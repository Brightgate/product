/*
 * Copyright 2018 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
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

