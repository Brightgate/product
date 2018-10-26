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

	"bg/common/cfgapi"
	"bg/common/cfgmsg"
	"bg/common/cfgtree"
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

func execute(level cfgapi.AccessLevel, ops []cfgapi.PropertyOp) (string, error) {
	var rval string
	var err error

	query, err := cfgmsg.NewPropQuery(ops)
	query.Level = int32(level)
	if err == nil {
		response := processOneEvent(query)
		if response.Response != cfgmsg.ConfigResponse_OK {
			err = fmt.Errorf("%s", response.Errmsg)
		} else if ops[0].Op == cfgapi.PropGet {
			rval = response.Value
		}
	}

	return rval, err
}

func executeInternal(ops []cfgapi.PropertyOp) (string, error) {
	return execute(cfgapi.AccessInternal, ops)
}

func executeUser(ops []cfgapi.PropertyOp) (string, error) {
	return execute(cfgapi.AccessUser, ops)
}

// utility function to attempt the 'get' of a single property.  The caller
// decides whether the operation should succeed or fail.
func checkOneProp(t *testing.T, prop, val string, succeed bool) {
	ops := []cfgapi.PropertyOp{
		{Op: cfgapi.PropGet, Name: prop},
	}

	treeVal, err := executeInternal(ops)
	if err != nil {
		if succeed {
			t.Errorf("Failed to get %s: %v", prop, err)
		}
	} else if !succeed {
		t.Errorf("Got %s -> %s.  Expected failure.", prop, treeVal)
	} else {
		var node cfgtree.PNode
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
		t.Errorf("Inserted %s.  Should have failed", prop)
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
	tree, err := cfgtree.NewPTree("@", testData)
	if err != nil {
		t.Fatalf("failed to import config tree: %v", err)
	}
	propTree = tree
	versionTree()

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
	query := cfgmsg.NewPingQuery()
	response := processOneEvent(query)
	if response.Response != cfgmsg.ConfigResponse_OK {
		t.Error(fmt.Errorf("%s", response.Value))
	}
}

func testPingBadVersion(version int32) error {
	query := cfgmsg.NewPingQuery()
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
		newProp = "@/siteid"
		newVal  = "7810"
	)

	a := testTreeInit(t)
	updateOneProp(t, newProp, newVal, false)
	testValidateTree(t, a)
}

// TestAddProp verifies that we can successfully add a single property
func TestAddProp(t *testing.T) {
	const (
		newProp = "@/siteid"
		newVal  = "7810"
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
		nonProp = "@/siteid"
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
		{Op: cfgapi.PropCreate, Name: "@/siteid", Value: "7810"},
	}

	badLevels := []cfgapi.AccessLevel{
		cfgapi.AccessLevel(-1),
		cfgapi.AccessUser + 1,
		cfgapi.AccessInternal + 1,
	}

	for _, level := range badLevels {
		for _, x := range testops {
			ops := []cfgapi.PropertyOp{x}
			a := testTreeInit(t)
			if _, err := execute(level, ops); err == nil {
				t.Errorf("%#v at level %d succeeded unexpectedly", x, level)
			} else if !strings.Contains(err.Error(), "invalid access level") {
				t.Errorf("%#v at level %d failed with unexpected error %v", x, level, err)
			}
			testValidateTree(t, a)
		}
	}
}

func TestMain(m *testing.M) {
	var err error

	*propdir = "."
	_, descriptions, err := loadDefaults()
	if err != nil {
		fail("Unable to load defaults %v", err)
	}

	if err = validationInit(descriptions); err != nil {
		fail("Validation init failed: %v\n", err)
	}

	prometheusInit()
	testData, err = ioutil.ReadFile(*testFile)
	if err != nil {
		log.Printf("Failed to load properties file %s: %v\n",
			*testFile, err)
		os.Exit(1)
	}

	os.Exit(m.Run())
}
