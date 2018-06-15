package main

import (
	"encoding/xml"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"strings"
)

const nmapCmd = "/usr/bin/nmap"

type nmapResult struct {
	HostResults []hostResult `xml:"host"`
}

type hostResult struct {
	PortResults   []portResult   `xml:"ports>port"`
	ScriptResults []scriptResult `xml:"hostscript>script"`
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

func nmapEval(script, results string) (bool, error) {
	var (
		r       nmapResult
		xmlData []byte
	)

	xmlData, err := ioutil.ReadFile(results)
	if err != nil {
		return false, fmt.Errorf("failed to load nmap results %s: %v",
			results, err)
	}

	if err = xml.Unmarshal(xmlData, &r); err != nil {
		return false, fmt.Errorf("failed to parse nmap results %s: %v",
			results, err)
	}

	if n := len(r.HostResults); n != 1 {
		// If we got results for 0 hosts, it means that the target went
		// away sometime between the scan being scheduled and being
		// executed.  That's not an error.
		if n > 1 {
			err = fmt.Errorf("got results for %d hosts", n)
		}
		return false, err
	}
	h := r.HostResults[0]

	// Some scripts report the result along with other port-specific data
	for _, p := range h.PortResults {
		for _, s := range p.ScriptResults {
			if s.ID == script && hasVulnerable(s) {
				return true, nil
			}
		}
	}

	// Others report the result along with host-level data
	for _, s := range h.ScriptResults {
		if s.ID == script && hasVulnerable(s) {
			return true, nil
		}
	}

	return false, nil
}

func nmapVuln(v vulnDescription, tgt net.IP) (bool, error) {
	resFile, err := ioutil.TempFile("", "nmap.")
	if err != nil {
		return false, fmt.Errorf("failed to create result file: %v", err)
	}
	name := resFile.Name()
	defer os.Remove(name)

	cmd := []string{"-oX", name}
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
		return false, fmt.Errorf("no 'script' option")
	}
	cmd = append(cmd, tgt.String())

	if err = exec.Command(nmapCmd, cmd...).Run(); err != nil {
		return false, fmt.Errorf("scan failed: %v", err)
	}

	vuln, err := nmapEval(script, name)
	if err != nil {
		err = fmt.Errorf("evaluation failed: %v", err)
	}
	return vuln, err
}

func init() {
	addTool("nmap", nmapVuln)
}
