//
// COPYRIGHT 2020 Brightgate Inc.  All rights reserved.
//
// This copyright notice is Copyright Management Information under 17 USC 1202
// and is included to protect this work and deter copyright infringement.
// Removal or alteration of this Copyright Management Information without the
// express written permission of Brightgate Inc is prohibited, and any
// such unauthorized removal or alteration will be a violation of federal law.
//

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
	"encoding/json"
	"fmt"
	"hash/fnv"
	"sort"
	"strconv"
	"strings"
	"time"

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

// A sentence is implemented using a map so that we can easily compute
// whether or not two sentences have similar (same terms, but possibly
// different frequencies) or identical information content (terms and
// frequencies identical).
type sentence struct {
	words map[string]int
}

func newSentence() sentence {
	s := sentence{}

	s.words = make(map[string]int)

	return s
}

// addTerm, like all "add" methods for the sentence structure, returns true
// when all added terms are redundant (in that they were already present in the
// sentence).  If any added term is new, then false is returned.
func (s sentence) addTerm(term string) bool {
	s.words[term] = s.words[term] + 1

	return s.words[term] > 1
}

func (s sentence) addTermf(format string, a ...interface{}) bool {
	lt := strings.Fields(fmt.Sprintf(format, a...))
	ret := true

	for _, v := range lt {
		addl := s.addTerm(v)
		ret = ret && addl
	}

	return ret
}

func newSentenceFromString(sent string) sentence {
	s := newSentence()

	lt := strings.Fields(sent)

	for _, v := range lt {
		s.addTerm(v)
	}

	return s
}

func (s sentence) terms() []string {
	t := make([]string, 0)

	for k := range s.words {
		t = append(t, k)
	}

	return t
}

func (s sentence) toString() string {
	ts := s.terms()

	sort.Strings(ts)

	return strings.Join(ts, " ")
}

func (s sentence) toNaryString() string {
	t := make([]string, 0)

	for k, v := range s.words {
		for u := 0; u < v; u++ {
			t = append(t, k)
		}
	}

	return strings.Join(t, " ")
}

func (s sentence) addString(sent string) bool {
	ret := true

	lt := strings.Fields(sent)

	for _, v := range lt {
		n := s.addTerm(v)
		ret = ret && n
	}

	return ret
}

func (s sentence) addSentence(s2 sentence) bool {
	ret := true

	for k, v := range s2.words {
		s.words[k] += v
		if s.words[k] == v {
			// Meaning that this word was exclusive to the added
			// sentence.
			ret = false
		}
	}

	return ret
}

func (s sentence) termCount() int {
	return len(s.words)
}

func (s sentence) termHash() uint64 {
	h := fnv.New64()

	ws := s.terms()

	sort.Strings(ws)

	for _, k := range ws {
		h.Write([]byte(k))
	}

	return h.Sum64()
}

func (s sentence) wordCount() int {
	n := 0

	for _, v := range s.words {
		n += v
	}

	return n
}

func (s sentence) wordHash() uint64 {
	h := fnv.New64()

	ws := s.terms()

	sort.Strings(ws)

	for _, k := range ws {
		h.Write([]byte(k))
		h.Write([]byte(strconv.Itoa(s.words[k])))
	}

	return h.Sum64()
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
		p := newSentence()

		for _, rt := range trainings {
			var sent sentence

			// Retrieve inventory.
			ri, err := inventoryFromTraining(B.db, rt)

			if err == nil && ri.BayesSentenceVersion == getCombinedVersion() {
				sent = newSentenceFromString(ri.BayesSentence)
			} else {
				rdr, err := readerFromTraining(B, rt)
				if err != nil {
					slog.Errorf("couldn't get reader for %v: %v\n", rt, err)
					continue
				}

				_, sent = genBayesSentenceFromReader(B.ouidb, rdr)
			}

			p.addSentence(sent)
		}

		m.set = append(m.set, machine{rdi.DeviceMAC, p.toString(), []string{target}})
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

		_, ierr := B.modeldb.Exec("INSERT OR REPLACE INTO model (generation_date, name, classifier_type, classifier_level, multibayes_min, certain_above, uncertain_below, model_json) VALUES ($1, $2, $3, $4, $5, $6, $7, $8);",
			time.Now(), k, "bayes", m.level, cl.MinClassSize, m.certainAbove, m.uncertainBelow, jm)
		if ierr != nil {
			slog.Errorf("could not update '%s' model: %s", k, ierr)
		}
	}

	slog.Infof("train %s finish", m.name)
}

func reviewBayes(m RecordedClassifier) string {
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
