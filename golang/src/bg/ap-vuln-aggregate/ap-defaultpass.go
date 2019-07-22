//
// COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
//
// This copyright notice is Copyright Management Information under 17 USC 1202
// and is included to protect this work and deter copyright infringement.
// Removal or alteration of this Copyright Management Information without the
// express written permission of Brightgate Inc is prohibited, and any
// such unauthorized removal or alteration will be a violation of federal law.
//

package main

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"bg/ap_common/aputil"
	"bg/ap_common/apvuln"
	"bg/ap_common/platform"
)

// TODO: Add timeout (to prevent entire vuln scan from getting hung up).

const dpcmd = "ap-defaultpass"

var (
	plat *platform.Platform
)

func getDataFile(name string) string {
	return plat.ExpandDirPath("__APDATA__", "defaultpass", name)
}

func getBanArg(ip net.IP) string {
	var banarg string

	banfile := getDataFile("banfile-" + ip.String())
	if aputil.FileExists(banfile) {
		if f, err := os.Open(banfile); err == nil {
			scanner := bufio.NewScanner(f)
			for scanner.Scan() {
				banarg = scanner.Text()
			}
			f.Close()
		}
	}

	// ensure banfile has correct format
	if banarg != "" {
		banFmt := regexp.MustCompile(`([A-Za-z]+:([0-9]+:)?[0-9]+(\,[0-9]+)*\.?)+`)
		if !banFmt.MatchString(banarg) {
			log.Printf("%s has incorrect format - deleting.", banfile)
			if err := os.RemoveAll(banfile); err != nil {
				log.Printf("Banfile removal error: %v\n", err)
			}
			banarg = ""
		}
	}

	return banarg
}

func parseToolOutput(output []byte) (string, error) {
	var rval string
	var err error

	// The JSON details are on the third line
	// TODO: This integration is super brittle.

	splitOutput := strings.Split(string(output), "\n")
	if len(splitOutput) < 2 {
		err = fmt.Errorf("bad output from %s: %s", dpcmd,
			string(output))
	} else {
		rval = splitOutput[len(splitOutput)-2]

		// sanity check the json before returning it
		if _, err = apvuln.UnmarshalDPvulns([]byte(rval)); err != nil {
			err = fmt.Errorf("Failed to unmarshal vulns: %v", err)
			rval = ""
		}
	}

	return rval, err
}

func dpvuln(v aggVulnDescription, tgt net.IP) (bool, string, error) {
	var vulnerable bool
	var rval string

	vendorfile := getDataFile("vendor_defaults.csv")
	if !aputil.FileExists(vendorfile) {
		return false, apvuln.MarshalNotVulnerable("No passfile at " + vendorfile), nil
	}

	cmd := []string{"-i", tgt.String(), "-f", vendorfile}
	if raw, ok := v.Options["raw"]; ok {
		cmd = append(cmd, raw)
	}

	if arg := getBanArg(tgt); arg != "" {
		cmd = append(cmd, "-t", arg)

	} else if v.Options["services"] != "" {
		// if no banfile, use nmap's results for ports/services
		cmd = append(cmd, "-t", v.Options["services"])

	} else {
		// otherwise, ports are closed (nothing to check)
		return false, apvuln.MarshalNotVulnerable("No ports to check"), nil
	}

	output, err := exec.Command(dpcmd, cmd...).CombinedOutput()
	if err != nil {
		err = fmt.Errorf("%s failed: %s", dpcmd, string(output))

	} else if strings.Contains(string(output), "vulnerable") {
		vulnerable = true
		rval, err = parseToolOutput(output)

	} else {
		log.Printf("%s\n", string(output))
		rval = apvuln.MarshalNotVulnerable(output)
	}

	return vulnerable, rval, err
}

func init() {
	plat = platform.NewPlatform()

	addTool(dpcmd, dpvuln)
}
