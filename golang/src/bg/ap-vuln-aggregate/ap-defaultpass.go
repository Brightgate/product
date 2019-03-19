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
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"bg/ap_common/apvuln"
	"bg/ap_common/platform"
)

// TODO: Add timeout (to prevent entire vuln scan from getting hung up).

const dpcmd = "ap-defaultpass"

var plat *platform.Platform

func dpvuln(v aggVulnDescription, tgt net.IP) (bool, string, error) {
	var bannedarg string
	vendorfile := plat.ExpandDirPath("__APPACKAGE__/etc/vendordefaults.csv") // source: https://www.liquidmatrix.org/blog/default-passwords/
	banfiledir := plat.ExpandDirPath("__APDATA__", "defaultpass")
	bannedfile, err := os.Open(filepath.Join(banfiledir,
		"banfile-"+tgt.String()))

	if err == nil {
		defer bannedfile.Close()
		scanner := bufio.NewScanner(bannedfile)
		for scanner.Scan() {
			bannedarg = scanner.Text()
		}
	}

	cmd := []string{"-i", tgt.String(), "-f", vendorfile}
	if raw, ok := v.Options["raw"]; ok {
		cmd = append(cmd, raw)
	}
	if bannedarg != "" { // banfile results take priority
		r := regexp.MustCompile(`([A-Za-z]+:([0-9]+:)?[0-9]+(\,[0-9]+)*\.?)+`) // ensure banfile has correct format
		if r.MatchString(bannedarg) {
			cmd = append(cmd, "-t", bannedarg)
		} else {
			log.Println("Banfile has incorrect format! Deleting.")
			if err := os.RemoveAll(filepath.Join(banfiledir, "banfile-"+tgt.String())); err != nil {
				log.Printf("Banfile removal error:%s\n", err)
			}
		}
	} else if v.Options["services"] != "" { // if no banfile, use nmap's results for ports/services
		cmd = append(cmd, "-t", v.Options["services"])
	} else { // otherwise, ports are closed (nothing to check)
		return false, apvuln.MarshalNotVulnerable("No ports to check"), nil
	}

	output, err := exec.Command(dpcmd, cmd...).CombinedOutput()
	if err != nil {
		log.Fatal("Output: ", string(output), "\nError: ", err)
	}
	if strings.Contains(string(output), "Banned") {
		// Drop through to return false
		log.Printf("%s\n", string(output))
	}
	if strings.Contains(string(output), "vulnerable") {
		// The JSON details are on the third line
		// TODO: This integration is super brittle.
		splitOutput := strings.Split(string(output), "\n")
		if len(splitOutput) < 2 {
			log.Fatal("Expected 2 or more lines; too short:\n",
				string(output))
		}
		jsonVulns := splitOutput[len(splitOutput)-2]
		if _, err := apvuln.UnmarshalDPvulns([]byte(jsonVulns)); err != nil {
			log.Fatal("Failed to unmarshal vulns: ",
				err, "\n", jsonVulns)
		}
		return true, jsonVulns, nil
	}
	return false, apvuln.MarshalNotVulnerable(output), nil
}

func init() {
	plat = platform.NewPlatform()
	addTool(dpcmd, dpvuln)
}
