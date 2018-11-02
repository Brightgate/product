package aputil

import (
	"flag"
	"log"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	levelFlag = zapcore.InfoLevel
)

func zapTimeEncoder(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
	enc.AppendString(t.Format("2006/01/02 15:04:05"))
}

// newChildLogger returns a 'sugared' zap logger, intended to be used to log the
// output from child daemons.  This logger differs from that returned by
// ChildLogger by omitting the caller name, allowing us to tag the output using
// the name of the child instead.  e.g.:
//	2018/11/02 12:51:46     INFO    hostapd: wlan1: AP-ENABLED
func newChildLogger() (*zap.SugaredLogger, error) {
	var slogger *zap.SugaredLogger

	zapConfig := zap.NewDevelopmentConfig()
	zapConfig.Level = zap.NewAtomicLevelAt(levelFlag)
	zapConfig.DisableStacktrace = true
	zapConfig.DisableCaller = true
	zapConfig.EncoderConfig.EncodeTime = zapTimeEncoder

	logger, err := zapConfig.Build()
	if err == nil {
		slogger = logger.Sugar()
	}

	return slogger, err
}

// NewLogger returns a 'sugared' zap logger.  Each logged line will include a
// timestamp, the log level, and 2 levels of caller name before the message.
// e.g.:
//	2018/11/02 10:23:27     INFO    ap.dns4d/dns4d.go:821   Adding ...
func NewLogger() *zap.SugaredLogger {
	zapConfig := zap.NewDevelopmentConfig()
	zapConfig.Level = zap.NewAtomicLevelAt(levelFlag)
	zapConfig.DisableStacktrace = true
	zapConfig.EncoderConfig.EncodeTime = zapTimeEncoder

	logger, err := zapConfig.Build()
	if err != nil {
		log.Panicf("can't zap: %s", err)
	}
	_ = zap.RedirectStdLog(logger)

	return logger.Sugar()
}

func init() {
	flag.Var(&levelFlag, "log-level",
		"Log level [debug,info,warn,error,panic,fatal]")
}
