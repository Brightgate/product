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
	_Visited       bool
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
		_Visited:       false,
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

func findResult(name string, results []classifyResult) (*classifyResult, error) {
	for _, r := range results {
		if r.ModelName == name {
			return &r, nil
		}
	}
	return nil, fmt.Errorf("result for model '%s' not found", name)
}

func updateClassificationTable(db *sqlx.DB, siteUUID string, deviceMac string, models []RecordedClassifier, results []classifyResult) {
	// Lookup our existing results in the classification table.
	var classifications []RecordedClassification
	err := db.Select(&classifications, "SELECT * FROM classification WHERE site_uuid = $1 AND mac = $2;",
		siteUUID, deviceMac)

	if err == sql.ErrNoRows {
		slog.Warnf("select classification shows no classifications for (%s, %s)",
			siteUUID, deviceMac)
		return
	} else if err != nil {
		slog.Warnf("select classification failed for (%s, %s): %v",
			siteUUID, deviceMac, err)
		return
	}

	t, err := db.Beginx()
	if err != nil {
		slog.Fatalf("no txn allowed: %v", err)
	}

	// Phase 1: updates.
	for _, rp := range classifications {
		result, err := findResult(rp.ModelName, results)
		if err != nil {
			slog.Warnf("no result matches classification model '%s' named in table", rp.ModelName)
			continue
		}

		result._Visited = true

		now := time.Now()

		switch result.Region {
		case classifyCertain:
			// If our result is the same, update the time
			// and the classification.  If our result is a
			// change, update the row.
			if rp.Classification == result.Classification {
				_, err = t.Exec(`UPDATE classification
				SET classification = $1, probability = $2, classification_updated = $3
				WHERE site_uuid = $4 AND mac = $5 AND model_name = $6;`,
					result.Classification, result.Probability, now,
					siteUUID, deviceMac, rp.ModelName)
				if err != nil {
					slog.Errorf("update classification failed: %v", err)
				}
			} else {
				slog.Infof("classifications differ '%s' != '%s'", rp.Classification, result.Classification)
				_, err = t.Exec(`UPDATE classification
				SET classification = $1, probability = $2, classification_created = $3, classification_updated = $4
				WHERE site_uuid = $5 AND mac = $6 AND model_name = $7;`,
					result.Classification, result.Probability, now, now,
					siteUUID, deviceMac, rp.ModelName)
				if err != nil {
					slog.Errorf("update classification failed: %v", err)
				}
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
			slog.Infof("remove/annul existing certain classification for %v", result)
			_, err = t.Exec(`DELETE FROM classification
				WHERE site_uuid = $1 AND mac = $2 AND model_name = $3;`,
				siteUUID, deviceMac, rp.ModelName)
			if err != nil {
				slog.Errorf("delete classification failed: %v", err)
			}
		}

	}

	err = t.Commit()
	if err != nil {
		slog.Fatalf("txn commit failed: %v", err)
	}

	// Phase 2: certain results that are not in the classification table.
	t, err = db.Beginx()
	if err != nil {
		slog.Fatalf("no txn allowed: %v", err)
	}

	for _, r := range results {
		if r._Visited {
			continue
		}

		if r.Region == classifyCertain {
			slog.Infof("insert new certain classification for %v", r)
			now := time.Now()

			_, err = t.Exec(`INSERT INTO classification (site_uuid,
					mac,
					model_name,
					classification,
					probability,
					classification_created,
					classification_updated)
				VALUES ($1, $2, $3, $4, $5, $6, $7);`,
				siteUUID, deviceMac, r.ModelName, r.Classification,
				r.Probability, now, now)
			if err != nil {
				slog.Errorf("insert classification failed: %v\n", err)
			}
		}
	}

	err = t.Commit()
	if err != nil {
		slog.Fatalf("txn commit failed: %v", err)
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
		ORDER BY inventory_date DESC
		LIMIT 12`, mac)
	if err != nil {
		slog.Errorf("select failed for %s: %+v", mac, err)
		return "-classify-fails-", newSentence()
	}

	sent := combineSentenceFromInventory(records)

	var siteUUIDs []string
	if siteUUID == "" {
		siteUUIDs = affectedSitesFromInventory(records)
	} else {
		siteUUIDs = append(siteUUIDs, siteUUID)
	}

	results := classifySentence(B, models, mac, sent)

	if persistent {
		for _, s := range siteUUIDs {
			updateClassificationTable(B.db, s, mac, models, results)
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
