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
	"log"

	"bg/common"
)

type mergable interface {
	Init()
	AppendJSON([]byte) error
	ExportJSON() ([]byte, error)
}

type drops struct {
	data []common.DropArchive

	mergable
}

func (d *drops) Init() {
	d.data = make([]common.DropArchive, 0)
}

func (d *drops) AppendJSON(data []byte) error {
	var el []common.DropArchive

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
	data []common.Snapshot

	mergable
}

func (s *stats) Init() {
	s.data = make([]common.Snapshot, 0)
}

func (s *stats) AppendJSON(data []byte) error {
	var el []common.Snapshot

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

func writeMerged(ctype string, merged mergable) error {
	data, err := merged.ExportJSON()
	if err != nil {
		return fmt.Errorf("unable to marshal merged data: %v", err)
	}

	r := bytes.NewReader(data)
	return writeData(ctype, r)
}

func importSnapshots(objects []string, list mergable) error {
	list.Init()
	if *verbose {
		log.Printf("Fetching data\n")
	}
	for _, n := range objects {
		if *verbose {
			log.Printf("  fetching %s\n", n)
		}
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

	if ctype == common.StatContentType {
		var s stats

		if err = importSnapshots(objs, &s); err == nil {
			err = writeMerged(ctype, &s)
		}
	} else if ctype == common.DropContentType {
		var d drops

		if err = importSnapshots(objs, &d); err == nil {
			err = writeMerged(ctype, &d)
		}
	} else {
		err = fmt.Errorf("unsupported content type: %s", ctype)
	}

	return err
}
