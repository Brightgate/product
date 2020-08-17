/*
 * Copyright 2020 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


// A Bayesian classifier is a supervised trained classifier.  With the
// accumulation of the training set, the classifier is capable of
// calculating the probability that a record matches a well-defined
// class in the training set.
//
// Training is dependent on the device and training tables in the
// primary database.

// Bayesian classifiers return the best matching class and probability.
// Three parameters are used to tune the behavior of a classifier, other
// than manipulating the membership of the training set:
//
// 1.  The _minimum class size_ represents the minimum number of entries
//     a class must have within the training set to be a valid result
//     returned in a classification.  Increasing the minimum class size
//     means that the training set must be large enough to contain at
//     least that many instances of each result.  Reducing the minimum
//     class size means that attributes shared among classes will
//     potentially be less well resolved.
//
// 2.  The _certain above_ parameter is a real-valued number
//     representing the probability above which we believe the
//     classification is meaningful.
//
// 3.  The _uncertain below_ parameter is a real-valued number
//     representing the probability below which we drop our belief that
//     a previously certain prediction is still meaningful.
//
// Because we are accumulating training data, we have kept our minimum
// class sizes small.  These should be increased gradually, as
// additional data is acquired; the trade-off is that a larger number of
// instances becomes required to add a new result to the classifier.
//
// The certain-uncertain parameters have typically been chosen such that
// the result is at least twice any next possible result to be certain,
// and that the result is less than 50-50 to lose that certainty.

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"bg/cl-obs/extract"
	"bg/cl-obs/modeldb"
	"bg/cl-obs/sentence"
	"bg/cl_common/deviceinfo"

	"github.com/jmoiron/sqlx"
	"github.com/lytics/multibayes"
	"github.com/pkg/errors"
)

// A machine is a identifiable target that has a MAC address.
type machine struct {
	mac     string
	Text    string   // Concatenation of attribute terms.
	Classes []string // Vector of generic OS values.
}

type bayesClassifier struct {
	name               string
	set                []machine
	classifiers        map[string]*multibayes.Classifier
	level              int
	certainAbove       float64
	uncertainBelow     float64
	unknownValue       string
	classificationProp string
	TargetValue        func(rdi RecordedDevice) string
}

// inventoryFromTuple pulls the inventory record which matches the tuple
func inventoryFromTuple(db *sqlx.DB, tup deviceinfo.Tuple) (*RecordedInventory, error) {
	ri := RecordedInventory{}
	err := db.Get(&ri, `
		SELECT * FROM inventory
		WHERE site_uuid=$1 AND device_mac=$2 AND unix_timestamp=$3;`,
		tup.SiteUUID, tup.MAC, tup.TS.Unix())
	if err != nil {
		return nil, errors.Wrap(err, "inventoryFromTraining failed")
	}
	return &ri, nil
}

func (m *bayesClassifier) GenSetFromDB(B *backdrop) error {
	var devices []RecordedDevice
	err := B.db.Select(&devices, "SELECT * FROM device;")
	if err != nil {
		return errors.Wrap(err, "select device failed")
	}

	n := 0

	for _, rdi := range devices {
		target := m.TargetValue(rdi)

		// Query training set for this DGroupID.
		var trainings []RecordedTraining
		err := B.db.Select(&trainings, "SELECT * FROM training WHERE dgroup_id = $1", rdi.DGroupID)
		if err != nil {
			return errors.Wrap(err, "select training failed")
		}
		p := sentence.New()

		for _, rt := range trainings {
			// Retrieve inventory.
			ri, err := inventoryFromTuple(B.db, rt.Tuple())

			if err == nil && ri.BayesSentenceVersion == extract.CombinedVersion {
				p.AddString(ri.BayesSentence)
			} else {
				di, err := B.store.ReadTuple(context.Background(), rt.Tuple())
				if err != nil {
					slog.Errorf("couldn't get DeviceInfo %s: %v\n", rt.Tuple(), err)
					continue
				}

				sent := extract.BayesSentenceFromDeviceInfo(B.ouidb, di)
				p.AddSentence(sent)
			}
		}

		m.set = append(m.set, machine{rdi.DeviceMAC, p.String(), []string{target}})
		n++
	}

	slog.Infof("model has %d rows, set has %d machines", n, len(m.set))

	return nil
}

func (m *bayesClassifier) instancesTrainSpecifiedSplit() ([]machine, []machine) {
	if len(m.set) == 0 {
		slog.Infof("empty source machine set from %s", m.name)
	}

	trainingRows := make([]machine, 0)
	testingRows := make([]machine, 0)

	// Create the return structure
	for _, s := range m.set {
		if len(s.Classes) == 1 && s.Classes[0] != m.unknownValue {
			slog.Infof("machine -> training: %+v", s)
			trainingRows = append(trainingRows, s)
		} else {
			slog.Infof("machine -> testing: %+v", s)
			testingRows = append(testingRows, s)
		}
	}

	slog.Infof("training set size %d (%f)", len(trainingRows), float64((1.*len(trainingRows))/len(m.set)))

	return trainingRows, testingRows
}

func (m *bayesClassifier) train(B *backdrop, trainData []machine) {
	slog.Infof("train %s start", m.name)

	for _, machine := range trainData {
		slog.Debugf("training on %v", machine)
		for _, cl := range m.classifiers {
			cl.Add(machine.Text, machine.Classes)
		}
	}

	for k, cl := range m.classifiers {
		jm, err := cl.MarshalJSON()
		if err == nil {
			slog.Infof("Model:\n%s\n", string(jm))
		} else {
			slog.Errorf("Cannot marshal '%s' classifier to JSON: %v", k, err)
			// XXX?
			continue
		}

		r := modeldb.RecordedClassifier{
			GenerationTS:    time.Now(),
			ModelName:       k,
			ClassifierType:  "bayes",
			ClassifierLevel: m.level,
			MultibayesMin:   cl.MinClassSize,
			CertainAbove:    m.certainAbove,
			UncertainBelow:  m.uncertainBelow,
			ModelJSON:       string(jm),
		}
		if ierr := B.modeldb.UpsertModel(r); ierr != nil {
			slog.Errorf("train recording failed: %s", ierr)
		}
	}

	slog.Infof("train %s finish", m.name)
}

func reviewBayes(m modeldb.RecordedClassifier) string {
	var msg strings.Builder

	fmt.Fprintf(&msg, "Bayesian Classifier, Name: %s\nGenerated: %s\nCutoff: %d\n", m.ModelName,
		m.GenerationTS, m.MultibayesMin)
	dec := json.NewDecoder(strings.NewReader(m.ModelJSON))
	cls := make(map[string]int)

	for {
		var v map[string]interface{}

		if err := dec.Decode(&v); err != nil {
			break
		}

		for j := range v {
			if j == "matrix" {
				w := v["matrix"].(map[string]interface{})
				for k := range w {
					if k == "classes" {
						x := w["classes"].(map[string]interface{})
						for l := range x {
							y := x[l].([]interface{})
							cls[l] = len(y)
						}
					}
				}
			}
		}
	}

	for k, v := range cls {
		s := "\u2714" // checkmark
		if v < m.MultibayesMin {
			s = "\u2717" // ballot x
		}
		fmt.Fprintf(&msg, "%s %30s %4d\n", s, k, v)
	}

	return msg.String()
}

