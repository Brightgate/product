/*
 * COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package main

import (
	"bg/base_def"
	"bg/common/cfgapi"
)

const (
	noVAP  = ""
	pskVAP = "psk"
	eapVAP = "eap"
)

func getVal(prop string) (string, error) {
	var val string

	node, err := propTree.GetNode(prop)
	if err == nil {
		val = node.Value
	}

	return val, err
}

func upgradeV17() error {
	var ssid, ssidEap, psk5ghz, eap5ghz, passphrase string
	var pskDefault, eapDefault string
	var err error

	deleteProps := make([]string, 0)

	// Determine the current ring->auth pairings
	ringToVAP := make(map[string]string)
	for ring := range cfgapi.ValidRings {
		authProp := "@/rings/" + ring + "/auth"
		deleteProps = append(deleteProps, authProp)
		if auth, err := getVal(authProp); err == nil {
			if auth == "wpa-psk" {
				ringToVAP[ring] = pskVAP
			} else if auth == "wpa-eap" {
				ringToVAP[ring] = eapVAP
			} else if auth == "open" {
				ringToVAP[ring] = noVAP
			}
		}
	}

	// Determine the current SSIDs and whether we are using different SSIDs
	// for the 5GHz bands
	if ssid, err = getVal("@/network/ssid"); err == nil {
		deleteProps = append(deleteProps, "@/network/ssid")
	} else {
		ssid = "setme" // shouldn't happen
	}

	if ssidEap, err = getVal("@/network/ssid-eap"); err == nil {
		deleteProps = append(deleteProps, "@/network/ssid-eap")
	} else {
		ssidEap = ssid + "-eap"
	}

	if _, err = getVal("@/network/ssid-5ghz"); err == nil {
		psk5ghz = "true"
		deleteProps = append(deleteProps, "@/network/ssid-5hgz")
	} else {
		psk5ghz = "false"
	}

	if _, err = getVal("@/network/ssid-eap-5ghz"); err == nil {
		eap5ghz = "true"
		deleteProps = append(deleteProps, "@/network/ssid-eap-5ghz")
	} else {
		eap5ghz = "false"
	}

	if passphrase, err = getVal("@/network/passphrase"); err == nil {
		deleteProps = append(deleteProps, "@/network/passphrase")
	}

	if pskDefault, err = getVal("@/network/default_ring/wpa-psk"); err != nil {
		pskDefault = base_def.RING_UNENROLLED
	}
	if eapDefault, err = getVal("@/network/default_ring/wpa-eap"); err != nil {
		eapDefault = base_def.RING_GUEST
	}
	deleteProps = append(deleteProps, "@/network/default_ring")

	slog.Info("Adding config properties for virtual AP " + pskVAP)
	propTree.Add("@/network/vap/"+pskVAP+"/ssid", ssid, nil)
	propTree.Add("@/network/vap/"+pskVAP+"/5ghz", psk5ghz, nil)
	propTree.Add("@/network/vap/"+pskVAP+"/keymgmt", "wpa-psk", nil)
	propTree.Add("@/network/vap/"+pskVAP+"/passphrase", passphrase, nil)
	propTree.Add("@/network/vap/"+pskVAP+"/default_ring", pskDefault, nil)

	slog.Info("Adding config properties for virtual AP " + eapVAP)
	propTree.Add("@/network/vap/"+eapVAP+"/ssid", ssidEap, nil)
	propTree.Add("@/network/vap/"+eapVAP+"/5ghz", eap5ghz, nil)
	propTree.Add("@/network/vap/"+eapVAP+"/keymgmt", "wpa-eap", nil)
	propTree.Add("@/network/vap/"+eapVAP+"/default_ring", eapDefault, nil)

	slog.Info("Adding virtual AP assignments to rings")
	for ring, vap := range ringToVAP {
		propTree.Add("@/rings/"+ring+"/vap", vap, nil)
	}

	slog.Info("Removing obsoleted @/network/* and @/rings/<ring>/auth" +
		" properties")
	for _, prop := range deleteProps {
		propTree.Delete(prop)
	}

	return nil
}

func init() {
	addUpgradeHook(17, upgradeV17)
}
