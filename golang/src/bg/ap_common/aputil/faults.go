/*
 * Copyright 2019 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package aputil

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"runtime"
	"runtime/debug"
	"runtime/pprof"
	"strconv"
	"syscall"
	"time"

	"bg/ap_common/platform"
	"bg/common"
	"bg/common/faults"

	"github.com/satori/uuid"
	"go.uber.org/zap"
)

// MemFaultWarnMB and MemFaultKillMB are the environment variables used by
// ap.mcp to notify a child daemon about the memory limits it is expected to
// stay within.
const (
	MemFaultWarnMB = "BG_MEMFAULTWARN"
	MemFaultKillMB = "BG_MEMFAULTKILL"
)

var (
	self      string
	nodeID    string
	reportDir string
	slog      *zap.SugaredLogger
)

// memWatch periodically checks the memory consumption of this process, and
// files a fault report with supporting information when the given limit is
// exceeded.
func memWatch() {
	const checkPeriod = 5 * time.Second // Check memory every few seconds
	const faultPeriod = 24 * time.Hour  // Report no more than once per day
	const activeProfileRate = 512

	var r syscall.Rusage
	var nextFault time.Time

	warn, _ := strconv.ParseUint(os.Getenv(MemFaultWarnMB), 10, 64)
	kill, _ := strconv.ParseUint(os.Getenv(MemFaultKillMB), 10, 64)

	// Report a fault when we're half way between the Warn and Kill stages
	// of memory overconsumption.
	fault := (warn + kill) / 2
	if fault == 0 {
		return
	}

	ticker := time.NewTicker(checkPeriod)
	defer ticker.Stop()

	baseProfileRate := runtime.MemProfileRate
	for {
		_ = syscall.Getrusage(syscall.RUSAGE_SELF, &r)
		used := uint64(r.Maxrss / 1024)

		if used > fault && time.Now().After(nextFault) {
			var profile *string

			w := new(bytes.Buffer)
			runtime.GC()
			if err := pprof.WriteHeapProfile(w); err == nil {
				p := hex.EncodeToString(w.Bytes())
				profile = &p
			}

			err := ReportMem(self, used, profile)
			if err != nil {
				log.Printf("failed to report fault: %v\n", err)
			}
			nextFault = time.Now().Add(faultPeriod)
		}

		// If we're above the Warn limit, increase the memory profile
		// rate to get better info in any reported fault.  If we drop
		// back below the Warn limit, return to the normal profile rate.
		if used > warn && runtime.MemProfileRate == baseProfileRate {
			runtime.MemProfileRate = activeProfileRate

		} else if used < warn && runtime.MemProfileRate != baseProfileRate {
			runtime.MemProfileRate = baseProfileRate
		}
		<-ticker.C
	}
}
func newReport(daemon, kind string) *faults.FaultReport {
	r := &faults.FaultReport{
		FaultVersion: faults.Version,
		APVersion:    common.GitVersion,
		UUID:         uuid.NewV4().String(),
		Date:         time.Now(),
		Appliance:    nodeID,
		Daemon:       daemon,
		Kind:         kind,
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

// ReportHardware is used to report a hardware issue
func ReportHardware(device, issue string) error {
	report := newReport(self, "hardware")
	report.Hardware = &faults.HardwareReport{
		Node:   nodeID,
		Device: device,
		Issue:  issue,
	}

	return writeReport(report)
}

// ReportMem is used to report that a daemon is consuming an unexpectedly large
// amount of memory.
func ReportMem(daemon string, mb uint64, profile *string) error {
	report := newReport(daemon, "mem")
	report.Mem = &faults.MemReport{
		MBytes:  mb,
		Profile: profile,
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

	go memWatch()
}

