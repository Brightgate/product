//
// COPYRIGHT 2020 Brightgate Inc.  All rights reserved.
//
// This copyright notice is Copyright Management Information under 17 USC 1202
// and is included to protect this work and deter copyright infringement.
// Removal or alteration of this Copyright Management Information without the
// express written permission of Brightgate Inc is prohibited, and any
// such unauthorized removal or alteration will be a violation of federal law.
//

package main

import (
	"fmt"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

// reviewSub is a function to assist in maintaining the cleanliness of
// the training data and to report on the characteristics of the trained
// classfiers.
//
// The training data consists of both the devices table, which contains
// the classifications made by the supervisor, and the training table,
// which is the set of meaningful deviceInfo objects associated with
// each member of the devices table.
func reviewSub(cmd *cobra.Command, args []string) error {
	if _B.ingester == nil {
		return errors.Errorf("You must provide --dir or --project")
	}

	rows, err := _B.db.Queryx("SELECT * FROM device;")
	if err != nil {
		return errors.Wrap(err, "select device failed")
	}

	dvcs := make(map[string]int)
	dds := make(map[string]*RecordedDevice)

	for rows.Next() {
		var dd RecordedDevice

		err = rows.StructScan(&dd)
		if err != nil {
			slog.Infof("training scan failed: %v\n", err)
			continue
		}

		dvcs[dd.DeviceMAC] = 0
		dds[dd.DeviceMAC] = &dd
	}

	rows, err = _B.db.Queryx("SELECT * FROM training ORDER BY device_mac;")
	if err != nil {
		return errors.Wrap(err, "select training failed")
	}

	rowCount := 0
	validCount := 0
	redundCount := 0

	var devicemac string
	devicesent := newSentence()
	missed := make([]RecordedTraining, 0)

	for rows.Next() {
		var dt RecordedTraining

		err = rows.StructScan(&dt)
		if err != nil {
			slog.Infof("training scan failed: %v\n", err)
			continue
		}

		rowCount++
		dvcs[dt.DeviceMAC]++

		if dgi, ok := dds[dt.DeviceMAC]; ok {
			if dgi.DGroupID != dt.DGroupID {
				slog.Infof("DGroupID mismatch for %s (device %d vs training %d): %v", dt.DeviceMAC, dgi.DGroupID, dt.DGroupID, dt)
			}
		}

		dtr, rerr := readerFromTraining(&_B, dt)
		if rerr != nil {
			missed = append(missed, dt)
			continue
		}

		validCount++

		_, sent := genBayesSentenceFromReader(_B.ouidb, dtr)
		if dt.DeviceMAC == devicemac {
			dupe := devicesent.addSentence(sent)
			if dupe {
				slog.Infof("no new information in (%s, %s, %s)", dt.SiteUUID, dt.DeviceMAC, dt.UnixTimestamp)
				redundCount++
			}
		} else {
			devicesent = sent
			devicemac = dt.DeviceMAC
		}
	}

	// Review missing sites, objects/files.
	for _, dt := range missed {
		se, _ := _B.ingester.SiteExists(&_B, dt.SiteUUID)
		if !se {
			slog.Infof("training entry refers to non-existent site %s", dt.SiteUUID)
		}

		slog.Infof("missing information for (%s, %s, %s)", dt.SiteUUID, dt.DeviceMAC, dt.UnixTimestamp)
	}

	// Review classified devices with no training data.
	missingData := 0
	for k, v := range dvcs {
		if v == 0 {
			slog.Infof("device entry %s has no training data: %v", k, dds[k])
			missingData++
		}
	}

	slog.Infof("device table has %d/%d dataless rows (%f)", missingData, len(dvcs), float32(missingData)/float32(len(dvcs)))
	slog.Infof("training table has %d/%d valid rows (%f)", validCount, rowCount, float32(validCount)/float32(rowCount))
	slog.Infof("training table has %d/%d redundant rows (%f)", redundCount, rowCount, float32(redundCount)/float32(rowCount))

	if !_B.modelsLoaded {
		return fmt.Errorf("model database does not exist")
	}

	// Model review
	models := []RecordedClassifier{}
	err = _B.modeldb.Select(&models, "SELECT * FROM model ORDER BY name ASC")
	if err != nil {
		return fmt.Errorf("model select failed: %+v", err)
	}

	slog.Infof("models: %d", len(models))

	for _, m := range models {
		switch m.ClassifierType {
		case "bayes":
			fmt.Println(reviewBayes(m))
		case "lookup":
			fmt.Printf("Lookup Classifier, Name: %s\n", m.ModelName)
		}
	}

	return nil
}
