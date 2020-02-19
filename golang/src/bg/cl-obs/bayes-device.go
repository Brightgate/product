//
// COPYRIGHT 2020 Brightgate Inc.  All rights reserved.
//
// This copyright notice is Copyright Management Information under 17 USC 1202
// and is included to protect this work and deter copyright infringement.
// Removal or alteration of this Copyright Management Information without the
// express written permission of Brightgate Inc is prohibited, and any
// such unauthorized removal or alteration will be a violation of federal law.
//

// Implementation of a Bayesian classifier for the device family.

package main

import (
	"fmt"

	"github.com/lytics/multibayes"
	"github.com/pkg/errors"
)

const (
	deviceCertainAbove   = 0.4
	deviceUncertainBelow = 0.25

	deviceGenusMinClassSize = 3

	deviceGenusProperty = "device_genus"
)

func initDeviceGenusBayesClassifier() bayesClassifier {
	var m bayesClassifier

	m.name = fmt.Sprintf("%s-%d", "bayes-device", deviceGenusMinClassSize)
	m.set = make([]machine, 0)
	m.classifiers = make(map[string]*multibayes.Classifier)
	m.certainAbove = deviceCertainAbove
	m.uncertainBelow = deviceUncertainBelow
	m.level = productionClassifier
	m.unknownValue = unknownDevice
	m.classificationProp = deviceGenusProperty

	m.TargetValue = deviceGenusTargetValue

	return m
}

func deviceGenusTargetValue(rdi RecordedDevice) string {
	_, present := deviceRevMap[rdi.AssignedDeviceGenus]
	if !present {
		slog.Warnf("deviceRevMap unknown device %s", rdi.AssignedDeviceGenus)
		return unknownDevice
	}

	return rdi.AssignedDeviceGenus
}

func trainDeviceGenusBayesClassifier(B *backdrop) error {
	var trainData []machine

	dgs := initDeviceGenusBayesClassifier()
	err := dgs.GenSetFromDB(B)
	if err != nil {
		return errors.Wrap(err, "unable to train os species")
	}

	trainData, _ = dgs.instancesTrainSpecifiedSplit()

	dgs.classifiers[dgs.name] = multibayes.NewClassifier()
	dgs.classifiers[dgs.name].MinClassSize = deviceGenusMinClassSize

	dgs.train(B, trainData)

	return nil
}
