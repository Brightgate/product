/*
 * COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 *
 */

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"strings"

	"bg/ap_common/aputil"
	"bg/ap_common/apvuln"
)

const pname = "ap-vuln-aggregate"

var (
	help     = flag.Bool("h", false, "get help")
	ipaddr   = flag.String("i", "", "IP to probe")
	vulnlist = flag.String("d", "", "vulnerability list")
	outfile  = flag.String("o", "", "output file")
	tests    = flag.String("t", "", "tests to (not) run")
	services = flag.String("services", "", "services from nmap scan")
	tools    = make(map[string]execFunc)

	allTests map[string]aggVulnDescription
)

type aggVulnDescription struct {
	Tool     string
	Nickname string            `json:"Nickname,omitempty"`
	Ports    []string          `json:"Ports,omitempty"`
	Options  map[string]string `json:"Options,omitempty"`
}

type execFunc func(aggVulnDescription, net.IP) (bool, string, error)

func addTool(name string, exec execFunc) {
	tools[name] = exec
}

func vulnDBLoad(name string) (map[string]aggVulnDescription, error) {
	vulns := make(map[string]aggVulnDescription, 0)

	file, err := ioutil.ReadFile(name)
	if err != nil {
		return nil, fmt.Errorf("file read failed: %v", err)
	}

	err = json.Unmarshal(file, &vulns)
	if err != nil {
		return nil, fmt.Errorf("json import failed: %v", err)
	}

	return vulns, nil
}

func testOne(name string, desc aggVulnDescription, ip net.IP) apvuln.TestResult {
	var (
		err     error
		vuln    bool
		show    string
		details string
	)

	if desc.Nickname == "" {
		show = name
	} else {
		show = desc.Nickname
	}
	aputil.Errorf("Testing for %s %s...\n", desc.Nickname, name)

	if desc.Tool == "ap-defaultpass" && len(*services) > 0 {
		desc.Options["services"] = *services
	}

	if tool, ok := tools[desc.Tool]; ok {
		vuln, details, err = tool(desc, ip)
		if err != nil {
			fmt.Printf("%s test failed: %v\n", show, err)
		} else if vuln {
			fmt.Printf("  vulnerable\n%s\n", details)
		} else {
			fmt.Printf("  not vulnerable\n%s\n", details)
		}
	} else {
		aputil.Errorf("%s: no support for '%s' tool\n", name, desc.Tool)
	}

	var detailsMap map[string]interface{}
	err = json.Unmarshal([]byte(details), &detailsMap)
	if err != nil {
		aputil.Errorf("Couldn't unmarshal for %s:\n%s\n", show, details)
	}

	return apvuln.TestResult{Vuln: vuln, Tool: desc.Tool, Name: name,
		Nickname: desc.Nickname, Details: detailsMap}
}

func output(found map[string]apvuln.TestResult) {
	if *outfile != "" {
		s, err := json.MarshalIndent(found, "", "  ")
		if err != nil {
			aputil.Fatalf("Failed to marshal results: %v\n", err)
		}

		err = ioutil.WriteFile(*outfile, s, 0644)
		if err != nil {
			aputil.Fatalf("Failed to write results file '%s': %v\n",
				*outfile, err)
		}
	} else {
		fmt.Printf("vulnerabilities: ")
		if len(found) == 0 {
			fmt.Printf("None")
		}
		spacer := ""
		for name, result := range found {
			if result.Vuln {
				fmt.Printf(spacer + name)
				spacer = " "
			}
		}
		fmt.Printf("\n")
	}
}

func buildTestSet() []string {

	include := make(map[string]bool)
	skip := make(map[string]bool)
	testSet := make([]string, 0)
	badNames := make([]string, 0)

	for _, i := range strings.Split(*tests, ",") {
		var name string

		if len(i) == 0 {
			continue
		}

		if string(i[0]) == "!" {
			name = i[1:]
			skip[name] = true
		} else {
			name = i
			include[name] = true
		}

		if _, ok := allTests[name]; !ok {
			badNames = append(badNames, name)
		}
	}

	if len(skip) > 0 && len(include) > 0 {
		aputil.Fatalf("tests can be included or skipped - not both\n")
	}

	if len(badNames) > 0 {
		aputil.Fatalf("unknown tests: %s\n", strings.Join(badNames, ","))
	}

	if len(include) > 0 {
		for name := range include {
			testSet = append(testSet, name)
		}
	} else {
		for name := range allTests {
			if !skip[name] {
				testSet = append(testSet, name)
			}
		}
	}

	return testSet
}

func usage() {
	aputil.Errorf("usage: %s [-h] [-o <output file>] [ -t <testlist> "+
		" -d <vuln db> -i <ip>\n", pname)
}

func main() {
	var err error

	flag.Parse()

	if *help || *ipaddr == "" || *vulnlist == "" {
		usage()
		os.Exit(1)
	}

	ip := net.ParseIP(*ipaddr)
	if ip == nil {
		aputil.Fatalf("'%s' is not a valid IP address\n", *ipaddr)
	}

	allTests, err = vulnDBLoad(*vulnlist)
	if err != nil {
		aputil.Fatalf("Unable to import vulnerability list '%s': %v\n",
			*vulnlist, err)
	}
	testSet := buildTestSet()

	found := make(map[string]apvuln.TestResult)
	for _, n := range testSet {
		if result := testOne(n, allTests[n], ip); result.Vuln {
			found[n] = result
		}
	}

	output(found)
	os.Exit(0)
}
