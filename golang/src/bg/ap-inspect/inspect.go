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
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"bg/ap_common/aputil"
	"bg/ap_common/apvuln"

	"github.com/hashicorp/go-version"
)

const (
	pname         = "ap-inspect"
	bannerTimeout = 10 * time.Second
)

var (
	help       = flag.Bool("h", false, "get help")
	ipaddr     = flag.String("i", "", "IP to inspect")
	listProbes = flag.Bool("l", false, "list supported probes")
	probeName  = flag.String("n", "", "probe type")
	outfile    = flag.String("o", "", "output file")
	portList   = flag.String("p", "", "port list")
	verbose    = flag.Bool("v", false, "verbose output")
)

type probeFunc func(net.IP, []int)

var probes = map[string]probeFunc{
	"CVE-2018-6789": eximProbe,
}

func outputResults(v *apvuln.InspectVulnProbe) error {
	jsonVuln, err := json.Marshal(v)
	if err != nil {
		aputil.Fatalf("ap-inspect:outputResults couldn't marshal %v\n", v)
	}

	if *outfile != "" {
		err = ioutil.WriteFile(*outfile, jsonVuln, 0644)
	} else if v.Vulnerable {
		fmt.Printf("%s is vulnerable!\n%s\n", *ipaddr, jsonVuln)
	} else {
		fmt.Printf("%s is ok\n", *ipaddr)
	}

	return err
}

func getBanner(ip net.IP, port int) (string, error) {
	var (
		conn   net.Conn
		banner string
		err    error
	)

	if ip != nil {
		addr := fmt.Sprintf("%v:%d", ip, port)
		conn, err = net.DialTimeout("tcp", addr, time.Second)
	} else {
		err = fmt.Errorf("missing IP address")
	}

	if conn != nil {
		conn.SetReadDeadline(time.Now().Add(bannerTimeout))
		banner, err = bufio.NewReader(conn).ReadString('\n')
		if err != nil {
			err = fmt.Errorf("network read failed: %v", err)
		}
	}

	return banner, err
}

// Check for CVE-2018-6789, which is a buffer overflow in Exim 4.90 and earlier
//
// (note: we are just checking Exim's self-reported version number here; we
//  aren't probing for the vulnerability directly.)
//
func eximProbe(ip net.IP, ports []int) {
	const cve = "CVE-2018-6789"

	var result apvuln.InspectVulnProbe
	result.Vulnerable = false
	result.Vulns = make(apvuln.Vulnerabilities, 0)

	if len(ports) == 0 {
		if smtp, _ := net.LookupPort("tcp", "smtp"); smtp != 0 {
			ports = []int{smtp}
		}
	}

	goodVersion, _ := version.NewVersion("4.90.1")
	for _, p := range ports {
		msg := ""
		banner, err := getBanner(ip, p)
		if err != nil {
			// An error here is actually OK, as it likely just means
			// the target has nothing running on this port,
			continue
		}

		// We're looking for a banner like:
		//     220 hostname ESMTP Exim 4.80 Tue, 13 Mar 2018 15:16:40 -0700
		fields := strings.Fields(banner)
		if len(fields) < 6 {
			msg = "unrecognized SMTP banner"
		} else if fields[0] != "220" {
			msg = "SMTP server returned status: " + fields[0]
		} else if fields[3] != "Exim" {
			msg = "found a non-Exim mail server"
		} else {
			v := fields[4]

			testVersion, err := version.NewVersion(v)
			if err != nil {
				msg = fmt.Sprintf("bad version # '%s': %v",
					v, err)
			} else if testVersion.LessThan(goodVersion) {
				msg = fmt.Sprintf("exim %s is vulnerable to %s",
					v, cve)
				dv := apvuln.InspectVulnerability{
					Identifier: cve, IP: ip.String(),
					Protocol: "tcp", Service: "smtp",
					Port:    strconv.Itoa(p),
					Program: "exim", ProgramVer: v}
				result.Vulnerable = true
				result.Vulns = append(result.Vulns, dv)
			}
		}
		if *verbose && len(msg) > 0 {
			aputil.Errorf("eximProbe of %v:%d: %s\n", ip, p, msg)
		}
	}

	outputResults(&result)
}

func usage(exitStatus int) {
	fmt.Printf("usage: %s [-hlv] [-i ipaddr] [-p ports] [-o outputfile] "+
		"-n <probeName>\n", pname)
	os.Exit(exitStatus)
}

func main() {
	var ip net.IP
	var ports []int

	flag.Parse()
	if *help {
		usage(0)
	}
	if *listProbes {
		aputil.Errorf("Supported probes:\n")
		for p := range probes {
			aputil.Errorf("    %s\n", p)
		}
		os.Exit(0)
	}

	if *probeName == "" || *ipaddr == "" {
		usage(1)
	}

	if *ipaddr != "" {
		if ip = net.ParseIP(*ipaddr); ip == nil {
			aputil.Fatalf("'%s' is not a valid IP address\n", *ipaddr)
		}
	}

	if *portList != "" {
		list := strings.Split(*portList, ",")
		for _, p := range list {
			portNo, err := strconv.Atoi(p)
			if err != nil {
				aputil.Fatalf("Invalid port #: %s\n", p)
			}
			ports = append(ports, portNo)
		}
	}

	f := probes[*probeName]
	if f == nil {
		aputil.Fatalf("unrecognized probe type: '%s'\n", *probeName)
	}

	if *verbose {
		aputil.Errorf("Probing %s for %s, ports: %v",
			*ipaddr, *probeName, *portList)
		if len(*outfile) > 0 {
			aputil.Errorf("...probe output in %s", *outfile)
		}
		aputil.Errorf("\n")
	}

	f(ip, ports)
}
