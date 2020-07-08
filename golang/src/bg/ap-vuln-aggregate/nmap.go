//
// COPYRIGHT 2018 Brightgate Inc. All rights reserved.
//
// This copyright notice is Copyright Management Information under 17 USC 1202
// and is included to protect this work and deter copyright infringement.
// Removal or alteration of this Copyright Management Information without the
// express written permission of Brightgate Inc is prohibited, and any
// such unauthorized removal or  alteration will be a violation of federal law.
//

package main

import (
	"encoding/xml"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"bg/ap_common/aputil"
	"bg/ap_common/apvuln"
)

const nmapCmd = "/usr/bin/nmap"

type nmapResult struct {
	HostResults []hostResult `xml:"host"`
}

type hostResult struct {
	AddressResults []addressResult `xml:"address"`
	PortResults    []portResult    `xml:"ports>port"`
	ScriptResults  []scriptResult  `xml:"hostscript>script"`
}

type addressResult struct {
	Address     string `xml:"addr,attr"`
	AddressType string `xml:"addrtype,attr"`
}

type portResult struct {
	Protocol      string         `xml:"protocol,attr"`
	PortID        int            `xml:"portid,attr"`
	ScriptResults []scriptResult `xml:"script"`
}

type scriptResult struct {
	ID     string  `xml:"id,attr"`
	Tables []table `xml:"table"`
}

type table struct {
	Key      string    `xml:"key,attr" `
	Elements []element `xml:"elem"`
	Tables   []table   `xml:"table"`
}

type element struct {
	Key   string `xml:"key,attr"`
	Value string `xml:",chardata"`
}

func hasVulnerable(s scriptResult) bool {
	for _, t := range s.Tables {
		for _, e := range t.Elements {
			if e.Key == "state" {
				if e.Value == "VULNERABLE" {
					return true
				}
			}
		}
	}
	return false
}

func nmapEval(script, results string) (bool, string, error) {
	var (
		r       nmapResult
		xmlData []byte
	)

	xmlData, err := ioutil.ReadFile(results)
	if err != nil {
		err = fmt.Errorf("failed to load nmap results %s: %v",
			results, err)
		return false, "", err
	}

	if err = xml.Unmarshal(xmlData, &r); err != nil {
		err = fmt.Errorf("failed to parse nmap results %s: %v",
			results, err)
		return false, "", err
	}

	if len(r.HostResults) < 1 {
		// If we got results for 0 hosts, it means that the target went
		// away sometime between the scan being scheduled and being
		// executed.  That's not an error.
		return false, apvuln.MarshalNotVulnerable("no hosts found"), nil
	} else if len(r.HostResults) > 1 {
		// When we scan 1 IP address and get results for multiple hosts,
		// we don't know what it means - we can't associate them
		err = fmt.Errorf("too many nmap results in %s", results)
		return false, "", err
	}
	found := false
	vulns := make(apvuln.Vulnerabilities, 0)

	h := r.HostResults[0]

	address := ""
	addressType := ""
	if len(h.AddressResults) > 0 {
		address = h.AddressResults[0].Address
		addressType = h.AddressResults[0].AddressType
	}

	// Some scripts report the result with other port-specific data
	for _, p := range h.PortResults {
		for _, s := range p.ScriptResults {
			// The + forces running on a strange port
			if s.ID == strings.TrimPrefix(script, "+") &&
				hasVulnerable(s) {
				found = true
				vulns = append(vulns,
					apvuln.NmapVulnerability{
						IP:       address,
						IPType:   addressType,
						Port:     strconv.Itoa(p.PortID),
						Protocol: p.Protocol,
						Script:   s.ID})
			}
		}
	}

	// Others report the result along with host-level data
	for _, s := range h.ScriptResults {
		// The + forces running on a strange port
		if s.ID == strings.TrimPrefix(script, "+") &&
			hasVulnerable(s) {
			found = true
			vulns = append(vulns,
				apvuln.NmapVulnerability{
					IP:     address,
					IPType: addressType,
					Script: s.ID})
		}
	}

	var jsonVulns []byte
	if jsonVulns, err = apvuln.MarshalVulns(vulns); err != nil {
		aputil.Fatalf("Couldn't marshal vulns: %v\n", vulns)
	}
	return found, string(jsonVulns), nil
}

func nmapVuln(v aggVulnDescription, tgt net.IP) (bool, string, error) {
	resFile, err := ioutil.TempFile("", "nmap.")
	if err != nil {
		return false, "", fmt.Errorf("failed to create result file: %v", err)
	}
	resFileName := resFile.Name()
	defer os.Remove(resFileName)

	cmd := []string{"-oX", resFileName}
	ports := strings.Join(v.Ports, ",")
	if len(ports) > 0 {
		cmd = append(cmd, "-p", ports)
	}
	options, ok := v.Options["raw"]
	if ok {
		cmd = append(cmd, strings.Split(options, " ")...)
	}

	script, ok := v.Options["script"]
	if ok {
		cmd = append(cmd, "--script", script)
	} else {
		return false, "", fmt.Errorf("no 'script' option")
	}
	cmd = append(cmd, tgt.String())

	aputil.Errorf("nmapVuln running command: %s %s\n", nmapCmd, cmd)

	if err = exec.Command(nmapCmd, cmd...).Run(); err != nil {
		return false, "", fmt.Errorf("scan failed: %v", err)
	}

	vuln, details, err := nmapEval(script, resFileName)
	if err != nil {
		err = fmt.Errorf("evaluation failed: %v", err)
	}
	return vuln, details, err
}

func init() {
	addTool("nmap", nmapVuln)
}
