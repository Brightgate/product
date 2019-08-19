/*
 * COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package aputil

import (
	"fmt"
	"log"
	"os"
	"runtime/debug"
	"time"

	"bg/ap_common/platform"
	"bg/common/faults"

	"github.com/satori/uuid"
	"go.uber.org/zap"
)

var (
	self      string
	nodeID    string
	reportDir string
	slog      *zap.SugaredLogger
)

func newReport(daemon, kind string) *faults.FaultReport {
	r := &faults.FaultReport{
		Version:   faults.Version,
		UUID:      uuid.NewV4().String(),
		Date:      time.Now(),
		Appliance: nodeID,
		Daemon:    daemon,
		Kind:      kind,
	}

	return r
}

func writeReport(report *faults.FaultReport) error {
	_, err := faults.WriteReport(reportDir, report)

	switch {
	case slog == nil && err == nil:
		log.Printf("\tINFO\tgenerated FaultReport %s", report.UUID)
	case slog != nil && err == nil:
		slog.Infof("generated FaultReport %s", report.UUID)
	case slog == nil && err != nil:
		log.Printf("\tERROR\twriting FaultReport: %v", err)
	case slog != nil && err != nil:
		slog.Errorf("writing FaultReport: %v", err)
	}

	return err
}

// ReportMem is used to report that a daemon is consuming an unexpectedly large
// amount of memory.
func ReportMem(daemon string, mb uint64) error {
	report := newReport(daemon, "mem")
	report.Mem = &faults.MemReport{
		MBytes: mb,
	}

	return writeReport(report)
}

// ReportCrash is used to report that a daemon has exited unexpectedly.  The
// crash includes the latest log messages from the daemon, to help root cause
// the reason for the crash.  In most cases, this log data will include the
// stack traces.
func ReportCrash(daemon, reason, log string) error {
	report := newReport(daemon, "crash")
	report.Crash = &faults.CrashReport{
		Reason: reason,
		Log:    log,
	}

	return writeReport(report)
}

// ReportError is used to report a variety of errors.  This should not be used
// to report administrative or transient errors - it should be used to report
// "should not happen" errors which do not quite rise to the level of a Fatal()
// severity.
func ReportError(format string, v ...interface{}) error {
	if slog == nil {
		log.Printf("ERROR\t"+format, v)
	} else {
		slog.Errorf(format, v)
	}

	report := newReport(self, "error")
	report.Error = &faults.ErrorReport{
		Msg:   fmt.Sprintf(format, v...),
		Stack: string(debug.Stack()),
	}

	return writeReport(report)
}

// ReportInit is used to set some common values required by the individual fault
// reporting routines.  It must be called before reporting any faults.
func ReportInit(zaplog *zap.SugaredLogger, name string) {
	self = name

	// Use the provided log facility, but change the reporting depth to omit
	// the Report routine
	if zaplog != nil {
		slog = zaplog.Desugar().WithOptions(zap.AddCallerSkip(1)).Sugar()
	}

	plat := platform.NewPlatform()
	nodeID, _ = plat.GetNodeID()

	// Need 0777 because some daemons run as non-root
	reportDir = plat.ExpandDirPath("__APDATA__", "faults")
	os.MkdirAll(reportDir, 0777)
}
