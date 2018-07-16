/*
 * COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package main

import (
	"bytes"
	"encoding/json"
	"fmt"

	"bg/common/archive"
)

// A datastructure is mergeable if multiple instances can be combined into a
// single instance of the same datastructure.  The nature of the combination is
// implementation dependent, but reasonable examples would be
//
// union: merge([0, 1, 3], [0, 2, 4], [0, 2, 3]) -> [0, 1, 2, 3]
// intersection: merge([0, 1, 3], [0, 2, 4], [0, 2, 3]) -> [0]
// concatenation: merge([0, 1, 3], [0, 2, 4], [0, 2, 3]) ->
//                                          [0, 1, 3, 0, 2, 4, 0, 2, 3]
//
// Both input and output datastructures must be represented in JSON.
type mergeable interface {
	Init()
	AppendJSON([]byte) error
	ExportJSON() ([]byte, error)
}

type drops struct {
	data []archive.DropArchive

	mergeable
}

func (d *drops) Init() {
	d.data = make([]archive.DropArchive, 0)
}

func (d *drops) AppendJSON(data []byte) error {
	var el []archive.DropArchive

	err := json.Unmarshal(data, &el)
	if err != nil {
		err = fmt.Errorf("parse failure: %v", err)
	} else {
		d.data = append(d.data, el...)
	}
	return err
}

func (d *drops) ExportJSON() ([]byte, error) {
	return json.Marshal(d.data)
}

type stats struct {
	data []archive.Snapshot

	mergeable
}

func (s *stats) Init() {
	s.data = make([]archive.Snapshot, 0)
}

func (s *stats) AppendJSON(data []byte) error {
	var el []archive.Snapshot

	err := json.Unmarshal(data, &el)
	if err != nil {
		err = fmt.Errorf("parse failure: %v", err)
	} else {
		s.data = append(s.data, el...)
	}
	return err
}

func (s *stats) ExportJSON() ([]byte, error) {
	return json.Marshal(s.data)
}

func writeMerged(ctype string, merged mergeable) error {
	data, err := merged.ExportJSON()
	if err != nil {
		return fmt.Errorf("unable to marshal merged data: %v", err)
	}

	r := bytes.NewReader(data)
	return writeData(ctype, r)
}

func importSnapshots(objects []string, list mergeable) error {
	list.Init()
	slog.Debugf("Fetching data\n")
	for _, n := range objects {
		slog.Debugf("  fetching %s\n", n)
		data, err := readData(n)
		if err != nil {
			return fmt.Errorf("failed to fetch %s: %v", n, err)
		}

		if err = list.AppendJSON(data); err != nil {
			return fmt.Errorf("parse failure: %v", err)
		}
	}

	return nil
}

func merge() error {
	objs, ctype, err := getObjects()
	if err != nil {
		return fmt.Errorf("failed to get object list: %v", err)
	}

	if ctype == archive.StatContentType {
		var s stats

		if err = importSnapshots(objs, &s); err == nil {
			err = writeMerged(ctype, &s)
		}
	} else if ctype == archive.DropContentType {
		var d drops

		if err = importSnapshots(objs, &d); err == nil {
			err = writeMerged(ctype, &d)
		}
	} else {
		err = fmt.Errorf("unsupported content type: %s", ctype)
	}

	return err
}
