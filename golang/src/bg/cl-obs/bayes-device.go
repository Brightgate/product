/*
 * COPYRIGHT 2020 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
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
