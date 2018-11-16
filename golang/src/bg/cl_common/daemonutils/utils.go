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
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"cloud.google.com/go/compute/metadata"
	"cloud.google.com/go/logging"
	"github.com/dhduvall/gcloudzap"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"golang.org/x/crypto/ssh/terminal"
)

type logType string

const (
	logTypeAuto logType = ""
	logTypeDev  logType = "dev"
	// If "stackdriver" ever gets renamed into "prod", making it the default,
	// provisions need to be made for cl-aggregate and cl-dtool, both of
	// which call SetupLogs().
	logTypeProd logType = "prod"
	logTypeSD   logType = "stackdriver"
)

var (
	globalLog        *zap.Logger
	globalSugaredLog *zap.SugaredLogger
	globalLevel      zap.AtomicLevel
	levelFlag        *zapcore.Level
	logTypeFlag      logType
	logTagPfxFlag    = flag.String("log-tag-prefix", "b10e", "Log tag prefix (for Stackdriver)")
	clrootFlag       = flag.String("root", "", "Root of cloud installation")
)

func (l *logType) String() string {
	if *l == logTypeDev {
		return "development"
	} else if *l == logTypeProd {
		return "production"
	} else if *l == logTypeSD {
		return "stackdriver"
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
	} else if ss == "sta" {
		*l = logTypeSD
		return nil
	}
	return fmt.Errorf("Unknown Log Type '%s'.  Try [dev|prod|stackdriver]", s)
}

func init() {
	levelFlag = zap.LevelFlag("log-level", zapcore.InfoLevel, "Log level [debug,info,warn,error,panic,fatal]")
	flag.Var(&logTypeFlag, "log-type", "Logging style [dev|prod|stackdriver]")
}

// SetupLogs creates a pair of zap loggers-- one structured and one
// "sugared" for use by cloud daemons.
func SetupLogs() (*zap.Logger, *zap.SugaredLogger) {
	var log *zap.Logger
	var err error

	if globalLog != nil {
		return GetLogs()
	}

	isTerm := terminal.IsTerminal(int(os.Stderr.Fd()))

	lt := logTypeFlag
	if lt == logTypeAuto {
		if isTerm {
			lt = logTypeDev
		} else {
			lt = logTypeProd
		}
	}

	pname, err := os.Executable()
	if err != nil {
		// Fall back to whatever's in $0
		pname = os.Args[0]
	}
	pname = filepath.Base(pname)

	var config zap.Config
	globalLevel = zap.NewAtomicLevelAt(*levelFlag)
	zapOptions := []zap.Option{
		zap.AddStacktrace(zapcore.ErrorLevel),
	}

	if lt == logTypeDev {
		config = zap.NewDevelopmentConfig()
		config.Level = globalLevel
		if isTerm {
			config.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
		}
		log, err = config.Build(zapOptions...)
	} else {
		// We take the defaults but choose our time format and set the
		// default level.
		config = zap.NewProductionConfig()
		config.Level = globalLevel
		config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

		if lt == logTypeSD {
			// The project ID is needed for the logger name
			proj, err := metadata.ProjectID()
			if err != nil {
				panic(fmt.Sprintf("can't get project ID: %s\n", err))
			}

			// Create the logging client.  The docs say we should
			// call .Close() on this to flush all the loggers, but
			// we do call .Sync() on the logger, which will perform
			// the flush.
			gcl, err := logging.NewClient(context.Background(), proj)
			if err != nil {
				panic(fmt.Sprintf("can't create google logging client: %s\n", err))
			}

			// Tag the logger; eg, "b10e.cloud.eventd"
			tag := *logTagPfxFlag + "." + strings.Replace(pname, "cl.", "cloud.", 1)

			log, err = gcloudzap.New(config, gcl, tag, zapOptions...)
		} else {
			log, err = config.Build(zapOptions...)
		}
	}
	if err != nil {
		panic(fmt.Sprintf("can't zap: %v", err))
	}

	// Make sure the program name is available in the log payload
	log = log.Named(pname)

	log.Debug(fmt.Sprintf("Zap %s Logging at %s", lt, config.Level))
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
