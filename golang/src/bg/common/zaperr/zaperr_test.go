/*
 * COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package zaperr

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

var (
	bufSinks map[string]*bufferSink
)

// bufferSink is an object providing an in-memory byte array sink for testing
// zap.  Zap implements this, but only in an internal package.
type bufferSink struct {
	bytes.Buffer
}

// Sync implements zapcore.WriteSyncer, part of zap.Sink
func (b *bufferSink) Sync() error {
	return nil
}

// Close implements zap.Sink
func (b *bufferSink) Close() error {
	return nil
}

func TestBasic(t *testing.T) {
	assert := require.New(t)

	config := zap.NewProductionConfig()
	config.OutputPaths = []string{"buffer://basic"}

	log, err := config.Build()
	assert.NoError(err)
	slog := log.Sugar()

	ze := Errorw("This is my message", "payload", "this is my payload")
	slog.Infow("Outer message", "error", ze)

	m := make(map[string]interface{})
	err = json.Unmarshal(bufSinks["basic"].Bytes(), &m)
	assert.NoError(err)

	val, ok := m["msg"]
	assert.True(ok)
	assert.Equal("Outer message", val)

	val, ok = m["error"]
	assert.True(ok)
	valObj, ok := val.(map[string]interface{})
	assert.True(ok)
	val, ok = valObj["msg"]
	assert.True(ok)
	assert.Equal("This is my message", val)
	val, ok = valObj["payload"]
	assert.True(ok)
	assert.Equal("this is my payload", val)
}

func TestNested(t *testing.T) {
	assert := require.New(t)

	config := zap.NewProductionConfig()
	config.OutputPaths = []string{"buffer://nested"}

	log, err := config.Build()
	assert.NoError(err)
	slog := log.Sugar()

	zeInner := Errorw("Inner message", "inner key", "inner value")
	ze := Errorw("Outer message", "error", zeInner)
	slog.Infow("Top-level message", "error", ze)

	m := make(map[string]interface{})
	err = json.Unmarshal(bufSinks["nested"].Bytes(), &m)
	assert.NoError(err)

	val, ok := m["error"]
	assert.True(ok)
	valObj, ok := val.(map[string]interface{})
	assert.True(ok)
	val, ok = valObj["error"]
	assert.True(ok)
	valObj, ok = val.(map[string]interface{})
	assert.True(ok)
	val, ok = valObj["inner key"]
	assert.True(ok)
	assert.Equal("inner value", val)
}

func TestNestedArray(t *testing.T) {
	assert := require.New(t)

	config := zap.NewProductionConfig()
	config.OutputPaths = []string{"buffer://nestedarray"}

	log, err := config.Build()
	assert.NoError(err)
	slog := log.Sugar()

	zeArray := ZapErrorArray{
		Errorw("Innermost error message", "key", "value"),
	}
	ze := Errorw("Outer message", "error", zeArray)
	slog.Infow("Top-level message", "error", ze)

	m := make(map[string]interface{})
	err = json.Unmarshal(bufSinks["nestedarray"].Bytes(), &m)
	assert.NoError(err)

	val, ok := m["error"]
	assert.True(ok)
	valObj, ok := val.(map[string]interface{})
	assert.True(ok)
	val, ok = valObj["error"]
	assert.True(ok)
	valArr, ok := val.([]interface{})
	assert.True(ok)
	assert.Len(valArr, 1)
	valObj, ok = valArr[0].(map[string]interface{})
	assert.True(ok)
	val, ok = valObj["msg"]
	assert.True(ok)
	assert.Equal("Innermost error message", val)
	val, ok = valObj["key"]
	assert.True(ok)
	assert.Equal("value", val)
}

func TestMain(m *testing.M) {
	bufSinks = make(map[string]*bufferSink, 0)
	zap.RegisterSink("buffer", func(u *url.URL) (zap.Sink, error) {
		name := u.Hostname()
		if _, ok := bufSinks[name]; ok {
			return nil, fmt.Errorf("Already created a buffer sink named %q", name)
		}
		bufSinks[name] = &bufferSink{}
		return bufSinks[name], nil
	})

	os.Exit(m.Run())
}
