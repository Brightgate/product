/*
 * COPYRIGHT 2020 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package classifier

import (
	"bg/cl-obs/defs"
	"bg/cl-obs/modeldb"
	"bg/cl-obs/sentence"
	"fmt"
	"math"
	"net"
	"strings"

	"github.com/klauspost/oui"
	"github.com/lytics/multibayes"
	"github.com/pkg/errors"
)

const (
	// ClassifyUncertain indicates the classification produced no usable result
	ClassifyUncertain = 0
	// ClassifyCrossing indicates the classification a result, but it may not be reliable
	ClassifyCrossing = 1
	// ClassifyCertain indicates the classification produced a good quality result
	ClassifyCertain = 2
)

// ClassifyResult represents the outcome of running one classifier for one
// client device
type ClassifyResult struct {
	ModelName      string
	Classification string
	Probability    float64
	NextProb       float64
	Region         int
	Unknown        bool
}

func (c ClassifyResult) String() string {
	return fmt.Sprintf("%s:%s [%.2f]", c.ModelName, c.Classification, c.Probability)
}

// Equal returns true if c and d are substantively equal classification results
func (c ClassifyResult) Equal(d ClassifyResult) bool {
	if c.ModelName != "" && d.ModelName != "" && c.ModelName != d.ModelName {
		panic("must not compare results from different classifiers")
	}
	if c.Classification != d.Classification {
		return false
	}
	if math.Abs(c.Probability-d.Probability) > 0.0001 {
		return false
	}
	return true
}

func newClassifyResultFromPosterior(name string, certainAbove float64, uncertainBelow float64, posterior map[string]float64) ClassifyResult {
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

	region := ClassifyUncertain
	if maxProb > certainAbove {
		region = ClassifyCertain
	} else if maxProb > uncertainBelow {
		region = ClassifyCrossing
	}

	return ClassifyResult{
		ModelName:      name,
		Classification: maxClass,
		Probability:    maxProb,
		NextProb:       nextProb,
		Region:         region,
		Unknown:        false,
	}
}

// BayesClassifier represents a single bayesian classifier
type BayesClassifier struct {
	modeldb.RecordedClassifier
	Bayes *multibayes.Classifier
}

// NewBayesClassifier creates a BayesClassifier which is ready to classify,
// using a RecordedClassifier as input.
func NewBayesClassifier(rc modeldb.RecordedClassifier) (*BayesClassifier, error) {
	if rc.ClassifierType != "bayes" {
		return nil, errors.Errorf("classifier type %s != bayes", rc.ClassifierType)
	}
	bayes, err := multibayes.NewClassifierFromJSON([]byte(rc.ModelJSON))
	if err != nil {
		return nil, errors.Wrap(err, "New classifier")
	}
	return &BayesClassifier{
		RecordedClassifier: rc,
		Bayes:              bayes,
	}, nil
}

// Classify runs the bayesian classification to produce a ClassifyResult
func (c *BayesClassifier) Classify(sent sentence.Sentence) ClassifyResult {
	posterior := c.Bayes.Posterior(sent.String())
	return newClassifyResultFromPosterior(c.RecordedClassifier.ModelName, c.RecordedClassifier.CertainAbove,
		c.RecordedClassifier.UncertainBelow, posterior)
}

// MfgLookupClassifier is a classifier which looks up the device MAC in the OUI
// database.
type MfgLookupClassifier struct {
	OuiDB oui.OuiDB
}

// NewMfgLookupClassifier creates a MfgLookupClassifier
func NewMfgLookupClassifier(oui oui.OuiDB) *MfgLookupClassifier {
	return &MfgLookupClassifier{
		OuiDB: oui,
	}
}

// Classify looks up the hwaddr and returns a classification result based on
// that lookup.
func (c *MfgLookupClassifier) Classify(hwaddr net.HardwareAddr) ClassifyResult {
	mfg := defs.UnknownMfg
	mac := hwaddr.String()
	if strings.HasPrefix(mac, "60:90:84:a") {
		mfg = "Brightgate, Inc."
	} else {
		entry, err := c.OuiDB.Query(mac)
		if err == nil {
			mfg = entry.Manufacturer
		}
	}

	return ClassifyResult{
		ModelName:      "lookup-mfg",
		Classification: mfg,
		Probability:    1.0,
		NextProb:       0.0,
		Region:         ClassifyCertain,
	}
}
