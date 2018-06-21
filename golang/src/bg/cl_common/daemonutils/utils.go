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

package daemonutils

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"golang.org/x/crypto/ssh/terminal"
)

type logType string

const (
	logTypeAuto logType = ""
	logTypeDev  logType = "dev"
	logTypeProd logType = "prod"
)

var (
	globalLog        *zap.Logger
	globalSugaredLog *zap.SugaredLogger
	globalLevel      zap.AtomicLevel
	levelFlag        *zapcore.Level
	logTypeFlag      logType
	clrootFlag       = flag.String("root", "", "Root of cloud installation")
)

func (l *logType) String() string {
	if *l == logTypeDev {
		return "development"
	} else if *l == logTypeProd {
		return "production"
	} else {
		return "auto"
	}
}

func (l *logType) Set(s string) error {
	ss := strings.ToLower(s)[0:3]
	if ss == "dev" {
		*l = logTypeDev
		return nil
	} else if ss == "pro" {
		*l = logTypeProd
		return nil
	}
	return fmt.Errorf("Unknown Log Type '%s'.  Try [dev|prod]", s)
}

func init() {
	levelFlag = zap.LevelFlag("log-level", zapcore.InfoLevel, "Log level [debug,info,warn,error,panic,fatal]")
	flag.Var(&logTypeFlag, "log-type", "Logging style [dev|prod]")
}

// SetupLogs creates a pair of zap loggers-- one structured and one
// "sugared" for use by cloud daemons.
func SetupLogs() (*zap.Logger, *zap.SugaredLogger) {
	var log *zap.Logger
	var err error

	if globalLog != nil {
		return GetLogs()
	}

	lt := logTypeFlag
	if lt == logTypeAuto {
		if terminal.IsTerminal(int(os.Stderr.Fd())) {
			lt = logTypeDev
		} else {
			lt = logTypeProd
		}
	}

	globalLevel = zap.NewAtomicLevelAt(*levelFlag)
	if lt == logTypeDev {
		config := zap.NewDevelopmentConfig()
		config.Level = globalLevel
		config.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
		log, err = config.Build(zap.AddStacktrace(zapcore.ErrorLevel))
		log.Debug(fmt.Sprintf("Zap %s Logging at %s", lt, config.Level))
	} else {
		// For now we'll take the defaults but choose our time format.
		// In the future we'll want to adjust this with more
		// customization.
		config := zap.NewProductionConfig()
		config.Level = globalLevel
		config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
		log, err = config.Build(zap.AddStacktrace(zapcore.ErrorLevel))
		log.Debug(fmt.Sprintf("Zap %s Logging at %s", lt, config.Level))
	}
	if err != nil {
		panic("can't zap")
	}
	globalLog = log
	globalSugaredLog = globalLog.Sugar()
	return GetLogs()
}

// ResetupLogs is intended for use after flags.Parse() has been called by
// the application, since the flags passed may necessitate rebuild of the
// loggers.
func ResetupLogs() (*zap.Logger, *zap.SugaredLogger) {
	globalLog = nil
	globalSugaredLog = nil
	return SetupLogs()
}

// GetLogs returns the current global pair of loggers.
func GetLogs() (*zap.Logger, *zap.SugaredLogger) {
	return globalLog, globalSugaredLog
}

// ClRoot computes the "cloud root".
// If the "-root" option is set, it returns that
// Else if CLROOT is set in the environment, it returns CLROOT
// Else it computes the CLROOT based on the executable's path
func ClRoot() string {
	if *clrootFlag != "" {
		return *clrootFlag
	}
	clrootEnv := os.Getenv("CLROOT")
	if clrootEnv != "" {
		return clrootEnv
	}
	executable, err := os.Executable()
	if err != nil {
		panic(err)
	}
	return filepath.Dir(filepath.Dir(executable))
}
