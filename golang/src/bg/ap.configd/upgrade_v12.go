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
	"encoding/json"
	"log"
	"time"
)

type oldPnode struct {
	Name     string
	Value    string      `json:"Value,omitempty"`
	Modified *time.Time  `json:"Modified,omitempty"`
	Expires  *time.Time  `json:"Expires,omitempty"`
	Children []*oldPnode `json:"Children,omitempty"`
}

func convertPnode(old *oldPnode, new *pnode) {
	new.name = old.Name
	new.Value = old.Value
	new.Modified = old.Modified
	new.Expires = old.Expires
	new.Children = make(map[string]*pnode)

	for _, c := range old.Children {
		var child pnode

		convertPnode(c, &child)
		new.Children[c.Name] = &child
	}
}

func oldPropTreeParse(data []byte) error {
	var root oldPnode

	if err := json.Unmarshal(data, &root); err != nil {
		return err
	}

	log.Printf("Importing pre-v12 config tree.\n")

	convertPnode(&root, propTreeRoot)

	return nil
}

func upgradeV12() error {
	return nil
}

func init() {
	addUpgradeHook(12, upgradeV12)
}
