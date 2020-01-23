//
// COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
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
	"log"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/lytics/multibayes"
	"github.com/pkg/errors"
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
			msg.WriteString(" ")
		}

		switch r.Region {
		case classifyCertain:
			msg.WriteString(color.GreenString("%s: %s (%f)", r.ModelName, r.Classification, r.Probability))
		case classifyCrossing:
			msg.WriteString(color.YellowString("%s: %s (%f)", r.ModelName, r.Classification, r.Probability))
		default:
			msg.WriteString(fmt.Sprintf("%s: %s (%f)", r.ModelName, r.Classification, r.Probability))
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

func updateClassificationTable(B *backdrop, siteUUID string, deviceMac string, models []RecordedClassifier, results []classifyResult) {
	// Lookup our existing results in the classification table.
	rows, err := B.db.Queryx("SELECT * FROM classification WHERE site_uuid = $1 AND mac = $2;",
		siteUUID, deviceMac)

	if err == sql.ErrNoRows {
		log.Printf("select classification shows no classifications for (%s, %s)",
			siteUUID, deviceMac)
		return
	} else if err != nil {
		log.Printf("select classification failed for (%s, %s): %v",
			siteUUID, deviceMac, err)
		return
	}

	defer rows.Close()

	// With the SQLite3 backend, we use a transaction to bundle
	// filesystem operations into larger groups for performance
	// reasons.  This transaction may be superfluous with other
	// backends.
	t, _ := B.db.Beginx()

	// Phase 1: updates.
	for rows.Next() {
		rp := RecordedClassification{}

		err = rows.StructScan(&rp)
		if err != nil {
			log.Printf("struct scan classification failed: %v", err)
			continue
		}

		result, err := findResult(rp.ModelName, results)
		if err != nil {
			log.Printf("no result matches classification model '%s' named in table", rp.ModelName)
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
					log.Printf("update classification failed: %v", err)
				}
			} else {
				log.Printf("classifications differ '%s' != '%s'", rp.Classification, result.Classification)
				_, err = t.Exec(`UPDATE classification
				SET classification = $1, probability = $2, classification_created = $3, classification_updated = $4
				WHERE site_uuid = $5 AND mac = $6 AND model_name = $7;`,
					result.Classification, result.Probability, now, now,
					siteUUID, deviceMac, rp.ModelName)
				if err != nil {
					log.Printf("update classification failed: %v", err)
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
			log.Printf("remove/annul existing certain classification for %v", result)
			_, err = t.Exec(`DELETE FROM classification
				WHERE site_uuid = $1 AND mac = $2 AND model_name = $3;`,
				siteUUID, deviceMac, rp.ModelName)
			if err != nil {
				log.Printf("delete classification failed: %v", err)
			}
		}

	}

	rows.Close()

	err = t.Commit()
	if err != nil {
		log.Printf("txn commit failed: %v", err)
	}

	// Phase 2: certain results that are not in the classification table.
	t, err = B.db.Beginx()
	if err != nil {
		log.Printf("no txn allowed: %v", err)
	}

	for _, r := range results {
		if r._Visited {
			continue
		}

		if r.Region == classifyCertain {
			log.Printf("insert new certain classification for %v", r)
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
				log.Printf("insert classification failed: %v\n", err)
			}
		}
	}

	err = t.Commit()
	if err != nil {
		log.Printf("txn commit failed: %v", err)
	}
}

func classifySentence(B *backdrop, models []RecordedClassifier, mac string, sent sentence) []classifyResult {
	results := make([]classifyResult, 0)

	for _, model := range models {
		switch model.ClassifierType {
		case "bayes":
			cl, err := multibayes.NewClassifierFromJSON([]byte(model.ModelJSON))
			if err != nil {
				log.Printf("skipping '%s'; could not create classifier: %+v", model.ModelName, err)
				continue
			}

			cl.MinClassSize = model.MultibayesMin

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
			log.Fatalf("Unknown classifier %s", model.ClassifierType)
		}
	}

	return results
}

func classifyMac(B *backdrop, models []RecordedClassifier, siteUUID string, mac string, persistent bool) string {
	records := []RecordedInventory{}
	err := B.db.Select(&records, "SELECT * FROM inventory WHERE device_mac = $1 ORDER BY inventory_date DESC LIMIT 12", mac)
	if err != nil {
		log.Printf("select failed for %s: %+v", mac, err)
		return "-classify-fails-"
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
			updateClassificationTable(B, s, mac, models, results)
		}
	}

	return displayPredictResults(results) + "\n\t" + sent.toString()
}

type siteMachine struct {
	SiteUUID  string
	DeviceMAC string
}

func classifySite(B *backdrop, models []RecordedClassifier, uuid string, persistent bool) error {
	var rows *sql.Rows
	var err error

	log.Printf("classify site %s", uuid)

	rows, err = B.db.Query("SELECT DISTINCT site.site_uuid, inventory.device_mac FROM site, inventory WHERE site.site_uuid = inventory.site_uuid AND ( site.site_uuid GLOB $1 OR site.site_name GLOB $1) ORDER BY inventory.inventory_date ASC;", uuid)
	if err != nil {
		return errors.Wrap(err, "select site failed")
	}

	defer rows.Close()

	machines := make([]siteMachine, 0)

	for rows.Next() {
		rsm := siteMachine{}

		err = rows.Scan(&rsm.SiteUUID, &rsm.DeviceMAC)
		if err != nil {
			log.Printf("site, inventory scan failed: %v\n", err)
			continue
		}

		machines = append(machines, rsm)
	}

	log.Printf("machines: %d", len(machines))

	for _, m := range machines {
		fmt.Printf("    %s %s\n", m.DeviceMAC,
			classifyMac(B, models, m.SiteUUID, m.DeviceMAC, persistent))
	}

	return nil
}
