/*
 * Copyright 2019 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package main

import (
	"os"
	"testing"

	"bg/ap_common/aputil"
	"bg/common/cfgapi"
)

type testCase struct {
	in       string
	expected string
	parse    bool
	build    bool
}

var testCases = []testCase{
	{
		in:       "ACCEPT",
		expected: "",
		parse:    false,
	},
	{
		in:       "ACCEPT FROM RING unknown",
		expected: "",
		parse:    true,
		build:    false, // bad ring name
	},
	{
		in:       "ACCEPT FROM RING core",
		expected: "-A FORWARD  -i brvlan3  -j ACCEPT",
		parse:    true,
		build:    true,
	},
	{
		in:       "ACCEPT TO AP FROM RING core",
		expected: "",
		parse:    false, // TO must be after FROM
		build:    false,
	},
	{
		in:       "ACCEPT FROM RING core TO AP",
		expected: "-A INPUT  -i brvlan3  -j ACCEPT",
		parse:    true,
		build:    true,
	},
	{
		in:       "BLOCK FROM RING devices TO RING core",
		expected: "-A FORWARD  -i brvlan5  -o brvlan3  -j dropped",
		parse:    true,
		build:    true,
	},
	{
		in:       "ACCEPT TO AP DPORTS 22",
		expected: "-A INPUT --dport 22 -j ACCEPT",
		parse:    true,
		build:    true,
	},
	{
		in:       "ACCEPT TO AP DPORTS 222222",
		expected: "",
		parse:    false, // invalid port number
		build:    false,
	},
	{
		in:       "ACCEPT UDP FROM RING devices PORTS 1",
		expected: "",
		parse:    false, // bad keyword: PORTS
		build:    false,
	},
	{
		in:       "ACCEPT UDP FROM RING devices SPORTS 1",
		expected: "-A FORWARD  -p udp -i brvlan5 --sport 1 -j ACCEPT",
		parse:    true,
		build:    true,
	},
	{
		in:       "ACCEPT UDP FROM RING devices SPORTS 1 2 3 4",
		expected: "-A FORWARD  -p udp -i brvlan5 -m multiport --sports 1,2,3,4 -j ACCEPT",
		parse:    true,
		build:    true,
	},
	{
		in:       "ACCEPT UDP FROM RING devices SPORTS 1 2 SPORTS 1000:1100",
		expected: "-A FORWARD  -p udp -i brvlan5 -m multiport --sports 1,2,1000:1100 -j ACCEPT",
		parse:    true,
		build:    true,
	},
	{
		in:       "ACCEPT UDP FROM RING devices SPORTS 49152:65535 DPORTS 49152:65535",
		expected: "-A FORWARD  -p udp -i brvlan5 --sport 49152:65535 --dport 49152:65535 -j ACCEPT",
		parse:    true,
		build:    true,
	},
}

func testOne(t *testing.T, test *testCase) {
	r, err := parseRule(test.in)

	if err != nil {
		if test.parse {
			t.Errorf("%s failed to parse: %v", test.in, err)
		}
		return
	} else if !test.parse {
		t.Errorf("%s should not have been parsed", test.in)
		return
	}

	chain, line, err := buildRule(r)
	if err != nil {
		if test.build {
			t.Errorf("%s failed to build: %v", test.in, err)
		}
	} else if !test.build {
		t.Errorf("%s should not have been built", test.in)
	} else {
		line = "-A " + chain + " " + line

		if line != test.expected {
			t.Errorf("%s \n  got:\n\t%s\n  expected:\n\t%s",
				test.in, line, test.expected)
		}
	}
}

func TestAll(t *testing.T) {
	for _, tc := range testCases {
		testOne(t, &tc)
	}
}

func buildRing(subnet, bridge string) *cfgapi.RingConfig {
	return &cfgapi.RingConfig{
		Subnet: subnet,
		Bridge: bridge,
	}
}

func buildRings() {
	rings = make(cfgapi.RingMap)

	rings["internal"] = buildRing("192.168.147.0/24", "brvlan1")
	rings["core"] = buildRing("192.168.148.0/24", "brvlan3")
	rings["standard"] = buildRing("192.168.149.0/24", "brvlan4")
	rings["devices"] = buildRing("192.168.150.0/24", "brvlan5")
}

func TestMain(m *testing.M) {
	slog = aputil.NewLogger(pname)

	buildRings()
	os.Exit(m.Run())
}

