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
	"strings"

	"bg/ap_common/aputil"
)

const dpcmd = "ap-defaultpass"

func dpvuln(v vulnDescription, tgt net.IP) (vuln bool, err error) {
	var bannedargs string
	vendorfile := aputil.ExpandDirPath("/etc/vendordefaults.csv") // source: https://www.liquidmatrix.org/blog/default-passwords/
	banfiledir := aputil.ExpandDirPath("/var/spool/defaultpass/")
	bannedfile, err := os.Open(banfiledir + "banfile-" + tgt.String())
	if err == nil {
		defer bannedfile.Close()
		scanner := bufio.NewScanner(bannedfile)
		for scanner.Scan() {
			bannedargs = scanner.Text()
		}
	}

	cmd := []string{ "-i", tgt.String(), "-f", vendorfile }
	if bannedargs != "" {
		s := strings.Split(bannedargs, "|")
		cmd = append(cmd, "-p", s[0], "-t", s[1], "-s", s[2])
	} else if len(v.Ports) > 0 {
		portlist := strings.Join(v.Ports, ",")
		cmd = append(cmd, "-p", portlist)
	}

	output, _ := exec.Command(dpcmd, cmd...).CombinedOutput()
	if strings.Contains(string(output), "Banned") {
		log.Printf("%s\n", string(output))
	}
	if strings.Contains(string(output), "vulnerable") {
		return true, nil
	}
	return
}

func init() {
	addTool(dpcmd, dpvuln)
}
