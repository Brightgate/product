/*
 * COPYRIGHT 2019 Brightgate Inc. All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package faults

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"regexp"
	"time"
)

// Version indicates the version of the FaultReport structure found in a json
// file.
const Version = 1

var (
	// Build a regexp to match RFC3339Nano
	dateFmt  = `\d\d\d\d-\d\d-\d\d` // 2006-02-02
	timeFmt  = `\d\d:\d\d:\d\d.\d*` // 15:04:05.999999999
	tzFmt    = `(?:\d\d:\d\d)?`     // 07:00 (optional)
	stampFmt = dateFmt + `T` + timeFmt + `Z` + tzFmt

	// A fault file is named "type-timestamp[.state].json"
	nameRE = regexp.MustCompile(`([a-z]+)-(` + stampFmt + `)\.?(.*).json`)
)

// FaultReport contains all the information about a fault event
type FaultReport struct {
	FaultVersion int
	APVersion    string
	UUID         string
	Date         time.Time
	Appliance    string
	Daemon       string
	Kind         string

	Hardware *HardwareReport `json:"Hardware,omitempty"`
	Crash    *CrashReport    `json:"Crash,omitempty"`
	Mem      *MemReport      `json:"Mem,omitempty"`
	Error    *ErrorReport    `json:"Error,omitempty"`
}

// HardwareReport contains data about hardware-related errors
type HardwareReport struct {
	Node   string
	Device string
	Issue  string
}

// CrashReport contains the data specific to daemon crashes
type CrashReport struct {
	Reason string
	Log    string
}

// MemReport contains the data specific to daemon memory consumption issues.
type MemReport struct {
	MBytes  uint64
	Profile *string `json:"Profile,omitempty"`
}

// ErrorReport contains the data about internal errors
type ErrorReport struct {
	Msg   string
	Stack string
}

// ParseFileName takes the name of a fault file, and attempts to break it into
// its constituent components.
func ParseFileName(name string) (kind, state string, t time.Time, err error) {
	m := nameRE.FindStringSubmatch(name)
	if len(m) < 3 {
		err = fmt.Errorf("invalid fault name: %s", name)
	} else {
		kind = m[1]
		t, err = time.Parse(time.RFC3339Nano, m[2])
		if err != nil {
			err = fmt.Errorf("invalid timestamp in %s: %v",
				name, err)
		}
		if len(m) >= 4 {
			state = m[3]
		}
	}
	return
}

func buildPath(dir, kind string, when time.Time) string {
	name := kind + "-" + when.Format(time.RFC3339Nano) + ".json"
	return filepath.Join(dir, name)
}

// ReportPath returns the path to a FaultReport, constructing its filename from
// its serialized form.
func ReportPath(dir string, data []byte) (string, error) {
	var header struct {
		Date time.Time
		Kind string
	}

	err := json.Unmarshal(data, &header)
	if err != nil {
		return "", fmt.Errorf("unmarshaling fault report: %v", err)
	}

	return buildPath(dir, header.Kind, header.Date), nil
}

// WriteReport attempts to store a FaultReport as a file in the provided
// directory.  It uses the data in the report to construct the file name.
func WriteReport(dir string, report *FaultReport) (string, error) {
	json, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshaling report: %v", err)
	}

	path := buildPath(dir, report.Kind, report.Date)
	return path, ioutil.WriteFile(path, json, 0644)
}

// WriteReportSerialized attempts to store a serialized FaultReport as a file in the
// provided directory.  It deserializes enough of the report to construct the
// filename, but doesn't attempt to parse fault-specific details.  This allows
// us to deploy new fault types on the client without making synchronized
// changes in the cloud.
func WriteReportSerialized(dir string, data []byte) (string, error) {
	path, err := ReportPath(dir, data)
	if err != nil {
		return "", err
	}

	return path, ioutil.WriteFile(path, data, 0644)
}
