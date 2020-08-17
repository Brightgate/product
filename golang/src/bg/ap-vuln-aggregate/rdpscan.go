//
// Copyright 2020 Brightgate Inc.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//


package main

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"bg/ap_common/aputil"
	"bg/ap_common/apvuln"
)

const rdpscanCmd = "/usr/sbin/rdpscan"

// 127.0.0.1 - UNKNOWN - no connection - refused (RST)
const rdpPattern = `^(?P<ipaddr>[0-9\.]+) - (?P<state>[A-Z]+) - (?P<details>.*)$`

// From https://github.com/robertdavidgraham/rdpscan/README.md:
//
//     SAFE - if target has determined bot be patched or at least require CredSSP/NLA
//     VULNERABLE - if the target has been confirmed to be vulnerable
//     UNKNOWN - if the target doesn't respond or has some protocol failure

func rdpscanEval(results string) (bool, string, error) {
	rdpRe := regexp.MustCompile(rdpPattern)

	resultf, err := os.Open(results)
	if err != nil {
		err = fmt.Errorf("failed to load open results file '%s': %v",
			results, err)
		return false, "", err
	}

	scanner := bufio.NewScanner(resultf)

	lc := 0
	vulns := make(apvuln.Vulnerabilities, 0)

	for scanner.Scan() {
		line := scanner.Text()

		if lc > 0 {
			log.Printf("unexpected output line %d: '%s'", lc, line)
			continue
		}

		match := rdpRe.FindAllStringSubmatch(line, -1)

		for _, m := range match {
			if m[2] == "VULNERABLE" {
				// append to vulnerability array
				vulns = append(vulns, apvuln.RDPVulnerability{
					IP:      m[1],
					Port:    "3389",
					Details: m[3]})
			}
		}

		lc++
	}

	if len(vulns) == 0 {
		return false, apvuln.MarshalNotVulnerable("not vulnerable"), nil
	}

	var jsonVulns []byte

	if jsonVulns, err = apvuln.MarshalVulns(vulns); err != nil {
		aputil.Fatalf("Couldn't marshal vulns: %v\n", vulns)
	}

	return true, string(jsonVulns), nil
}

// This implementation of an RDP vulnerability probe only scans for the
// default port.  We could repeat the probe with distinct ports, if we
// find that deployments typically use other ports for RDP on a regular
// basis.
func rdpscanVuln(v aggVulnDescription, tgt net.IP) (bool, string, error) {
	resFile, err := ioutil.TempFile("", "rdpscan.")
	if err != nil {
		return false, "", fmt.Errorf("failed to create result file: %v", err)
	}
	resFileName := resFile.Name()
	defer os.Remove(resFileName)

	args := []string{}

	options, ok := v.Options["raw"]
	if ok {
		args = append(args, strings.Split(options, " ")...)
	}

	args = append(args, tgt.String())

	aputil.Errorf("rdpscanVuln running command: %s %s\n", rdpscanCmd, args)

	cmd := exec.Command(rdpscanCmd, args...)

	cmd.Stdout = resFile

	if err = cmd.Start(); err != nil {
		return false, "", fmt.Errorf("scan failed: %v", err)
	}

	cmd.Wait()

	vuln, details, err := rdpscanEval(resFileName)
	if err != nil {
		err = fmt.Errorf("evaluation failed: %v", err)
	}

	return vuln, details, err
}

func init() {
	addTool("rdpscan", rdpscanVuln)
}

