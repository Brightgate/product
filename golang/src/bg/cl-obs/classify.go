/*
 * Copyright 2020 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package main

import (
	"database/sql"
	"fmt"
	"math"
	"net"
	"strings"
	"time"

	"bg/cl-obs/classifier"
	"bg/cl-obs/modeldb"
	"bg/cl-obs/sentence"

	"github.com/fatih/color"
	"github.com/jmoiron/sqlx"
	"github.com/pkg/errors"
	"github.com/satori/uuid"
	"go.uber.org/zap/zapcore"
)

func displayPredictResults(results []*classifier.ClassifyResult) string {
	var msg strings.Builder

	for _, r := range results {
		if msg.Len() > 0 {
			msg.WriteString("  ")
		}

		var prob string
		if r.Probability == 1.0 {
			prob = "1.0"
		} else {
			prob = fmt.Sprintf("%.2f", r.Probability)
		}
		switch r.Region {
		case classifier.ClassifyCertain:
			msg.WriteString(fmt.Sprintf("%s: %s (%s)", r.ModelName, color.GreenString(r.Classification), prob))
		case classifier.ClassifyCrossing:
			msg.WriteString(fmt.Sprintf("%s: %s (%s)", r.ModelName, color.YellowString(r.Classification), prob))
		default:
			msg.WriteString(fmt.Sprintf("%s: %s (%s)", r.ModelName, r.Classification, prob))
		}

	}

	return msg.String()
}

func affectedSitesFromInventory(rs []RecordedInventory) []string {
	siteUUMap := make(map[string]bool)

	for _, r := range rs {
		siteUUMap[r.SiteUUID] = true
	}
	siteUUIDs := make([]string, len(siteUUMap))
	i := 0
	for u := range siteUUMap {
		siteUUIDs[i] = u
		i++
	}
	return siteUUIDs
}

func updateOneClassification(db *sqlx.DB, siteUUID string, deviceMac string, newCl *classifier.ClassifyResult) error {
	// Lookup our existing results in the classification table.
	now := time.Now()

	var oldCl RecordedClassification
	err := db.Get(&oldCl, `
		SELECT *
		FROM classification
		WHERE site_uuid = $1 AND mac = $2 AND model_name = $3;`,
		siteUUID, deviceMac, newCl.ModelName)
	if err != nil && err != sql.ErrNoRows {
		return errors.Wrap(err, "select classification")
	}

	// If the classification isn't present, we can just add it, if it's
	// "certain"; else do nothing, as we don't record new uncertain results.
	if err == sql.ErrNoRows {
		if newCl.Region != classifier.ClassifyCertain {
			return nil
		}

		slog.Infof("new %s classification %q (%f)", newCl.ModelName, newCl.Classification, newCl.Probability)
		_, err = db.Exec(`
			INSERT INTO classification
			  (site_uuid, mac, model_name, classification,
			   probability, classification_created, classification_updated)
			VALUES ($1, $2, $3, $4, $5, $6, $7);`,
			siteUUID, deviceMac, newCl.ModelName, newCl.Classification,
			newCl.Probability, now, now)
		if err != nil {
			return errors.Wrap(err, "insert classification")
		}
		return nil
	}

	switch newCl.Region {
	case classifier.ClassifyCertain:
		var created time.Time

		// If nothing has changed, keep going; there's no need to touch
		// the database at all.
		pDelta := math.Abs(oldCl.Probability - newCl.Probability)
		if oldCl.Classification == newCl.Classification && pDelta < 0.001 {
			slog.Debugf("old and new classifications look the same")
			return nil
		}

		// If our classification result is different, then reset
		// the created date.  Otherwise, copy it from the
		// existing row.
		if newCl.Classification == oldCl.Classification {
			slog.Infof("update %s %s probability %f --> %f",
				newCl.ModelName, newCl.Classification,
				oldCl.Probability, newCl.Probability)
			created = oldCl.ClassificationCreated
		} else {
			created = now
			slog.Infof("update %s classification %q --> %q",
				newCl.ModelName, oldCl.Classification,
				newCl.Classification)
		}

		_, err = db.Exec(`
			UPDATE classification
			SET
			  classification = $1, probability = $2,
			  classification_created = $3, classification_updated = $4
			WHERE
			  site_uuid = $5 AND mac = $6 AND model_name = $7;`,
			newCl.Classification, newCl.Probability, created, now,
			siteUUID, deviceMac, newCl.ModelName)
		if err != nil {
			return errors.Wrap(err, "update classification")
		}
	case classifier.ClassifyCrossing:
		// Nothing to do.
	default:
		// If our result is below the threshold, delete the row (or
		// update to unknown).
		// XXX In the production version, we would expect there
		// to be a TRIGGER on the classification table, and one
		// or more agents using LISTEN/NOTIFY to handle
		// classification updates and deletions.
		slog.Infof("delete %s classification %q", newCl.ModelName, newCl.Classification)
		_, err = db.Exec(`DELETE FROM classification
			WHERE site_uuid = $1 AND mac = $2 AND model_name = $3;`,
			siteUUID, deviceMac, newCl.ModelName)
		if err != nil {
			return errors.Wrap(err, "delete classification")
		}
	}
	return nil
}

// updateClassificationTable walks the result set, updating or adding each
// classification in the classification table.  Finally it removes any
// stray classification entries corresponding to models outside of the result
// set.
func updateClassificationTable(db *sqlx.DB, siteUUID string, deviceMac string, results []*classifier.ClassifyResult) {
	var cleanupQ string
	for _, result := range results {
		err := updateOneClassification(db, siteUUID, deviceMac, result)
		if err != nil {
			slog.Errorf("failed updating classification %s %s %v: %v",
				siteUUID, deviceMac, result, err)
		}

		if len(cleanupQ) > 0 {
			cleanupQ += ", "
		}
		cleanupQ += "'" + result.ModelName + "'"
	}

	_, err := db.Exec(`DELETE FROM classification
		WHERE site_uuid = $1 AND mac = $2 AND model_name NOT IN (`+cleanupQ+`);`,
		siteUUID, deviceMac)
	if err != nil {
		slog.Fatalf("failed to cleanup: %v", err)
	}
}

func classifySentence(B *backdrop, mac string, sent sentence.Sentence) []*classifier.ClassifyResult {
	var err error
	results := make([]*classifier.ClassifyResult, 0)
	for _, c := range B.bayesClassifiers {
		res := c.Classify(sent)
		results = append(results, &res)
	}
	hwaddr, err := net.ParseMAC(mac)
	if err != nil {
		slog.Warnf("bad mac %s: %v", mac, err)
	} else {
		lookupRes := B.lookupMfgClassifier.Classify(hwaddr)
		results = append(results, &lookupRes)
	}
	return results
}

func classifyMac(B *backdrop, models []modeldb.RecordedClassifier, siteUUID string, mac string, persistent bool) (string, sentence.Sentence) {
	records := []RecordedInventory{}
	err := B.db.Select(&records, `
		SELECT * FROM inventory
		WHERE device_mac = $1
		ORDER BY inventory_date DESC`, mac)
	if err != nil {
		slog.Errorf("select failed for %s: %+v", mac, err)
		return "-classify-fails-", sentence.New()
	}

	filteredRecords := []RecordedInventory{}

	ninetyDaysAgo := time.Now().Add(-90 * 24 * time.Hour)
	for _, rec := range records {
		if len(filteredRecords) < 50 || rec.InventoryDate.After(ninetyDaysAgo) {
			filteredRecords = append(filteredRecords, rec)
		}
	}
	slog.Debugf("combined %d records from %v - %v to use in classification",
		len(filteredRecords),
		filteredRecords[len(filteredRecords)-1].InventoryDate,
		filteredRecords[0].InventoryDate)

	sent := sentence.New()
	for _, r := range filteredRecords {
		sent.AddString(r.BayesSentence)
	}
	slog.Debugf("combined sentence: %s", sent)

	var siteUUIDs []string
	if siteUUID == "" {
		siteUUIDs = affectedSitesFromInventory(filteredRecords)
	} else {
		siteUUIDs = append(siteUUIDs, siteUUID)
	}

	results := classifySentence(B, mac, sent)

	if persistent {
		for _, s := range siteUUIDs {
			updateClassificationTable(B.db, s, mac, results)
		}
	}

	return displayPredictResults(results), sent
}

func classifySite(B *backdrop, models []modeldb.RecordedClassifier, siteUUID string, persistent bool) error {
	_ = uuid.Must(uuid.FromString(siteUUID))

	var machines []string
	err := B.db.Select(&machines, `
		SELECT DISTINCT device_mac
		FROM inventory
		WHERE site_uuid = $1
		ORDER BY device_mac`, siteUUID)
	if err != nil {
		return errors.Wrap(err, "select site failed")
	}

	fmt.Printf("\nclassify %s; machines: %d\n", siteUUID, len(machines))

	for _, mac := range machines {
		desc, sentence := classifyMac(B, models, siteUUID, mac, persistent)
		fmt.Printf("    %s %s\n", mac, desc)
		if ce := log.Check(zapcore.DebugLevel, "debugging"); ce != nil {
			fmt.Printf("\t%s\n", sentence)
		}
	}

	return nil
}

