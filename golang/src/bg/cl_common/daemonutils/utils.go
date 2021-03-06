/*
 * Copyright 2020 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package daemonutils

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"cloud.google.com/go/compute/metadata"
	"cloud.google.com/go/logging"
	"github.com/dhduvall/gcloudzap"
	"github.com/grpc-ecosystem/go-grpc-middleware/util/metautils"
	"github.com/pkg/errors"
	"github.com/spf13/pflag"
	"github.com/tomazk/envcfg"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"golang.org/x/crypto/ssh/terminal"
	"google.golang.org/grpc/peer"
)

type logType struct {
	set   bool
	value string
}

const (
	logTypeAuto string = ""
	logTypeDev  string = "dev"
	// If "stackdriver" ever gets renamed into "prod", making it the default,
	// provisions need to be made for cl-aggregate and cl-dtool, both of
	// which call SetupLogs().
	logTypeProd string = "prod"
	logTypeSD   string = "stackdriver"
)

var (
	globalLog        *zap.Logger
	globalSugaredLog *zap.SugaredLogger
	globalLevel      zap.AtomicLevel
	logConfig        LogConfig
	clrootFlag       = flag.String("root", "", "Root of cloud installation")

	// SkipField is a zap field that can be used to prevent a log entry from
	// being submitted to Stackdriver.
	SkipField = gcloudzap.SkipField
)

func (l *logType) String() string {
	if l.value == logTypeDev {
		return "development"
	} else if l.value == logTypeProd {
		return "production"
	} else if l.value == logTypeSD {
		return "stackdriver"
	} else {
		return "auto"
	}
}

func (l *logType) Set(s string) error {
	ss := strings.ToLower(s)[0:3]
	if ss == "dev" {
		*l = logType{set: true, value: logTypeDev}
		return nil
	} else if ss == "pro" {
		*l = logType{set: true, value: logTypeProd}
		return nil
	} else if ss == "sta" {
		*l = logType{set: true, value: logTypeSD}
		return nil
	}
	return fmt.Errorf("Unknown Log Type '%s'.  Try [dev|prod|stackdriver]", s)
}

func (l *logType) Type() string {
	return "logType"
}

func (l *logType) UnmarshalText(text []byte) error {
	return l.Set(string(text))
}

type optionalString struct {
	set   bool
	value string
}

func (os *optionalString) Set(s string) error {
	os.value = s
	os.set = true
	return nil
}

func (os *optionalString) String() string {
	return os.value
}

func (os *optionalString) Type() string {
	return "string"
}

func (os *optionalString) UnmarshalText(text []byte) error {
	if len(text) == 0 {
		return nil
	}
	return os.Set(string(text))
}

type optionalLevel struct {
	set   bool
	value zapcore.Level
}

func (ol *optionalLevel) Set(s string) error {
	ol.set = true
	return ol.value.Set(s)
}

func (ol *optionalLevel) String() string {
	return ol.value.String()
}

func (ol *optionalLevel) Type() string {
	return "zapcore.Level"
}

func (ol *optionalLevel) UnmarshalText(text []byte) error {
	return ol.Set(string(text))
}

// LogConfig represents the logging configuration which can be set by
// environment variables and command-line flags.  The tag can also be set by
// instance metadata; this value, if present, will override all other sources.
type LogConfig struct {
	Level     optionalLevel  `envcfg:"B10E_LOG_LEVEL"`
	TagPrefix optionalString `envcfg:"B10E_LOG_TAG_PREFIX"`
	Type      logType        `envcfg:"B10E_LOG_TYPE"`
}

func init() {
	envcfg.Unmarshal(&logConfig)

	// If we didn't find the environment variables, set some defaults.
	if !logConfig.Level.set {
		logConfig.Level.value = zapcore.InfoLevel
		logConfig.Level.set = true
	}
	if !logConfig.Type.set {
		// The default value is "", and Set() doesn't allow that.
		logConfig.Type.set = true
	}
	if !logConfig.TagPrefix.set {
		logConfig.TagPrefix.Set("b10e")
	}

	// Set up commandline flags for programs using the flag package.
	flag.Var(&logConfig.Level, "log-level", "Log level [debug,info,warn,error,panic,fatal]")
	flag.Var(&logConfig.Type, "log-type", "Logging style [dev|prod|stackdriver]")
	flag.Var(&logConfig.TagPrefix, "log-tag-prefix", "Log tag prefix (for Stackdriver)")
}

// GetLogFlagSet returns a pflag.FlagSet of the log-relevant flags for programs
// using cobra to add to their flag sets.
func GetLogFlagSet() *pflag.FlagSet {
	logFlagSet := pflag.NewFlagSet("log", pflag.ExitOnError)
	levelFlag := flag.Lookup("log-level")
	logFlagSet.Var(&logConfig.Level, "log-level", levelFlag.Usage)
	typeFlag := flag.Lookup("log-type")
	logFlagSet.Var(&logConfig.Type, "log-type", typeFlag.Usage)
	prefixFlag := flag.Lookup("log-tag-prefix")
	logFlagSet.Var(&logConfig.TagPrefix, "log-tag-prefix", prefixFlag.Usage)

	return logFlagSet
}

// This mirrors the stackTracer interface in pkg/errors.
type stackTracer interface {
	StackTrace() errors.StackTrace
}

// bgCore is a pass-through zapcore.Core which overrides the Caller and Stack
// fields of an entry to use that data from errors in the fields rather than
// from the point of calling the logging function.  When we log to stackdriver,
// gcloudzap takes care of this for us, but we want these benefits when looking
// at the console logs, too.
type bgCore struct {
	c zapcore.Core
}

func (c *bgCore) Enabled(lvl zapcore.Level) bool {
	return c.c.Enabled(lvl)
}

func (c *bgCore) With(fields []zapcore.Field) zapcore.Core {
	return &bgCore{c.c.With(fields)}
}

func (c *bgCore) Check(ent zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if c.Enabled(ent.Level) {
		return ce.AddCore(ent, c)
	}
	return ce
}

// This is where we can override ent.Caller and ent.Stack based on what we get
// out of the fields.  We take the first field with the key gcloudzap.CallerKey
// (which has been added for us in echozap) that is a stackTracer.
func (c *bgCore) Write(ent zapcore.Entry, fields []zapcore.Field) error {
	for _, f := range fields {
		if f.Key != gcloudzap.CallerKey {
			continue
		}

		if err, ok := f.Interface.(stackTracer); ok {
			trace := err.StackTrace()

			// If there's no actual stack associated with the error,
			// set the members to something that indicates that, but
			// don't stop looking for other possible errors.
			if len(trace) == 0 {
				if ent.Stack != "" {
					ent.Stack = "no stack available"
				}
				ent.Caller = zapcore.NewEntryCaller(0, "", 0, false)
				continue
			}

			// We should only add the stack if the stack is enabled.
			// That information is stashed in the logger with no way
			// to access it, so we look at whether a stack has
			// already been recorded, which there will be if and
			// only if the logger determined that it is enabled.
			if ent.Stack != "" {
				ent.Stack = fmt.Sprintf("%+v", trace)
			}

			pc := uintptr(trace[0]) - 1
			if fn := runtime.FuncForPC(pc); fn != nil {
				file, line := fn.FileLine(pc)
				ent.Caller = zapcore.NewEntryCaller(pc, file, line, true)
			}

			break
		}
	}
	return c.c.Write(ent, fields)
}

func (c *bgCore) Sync() error {
	return c.c.Sync()
}

// SetupLogs creates a pair of zap loggers-- one structured and one
// "sugared" for use by cloud daemons.
func SetupLogs(opts ...zap.Option) (*zap.Logger, *zap.SugaredLogger) {
	var log *zap.Logger
	var err error

	if globalLog != nil {
		return GetLogs()
	}

	isTerm := terminal.IsTerminal(int(os.Stderr.Fd()))

	lt := logConfig.Type.value
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
	globalLevel = zap.NewAtomicLevelAt(logConfig.Level.value)
	zapOptions := make([]zap.Option, 0)
	zapOptions = append(zapOptions, opts...)
	zapOptions = append(zapOptions, zap.AddStacktrace(zapcore.ErrorLevel))
	zapOptions = append(zapOptions, zap.WrapCore(
		func(c zapcore.Core) zapcore.Core {
			return &bgCore{c}
		}))

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

			tagPrefix, _ := metadata.InstanceAttributeValue("log-tag-prefix")
			if tagPrefix == "" {
				tagPrefix = logConfig.TagPrefix.String()
			}

			// When the logs are coming from multiple machines, it's
			// very useful to have fields in the logs that identify
			// the original machines, so we always add zone and
			// instance name.
			zone, err := metadata.Zone()
			if err != nil {
				panic(fmt.Sprintf("Unable to retrieve GCP zone: %v", err))
			}

			inst, err := metadata.InstanceName()
			if err != nil {
				panic(fmt.Sprintf("Unable to retrieve instance name: %v", err))
			}

			zapOptions = append(zapOptions, zap.Fields(
				zap.String("gcp_zone", zone),
				zap.String("gcp_instance_name", inst),
			))

			// Create the logging client.  The docs say we should
			// call .Close() on this to flush all the loggers, but
			// we do call .Sync() on the logger, which will perform
			// the flush.
			//
			// We rely on ADC to provide the logging.logWriter role,
			// rather than trying to find a way to get an access
			// token from Vault.  In production, the role should be
			// granted to the instance service account.
			gcl, err := logging.NewClient(context.Background(), proj)
			if err != nil {
				panic(fmt.Sprintf("can't create google logging client: %s\n", err))
			}

			// Tag the logger; eg, "b10e.cloud.eventd"
			tag := tagPrefix + "." + strings.Replace(pname, "cl.", "cloud.", 1)

			log, err = gcloudzap.New(config, gcl, tag, zapOptions...)
		} else {
			log, err = config.Build(zapOptions...)
		}
	}
	if err != nil {
		panic(fmt.Sprintf("can't zap: %v", err))
	}

	// Redirect the Go standard logger.
	_ = zap.RedirectStdLog(log)

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

// SetLogLevel sets the level of the global loggers and all their descendants.
func SetLogLevel(l zapcore.Level) {
	globalLevel.SetLevel(l)
}

// EndpointLogger builds a zap logger customized for use by an endpoint.  It
// attaches useful context to the logger.
func EndpointLogger(ctx context.Context) (*zap.Logger, *zap.SugaredLogger) {
	// An alternative here is to attach the logger to the context and
	// get it out that way.
	// In fact, ctx_zap has already done this for us, however the grpc zap
	// child logger adds an avalanche of information to the logger, and for
	// now it seems a bit much.
	fields := make([]zapcore.Field, 0)
	siteUUID := metautils.ExtractIncoming(ctx).Get("site_uuid")
	if siteUUID != "" {
		fields = append(fields, zap.String("site_uuid", siteUUID))
	}
	applianceUUID := metautils.ExtractIncoming(ctx).Get("appliance_uuid")
	if applianceUUID != "" {
		fields = append(fields, zap.String("appliance_uuid", applianceUUID))
	}
	pr, ok := peer.FromContext(ctx)
	if ok && pr != nil {
		fields = append(fields, zap.String("peer", pr.Addr.String()))
	}
	childLog := globalLog.With(fields...)
	return childLog, childLog.Sugar()
}

// SetGlobalLogTest allows override of globalLog so that test cases can set the
// logger to the test's logger.
func SetGlobalLogTest(logger *zap.Logger, slogger *zap.SugaredLogger) {
	globalLog = logger
	globalSugaredLog = slogger
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

