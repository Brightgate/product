/*
 * Copyright 2020 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


// A lookup classifier for the manufacturer of the hardware Ethernet
// interface, based on the IEEE OUI database.
package main

import "bg/cl-obs/defs"

func initInterfaceMfgLookupClassifier() lookupClassifier {
	return lookupClassifier{
		name:               "lookup-mfg",
		level:              productionClassifier,
		certainAbove:       0.9,
		uncertainBelow:     0.5,
		unknownValue:       defs.UnknownMfg,
		classificationProp: "oui_mfg",
		TargetValue:        lookupInterfaceMfgTargetValue,
	}
}

func lookupInterfaceMfgTargetValue(rdi RecordedDevice) string {
	return ""
}

func trainInterfaceMfgLookupClassifier(B *backdrop) {
	ims := initInterfaceMfgLookupClassifier()

	ims.train(B)
}

