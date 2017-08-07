package main

import (
	"log"
)

func upgradeV1() error {
	log.Printf("Adding @/apversion property\n")
	property_update("@/apversion", ApVersion, nil, true)
	return nil
}

func init() {
	addUpgradeHook(1, upgradeV1)
}
