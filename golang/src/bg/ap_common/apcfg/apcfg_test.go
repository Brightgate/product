/*
 * COPYRIGHT 2017 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package apcfg

import "testing"

// We construct a minimal PropertyNode tree, with parent node P and child node
// Q.
var (
	Q = PropertyNode{"example", "7", nil, nil}
	P = PropertyNode{"parent", "0", nil, []*PropertyNode{&Q}}
)

func TestGetName(t *testing.T) {
	if P.GetName() != "parent" {
		t.Error()
	}
	if Q.GetName() != "example" {
		t.Error()
	}
}

func TestGetValue(t *testing.T) {
	if P.GetValue() != "0" {
		t.Error()
	}
	if Q.GetValue() != "7" {
		t.Error()
	}
}

func TestGetChild(t *testing.T) {
	if P.GetChild("example") != &Q {
		t.Error()
	}
}

func TestGetChildByValue(t *testing.T) {
	if P.GetChildByValue("7") != &Q {
		t.Error()
	}
}
