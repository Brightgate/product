/*
 * COPYRIGHT 2020 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package vaultgcpauth

import (
	"io"
	"log"

	"github.com/hashicorp/go-hclog"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type zapHCLogAdapter struct {
	zlog     *zap.SugaredLogger
	ancestor *zap.SugaredLogger
	name     string
	implied  []interface{}
}

func (l *zapHCLogAdapter) Log(level hclog.Level, msg string, args ...interface{}) {
	switch level {
	case hclog.NoLevel:
		l.Info(msg, args...)
	case hclog.Trace, hclog.Debug:
		l.Debug(msg, args...)
	case hclog.Info:
		l.Info(msg, args...)
	case hclog.Warn:
		l.Warn(msg, args...)
	case hclog.Error:
		l.Error(msg, args...)
	}
}

func (l *zapHCLogAdapter) Trace(msg string, args ...interface{}) {
	l.Debug(msg, args...)
}

func (l *zapHCLogAdapter) Debug(msg string, args ...interface{}) {
	l.zlog.Debugw(msg, args...)
}

func (l *zapHCLogAdapter) Info(msg string, args ...interface{}) {
	l.zlog.Infow(msg, args...)
}

func (l *zapHCLogAdapter) Warn(msg string, args ...interface{}) {
	l.zlog.Warnw(msg, args...)
}

func (l *zapHCLogAdapter) Error(msg string, args ...interface{}) {
	l.zlog.Errorw(msg, args...)
}

func (l *zapHCLogAdapter) IsTrace() bool {
	return l.IsDebug()
}

func (l *zapHCLogAdapter) IsDebug() bool {
	return l.zlog.Desugar().Core().Enabled(zapcore.DebugLevel)
}

func (l *zapHCLogAdapter) IsInfo() bool {
	return l.zlog.Desugar().Core().Enabled(zapcore.InfoLevel)
}

func (l *zapHCLogAdapter) IsWarn() bool {
	return l.zlog.Desugar().Core().Enabled(zapcore.WarnLevel)
}

func (l *zapHCLogAdapter) IsError() bool {
	return l.zlog.Desugar().Core().Enabled(zapcore.ErrorLevel)
}

func (l *zapHCLogAdapter) ImpliedArgs() []interface{} {
	return l.implied
}

func (l *zapHCLogAdapter) With(args ...interface{}) hclog.Logger {
	nl := *l
	nl.zlog = l.zlog.With(args...)
	nl.implied = append(l.implied, args)
	return &nl
}

func (l *zapHCLogAdapter) Name() string {
	return l.name
}

func (l *zapHCLogAdapter) Named(name string) hclog.Logger {
	nl := *l
	nl.zlog = l.zlog.Named(name)
	nl.name = l.name + "." + name
	return &nl
}

func (l *zapHCLogAdapter) ResetNamed(name string) hclog.Logger {
	nl := *l
	nl.zlog = l.ancestor.With(l.implied...).Named(name)
	nl.name = "name"
	return &nl
}

func (l *zapHCLogAdapter) SetLevel(level hclog.Level) {
	panic("SetLevel() not implemented for zap adapter")
}

func (l *zapHCLogAdapter) StandardLogger(opts *hclog.StandardLoggerOptions) *log.Logger {
	panic("StandardLogger() not implemented for zap adapter")
}

func (l *zapHCLogAdapter) StandardWriter(opts *hclog.StandardLoggerOptions) io.Writer {
	panic("StandardWriter() not implemented for zap adapter")
}

// ZapToHCLog returns a zap-based logger implementing the hclog.Logger
// interface.
func ZapToHCLog(zlog *zap.SugaredLogger) hclog.Logger {
	zlog = zlog.Desugar().WithOptions(zap.AddCallerSkip(1)).Sugar()
	return &zapHCLogAdapter{
		zlog:     zlog,
		ancestor: zlog,
	}
}
