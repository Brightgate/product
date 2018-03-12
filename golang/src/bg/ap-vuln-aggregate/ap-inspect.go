package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"strings"
)

const inspectCmd = "ap-inspect"

type vulnProbe struct {
	Error      string
	Vulnerable bool
	VulnIDs    []string
}

func bgEval(name string) (bool, error) {
	var v vulnProbe

	vuln := false
	file, err := ioutil.ReadFile(name)
	if err != nil {
		err = fmt.Errorf("failed to read '%s': %v", name, err)
	} else if err = json.Unmarshal(file, &v); err != nil {
		err = fmt.Errorf("unmarshal of '%s' failed: %v", name, err)
	} else {
		vuln = v.Vulnerable
	}

	return vuln, err
}

func bgVuln(v vulnDescription, tgt net.IP) (vuln bool, err error) {
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

	cmd := []string{"-i", tgt.String(), "-n", probe, "-o", resName}
	if len(v.Ports) > 0 {
		portlist := strings.Join(v.Ports, ",")
		cmd = append(cmd, "-p", portlist)
	}

	if err = exec.Command(inspectCmd, cmd...).Run(); err != nil {
		err = fmt.Errorf("scan failed: %v", err)
		return
	}

	if vuln, err = bgEval(resName); err != nil {
		err = fmt.Errorf("evaluation failed: %v", err)
	}
	return
}

func init() {
	addTool("ap-inspect", bgVuln)
}
