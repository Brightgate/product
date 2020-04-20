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
	"fmt"
	"io"
	"log"

	"github.com/hashicorp/go-hclog"

	gommonlog "github.com/labstack/gommon/log"
)

// This has to match the Logger interface from hashicorp/go-hclog.
type gommonHCLogAdapter struct {
	glog *gommonlog.Logger

	implied []interface{}
}

func hcLevelToGommon(level hclog.Level) gommonlog.Lvl {
	switch level {
	case hclog.Trace, hclog.Debug:
		return gommonlog.DEBUG
	case hclog.Info:
		return gommonlog.INFO
	case hclog.Warn:
		return gommonlog.WARN
	case hclog.Error:
		return gommonlog.ERROR
	}
	return gommonlog.OFF
}

func (l *gommonHCLogAdapter) Log(level hclog.Level, msg string, args ...interface{}) {
	var j gommonlog.JSON = make(map[string]interface{})
	j["message"] = msg
	args = append(l.implied, args...)
	for i := 0; i < len(args); i += 2 {
		switch k := args[i].(type) {
		case string:
			j[k] = args[i+1]
		default:
			j[fmt.Sprintf("%s", k)] = args[i+1]
		}
	}

	switch level {
	case hclog.Trace, hclog.Debug:
		l.glog.Debugj(j)
	case hclog.Info:
		l.glog.Infoj(j)
	case hclog.Warn:
		l.glog.Warnj(j)
	case hclog.Error:
		l.glog.Errorj(j)
	case hclog.NoLevel:
		l.glog.Printj(j)
	}
}

func (l *gommonHCLogAdapter) Trace(msg string, args ...interface{}) {
	l.Log(hclog.Trace, msg, args...)
}

func (l *gommonHCLogAdapter) Debug(msg string, args ...interface{}) {
	l.Log(hclog.Debug, msg, args...)
}

func (l *gommonHCLogAdapter) Info(msg string, args ...interface{}) {
	l.Log(hclog.Info, msg, args...)
}

func (l *gommonHCLogAdapter) Warn(msg string, args ...interface{}) {
	l.Log(hclog.Warn, msg, args...)
}

func (l *gommonHCLogAdapter) Error(msg string, args ...interface{}) {
	l.Log(hclog.Error, msg, args...)
}

func (l *gommonHCLogAdapter) IsTrace() bool {
	return l.IsDebug()
}

func (l *gommonHCLogAdapter) IsDebug() bool {
	return l.glog.Level() == gommonlog.DEBUG
}

func (l *gommonHCLogAdapter) IsInfo() bool {
	return l.glog.Level() <= gommonlog.INFO
}

func (l *gommonHCLogAdapter) IsWarn() bool {
	return l.glog.Level() <= gommonlog.WARN
}

func (l *gommonHCLogAdapter) IsError() bool {
	return l.glog.Level() <= gommonlog.ERROR
}

func (l *gommonHCLogAdapter) ImpliedArgs() []interface{} {
	return l.implied
}

func (l *gommonHCLogAdapter) With(args ...interface{}) hclog.Logger {
	var extra interface{}
	if len(args)%2 != 0 {
		extra = args[len(args)-1]
		args = args[:len(args)-1]
	}
	newLogger := *l
	newLogger.implied = append(newLogger.implied, args...)
	if extra != nil {
		newLogger.implied = append(newLogger.implied, hclog.MissingKey, extra)
	}

	return &newLogger
}

func (l *gommonHCLogAdapter) Name() string {
	return l.glog.Prefix()
}

func (l *gommonHCLogAdapter) Named(name string) hclog.Logger {
	oldPrefix := l.glog.Prefix()
	var prefix string
	if oldPrefix != "" {
		prefix = oldPrefix + "." + name
	} else {
		prefix = name
	}
	return GommonToHCLog(l.glog).Named(prefix)
}

func (l *gommonHCLogAdapter) ResetNamed(name string) hclog.Logger {
	return GommonToHCLog(l.glog).Named(name)
}

func (l *gommonHCLogAdapter) SetLevel(level hclog.Level) {
	l.glog.SetLevel(hcLevelToGommon(level))
}

func (l *gommonHCLogAdapter) StandardLogger(opts *hclog.StandardLoggerOptions) *log.Logger {
	if opts == nil {
		opts = &hclog.StandardLoggerOptions{}
	}
	return log.New(l.StandardWriter(opts), "", 0)
}

func (l *gommonHCLogAdapter) StandardWriter(opts *hclog.StandardLoggerOptions) io.Writer {
	// This would look a lot like go-hclog/stdlog.go
	panic("StandardWriter() not implemented for gommon adapter")
}

// GommonToHCLog returns a gommon-based logger (for labstack/echo) implementing
// the hclog.Logger interface.
func GommonToHCLog(glog *gommonlog.Logger) hclog.Logger {
	nlog := gommonlog.New("")
	nlog.SetLevel(glog.Level())
	return &gommonHCLogAdapter{
		glog: nlog,
	}
}
