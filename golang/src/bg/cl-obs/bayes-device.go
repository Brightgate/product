/*
 * Copyright 2020 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


// Implementation of a Bayesian classifier for the device family.

package main

import (
	"bg/cl-obs/defs"
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
	return bayesClassifier{
		name:               fmt.Sprintf("%s-%d", "bayes-device", deviceGenusMinClassSize),
		set:                make([]machine, 0),
		classifiers:        make(map[string]*multibayes.Classifier),
		certainAbove:       deviceCertainAbove,
		uncertainBelow:     deviceUncertainBelow,
		level:              productionClassifier,
		unknownValue:       defs.UnknownDevice,
		classificationProp: deviceGenusProperty,
		TargetValue:        deviceGenusTargetValue,
	}
}

func deviceGenusTargetValue(rdi RecordedDevice) string {
	_, present := defs.DeviceRevMap[rdi.AssignedDeviceGenus]
	if !present {
		slog.Warnf("deviceRevMap unknown device %s", rdi.AssignedDeviceGenus)
		return defs.UnknownDevice
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

