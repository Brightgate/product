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
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"os"
)

const pname = "ap-vuln-aggregate"

var (
	help     = flag.Bool("h", false, "get help")
	ipaddr   = flag.String("i", "", "IP to probe")
	vulnlist = flag.String("d", "", "vulnerability list")
	outfile  = flag.String("o", "", "output file")
	verbose  = flag.Bool("v", false, "verbose output")
	tools    = make(map[string]execFunc)
)

type vulnDescription struct {
	Tool     string
	Nickname string            `json:"Nickname,omitempty"`
	Ports    []string          `json:"Ports,omitempty"`
	Options  map[string]string `json:"Options,omitempty"`
}

type execFunc func(vulnDescription, net.IP) (bool, error)

func addTool(name string, exec execFunc) {
	tools[name] = exec
}

func vulnDBLoad(name string) (map[string]vulnDescription, error) {
	vulns := make(map[string]vulnDescription, 0)

	file, err := ioutil.ReadFile(name)
	if err != nil {
		return nil, fmt.Errorf("file read failed: %v", err)
	}

	err = json.Unmarshal(file, &vulns)
	if err != nil {
		return nil, fmt.Errorf("json import failed: %v", err)
	}

	return vulns, nil
}

func testOne(name string, desc vulnDescription, ip net.IP) bool {
	var (
		err  error
		vuln bool
		show string
	)

	if desc.Nickname == "" {
		show = name
	} else {
		show = desc.Nickname
	}
	if *verbose {
		fmt.Printf("Testing for %s...", show)
	}

	if tool, ok := tools[desc.Tool]; ok {
		vuln, err = tool(desc, ip)
		if err != nil {
			fmt.Printf("%s test failed: %v\n", show, err)
		} else if *verbose {
			if vuln {
				fmt.Printf("  vulnerable\n")
			} else {
				fmt.Printf("  not vulnerable\n")
			}
		}
	} else {
		fmt.Fprintf(os.Stderr, "%s: no support for '%s' tool\n",
			name, desc.Tool)
	}

	return vuln
}

func output(found map[string]bool) {
	if *outfile != "" {
		s, err := json.MarshalIndent(found, "", "  ")
		if err != nil {
			log.Fatalf("Failed to marshal results: %v\n", err)
		}

		err = ioutil.WriteFile(*outfile, s, 0644)
		if err != nil {
			log.Fatalf("Failed to write results file '%s': %v\n",
				*outfile, err)
		}
	} else {
		fmt.Printf("vulnerabilities: ")
		if len(found) == 0 {
			fmt.Printf("None")
		}
		spacer := ""
		for name, vuln := range found {
			if vuln {
				fmt.Printf(spacer + name)
				spacer = " "
			}
		}
		fmt.Printf("\n")
	}
}

func usage() {
	log.Printf("usage: %s [-hv] [-o <output file>] -d <vuln list> -i <ip>\n",
		pname)
}

func main() {
	flag.Parse()
	if *help || *ipaddr == "" || *vulnlist == "" {
		usage()
		os.Exit(1)
	}

	ip := net.ParseIP(*ipaddr)
	if ip == nil {
		log.Printf("'%s' is not a valid IP address\n", *ipaddr)
		os.Exit(1)
	}

	vulnList, err := vulnDBLoad(*vulnlist)
	if err != nil {
		log.Printf("Unable to import vulnerability list '%s': %v\n",
			*vulnlist, err)
		os.Exit(1)
	}

	found := make(map[string]bool)
	for n, desc := range vulnList {
		if testOne(n, desc, ip) {
			found[n] = true
		}
	}

	output(found)
	os.Exit(0)
}
