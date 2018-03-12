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
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/go-version"
)

const (
	pname         = "ap-inspect"
	bannerTimeout = 2 * time.Second
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

type vulnProbe struct {
	Error      string
	Vulnerable bool
	VulnIDs    []string
}

func outputResults(v *vulnProbe) error {
	var err error

	if *outfile != "" {
		var s []byte

		if s, err = json.MarshalIndent(v, "", "  "); err == nil {
			err = ioutil.WriteFile(*outfile, s, 0644)
		}
	} else if v.Vulnerable {
		fmt.Printf("%s is vulnerable to %v\n", *ipaddr, v.VulnIDs)
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

	var result vulnProbe

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
				result.Vulnerable = true
				result.VulnIDs = []string{cve}
			}
		}
		if *verbose && len(msg) > 0 {
			log.Printf("eximProbe of %v:%d: %s\n", ip, p, msg)
		}
	}

	outputResults(&result)
}

func usage(exitStatus int) {
	fmt.Printf("usage: %s [-hlv] [-i ipaddr] [-p ports] "+
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
		fmt.Printf("Supported probes:\n")
		for p := range probes {
			fmt.Printf("    %s\n", p)
		}
		os.Exit(0)
	}

	if *probeName == "" || *ipaddr == "" {
		usage(1)
	}

	if *ipaddr != "" {
		if ip = net.ParseIP(*ipaddr); ip == nil {
			fmt.Printf("'%s' is not a valid IP address\n", *ipaddr)
			os.Exit(1)
		}
	}

	if *portList != "" {
		list := strings.Split(*portList, ",")
		for _, p := range list {
			portNo, err := strconv.Atoi(p)
			if err != nil {
				fmt.Printf("Invalid port #: %s\n", p)
				os.Exit(1)
			}
			ports = append(ports, portNo)
		}
	}

	f := probes[*probeName]
	if f == nil {
		fmt.Printf("unrecognized probe type: '%s'\n", *probeName)
		os.Exit(1)
	}

	f(ip, ports)
}
