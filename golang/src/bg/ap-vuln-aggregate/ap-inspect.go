//
// Copyright 2018 Brightgate Inc.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//


package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"strings"

	"bg/ap_common/aputil"
	"bg/ap_common/apvuln"
)

const inspectCmd = "ap-inspect"

func bgEval(name string) (bool, string, error) {
	var v apvuln.InspectVulnProbe

	vuln := false
	file, err := ioutil.ReadFile(name)
	if err != nil {
		err = fmt.Errorf("failed to read '%s': %v", name, err)
	} else if err = json.Unmarshal(file, &v); err != nil {
		err = fmt.Errorf("unmarshal of '%s' failed: %v", name, err)
	} else {
		vuln = v.Vulnerable
	}

	var jsonVulns []byte
	if jsonVulns, err = apvuln.MarshalVulns(v.Vulns); err != nil {
		err = fmt.Errorf("bgEval: marshaling vulns failed: %v", err)
	}

	return vuln, string(jsonVulns), err
}

func bgVuln(v aggVulnDescription, tgt net.IP) (vuln bool, details string, err error) {
	resFile, err := ioutil.TempFile("", "ap-inspect.")
	if err != nil {
		err = fmt.Errorf("failed to create result file: %v", err)
		return
	}
	resName := resFile.Name()
	defer os.Remove(resName)

	probe, ok := v.Options["probe"]
	if !ok {
		err = fmt.Errorf("no probe specified")
		return
	}

	details = ""

	cmd := []string{"-i", tgt.String(), "-n", probe, "-o", resName}
	if len(v.Ports) > 0 {
		portlist := strings.Join(v.Ports, ",")
		cmd = append(cmd, "-p", portlist)
	}

	aputil.Errorf("bgVuln running %s %v\n", inspectCmd, cmd)
	if err = exec.Command(inspectCmd, cmd...).Run(); err != nil {
		err = fmt.Errorf("scan failed: %v", err)
		return
	}

	if vuln, details, err = bgEval(resName); err != nil {
		err = fmt.Errorf("evaluation failed: %v", err)
	}
	return
}

func init() {
	addTool("ap-inspect", bgVuln)
}

