/*
* COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
*
* This copyright notice is Copyright Management Information under 17 USC 1202
* and is included to protect this work and deter copyright infringement.
* Removal or alteration of this Copyright Management Information without the
* express written permission of Brightgate Inc is prohibited, and any
* such unauthorized removal or alteration will be a violation of federal law.
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
	var spec PasswordSpec
	spec = HumanPasswordSpec
	assert := require.New(t)
	// Run the function many times and check the lengths and passwords generated
	for range [50]int{} {
		pass, err := EntropyPassword(spec)
		assert.NoError(err)
		slog.Infof("generated %s", pass)
		assert.True(len(pass) <= spec.MaxLength, "length of password exceeded spec max length")
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
