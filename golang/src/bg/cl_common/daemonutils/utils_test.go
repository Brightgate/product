/*
 * Copyright 2019 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package daemonutils

import (
	// "flag"
	"fmt"
	"os"
	"os/exec"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func TestAutoSetup(t *testing.T) {
	globalLog = nil
	logConfig.Type.value = logTypeAuto
	_, _ = SetupLogs()
}

func TestDevSetup(t *testing.T) {
	globalLog = nil
	logConfig.Type.value = logTypeDev
	_, _ = SetupLogs()
}

func TestProdSetup(t *testing.T) {
	globalLog = nil
	logConfig.Type.value = logTypeProd
	_, _ = SetupLogs()
}

func TestResetup(t *testing.T) {
	l1, _ := SetupLogs()
	l2, _ := ResetupLogs()
	if l1 == l2 {
		t.Errorf("Resetup seemed to have no effect")
	}
}

func TestLogType(t *testing.T) {
	var l logType
	for _, v := range []string{"dev", "DEV", "Development", "pro", "prod", "PRODUCTION"} {
		err := l.Set(v)
		if err != nil {
			t.Errorf("Unexpected error logtype %s", v)
		}
		if l.value != logTypeDev && l.value != logTypeProd {
			t.Errorf("Unexpected devtype string")
		}
	}
	err := l.Set("foo")
	if err == nil {
		t.Errorf("Didn't get expected error from bad logtype")
	}
}

func zapBuffer() (zap.Option, *[]zapcore.Entry) {
	buf := make([]zapcore.Entry, 0)
	hook := func(e zapcore.Entry) error {
		buf = append(buf, e)
		return nil
	}
	return zap.Hooks(hook), &buf
}

// Run the named test in a subprocess, with the given extra arguments and
// environment.
func setupExternal(t *testing.T, name string, args, env []string) bool {
	if os.Getenv("B10E_TEST_SUBPROC") == "1" {
		// We're in the subprocess, so we don't need to set up, but the
		// calling function should continue.
		return true
	}

	runFlag := fmt.Sprintf("-test.run=%s", name)
	args = append([]string{runFlag}, args...)
	cmd := exec.Command(os.Args[0], args...)
	env = append([]string{"B10E_TEST_SUBPROC=1"}, env...)
	cmd.Env = append(os.Environ(), env...)

	out, err := cmd.CombinedOutput()
	t.Log(string(out))
	if e, ok := err.(*exec.ExitError); ok && !e.Success() {
		t.Error("Subprocess test failed; see above for output")
	}
	return false
}

func TestFlag(t *testing.T) {
	if !setupExternal(t, "TestFlag", []string{"-log-level", "warn"}, []string{}) {
		return
	}

	assert := require.New(t)

	// Set up a logger with a hook that records each logged entry.
	hook, bufp := zapBuffer()
	l, _ := SetupLogs(hook)

	// The warning will get logged, but not the info or debug messages.
	l.Warn("warnmsg")
	l.Info("infomsg")
	l.Debug("debugmsg")
	assert.Len(*bufp, 1)
	assert.Equal("warnmsg", (*bufp)[0].Message)
}

func TestPFlag(t *testing.T) {
	// We use a subcommand so that the flags don't get picked up by the
	// global flags managed by the flag package.
	if !setupExternal(t, "TestPFlag", []string{"subcmd", "--log-level", "warn"},
		[]string{}) {
		return
	}

	assert := require.New(t)

	subCmd := cobra.Command{
		Args: cobra.NoArgs,
	}
	subCmd.Flags().AddFlagSet(GetLogFlagSet())
	// Only parse the arguments after "subcmd"; 0, 1, and 2 should be the
	// executable, the -test.run flag and argument, and "subcmd".
	assert.Equal("subcmd", os.Args[2])
	err := subCmd.ParseFlags(os.Args[3:])
	assert.NoError(err)

	// Set up a logger with a hook that records each logged entry.
	hook, bufp := zapBuffer()
	l, _ := SetupLogs(hook)

	// The warning will get logged, but not the info or debug messages.
	l.Warn("warnmsg")
	l.Info("infomsg")
	l.Debug("debugmsg")
	assert.Len(*bufp, 1)
	assert.Equal("warnmsg", (*bufp)[0].Message)
}

func TestEnv(t *testing.T) {
	if !setupExternal(t, "TestEnv$", []string{}, []string{"B10E_LOG_LEVEL=warn"}) {
		return
	}

	assert := require.New(t)

	// Set up a logger with a hook that records each logged entry.
	hook, bufp := zapBuffer()
	l, _ := SetupLogs(hook)

	// The warning will get logged, but not the info or debug messages.
	l.Warn("warnmsg")
	l.Info("infomsg")
	l.Debug("debugmsg")
	assert.Len(*bufp, 1)
	assert.Equal("warnmsg", (*bufp)[0].Message)
}

func TestEnvAndFlag(t *testing.T) {
	if !setupExternal(t, "TestEnvAndFlag", []string{"-log-level", "info"},
		[]string{"B10E_LOG_LEVEL=warn"}) {
		return
	}

	assert := require.New(t)

	// Set up a logger with a hook that records each logged entry.
	hook, bufp := zapBuffer()
	l, _ := SetupLogs(hook)

	// The warning and info messages will get logged, because the flag
	// overrides the environment variable, but the debug message doesn't.
	l.Warn("warnmsg")
	l.Info("infomsg")
	l.Debug("debugmsg")
	assert.Len(*bufp, 2)
	assert.Equal("warnmsg", (*bufp)[0].Message)
}

