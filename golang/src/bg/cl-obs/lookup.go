//
// COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
//
// This copyright notice is Copyright Management Information under 17 USC 1202
// and is included to protect this work and deter copyright infringement.
// Removal or alteration of this Copyright Management Information without the
// express written permission of Brightgate Inc is prohibited, and any
// such unauthorized removal or alteration will be a violation of federal law.
//

// A "lookup" classifier isn't a trained classifier, like the supervised
// Bayesian classifiers used elsewhere, but is instead a classifier that
// looks up the record in a table and returns the value.
//
// Training is trivial, in that the external data source is treated as
// entirely correct for all classification requests.

// Lookup models associate probability 1. for a found result.  For records that
// return no entry, the unknown value is returned, with probability 0.  (The
// class size, certainty, and uncertainty parameters have no meaning for lookup
// classifiers.)

package main

import (
	"time"
)

type lookupClassifier struct {
	name               string
	level              int
	certainAbove       float64
	uncertainBelow     float64
	unknownValue       string
	classificationProp string
	TargetValue        func(rdi RecordedDeviceInfo) string
	Lookup             func(B *backdrop, datum string) string
}

func (m *lookupClassifier) train(B *backdrop) {
	_, ierr := B.modeldb.Exec(`INSERT OR REPLACE INTO model (
					generation_date,
					name,
					classifier_type,
					classifier_level,
					multibayes_min,
					certain_above,
					uncertain_below,
					model_json
				) VALUES ($1, $2, $3, $4, $5, $6, $7,$8);`,
		time.Now(),
		m.name,
		"lookup",
		m.level,
		0,
		m.certainAbove,
		m.uncertainBelow,
		"")
	if ierr != nil {
		slog.Fatalf("could not update '%s' model: %s", m.name, ierr)
	}
}

func (m *lookupClassifier) classify(B *backdrop, mac string) (string, float64) {
	return m.Lookup(B, mac), 1.
}
