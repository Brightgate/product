//
// COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
//
// This copyright notice is Copyright Management Information under 17 USC 1202
// and is included to protect this work and deter copyright infringement.
// Removal or alteration of this Copyright Management Information without the
// express written permission of Brightgate Inc is prohibited, and any
// such unauthorized removal or alteration will be a violation of federal law.
//

// Bayesian classifier for operating system genus and species.
package main

import (
	"fmt"

	"github.com/lytics/multibayes"
	"github.com/pkg/errors"
)

const (
	osCertainAbove       = 0.6
	osUncertainBelow     = 0.4
	distroCertainAbove   = 0.6
	distroUncertainBelow = 0.4

	osGenusMinClassSize   = 4
	osSpeciesMinClassSize = 3

	osGenusProperty   = "os_genus"
	osSpeciesProperty = "os_species"
)

func initOSGenusBayesClassifier() bayesClassifier {
	var m bayesClassifier

	m.name = fmt.Sprintf("%s-%d", "bayes-os", osGenusMinClassSize)
	m.level = productionClassifier
	m.set = make([]machine, 0)
	m.classifiers = make(map[string]*multibayes.Classifier)
	m.certainAbove = osCertainAbove
	m.uncertainBelow = osUncertainBelow
	m.unknownValue = unknownOs
	m.TargetValue = osGenusTargetValue
	m.classificationProp = osGenusProperty

	return m
}

func osGenusTargetValue(rdi RecordedDevice) string {
	_, present := osRevGenusMap[rdi.AssignedOSGenus]
	if !present {
		slog.Warnf("osRevGenusMap unknown OS %s", rdi.AssignedOSGenus)
		return unknownOs
	}

	return rdi.AssignedOSGenus
}

func initOSSpeciesBayesClassifier() bayesClassifier {
	var m bayesClassifier

	m.name = fmt.Sprintf("%s-%d", "bayes-distro", osSpeciesMinClassSize)
	m.level = experimentalClassifier
	m.set = make([]machine, 0)
	m.classifiers = make(map[string]*multibayes.Classifier)
	m.certainAbove = distroCertainAbove
	m.uncertainBelow = distroUncertainBelow
	m.unknownValue = unknownOs
	m.TargetValue = osSpeciesTargetValue
	m.classificationProp = osSpeciesProperty

	return m
}

func osSpeciesTargetValue(rdi RecordedDevice) string {
	_, present := osRevSpeciesMap[rdi.AssignedOSSpecies]
	if !present {
		slog.Warnf("osRevSpeciesMap unknown OS %s", rdi.AssignedOSSpecies)
		return unknownOs
	}

	return rdi.AssignedOSSpecies
}

func trainOSGenusBayesClassifier(B *backdrop) error {
	var trainData []machine

	ogs := initOSGenusBayesClassifier()
	err := ogs.GenSetFromDB(B)
	if err != nil {
		return errors.Wrap(err, "unable to train os genus")
	}

	trainData, _ = ogs.instancesTrainSpecifiedSplit()

	ogs.classifiers[ogs.name] = multibayes.NewClassifier()
	ogs.classifiers[ogs.name].MinClassSize = osGenusMinClassSize

	ogs.train(B, trainData)

	return nil
}

func trainOSSpeciesBayesClassifier(B *backdrop) error {
	var trainData []machine

	oss := initOSSpeciesBayesClassifier()
	err := oss.GenSetFromDB(B)
	if err != nil {
		return errors.Wrap(err, "unable to train os species")
	}

	trainData, _ = oss.instancesTrainSpecifiedSplit()

	oss.classifiers[oss.name] = multibayes.NewClassifier()
	oss.classifiers[oss.name].MinClassSize = osSpeciesMinClassSize

	oss.train(B, trainData)

	return nil
}
