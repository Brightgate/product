//
// COPYRIGHT 2020 Brightgate Inc.  All rights reserved.
//
// This copyright notice is Copyright Management Information under 17 USC 1202
// and is included to protect this work and deter copyright infringement.
// Removal or alteration of this Copyright Management Information without the
// express written permission of Brightgate Inc is prohibited, and any
// such unauthorized removal or alteration will be a violation of federal law.
//

package zapgommon

import (
	"bytes"
	"io"

	"go.uber.org/zap"

	"github.com/labstack/echo"
	gommonlog "github.com/labstack/gommon/log"
)

type logger struct {
	zlog     *zap.SugaredLogger
	ancestor *zap.SugaredLogger
	prefix   string
}

// loggerWriter replicates a similar private struct in zap, allowing code to
// call logger.Output() and get a Writer that dumps its bytes into the zap
// logstream.
type loggerWriter struct {
	logFunc func(...interface{})
}

func (l *loggerWriter) Write(p []byte) (int, error) {
	p = bytes.TrimSpace(p)
	l.logFunc(string(p))
	return len(p), nil
}

func (l *logger) Output() io.Writer {
	// We need to skip two frames, which involves sugaring and desugaring.
	zl := l.ancestor.Desugar().WithOptions(zap.AddCallerSkip(2)).Sugar()
	return &loggerWriter{zl.Info}
}

func (l *logger) SetOutput(w io.Writer) {
	panic("SetOutput() not implemented for zap adapter")
}

func (l *logger) Prefix() string {
	return l.prefix
}

// This really just appends the argument to the existing logger name.
func (l *logger) SetPrefix(p string) {
	l.prefix = p
	l.zlog = l.ancestor.Named(p)
}

func (l *logger) Level() gommonlog.Lvl {
	panic("Level() not implemented for zap adapter")
}

func (l *logger) SetLevel(v gommonlog.Lvl) {
	// Well, we do actually use this ...
	// panic("SetLevel() not implemented for zap adapter")
}

func (l *logger) SetHeader(h string) {
	panic("SetHeader() not implemented for zap adapter")
}

func jsonToSlice(j gommonlog.JSON) []interface{} {
	args := make([]interface{}, 0, 2*len(j))
	for k, v := range j {
		args = append(args, k, v)
	}
	return args
}

// zap doesn't allow for an un-leveled log entry; just map these to "Info".
func (l *logger) Print(i ...interface{}) {
	l.Info(i...)
}

func (l *logger) Printf(format string, args ...interface{}) {
	l.Infof(format, args...)
}

func (l *logger) Printj(j gommonlog.JSON) {
	l.Infoj(j)
}

func (l *logger) Debug(i ...interface{}) {
	l.zlog.Debug(i...)
}

func (l *logger) Debugf(format string, args ...interface{}) {
	l.zlog.Debugf(format, args...)
}

func (l *logger) Debugj(j gommonlog.JSON) {
	args := jsonToSlice(j)
	// zap requires a "msg" parameter, which gets encoded in the JSON output
	// as a key/value pair with the key "msg".  In the *j() functions,
	// gommon emits only the key/value pairs it's given, but the other two
	// put all their args into a "message" key.
	l.zlog.Debugw("", args...)
}

func (l *logger) Info(i ...interface{}) {
	l.zlog.Info(i...)
}

func (l *logger) Infof(format string, args ...interface{}) {
	l.zlog.Infof(format, args...)
}

func (l *logger) Infoj(j gommonlog.JSON) {
	args := jsonToSlice(j)
	l.zlog.Infow("", args...)
}

func (l *logger) Warn(i ...interface{}) {
	l.zlog.Warn(i...)
}

func (l *logger) Warnf(format string, args ...interface{}) {
	l.zlog.Warnf(format, args...)
}

func (l *logger) Warnj(j gommonlog.JSON) {
	args := jsonToSlice(j)
	l.zlog.Warnw("", args...)
}

func (l *logger) Error(i ...interface{}) {
	l.zlog.Error(i...)
}

func (l *logger) Errorf(format string, args ...interface{}) {
	l.zlog.Errorf(format, args...)
}

func (l *logger) Errorj(j gommonlog.JSON) {
	args := jsonToSlice(j)
	l.zlog.Errorw("", args...)
}

func (l *logger) Fatal(i ...interface{}) {
	l.zlog.Fatal(i...)
}

func (l *logger) Fatalf(format string, args ...interface{}) {
	l.zlog.Fatalf(format, args...)
}

func (l *logger) Fatalj(j gommonlog.JSON) {
	args := jsonToSlice(j)
	l.zlog.Fatalw("", args...)
}

func (l *logger) Panic(i ...interface{}) {
	l.zlog.Panic(i...)
}

func (l *logger) Panicf(format string, args ...interface{}) {
	l.zlog.Fatalf(format, args...)
}

func (l *logger) Panicj(j gommonlog.JSON) {
	args := jsonToSlice(j)
	l.zlog.Fatalw("", args...)
}

// ZapToGommonLog returns a zap-based logger implementing the echo.Logger
// interface.
func ZapToGommonLog(zlog *zap.Logger) echo.Logger {
	slog := zlog.WithOptions(zap.AddCallerSkip(1)).Sugar()
	return &logger{
		zlog:     slog,
		ancestor: slog,
	}
}
