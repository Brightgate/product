//
// COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
//
// This copyright notice is Copyright Management Information under 17 USC 1202
// and is included to protect this work and deter copyright infringement.
// Removal or alteration of this Copyright Management Information without the
// express written permission of Brightgate Inc is prohibited, and any
// such unauthorized removal or alteration will be a violation of federal law.
//

// A lookup classifier for the manufacturer of the hardware Ethernet
// interface, based on the IEEE OUI database.
package main

func initInterfaceMfgLookupClassifier() lookupClassifier {
	return lookupClassifier{
		name:               "lookup-mfg",
		level:              productionClassifier,
		certainAbove:       0.9,
		uncertainBelow:     0.5,
		unknownValue:       unknownMfg,
		classificationProp: "oui_mfg",
		TargetValue:        lookupInterfaceMfgTargetValue,
		Lookup:             lookupInterfaceMfgLookup,
	}
}

func lookupInterfaceMfgTargetValue(rdi RecordedDeviceInfo) string {
	return ""
}

func lookupInterfaceMfgLookup(B *backdrop, datum string) string {
	return getMfgFromMAC(B, datum)
}

func trainInterfaceMfgLookupClassifier(B *backdrop) {
	ims := initInterfaceMfgLookupClassifier()

	ims.train(B)
}
