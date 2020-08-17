/*
 * Copyright 2019 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package main

import (
	"bytes"
	"encoding/gob"
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
// Both input and output datastructures must be represented in JSON or GOB.
type mergeable interface {
	Init()
	AppendData(string, []byte) error
	ExportJSON() ([]byte, error)
	ExportBinary() ([]byte, error)
}

type drops struct {
	data []archive.DropArchive

	mergeable
}

func (d *drops) Init() {
	d.data = make([]archive.DropArchive, 0)
}

func (d *drops) AppendData(ctype string, data []byte) error {
	var err error
	var el []archive.DropArchive

	if ctype == archive.DropContentType {
		err = json.Unmarshal(data, &el)
	} else {
		in := bytes.NewBuffer(data)
		dec := gob.NewDecoder(in)
		err = dec.Decode(&el)
	}

	if err != nil {
		err = fmt.Errorf("parse failure: %v", err)
	} else {
		d.data = append(d.data, el...)
	}
	return err
}

func (d *drops) ExportBinary() ([]byte, error) {
	buf := new(bytes.Buffer)
	enc := gob.NewEncoder(buf)
	err := enc.Encode(d.data)
	return buf.Bytes(), err
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

func (s *stats) AppendData(ctype string, data []byte) error {
	var err error
	var el []archive.Snapshot

	if ctype == archive.StatContentType {
		err = json.Unmarshal(data, &el)
	} else {
		in := bytes.NewBuffer(data)
		dec := gob.NewDecoder(in)
		err = dec.Decode(&el)
	}

	if err != nil {
		err = fmt.Errorf("parse failure: %v", err)
	} else {
		s.data = append(s.data, el...)
	}
	return err
}

func (s *stats) ExportBinary() ([]byte, error) {
	buf := new(bytes.Buffer)
	enc := gob.NewEncoder(buf)
	err := enc.Encode(s.data)
	return buf.Bytes(), err
}

func (s *stats) ExportJSON() ([]byte, error) {
	return json.Marshal(s.data)
}

func writeMerged(ctype string, merged mergeable) error {
	var data []byte
	var err error

	if *binFlag {
		data, err = merged.ExportBinary()
	} else {
		data, err = merged.ExportJSON()
	}
	if err != nil {
		return fmt.Errorf("unable to marshal merged data: %v", err)
	}

	r := bytes.NewReader(data)
	return writeData(ctype, r)
}

func importSnapshots(src string, objects []object, list mergeable) error {
	list.Init()
	slog.Debugf("Fetching data")
	for _, n := range objects {
		slog.Debugf("  fetching %s", n)
		data, err := readData(n.name)
		if err != nil {
			return fmt.Errorf("fetching %s/%s: %v", src, n.name, err)
		}

		if err = list.AppendData(n.ctype, data); err != nil {
			return fmt.Errorf("parsing %s/%s: %v", src, n.name, err)
		}
		data = nil
	}

	return nil
}

func merge() error {
	var outtype string

	src, objs, err := getObjects()
	if err != nil {
		return fmt.Errorf("getting object list: %v", err)
	}

	if len(objs) == 0 {
		return nil
	}

	ctype := objs[0].ctype
	switch typeFamily[ctype] {
	case "stat":
		var s stats

		if *binFlag {
			outtype = archive.StatBinaryType
		} else {
			outtype = archive.StatContentType
		}
		if err = importSnapshots(src, objs, &s); err == nil {
			err = writeMerged(outtype, &s)
		}
	case "drop":
		var d drops

		if *binFlag {
			outtype = archive.DropBinaryType
		} else {
			outtype = archive.DropContentType
		}
		if err = importSnapshots(src, objs, &d); err == nil {
			err = writeMerged(outtype, &d)
		}
	default:
		err = fmt.Errorf("unsupported content type: %s", ctype)
	}

	return err
}

