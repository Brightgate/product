/*
 * Copyright 2020 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


// Bayesian classifier for operating system genus and species.
package main

import (
	"bg/cl-obs/defs"
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
	return bayesClassifier{
		name:               fmt.Sprintf("%s-%d", "bayes-os", osGenusMinClassSize),
		level:              productionClassifier,
		set:                make([]machine, 0),
		classifiers:        make(map[string]*multibayes.Classifier),
		certainAbove:       osCertainAbove,
		uncertainBelow:     osUncertainBelow,
		unknownValue:       defs.UnknownOS,
		TargetValue:        osGenusTargetValue,
		classificationProp: osGenusProperty,
	}
}

func osGenusTargetValue(rdi RecordedDevice) string {
	_, present := defs.OSRevGenusMap[rdi.AssignedOSGenus]
	if !present {
		slog.Warnf("OSRevGenusMap unknown OS %s", rdi.AssignedOSGenus)
		return defs.UnknownOS
	}

	return rdi.AssignedOSGenus
}

func initOSSpeciesBayesClassifier() bayesClassifier {
	return bayesClassifier{
		name:               fmt.Sprintf("%s-%d", "bayes-distro", osSpeciesMinClassSize),
		level:              experimentalClassifier,
		set:                make([]machine, 0),
		classifiers:        make(map[string]*multibayes.Classifier),
		certainAbove:       distroCertainAbove,
		uncertainBelow:     distroUncertainBelow,
		unknownValue:       defs.UnknownOS,
		TargetValue:        osSpeciesTargetValue,
		classificationProp: osSpeciesProperty,
	}
}

func osSpeciesTargetValue(rdi RecordedDevice) string {
	_, present := defs.OSRevSpeciesMap[rdi.AssignedOSSpecies]
	if !present {
		slog.Warnf("OSRevSpeciesMap unknown OS %s", rdi.AssignedOSSpecies)
		return defs.UnknownOS
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

