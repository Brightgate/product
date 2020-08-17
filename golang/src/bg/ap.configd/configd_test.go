/*
 * Copyright 2020 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


/*
 * The following tests all perform manipulations of the config tree using the
 * same top-level interface invoked by the ZeroMQ handlers that provide RPC
 * services to the other daemons in the system.
 *
 * To validate the results of each tree operation, we use a parallel map
 * structure.  For every leaf node in the config tree, we have an entry in the
 * map.  The index for each entry is the full path of the node, and the value is
 * the value of property.
 *
 * Each test will import a fresh config tree and construct a parallel map from
 * that tree.  It will then manipulate the config tree using ap.configd
 * interfaces and also update its map structure to reflect the expected results
 * of those manipulations.  Finally, it will generate a new map from the config
 * tree and compare that to the map of expected properties/values.
 */

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strings"
	"testing"

	"bg/ap_common/aputil"
	"bg/ap_common/broker"
	"bg/common/cfgapi"
	"bg/common/cfgmsg"
	"bg/common/cfgtree"
)

var (
	testFile = flag.String("testdata", "testdata/ap_testing.json",
		"path to an initial config tree")
	testData []byte
	config   = internalConfig{
		level: cfgapi.AccessUser,
	}
)

type leafMap map[string]string

// utility function to check a leaf map for a single value
func checkLeaf(t *testing.T, prop, val string, leaves leafMap) {
	got, ok := leaves[prop]
	if !ok {
		t.Error("Missing " + prop)
	}
	if got != val {
		t.Errorf("%s is '%s'.  Expected '%s'", prop, got, val)
	}
}

func executeInternal(ops []cfgapi.PropertyOp) (string, error) {
	ctx := context.Background()

	return config.ExecuteAt(ctx, ops, cfgapi.AccessInternal).Wait(ctx)
}

func executeUser(ops []cfgapi.PropertyOp) (string, error) {
	ctx := context.Background()

	return config.Execute(ctx, ops).Wait(ctx)
}

func fetchSubtree(t *testing.T, prop string, succeed bool) string {
	var rval string

	ops := []cfgapi.PropertyOp{
		{Op: cfgapi.PropGet, Name: prop},
	}

	blob, err := executeInternal(ops)
	if err != nil {
		if succeed {
			t.Errorf("Failed to get %s: %v", prop, err)
		}
	} else {
		rval = string(blob)
		if !succeed {
			t.Errorf("Got %s -> %s.  Expected failure.", prop, rval)
		}
	}

	return rval
}

func checkSubtree(t *testing.T, prop, expected string) {
	val := fetchSubtree(t, prop, true)

	if !t.Failed() && expected != val {
		t.Errorf("Got %s -> %s.  Expected '%s'", prop, val, expected)
	}
}

// utility function to attempt the 'get' of a single property.  The caller
// decides whether the operation should succeed or fail.
func checkOneProp(t *testing.T, prop, val string, succeed bool) {
	treeVal := fetchSubtree(t, prop, succeed)

	if !t.Failed() && succeed {
		var node cfgtree.PNode
		if err := json.Unmarshal([]byte(treeVal), &node); err != nil {
			t.Errorf("Failed to decode %s: %v", prop, err)
		} else if node.Value != val {
			t.Errorf("Got %s -> %s.  Expected '%s'", prop,
				node.Value, val)
		}
	}
}

// utility function to change a single property setting and validate the result
func updateOneProp(t *testing.T, prop, val string, succeed bool) {
	ops := []cfgapi.PropertyOp{
		{Op: cfgapi.PropSet, Name: prop, Value: val},
	}

	if _, err := executeInternal(ops); err != nil {
		if succeed {
			t.Errorf("Failed to change %s to %s: %v", prop, val, err)
		}
	} else if succeed {
		checkOneProp(t, prop, val, true)
	} else {
		t.Errorf("Changed %s to %s.  Should have failed", prop, val)
	}
}

// utility function to insert a single new property and validate the result
func insertOneProp(t *testing.T, prop, val string, succeed bool) {
	ops := []cfgapi.PropertyOp{
		{Op: cfgapi.PropCreate, Name: prop, Value: val},
	}

	if _, err := executeInternal(ops); err != nil {
		if succeed {
			t.Errorf("Failed to insert %s: %v", prop, err)
		}
	} else if succeed {
		checkOneProp(t, prop, val, true)
	} else {
		t.Errorf("Inserted %s -> %s.  Should have failed", prop, val)
	}
}

// utility function to remove a single new property and validate the result
func deleteOneProp(t *testing.T, prop string, succeed bool) {
	ops := []cfgapi.PropertyOp{
		{Op: cfgapi.PropDelete, Name: prop},
	}
	if _, err := executeInternal(ops); err != nil {
		if succeed {
			t.Errorf("Failed to delete %s: %v", prop, err)
		}
	} else if succeed {
		checkOneProp(t, prop, "", false)
	}
}

// construct a fresh config tree from the pre-loaded JSON file.  Return a map of
// all the property leaves in the fresh tree.
func testTreeInit(t *testing.T) leafMap {
	tree, err := cfgtree.NewPTree("@/", testData)
	if err != nil {
		t.Fatalf("failed to import config tree: %v", err)
	}
	propTree = tree
	propTree.SetCacheable()
	if err := versionTree(); err != nil {
		t.Fatalf("failed to upgrade config tree: %v", err)
	}
	ringSubnetInit()

	return testBuildMap(t)
}

func addLeaves(leaves leafMap, node *cfgtree.PNode) {
	if len(node.Children) == 0 {
		leaves[node.Path()] = node.Value
	} else {
		for _, n := range node.Children {
			addLeaves(leaves, n)
		}
	}
}

// return a map containing all of the leaf paths and their values
func testBuildMap(t *testing.T) leafMap {
	leaves := make(leafMap)

	root, err := propTree.GetNode("@/")
	if err != nil {
		t.Fatalf("failed to get root node")
	}

	addLeaves(leaves, root)

	return leaves
}

// compare two sets of leaf/value maps.  Report any expected leaves that are
// missing, any unexpected leaves that are present, and any leaves that have
// incorrect values.
func testCompareMaps(t *testing.T, expected, got leafMap) {
	var extra, missing, incorrect []string

	for leaf, expectedVal := range expected {
		if gotVal, ok := got[leaf]; ok {
			if gotVal != expectedVal {
				incorrect = append(incorrect, leaf)
			}
		} else {
			missing = append(missing, leaf)
		}
	}

	for leaf := range got {
		if _, ok := expected[leaf]; !ok {
			extra = append(extra, leaf)
		}
	}

	msg := ""
	if len(missing) > 0 {
		msg += "missing leaf nodes:\n"
		for _, l := range missing {
			msg += "    " + l + "\n"
		}
	}
	if len(extra) > 0 {
		msg += "extra leaf nodes:\n"
		for _, l := range extra {
			msg += "    " + l + "\n"
		}
	}
	if len(incorrect) > 0 {
		msg += "incorrect values:\n"
		for _, l := range incorrect {
			msg += "    " + l + "\n"
			msg += "             got: " + got[l] + "\n"
			msg += "        expected: " + expected[l] + "\n"
		}
	}

	if len(msg) > 0 {
		t.Error(msg)
	}
}

func testValidateTree(t *testing.T, expected leafMap) {

	root, _ := propTree.GetNode("@/")
	if !root.Validate() {
		t.Error("hash mismatch")
	}

	testCompareMaps(t, expected, testBuildMap(t))
}

// TestNull is a sanity test of the testCompareMaps function.  It ensures that
// two comparisons of the same data succeed.
func TestNull(t *testing.T) {
	a := testTreeInit(t)
	b := testBuildMap(t)
	testCompareMaps(t, a, b)
}

// TestReinitialize checks to be sure that importing the same config tree twice
// in a row give us the same data both times.
func TestReinitialize(t *testing.T) {
	a := testTreeInit(t)
	b := testTreeInit(t)
	testCompareMaps(t, a, b)
}

// TestPing verifies that a simple ping succeeds
func TestPing(t *testing.T) {
	query := cfgapi.NewPingQuery()
	response := processOneEvent(query)
	if response.Response != cfgmsg.ConfigResponse_OK {
		t.Error(fmt.Errorf("%s", response.Value))
	}
}

func testPingBadVersion(version int32) error {
	query := cfgapi.NewPingQuery()
	majorMinor := cfgmsg.Version{Major: version}
	query.Version = &majorMinor

	response := processOneEvent(query)
	if response.Response == cfgmsg.ConfigResponse_OK {
		return fmt.Errorf("configd of version %d accepted version %d",
			cfgapi.Version, version)
	} else if response.Response != cfgmsg.ConfigResponse_BADVERSION {
		return fmt.Errorf("unexpected error: %d", response.Response)
	}

	return nil
}

// TestOlderVersion verifies that configd will correctly refuse to execute a
// command with an newer version
func TestOlderVersion(t *testing.T) {
	if err := testPingBadVersion(cfgapi.Version - 1); err != nil {
		t.Error(err)
	}
}

// TestNewerVersion verifies that configd will correctly refuse to execute a
// command with an old version
func TestNewerVersion(t *testing.T) {
	if err := testPingBadVersion(cfgapi.Version + 1); err != nil {
		t.Error(err)
	}
}

// TestChangeProp verifies that we can successfully change a single property
func TestChangeProp(t *testing.T) {
	const (
		ssidProp = "@/network/vap/psk/ssid"
		origSSID = "setme"
		newSSID  = "newssid"
	)

	a := testTreeInit(t)
	checkLeaf(t, ssidProp, origSSID, a)

	updateOneProp(t, ssidProp, newSSID, true)

	a[ssidProp] = newSSID
	testValidateTree(t, a)
}

// TestChangeNonProp verifies that we cannot change a non-existent property
func TestAddNonProp(t *testing.T) {
	const (
		// This has to be a legal property that's not set in the testing
		// data or added by an upgrade routine.
		newProp = "@/certs/blah/state"
		newVal  = "unavailable"
	)

	a := testTreeInit(t)
	updateOneProp(t, newProp, newVal, false)
	testValidateTree(t, a)
}

// TestAddProp verifies that we can successfully add a single property
func TestAddProp(t *testing.T) {
	const (
		newProp = "@/siteid"
		newVal  = "7810.b10e.net"
	)

	a := testTreeInit(t)
	a[newProp] = newVal
	insertOneProp(t, newProp, newVal, true)
	testValidateTree(t, a)
}

// TestAddBadProp verifies that we cannot add an invalid property
func TestAddBadProp(t *testing.T) {
	const (
		newProp = "@/rings/invalid/auth"
		newVal  = "wpa-eap"
	)

	a := testTreeInit(t)
	insertOneProp(t, newProp, newVal, false)
	testValidateTree(t, a)
}

// TestSetInternalProp verifies that a user cannot set an internal property
func TestSetInternalProp(t *testing.T) {
	const (
		newProp = "@/cfgversion"
		newVal  = "22"
	)

	ops := []cfgapi.PropertyOp{
		{Op: cfgapi.PropSet, Name: newProp, Value: newVal},
	}

	a := testTreeInit(t)
	if _, err := executeUser(ops); err == nil {
		t.Errorf("Inserted %s.  Should have failed", newProp)
	}
	testValidateTree(t, a)
}

// TestAddInternalProp verifies that a user cannot add an internal property
func TestAddInternalProp(t *testing.T) {
	const (
		newProp = "@/cloud/update/bucket"
		newVal  = "bucketName"
	)

	ops := []cfgapi.PropertyOp{
		{Op: cfgapi.PropCreate, Name: newProp, Value: newVal},
	}

	a := testTreeInit(t)
	if _, err := executeUser(ops); err == nil {
		t.Errorf("Inserted %s.  Should have failed", newProp)
	}
	testValidateTree(t, a)
}

// TestDeleteNonProp verifies that we can't remove a non-existent property
func TestDeleteNonProp(t *testing.T) {
	const (
		// This has to be a legal property that's not set in the testing
		// data or added by an upgrade routine.
		nonProp = "@/certs/blah/state"
	)

	a := testTreeInit(t)
	deleteOneProp(t, nonProp, false)
	testValidateTree(t, a)
}

// TestDeleteProp verifies that we can successfully remove a single property
func TestDeleteProp(t *testing.T) {
	const (
		delProp = "@/network/base_address"
	)

	a := testTreeInit(t)
	delete(a, delProp)
	deleteOneProp(t, delProp, true)
	testValidateTree(t, a)
}

// TestDeleteInternalProp verifies that a user cannot delete an internal
// property
func TestDeleteInternalProp(t *testing.T) {
	const (
		delProp = "@/cfgversion"
	)

	ops := []cfgapi.PropertyOp{
		{Op: cfgapi.PropDelete, Name: delProp},
	}

	a := testTreeInit(t)
	if _, err := executeUser(ops); err == nil {
		t.Errorf("Deleted %s.  Should have failed", delProp)
	}
	testValidateTree(t, a)
}

// TestAddNestedProp verifies that we can successfully add a new property
// multiple levels deep
func TestAddNestedProp(t *testing.T) {
	const (
		newProp = "@/clients/00:00:00:00:00:00/dns_name"
		newVal  = "newName"
	)

	a := testTreeInit(t)
	a[newProp] = newVal
	insertOneProp(t, newProp, newVal, true)
	testValidateTree(t, a)
}

// TestMoveLeaf
func TestMoveLeaf(t *testing.T) {
	const (
		oldProp = "@/clients/64:9a:be:da:b1:9a/dhcp_name"
		newProp = "@/clients/64:9a:be:da:b1:9a/dns_name"
	)

	a := testTreeInit(t)

	n, err := propTree.GetNode(oldProp)
	if err != nil {
		t.Errorf("GetNode() failed: %v", err)
	}
	val := n.Value

	delete(a, oldProp)
	a[newProp] = val

	propTree.ChangesetInit()
	err = n.Move(newProp)
	if err != nil {
		t.Errorf("Move() failed: %v", err)
	} else {
		propTree.ChangesetCommit()
		checkOneProp(t, oldProp, val, false)
		checkOneProp(t, newProp, val, true)
		testValidateTree(t, a)
	}
}

// TestDeleteSubtree verifies that we can successfully remove a subtree
func TestDeleteSubtree(t *testing.T) {
	const (
		subtree = "@/network"
	)

	a := testTreeInit(t)
	for p := range a {
		if strings.HasPrefix(p, subtree) {
			delete(a, p)
		}
	}
	deleteOneProp(t, subtree, true)

	testValidateTree(t, a)
}

// TestDeleteInternalSubtree verifies that a user cannot delete a subtree
// that includes at least one internal property
func TestDeleteInternalSubtree(t *testing.T) {
	const (
		delProp = "@/nodes"
	)

	ops := []cfgapi.PropertyOp{
		{Op: cfgapi.PropDelete, Name: delProp},
	}

	a := testTreeInit(t)
	if _, err := executeUser(ops); err == nil {
		t.Errorf("Deleted %s.  Should have failed", delProp)
	}
	testValidateTree(t, a)
}

// TestTestBasic verifies the functionality of ConfigOp_Test
func TestTestBasic(t *testing.T) {
	testCases := []struct {
		prop string
		err  error
	}{
		{"@/clients/00:00:00:00:00:00/dhcp_name", cfgapi.ErrNoProp},
		{"@/clients/64:9a:be:da:b1:9a/dhcp_name", nil},
		{"@/", nil},
	}

	for _, tc := range testCases {
		t.Run(tc.prop, func(t *testing.T) {
			a := testTreeInit(t)
			ops := []cfgapi.PropertyOp{
				{Op: cfgapi.PropTest, Name: tc.prop},
			}
			_, err := executeInternal(ops)
			if err != tc.err {
				t.Errorf("Test had error %#v.  Should have had %#v", err, tc.err)
			}
			testValidateTree(t, a)
		})
	}
}

// TestTestCompound verifies the functionality of ConfigOp_Test in compound
// operations.
func TestTestCompound(t *testing.T) {
	propTest := "@/clients/64:9a:be:da:b1:9a/dhcp_name"
	badPropTest := "@/clients/00:00:00:00:00:00/dhcp_name"
	oldVal := "test-client"
	newVal1 := "test1"
	newVal2 := "test2"
	a := testTreeInit(t)

	// Sanity check
	checkOneProp(t, propTest, oldVal, true)

	ops := []cfgapi.PropertyOp{
		{Op: cfgapi.PropTest, Name: propTest},
		{Op: cfgapi.PropCreate, Name: propTest, Value: newVal1},
	}
	_, err := executeInternal(ops)
	if err != nil {
		t.Errorf("Test had unexpected error %v", err)
	}
	a[propTest] = newVal1
	testValidateTree(t, a)

	// If badPropTest exists (it doesn't), then set propTest (which does
	// exist) to "test2"
	ops = []cfgapi.PropertyOp{
		{Op: cfgapi.PropTest, Name: badPropTest},
		{Op: cfgapi.PropCreate, Name: propTest, Value: newVal2},
	}
	_, err = executeInternal(ops)
	if err != cfgapi.ErrNoProp {
		t.Errorf("Test did not have expected error: %v", err)
	}
	testValidateTree(t, a)
}

// TestTestEqBasic verifies the functionality of ConfigOp_TestEq
func TestTestEqBasic(t *testing.T) {
	testCases := []struct {
		prop string
		val  string
		err  error
	}{
		{"@/clients/00:00:00:00:00:00/dhcp_name", "foo", cfgapi.ErrNoProp},
		{"@/clients/64:9a:be:da:b1:9a/dhcp_name", "", cfgapi.ErrNotEqual},
		{"@/clients/64:9a:be:da:b1:9a/dhcp_name", "badvalue", cfgapi.ErrNotEqual},
		{"@/clients/64:9a:be:da:b1:9a/dhcp_name", "test-client", nil},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("%s==%s", tc.prop, tc.val), func(t *testing.T) {
			a := testTreeInit(t)
			ops := []cfgapi.PropertyOp{
				{Op: cfgapi.PropTestEq, Value: tc.val, Name: tc.prop},
			}
			_, err := executeInternal(ops)
			if err != tc.err {
				t.Errorf("Test had error %#v.  Should have had %#v", err, tc.err)
			}
			testValidateTree(t, a)
		})
	}
}

// TestTestEqCompound verifies the functionality of ConfigOp_TestEq in compound
// operations.
func TestTestEqCompound(t *testing.T) {
	propTest := "@/clients/64:9a:be:da:b1:9a/dhcp_name"
	oldVal := "test-client"
	newVal1 := "test1"
	a := testTreeInit(t)

	ops := []cfgapi.PropertyOp{
		{Op: cfgapi.PropTestEq, Name: propTest, Value: oldVal},
		{Op: cfgapi.PropCreate, Name: propTest, Value: newVal1},
	}
	_, err := executeInternal(ops)
	if err != nil {
		t.Errorf("Test had unexpected error %v", err)
	}
	a[propTest] = newVal1
	testValidateTree(t, a)

	// Do it again-- this time it should fail
	_, err = executeInternal(ops)
	if err != cfgapi.ErrNotEqual {
		t.Errorf("Test had unexpected error %v", err)
	}
	testValidateTree(t, a)
}

// TestBadSSID verifies that we are invoking the context-sensitive handlers
// XXX: todo - add more tests to verify that the remaining handlers are catching
// the errors they are expected to.
func TestBadSSID(t *testing.T) {
	const (
		ssidProp = "@/network/vap/psk/ssid"
		origSSID = "setme"
	)

	nullSSID := ""
	longSSID := "abcdefghijklmnopqrstuvwxyzabcdefghijkl"
	badSSIDs := []string{nullSSID, longSSID}

	a := testTreeInit(t)
	checkLeaf(t, ssidProp, origSSID, a)

	for _, ssid := range badSSIDs {
		updateOneProp(t, ssidProp, ssid, false)
		checkOneProp(t, ssidProp, origSSID, true)
	}

	testValidateTree(t, a)
}

func TestBadDNS(t *testing.T) {
	const (
		dnsProp = "@/clients/64:9a:be:da:b1:9a/dns_name"
	)

	badNames := []string{
		".startswithdot",
		"middle.dot",
		"endswithdot.",
		"illegal^char",
		"has a space",
		"tooLongtooLongtooLongtooLongtooLongtooLongtooLongtooLongtooLongtooLong",
	}

	a := testTreeInit(t)
	for _, name := range badNames {
		insertOneProp(t, dnsProp, name, false)
		testValidateTree(t, a)
	}
}

func TestListType(t *testing.T) {
	const (
		ringProp = "@/policy/site/vpn/server/0/rings"
	)

	goodRings := []string{
		"core",
		"standard",
		"core,standard",
		"core, standard",
		"core,core,standard",
	}

	a := testTreeInit(t)
	for _, ring := range goodRings {
		insertOneProp(t, ringProp, ring, true)
		a[ringProp] = ring
		testValidateTree(t, a)
	}
}

func TestBadListType(t *testing.T) {
	const (
		ringProp = "@/policy/site/vpn/server/0/rings"
	)

	badRings := []string{
		"foo",
		"core,foo,standard",
		"core/standard",
		"core:standard",
		"core,standard,",
		"core,standard,12",
	}

	a := testTreeInit(t)
	for _, ring := range badRings {
		insertOneProp(t, ringProp, ring, false)
		testValidateTree(t, a)
	}
}

// TestMultiInsert inserts multiple values in a single operation
func TestMultiInsert(t *testing.T) {
	ops := []cfgapi.PropertyOp{
		{
			Op:    cfgapi.PropCreate,
			Name:  "@/network/ntpservers/1",
			Value: "time1.google.com",
		},
		{
			Op:    cfgapi.PropCreate,
			Name:  "@/network/ntpservers/2",
			Value: "time2.google.com",
		},

		{
			Op:    cfgapi.PropCreate,
			Name:  "@/network/ntpservers/3",
			Value: "time3.google.com",
		},
		{
			Op:    cfgapi.PropCreate,
			Name:  "@/network/ntpservers/4",
			Value: "time4.google.com",
		},
	}

	a := testTreeInit(t)
	for _, op := range ops {
		a[op.Name] = op.Value
	}

	if _, err := executeInternal(ops); err != nil {
		t.Error(err)
	} else {
		testValidateTree(t, a)
	}
}

// TestMultiMixed inserts, sets, and removes multiple values in a single
// operation
func TestMultiMixed(t *testing.T) {
	ops := []cfgapi.PropertyOp{
		{
			Op:    cfgapi.PropCreate,
			Name:  "@/network/ntpservers/1",
			Value: "time1.google.com",
		},
		{
			Op:    cfgapi.PropCreate,
			Name:  "@/network/ntpservers/2",
			Value: "time2.google.com",
		},

		{
			Op:    cfgapi.PropSet,
			Name:  "@/network/ntpservers/1",
			Value: "time1.google.com",
		},
		{
			Op:   cfgapi.PropDelete,
			Name: "@/network/ntpservers/2",
		},
	}

	a := testTreeInit(t)
	a["@/network/ntpservers/1"] = "time1.google.com"

	if _, err := executeInternal(ops); err != nil {
		t.Error(err)
	} else {
		testValidateTree(t, a)
	}
}

// TestMultiInsertFail inserts multiple legal values and one illegal.  It is
// expected to complete with the config tree unchanged.
func TestMultiInsertFail(t *testing.T) {
	ops := []cfgapi.PropertyOp{
		{
			Op:    cfgapi.PropCreate,
			Name:  "@/network/ntpservers/1",
			Value: "time1.google.com",
		},
		{
			Op:    cfgapi.PropCreate,
			Name:  "@/network/ntpservers/2",
			Value: "time2.google.com",
		},
		{
			// Illegal 'set' operation that should cause the whole
			// transaction to fail.
			Op:    cfgapi.PropSet,
			Name:  "@/uuid",
			Value: "time1.google.com",
		},
		{
			Op:    cfgapi.PropCreate,
			Name:  "@/network/ntpservers/6",
			Value: "time6.google.com",
		},
	}

	a := testTreeInit(t)

	if _, err := executeInternal(ops); err == nil {
		t.Error(err)
	} else {
		testValidateTree(t, a)
	}
}

// TestMultiMixedFail performs a series of legal inserts and deletes, with one
// illegal PropSet included.  It is expected to complete with the config tree
// unchanged.  This is specifically exercising the 'undo' of a subtree deletion.
func TestMultiMixedFail(t *testing.T) {
	ops := []cfgapi.PropertyOp{
		{
			Op:    cfgapi.PropCreate,
			Name:  "@/network/ntpservers/1",
			Value: "time1.google.com",
		},
		{
			Op:    cfgapi.PropCreate,
			Name:  "@/network/ntpservers/2",
			Value: "time2.google.com",
		},

		{
			Op:   cfgapi.PropDelete,
			Name: "@/network",
		},
		{
			// Illegal 'set' operation that should cause the whole
			// transaction to fail.
			Op:    cfgapi.PropSet,
			Name:  "@/branch/nonexistent",
			Value: "time1.google.com",
		},
	}

	a := testTreeInit(t)

	if _, err := executeInternal(ops); err == nil {
		t.Error(err)
	} else {
		testValidateTree(t, a)
	}
}

// TestMultiSetFail performs a series of legal inserts and updates, with one
// illegal PropSet included.  It is expected to complete with the config tree
// unchanged.  This is to verify that a failure causes us to revert to the
// original state - not just the last state.
func TestMultiSetFail(t *testing.T) {
	ops := []cfgapi.PropertyOp{
		{
			Op:    cfgapi.PropCreate,
			Name:  "@/network/ntpservers/1",
			Value: "time1.google.com",
		},
		{
			Op:    cfgapi.PropSet,
			Name:  "@/network/ntpservers/1",
			Value: "time2.google.com",
		},
		{
			Op:    cfgapi.PropSet,
			Name:  "@/network/ntpservers/1",
			Value: "time3.google.com",
		},
		{
			// Illegal 'set' operation that should cause the whole
			// transaction to fail.
			Op:    cfgapi.PropSet,
			Name:  "@/branch/nonexistent",
			Value: "time1.google.com",
		},
		{
			Op:    cfgapi.PropSet,
			Name:  "@/network/ntpservers/1",
			Value: "time4.google.com",
		},
	}

	a := testTreeInit(t)

	if _, err := executeInternal(ops); err == nil {
		t.Error(err)
	} else {
		testValidateTree(t, a)
	}
}

// TestInvalidAccessLevel verifies that requests with invalid levels
// cannot get or manipulate properties
func TestInvalidAccessLevel(t *testing.T) {
	testops := []cfgapi.PropertyOp{
		{Op: cfgapi.PropSet, Name: "@/network/ssid", Value: "test123"},
		{Op: cfgapi.PropGet, Name: "@/network/ssid"},
		{Op: cfgapi.PropDelete, Name: "@/network/ssid"},
		{Op: cfgapi.PropCreate, Name: "@/siteid", Value: "7810.b10e.net"},
	}

	badLevels := []cfgapi.AccessLevel{
		cfgapi.AccessLevel(-1),
		cfgapi.AccessUser + 1,
		cfgapi.AccessInternal + 1,
	}

	ctx := context.Background()
	for _, level := range badLevels {
		for _, x := range testops {
			ops := []cfgapi.PropertyOp{x}
			a := testTreeInit(t)
			_, err := config.ExecuteAt(ctx, ops, level).Wait(ctx)
			if err == nil {
				t.Errorf("%#v at level %d succeeded unexpectedly", x, level)
			} else if !strings.Contains(err.Error(), "invalid access level") {
				t.Errorf("%#v at level %d failed with unexpected error %v", x, level, err)
			}
			testValidateTree(t, a)
		}
	}
}

func TestValidExpansions(t *testing.T) {
	newProps := []string{
		"@/policy/site/scans/udp/period",
		"@/policy/rings/standard/scans/udp/period",
		"@/policy/clients/00:40:54:00:00:2f/scans/udp/period",
	}
	newVal := "10m"

	a := testTreeInit(t)
	for _, prop := range newProps {
		a[prop] = newVal
		insertOneProp(t, prop, newVal, true)
		testValidateTree(t, a)
	}
}

func TestInvalidExpansions(t *testing.T) {
	newProps := []string{
		"@/policy/site/00:40:54:00:00:2f/scans/udp/period",
		"@/policy/rings/site/scans/udp/period",
		"@/policy/rings/scans/udp/period",
		"@/policy/clients/standard/00:40:54:00:00:2f/scans/udp/period",
		"@/policy/00:40:54:00:00:2f/scans/udp/period",
		"@/policy/00:40:54:00:00:2f/clients/scans/udp/period",
		"@/policy/rings/standard/vpn/enabled",
		"@/policy/clients/00:40:54:00:00:2f/vpn/enabled",
	}
	newVal := "10m"

	a := testTreeInit(t)
	for _, prop := range newProps {
		insertOneProp(t, prop, newVal, false)
		testValidateTree(t, a)
	}
}

// TestCacheStale checks to see whether stale data is left in the config tree
// cache after deleting the subtree that includes it.
func TestCacheStale(t *testing.T) {
	const nicsProp = "@/nodes/001-201913ZZ-000039/nics"
	const lan0Prop = nicsProp + "/lan0"
	const ringProp = lan0Prop + "/ring"

	a := testTreeInit(t)

	// Fetch the same subtree a few times to ensure that it's cached
	oldSubtree := fetchSubtree(t, nicsProp, true)
	checkSubtree(t, nicsProp, oldSubtree)
	checkSubtree(t, nicsProp, oldSubtree)
	checkSubtree(t, nicsProp, oldSubtree)
	checkSubtree(t, nicsProp, oldSubtree)
	checkSubtree(t, nicsProp, oldSubtree)

	// Delete the subtree
	deleteOneProp(t, nicsProp, true)

	// Repopulate it, with the ring property changed from 'standard' to
	// 'internal'
	ops := []cfgapi.PropertyOp{
		{
			Op:    cfgapi.PropCreate,
			Name:  lan0Prop + "/kind",
			Value: "wired",
		},
		{
			Op:    cfgapi.PropCreate,
			Name:  lan0Prop + "/mac",
			Value: "60:90:84:a0:00:22",
		},
		{
			Op:    cfgapi.PropCreate,
			Name:  lan0Prop + "/name",
			Value: "lan0",
		},
		{
			Op:    cfgapi.PropCreate,
			Name:  lan0Prop + "/pseudo",
			Value: "false",
		},
		{
			Op:    cfgapi.PropCreate,
			Name:  lan0Prop + "/ring",
			Value: "internal",
		},
	}

	for _, op := range ops {
		a[op.Name] = op.Value
	}

	if _, err := executeInternal(ops); err != nil {
		t.Error(err)
	} else {
		testValidateTree(t, a)
	}

	// Check to see whether the old data is still in the cache
	newSubtree := fetchSubtree(t, nicsProp, true)
	if oldSubtree == newSubtree {
		t.Errorf("Got stale data")
	}
}

func TestMetrics(t *testing.T) {
	const prop = "@/metrics/clients/11:22:33:44:55:66/signal_str"
	const str1 = "-22"
	const str2 = "3"

	tree, err := cfgtree.NewPTree("@/metrics/", nil)
	if err != nil {
		fail("unable to construct @/metrics/: %v", err)
	}

	tree.ChangesetInit()
	if err = tree.Add(prop, str1, nil); err != nil {
		fail("unable to add %s: %v", prop, err)
	}
	tree.ChangesetCommit()

	p, err := tree.GetNode(prop)
	if err != nil {
		fail("unable to get %s: %v", prop, err)
	}
	if val := p.Value; val != str1 {
		fail("%s was %s - expected %s", prop, val, str1)
	}

	tree.ChangesetInit()
	if err = tree.Set(prop, str2, nil); err != nil {
		fail("unable to set %s: %v", prop, err)
	}
	tree.ChangesetCommit()

	p, err = tree.GetNode(prop)
	if err != nil {
		fail("unable to get %s: %v", prop, err)
	}
	if val := p.Value; val != str2 {
		fail("%s was %s - expected %s", prop, val, str2)
	}
}

func TestBaseIndex(t *testing.T) {
	const baseProp = "@/network/base_address"
	const idxProp = "@/site_index"
	const clientIPProp = "@/clients/64:9a:be:da:b1:9a/ipv4"
	const clientIPVal = "192.168.7.8"

	var goodBase = []string{
		"192.168.5.0/24",
		"192.168.42.0/24",
		"172.16.0.0/24",
		"172.16.100.0/24",
		"172.30.100.0/24",
		"172.16.100.0/20",
		"10.0.0.0/12",
		"10.1.0.0/12",
		"10.1.1.0/20",
		"10.100.100.0/20",
	}

	var badBase = []string{
		"192.168.5.0/1",
		"192.168.5.0",
		"192.168.250.0/24",
		"34.82.77.0/24",
		"34.82.77.0/24",
		"a.b.c.d/24",
	}

	var goodIndex = []string{"0", "1", "7", "9", "15"}
	var badIndex = []string{"16", "-1", "a", ""}

	for _, val := range goodBase {
		a := testTreeInit(t)
		updateOneProp(t, baseProp, val, true)
		a[baseProp] = val
		testValidateTree(t, a)
	}

	for _, val := range badBase {
		a := testTreeInit(t)
		updateOneProp(t, baseProp, val, false)
		testValidateTree(t, a)
	}

	for _, val := range goodIndex {
		a := testTreeInit(t)
		updateOneProp(t, idxProp, val, true)
		a[idxProp] = val
		testValidateTree(t, a)
	}

	for _, val := range badIndex {
		a := testTreeInit(t)
		updateOneProp(t, idxProp, val, false)
		testValidateTree(t, a)
	}

	// Insert a static IP assignment and verify that all attempts to change
	// the standard ranges now fail.
	for _, val := range goodBase {
		a := testTreeInit(t)
		updateOneProp(t, clientIPProp, clientIPVal, true)
		updateOneProp(t, baseProp, val, false)
		testValidateTree(t, a)
	}

	for _, val := range goodIndex {
		if val == "0" {
			// this is a no-op, and should succeed even with a
			// static IP.
			continue
		}
		a := testTreeInit(t)
		updateOneProp(t, clientIPProp, clientIPVal, true)
		updateOneProp(t, idxProp, val, false)
		testValidateTree(t, a)
	}

}

func TestRingSubnets(t *testing.T) {
	const standardSubnetProp = "@/rings/standard/subnet"
	const guestSubnetProp = "@/rings/guest/subnet"
	const clientIPProp = "@/clients/64:9a:be:da:b1:9a/ipv4"
	const clientIPVal = "192.168.7.8"

	var goodVal = []string{
		"192.168.1.0/24",
		"192.168.12.0/24",
		"192.168.253.0/24",
		"10.0.0.0/20",
	}

	var badVal = []string{
		"192.168.8.0/24", // conflicts with quarantine ring
		"34.82.77.1/24",  // not a private subnet
		"34.82.77.1",     // not a subnet
		"a.b.c.d/24",     // invalid syntax
	}

	for _, val := range goodVal {
		a := testTreeInit(t)
		insertOneProp(t, standardSubnetProp, val, true)
		a[standardSubnetProp] = val
		testValidateTree(t, a)
	}

	for _, val := range badVal {
		a := testTreeInit(t)
		insertOneProp(t, standardSubnetProp, val, false)
		testValidateTree(t, a)
	}

	// Insert a static guest IP assignment and verify that all attempts to
	// change the standard subnet still succeed.
	for _, val := range goodVal {
		a := testTreeInit(t)
		updateOneProp(t, clientIPProp, clientIPVal, true)
		insertOneProp(t, standardSubnetProp, val, true)
		a[standardSubnetProp] = val
		testValidateTree(t, a)
	}

	// Insert a static guest IP assignment and verify that all attempts to
	// change the guest subnet now fail.
	for _, val := range goodVal {
		a := testTreeInit(t)
		updateOneProp(t, clientIPProp, clientIPVal, true)
		insertOneProp(t, guestSubnetProp, val, false)
		testValidateTree(t, a)
	}
}

func TestMain(m *testing.M) {
	var err error
	slog = aputil.NewLogger(pname)

	_, descriptions, err := loadDefaults()
	if err != nil {
		fail("Unable to load defaults %v", err)
	}

	if err = validationInit(descriptions); err != nil {
		fail("Validation init failed: %v\n", err)
	}

	metricsInit()
	testData, err = ioutil.ReadFile(*testFile)
	if err != nil {
		log.Printf("Failed to load properties file %s: %v\n",
			*testFile, err)
		os.Exit(1)
	}

	go func() {
		// When testing, we don't have a go routine periodically flushing
		// the tree to disk and monitoring the trigger channel.  The
		// following loop absorbs signals to the channel, preventing the
		// tests from blocking.
		for {
			<-propTreeStoreTrigger
		}
	}()

	brokerd = new(broker.Broker)
	os.Exit(m.Run())
}

