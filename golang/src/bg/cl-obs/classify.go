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
	"database/sql"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/jmoiron/sqlx"
	"github.com/lytics/multibayes"
	"github.com/pkg/errors"
	"github.com/satori/uuid"
	"go.uber.org/zap/zapcore"
)

const (
	classifyUncertain = 0
	classifyCrossing  = 1
	classifyCertain   = 2
)

type classifyResult struct {
	ModelName      string
	Classification string
	Probability    float64
	NextProb       float64
	Region         int
	Unknown        bool
}

func convertPosteriorToResult(name string, certainAbove float64, uncertainBelow float64, posterior map[string]float64) classifyResult {
	var maxProb = -1.
	var maxClass string
	var nextProb = -1.

	for k, v := range posterior {
		if v > maxProb {
			nextProb = maxProb

			maxProb = v
			maxClass = k

			continue
		}

		if v > nextProb {
			nextProb = v
		}
	}

	region := classifyUncertain
	if maxProb > certainAbove {
		region = classifyCertain
	} else if maxProb > uncertainBelow {
		region = classifyCrossing
	}

	return classifyResult{
		ModelName:      name,
		Classification: maxClass,
		Probability:    maxProb,
		NextProb:       nextProb,
		Region:         region,
		Unknown:        false,
	}
}

func displayPredictResults(results []classifyResult) string {
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
		case classifyCertain:
			msg.WriteString(fmt.Sprintf("%s: %s (%s)", r.ModelName, color.GreenString(r.Classification), prob))
		case classifyCrossing:
			msg.WriteString(fmt.Sprintf("%s: %s (%s)", r.ModelName, color.YellowString(r.Classification), prob))
		default:
			msg.WriteString(fmt.Sprintf("%s: %s (%s)", r.ModelName, r.Classification, prob))
		}

	}

	return msg.String()
}

func affectedSitesFromInventory(rs []RecordedInventory) []string {
	siteUUIDs := make([]string, 0)

	for _, r := range rs {
		siteUUIDs = appendOnlyNew(siteUUIDs, r.SiteUUID)
	}

	return siteUUIDs
}

func combineSentenceFromInventory(rs []RecordedInventory) sentence {
	paragraph := newSentence()

	for _, r := range rs {
		paragraph.addSentence(newSentenceFromString(r.BayesSentence))
	}

	return paragraph
}

func updateOneClassification(db *sqlx.DB, siteUUID string, deviceMac string, newCl classifyResult) error {
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
		if newCl.Region != classifyCertain {
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
	case classifyCertain:
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
	case classifyCrossing:
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
func updateClassificationTable(db *sqlx.DB, siteUUID string, deviceMac string, results []classifyResult) {
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

var classifierCache = make(map[string]*multibayes.Classifier)

func classifySentence(B *backdrop, models []RecordedClassifier, mac string, sent sentence) []classifyResult {
	var err error
	results := make([]classifyResult, 0)

	for _, model := range models {
		switch model.ClassifierType {
		case "bayes":
			cl := classifierCache[model.ModelName]
			if cl == nil {
				cl, err = multibayes.NewClassifierFromJSON([]byte(model.ModelJSON))
				if err != nil {
					slog.Errorf("skipping '%s'; could not create classifier: %+v", model.ModelName, err)
					continue
				}
				cl.MinClassSize = model.MultibayesMin
				classifierCache[model.ModelName] = cl
			}

			// Run sentence through each classifier.
			post := cl.Posterior(sent.toString())
			spost := convertPosteriorToResult(model.ModelName,
				model.CertainAbove, model.UncertainBelow, post)

			results = append(results, spost)

		case "lookup":
			lo := initInterfaceMfgLookupClassifier()

			lresult, lprob := lo.classify(B, mac)

			lr := classifyResult{
				ModelName:      model.ModelName,
				Classification: lresult,
				Probability:    lprob,
				NextProb:       0.,
				Unknown:        false,
				Region:         classifyCertain,
			}

			results = append(results, lr)

		default:
			slog.Fatalf("Unknown classifier %s", model.ClassifierType)
		}
	}

	return results
}

func classifyMac(B *backdrop, models []RecordedClassifier, siteUUID string, mac string, persistent bool) (string, sentence) {
	records := []RecordedInventory{}
	err := B.db.Select(&records, `
		SELECT * FROM inventory
		WHERE device_mac = $1
		ORDER BY inventory_date DESC`, mac)
	if err != nil {
		slog.Errorf("select failed for %s: %+v", mac, err)
		return "-classify-fails-", newSentence()
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

	sent := combineSentenceFromInventory(filteredRecords)
	slog.Debugf("90 day sentence: %s", sent.toString())

	var siteUUIDs []string
	if siteUUID == "" {
		siteUUIDs = affectedSitesFromInventory(filteredRecords)
	} else {
		siteUUIDs = append(siteUUIDs, siteUUID)
	}

	results := classifySentence(B, models, mac, sent)

	if persistent {
		for _, s := range siteUUIDs {
			updateClassificationTable(B.db, s, mac, results)
		}
	}

	return displayPredictResults(results), sent
}

func classifySite(B *backdrop, models []RecordedClassifier, siteUUID string, persistent bool) error {
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
			fmt.Printf("\t%s\n", sentence.toString())
		}
	}

	return nil
}
