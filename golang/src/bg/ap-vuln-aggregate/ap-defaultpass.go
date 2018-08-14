//
// COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
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
	"regexp"
	"strings"

	"bg/ap_common/aputil"
)

// TODO: Add timeout (to prevent entire vuln scan from getting hung up).

const dpcmd = "ap-defaultpass"

func dpvuln(v vulnDescription, tgt net.IP) (bool, error) {
	var bannedarg string
	vendorfile := aputil.ExpandDirPath("/etc/vendordefaults.csv") // source: https://www.liquidmatrix.org/blog/default-passwords/
	banfiledir := aputil.ExpandDirPath("/var/spool/defaultpass/")
	bannedfile, err := os.Open(banfiledir + "banfile-" + tgt.String())
	if err == nil {
		defer bannedfile.Close()
		scanner := bufio.NewScanner(bannedfile)
		for scanner.Scan() {
			bannedarg = scanner.Text()
		}
	}

	cmd := []string{"-i", tgt.String(), "-f", vendorfile}
	if bannedarg != "" { // banfile results take priority
		r := regexp.MustCompile(`([A-Za-z]+:([0-9]+:)?[0-9]+([,][0-9]+)*\.?)+`)
		if r.MatchString(bannedarg) {
			cmd = append(cmd, "-t", bannedarg)
		} else {
			log.Println("Banfile has incorrect format! Deleting.")
			if err := os.RemoveAll(banfiledir + "banfile-" + tgt.String()); err != nil {
				log.Printf("Banfile removal error:%s\n", err)
			}
		}
	} else if v.Options["services"] != "" { // if no banfile, use nmap's results for ports/services
		cmd = append(cmd, "-t", v.Options["services"])
	} else { // otherwise, ports are closed (nothing to check)
		return false, nil
	}

	output, err := exec.Command(dpcmd, cmd...).CombinedOutput()
	if err != nil {
		log.Fatal(err)
	}
	if strings.Contains(string(output), "Banned") {
		log.Printf("%s\n", string(output))
	}
	if strings.Contains(string(output), "vulnerable") {
		return true, nil
	}
	return false, nil
}

func init() {
	addTool(dpcmd, dpvuln)
}
