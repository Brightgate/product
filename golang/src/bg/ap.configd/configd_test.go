/*
 * COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
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
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strings"
	"testing"

	"bg/ap_common/apcfg"
	"bg/base_msg"

	"github.com/golang/protobuf/proto"
)

var (
	testFile = flag.String("testdata", "testdata/ap_testing.json",
		"path to an initial config tree")
	testData []byte
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

func execute(ops []apcfg.PropertyOp) (string, error) {
	var rval string
	var err error

	query, err := apcfg.GeneratePropQuery(ops)
	if err == nil {
		response := processOneEvent(query)
		if *response.Response != base_msg.ConfigResponse_OK {
			err = fmt.Errorf("%s", *response.Value)
		} else if ops[0].Op == apcfg.PropGet {
			rval = *response.Value
		}
	}

	return rval, err
}

// utility function to attempt the 'get' of a single property.  The caller
// decides whether the operation should succeed or fail.
func checkOneProp(t *testing.T, prop, val string, succeed bool) {
	ops := []apcfg.PropertyOp{
		{Op: apcfg.PropGet, Name: prop},
	}

	treeVal, err := execute(ops)
	if err != nil {
		if succeed {
			t.Errorf("Failed to get %s: %v", prop, err)
		}
	} else if !succeed {
		t.Errorf("Got %s -> %s.  Expected failure.", prop, treeVal)
	} else {
		var node pnode
		if err = json.Unmarshal([]byte(treeVal), &node); err != nil {
			t.Errorf("Failed to decode %s: %v", prop, err)
		} else if node.Value != val {
			t.Errorf("Got %s -> %s.  Expected '%s'", prop,
				node.Value, val)
		}
	}
}

// utility function to change a single property setting and validate the result
func updateOneProp(t *testing.T, prop, val string, succeed bool) {
	ops := []apcfg.PropertyOp{
		{Op: apcfg.PropSet, Name: prop, Value: val},
	}

	if _, err := execute(ops); err != nil {
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
	ops := []apcfg.PropertyOp{
		{Op: apcfg.PropCreate, Name: prop, Value: val},
	}

	if _, err := execute(ops); err != nil {
		if succeed {
			t.Errorf("Failed to insert %s: %v", prop, err)
		}
	} else if succeed {
		checkOneProp(t, prop, val, true)
	} else {
		t.Errorf("Inserted %s.  Should have failed", prop)
	}
}

// utility function to remove a single new property and validate the result
func deleteOneProp(t *testing.T, prop string, succeed bool) {
	ops := []apcfg.PropertyOp{
		{Op: apcfg.PropDelete, Name: prop},
	}
	if _, err := execute(ops); err != nil {
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
	if err := propTreeImport(testData); err != nil {
		t.Fatalf("failed to import config tree: %v", err)
	}

	return testBuildMap()
}

func addLeaves(leaves leafMap, node *pnode) {
	if len(node.Children) == 0 {
		leaves[node.path] = node.Value
	} else {
		for _, n := range node.Children {
			addLeaves(leaves, n)
		}
	}
}

// return a map containing all of the leaf paths and their values
func testBuildMap() leafMap {
	leaves := make(leafMap)

	addLeaves(leaves, &propTreeRoot)

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
	testCompareMaps(t, expected, testBuildMap())
}

// TestNull is a sanity test of the testCompareMaps function.  It ensures that
// two comparisons of the same data succeed.
func TestNull(t *testing.T) {
	a := testTreeInit(t)
	b := testBuildMap()
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
	query := apcfg.GeneratePingQuery()
	response := processOneEvent(query)
	if *response.Response != base_msg.ConfigResponse_OK {
		t.Error(fmt.Errorf("%s", *response.Value))
	}
}

func testPingBadVersion(version int32) error {
	query := apcfg.GeneratePingQuery()
	majorMinor := base_msg.Version{Major: proto.Int32(version)}
	query.Version = &majorMinor

	response := processOneEvent(query)
	if *response.Response == base_msg.ConfigResponse_OK {
		return fmt.Errorf("configd of version %d accepted version %d",
			apcfg.Version, version)
	} else if *response.Response != base_msg.ConfigResponse_BADVERSION {
		return fmt.Errorf("unexpected error: %d", *response.Response)
	}

	return nil
}

// TestOlderVersion verifies that configd will correctly refuse to execute a
// command with an newer version
func TestOlderVersion(t *testing.T) {
	if err := testPingBadVersion(apcfg.Version - 1); err != nil {
		t.Error(err)
	}
}

// TestNewerVersion verifies that configd will correctly refuse to execute a
// command with an old version
func TestNewerVersion(t *testing.T) {
	if err := testPingBadVersion(apcfg.Version + 1); err != nil {
		t.Error(err)
	}
}

// TestChangeProp verifies that we can successfully change a single property
func TestChangeProp(t *testing.T) {
	const (
		ssidProp = "@/network/ssid"
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
		newProp = "@/foo"
		newVal  = "new property value"
	)

	a := testTreeInit(t)
	updateOneProp(t, newProp, newVal, false)
	testValidateTree(t, a)
}

// TestAddProp verifies that we can successfully add a single property
func TestAddProp(t *testing.T) {
	const (
		newProp = "@/foo"
		newVal  = "new property value"
	)

	a := testTreeInit(t)
	a[newProp] = newVal
	insertOneProp(t, newProp, newVal, true)
	testValidateTree(t, a)
}

// TestDeleteNonProp verifies that we can't remove a non-existent property
func TestDeleteNonProp(t *testing.T) {
	const (
		nonProp = "@/foo"
	)

	a := testTreeInit(t)
	deleteOneProp(t, nonProp, false)
	testValidateTree(t, a)
}

// TestDeleteProp verifies that we can successfully remove a single property
func TestDeleteProp(t *testing.T) {
	const (
		delProp = "@/network/ssid"
	)

	a := testTreeInit(t)
	delete(a, delProp)
	deleteOneProp(t, delProp, true)
	testValidateTree(t, a)
}

// TestAddNestedProp verifies that we can successfully add a new property
// multiple levels deep
func TestAddNestedProp(t *testing.T) {
	const (
		newProp = "@/foo/bar/baz"
		newVal  = "new property value"
	)

	a := testTreeInit(t)
	a[newProp] = newVal
	insertOneProp(t, newProp, newVal, true)
	testValidateTree(t, a)
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

// TestBadSSID verifies that we are invoking the context-sensitive handlers
// XXX: todo - add more tests to verify that the remaining handlers are catching
// the errors they are expected to.
func TestBadSSID(t *testing.T) {
	const (
		ssidProp = "@/network/ssid"
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

// TestMultiInsert inserts multiple values in a single operation
func TestMultiInsert(t *testing.T) {
	ops := []apcfg.PropertyOp{
		{
			Op:    apcfg.PropCreate,
			Name:  "@/branch/subbranch/propa",
			Value: "valuea",
		},
		{
			Op:    apcfg.PropCreate,
			Name:  "@/branch/subbranch/propb",
			Value: "valueb",
		},

		{
			Op:    apcfg.PropCreate,
			Name:  "@/branch/subbranch/propc",
			Value: "valuec",
		},
		{
			Op:    apcfg.PropCreate,
			Name:  "@/branch/subbranch/propf",
			Value: "valued",
		},
	}

	a := testTreeInit(t)
	for _, op := range ops {
		a[op.Name] = op.Value
	}

	if _, err := execute(ops); err != nil {
		t.Error(err)
	} else {
		testValidateTree(t, a)
	}
}

// TestMultiMixed inserts, sets, and removes multiple values in a single
// operation
func TestMultiMixed(t *testing.T) {
	ops := []apcfg.PropertyOp{
		{
			Op:    apcfg.PropCreate,
			Name:  "@/branch/subbranch/propa",
			Value: "valuea",
		},
		{
			Op:    apcfg.PropCreate,
			Name:  "@/branch/subbranch/propb",
			Value: "valueb",
		},

		{
			Op:    apcfg.PropSet,
			Name:  "@/branch/subbranch/propa",
			Value: "valuec",
		},
		{
			Op:   apcfg.PropDelete,
			Name: "@/branch/subbranch/propb",
		},
	}

	a := testTreeInit(t)
	a["@/branch/subbranch/propa"] = "valuec"

	if _, err := execute(ops); err != nil {
		t.Error(err)
	} else {
		testValidateTree(t, a)
	}
}

func TestMain(m *testing.M) {
	var err error

	testData, err = ioutil.ReadFile(*testFile)
	if err != nil {
		log.Printf("Failed to load properties file %s: %v\n",
			*testFile, err)
		os.Exit(1)
	}

	os.Exit(m.Run())
}
