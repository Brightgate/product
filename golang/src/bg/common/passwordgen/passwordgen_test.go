/*
 * Copyright 2019 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package passwordgen

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

var (
	slog *zap.SugaredLogger
)

func setupLogging(t *testing.T) (*zap.Logger, *zap.SugaredLogger) {
	logger := zaptest.NewLogger(t)
	slogger := logger.Sugar()
	return logger, slogger
}

type passTestFunc func(*testing.T, *zap.Logger, *zap.SugaredLogger)

func testHumanPassword(t *testing.T, logger *zap.Logger, slogger *zap.SugaredLogger) {
	assert := require.New(t)
	newpw, err := HumanPassword(HumanPasswordSpec)
	if err != nil {
		t.Fatalf("HumanPassword() failed: %s %v", newpw, err)
	}

	newpw, err = HumanPassword(WordPasswordSpec)
	assert.Error(err)
	if err != nil {
		slog.Debugf("password spec successfully rejected: %s %+v", newpw, err)
	}
}

func testSecurityTheaterPassword(t *testing.T, logger *zap.Logger, slogger *zap.SugaredLogger) {
	assert := require.New(t)
	_, err := EntropyPassword(SecurityTheaterPasswordSpec)
	assert.NoError(err)
	_, err = HumanPassword(SecurityTheaterPasswordSpec)
	assert.Error(err)
}

func testBadHumanSpec(t *testing.T, logger *zap.Logger, slogger *zap.SugaredLogger) {
	assert := require.New(t)
	_, err := HumanPassword(LimitedPasswordSpec10)
	assert.Error(err)
}

func testManyPassword(t *testing.T, logger *zap.Logger, slogger *zap.SugaredLogger) {
	assert := require.New(t)
	// Run the function many times and check the lengths and passwords generated
	for range [50]int{} {
		pass, err := EntropyPassword(HumanPasswordSpec)
		assert.NoError(err)
		slog.Infof("generated %s", pass)
		assert.True(len(pass) <= HumanPasswordSpec.MaxLength,
			"length of password exceeded spec max length")
	}
}

func TestGenerators(t *testing.T) {
	testCases := []struct {
		name  string
		tFunc passTestFunc
	}{
		{"TestHumanPassword", testHumanPassword},
		{"TestSecurityTheaterPassword", testSecurityTheaterPassword},
		{"TestBadHumanSpec", testBadHumanSpec},
		{"Test Many Password", testManyPassword},
	}
	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {
			logger, slogger := setupLogging(t)
			slog = slogger
			test.tFunc(t, logger, slogger)
		})
	}
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}

