/*
 * Copyright 2020 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


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
	"bg/cl-obs/modeldb"
	"time"
)

type lookupClassifier struct {
	name               string
	level              int
	certainAbove       float64
	uncertainBelow     float64
	unknownValue       string
	classificationProp string
	TargetValue        func(rdi RecordedDevice) string
}

func (m *lookupClassifier) train(B *backdrop) {
	r := modeldb.RecordedClassifier{
		GenerationTS:    time.Now(),
		ModelName:       m.name,
		ClassifierType:  "lookup",
		ClassifierLevel: m.level,
		MultibayesMin:   0,
		CertainAbove:    m.certainAbove,
		UncertainBelow:  m.uncertainBelow,
		ModelJSON:       "",
	}
	if err := B.modeldb.UpsertModel(r); err != nil {
		slog.Fatalf("could not update '%s' model: %s", m.name, err)
	}
}

