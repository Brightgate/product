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
	"bg/common/cfgapi"
	"testing"
)

type validationTest struct {
	name     string
	goodVals []string
	badVals  []string
	testFunc typeValidate
}

var (
	allTests = []validationTest{
		{
			name:     "int",
			goodVals: []string{"0", "1", "32335439", "-484"},
			badVals:  []string{"a", "aaa", "1.", "1.1"},
			testFunc: validateInt,
		},
		{
			name:     "float",
			goodVals: []string{"0", "1", "1.0", "0.1", ".1"},
			badVals:  []string{"a", "aaa", "1.1.1"},
			testFunc: validateFloat,
		},
		{
			name:     "bool",
			goodVals: []string{"true", "false", "TRUE", "False"},
			badVals:  []string{"a", "aaa", "no", "yes", ""},
			testFunc: validateBool,
		},
		{
			name: "time",
			goodVals: []string{
				"2018-07-05T10:53:35.792659107-07:00",
				"2018-07-05T10:53:35-07:00",
				"01-02-15:04-2006",
				"Jan 2 15:04 2006",
			},
			badVals:  []string{"a", "aaa", "no", "yes", ""},
			testFunc: validateTime,
		},
		{
			name: "uuid",
			goodVals: []string{
				"00000000-0000-0000-0000-000000000000",
				"a0e3488a-85ce-44ce-ba96-9b30c635e0a2",
				"A0E3488A-85CE-44CE-BA96-9B30C635E0A2",
			},
			badVals: []string{"a", "aaa", "no", "yes", "",
				"0e3488a-85ce-44ce-ba96-9b30c635e0a2",
				"0a0e3488a-85ce-44ce-ba96-9b30c635e0a2",
				"a0e3488a.85ce.44ce.ba96.9b30c635e0a2",
				"z0e3488a-85ce-44ce-ba96-9b30c635e0a2",
			},
			testFunc: validateUUID,
		},
		{
			name: "ring",
			goodVals: []string{
				"unenrolled", "core", "standard", "devices",
				"guest", "quarantine", "internal",
			},
			badVals: []string{"a", "aaa", "no", "yes", "lan",
				"c0re"},
			testFunc: validateRing,
		},
		{
			name: "macaddr",
			goodVals: []string{
				"70:88:6b:82:60:68",
				"9e:ef:d5:fe:cc:f0",
			},
			badVals: []string{"a", "aaa", "no", "yes", "",
				"88:6b:82:60:68",
				":70:88:6b:82:60:68",
				"70:70:88:6b:82:60:68",
				"70:88:6b:82:60:GG",
			},
			testFunc: validateMac,
		},
		{
			name: "cidr",
			goodVals: []string{
				"192.0.2.0/24",
				"192.0.2.1/32",
			},
			badVals: []string{"a", "aaa", "no", "yes", "",
				"512.0.2.0/24",
				"192.0.2.0/33",
				"192.0.2/24",
			},
			testFunc: validateCIDR,
		},
		{
			name: "hostname",
			goodVals: []string{
				"test", "test0", "test_0", "test-0", "test_",
				"_test", "_test_",
			},
			badVals: []string{
				"test^", "te^st", "test-0-", "-test",
			},
			testFunc: validateHostname,
		},
		{
			name:     "ipoptport",
			goodVals: []string{"192.168.1.1", "192.168.1.1:53"},
			badVals: []string{
				"192.168.1",
				"192.168.1:53",
				"hostname",
				"hostname:53",
				"hostname.domain.com",
				"8.8.8.8:53:53",
				"53:8.8.8.8",
				"8.8.8.8:123456",
				"",
			},
			testFunc: validateIPOptPort,
		},
	}
)

func testOneType(t *testing.T, x validationTest) {
	for _, val := range x.goodVals {
		if err := x.testFunc(val); err != nil {
			t.Errorf("%s is incorrectly flagged as bad %s: %v\n",
				val, x.name, err)
		}
	}

	for _, val := range x.badVals {
		if err := x.testFunc(val); err == nil {
			t.Errorf("%s is incorrectly identified as good %s\n",
				val, x.name)
		}
	}
}

func TestValidation(t *testing.T) {
	for _, test := range allTests {
		testOneType(t, test)
	}
}

func TestValidationTree(t *testing.T) {
	tests := []struct {
		oldProp string
		newProp string
		fail    bool
	}{
		{
			oldProp: "@/some/%string%",
			newProp: "@/some/realstring",
			fail:    true,
		},
		{
			oldProp: "@/nodes/%uuid%/nics/%nic%/disabled",
			newProp: "@/nodes/%uuid%/platform",
			fail:    false,
		},
		{
			oldProp: "@/some/%bool%",
			newProp: "@/some/false",
			fail:    true,
		},
		{
			oldProp: "@/some/%bool%",
			newProp: "@/some/otherstring",
			fail:    false,
		},
	}

	newVRoot := func() *vnode {
		return &vnode{
			path:     "@",
			keyType:  "const",
			keyText:  "@",
			level:    cfgapi.AccessInternal,
			valType:  "none",
			children: make(map[string]*vnode),
		}
	}

	runTest := func(t *testing.T, prop1, prop2 string, fail bool) {
		vRoot = newVRoot()
		_, _ = newVnode(prop1)
		_, err := newVnode(prop2)
		if fail && err == nil {
			t.Errorf("%s and %s conflict, but newVnode() didn't find it",
				prop1, prop2)
		}
		if !fail && err != nil {
			t.Errorf("%s and %s don't conflict, but newVnode() thought they did",
				prop1, prop2)
		}
	}

	for _, test := range tests {
		runTest(t, test.oldProp, test.newProp, test.fail)
		runTest(t, test.newProp, test.oldProp, test.fail)
	}
}
