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
	"bytes"
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"bg/ap_common/aputil"

	"github.com/jlaffaye/ftp"
	"golang.org/x/crypto/ssh"
)

/*	TODO:
	1. Eventually, the goal is to use a device identity or MAC address to directly lookup the relevant credentials.
	   For now, we will brute force all known default credentials (around 1000 entries).
	2. Handle the cases which dial attempts get blocked with a message other than connection refused.
	3. Change credentials after vulnerability has been discovered (doable through SSH using "passwd", but not through FTP)
*/

type probefunc func([]credentials, int, *[]dpvulnerability, net.IP, int) int

type credentials struct {
	username string
	password string
}

type dpvulnerability struct {
	protocol string
	port     int
	info     credentials
}

var (
	dpfile     = flag.String("f", "", "file to retrieve credentials from (required)")
	ipaddr     = flag.String("i", "", "ip address to probe (required)")
	verbose    = flag.Bool("v", false, "verbose output (optional)")
	teststorun = flag.String("t", "http:80.ftp:21.ssh:22", "format, dot-separated = test:(starting index:)port(,more,ports)")
)

var testMap = map[string]probefunc{
	"http": httpProbe,
	"ftp":  ftpProbe,
	"ssh":  sshProbe,
}

func fetchdefaults(dpfile string) ([]credentials, error) {
	var clist []credentials
	defaultslist, err := os.Open(dpfile)
	if err != nil {
		log.Fatalf("Error opening vendor passwords file:%s\n", err)
	}
	defer defaultslist.Close()
	defaultsreader := csv.NewReader(bufio.NewReader(defaultslist))
	defaultsreader.Read() // get rid of headers line
	l := 1
	for {
		line, err := defaultsreader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return clist, err
		}
		if len(line) != 3 {
			log.Printf("Warning: line %d in vendor passwords file has unexpected format!\n", l)
		} else {
			tempcreds := credentials{line[1], line[2]}
			clist = append(clist, tempcreds)
		}
		l++
	}
	return clist, nil
}

func httpProbe(clist []credentials, startfrom int, vulnports *[]dpvulnerability, ip net.IP, p int) int {
	httpclient := &http.Client{
		Timeout: time.Second,
	}
	req, err := http.NewRequest("GET", "http://"+fmt.Sprintf("%s:%d", ip.String(), p), nil)
	if err != nil {
		return 0
	}
	resp, err := httpclient.Do(req)
	if err != nil {
		return 0
	}
	if len(resp.Header["Www-Authenticate"]) > 0 { // authenticate header detecter, check for basic auth
		for _, authstr := range resp.Header["Www-Authenticate"] {
			if strings.Contains(strings.ToLower(authstr), "basic") { // case insensitive matching
				if *verbose {
					fmt.Println("HTTP Basic Auth detected, probing... ")
				}
				for i, creds := range clist[startfrom:] {
					if *verbose {
						fmt.Printf("HTTP Basic Auth test: [ %d / %d ]\n", i+startfrom+1, len(clist))
					}
					req.SetBasicAuth(creds.username, creds.password)
					resp, err := httpclient.Do(req)
					if err != nil {
						if strings.Contains(err.Error(), "connection refused") {
							// note: error may be something other than connection refused. Possibly handle this in the future.
							fmt.Printf("Banned. Will resume probing this service during the next scan.\n")
							return i + startfrom
						}
						continue
					}
					if resp.StatusCode == 200 { // vulnerable
						if *verbose {
							fmt.Printf("%s is vulnerable on port %d to HTTP Basic Auth default username/password!\n", ip.String(), p)
						}
						*vulnports = append(*vulnports, dpvulnerability{"http", p, creds})
						break
					}
				}
			}
		}
	}
	return 0
}

func ftpProbe(clist []credentials, startfrom int, vulnports *[]dpvulnerability, ip net.IP, p int) int {
	validftpport := false
	for i, creds := range clist[startfrom:] {
		// note: there is an issue where DialTimeout doesn't always time out
		if ftpclient, err := ftp.DialTimeout(fmt.Sprintf("%s:%d", ip.String(), p), time.Second); err != nil { // port is closed
			if validftpport {
				fmt.Printf("Banned. Will resume probing this service during the next scan.\n")
				return i + startfrom
			}
			break
		} else { // valid ftp port
			if *verbose {
				if !validftpport && *verbose {
					fmt.Println("FTP detected, probing... ")
				}
				fmt.Printf("FTP test: [ %d / %d ]\n", i+startfrom+1, len(clist))
			}
			validftpport = true
			if err := ftpclient.Login(creds.username, creds.password); err != nil { // incorrect login
				continue
			} else {
				if *verbose { // vulnerable
					fmt.Printf("%s is vulnerable on port %d to FTP default username/password!\n", ip.String(), p)
				}
				*vulnports = append(*vulnports, dpvulnerability{"ftp", p, creds})
				break
			}
		}
	}
	return 0
}

func sshProbe(clist []credentials, startfrom int, vulnports *[]dpvulnerability, ip net.IP, p int) int {
	validsshport := false
	sshconfig := &ssh.ClientConfig{ // safe, since publically available default passwords are being used
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         time.Second,
	}
	for i, creds := range clist[startfrom:] {
		if *verbose && validsshport {
			fmt.Printf("SSH test: [ %d / %d ]\n", i+startfrom+1, len(clist))
		}
		sshconfig.User = creds.username
		sshconfig.Auth = []ssh.AuthMethod{ssh.Password(creds.password)}
		if sshclient, err := ssh.Dial("tcp", fmt.Sprintf("%s:%d", ip.String(), p), sshconfig); err != nil {
			if strings.Contains(err.Error(), "unable to authenticate") { // note: there may be various other response errors
				if *verbose && !validsshport {
					fmt.Println("SSH detected, probing... ")
					fmt.Printf("SSH test: [ %d / %d ]\n", i+startfrom+1, len(clist))
				}
				validsshport = true
				continue
			} else if validsshport {
				fmt.Printf("Banned. Will resume probing this service during the next scan.\n")
				return i + startfrom
			}
			break // invalid ssh port
		} else { // dial succeeded, try a command
			if session, err := sshclient.NewSession(); err != nil {
				continue
			} else {
				defer session.Close()
				var b bytes.Buffer
				session.Stdout = &b
				if err := session.Run("echo "); err != nil {
					continue
				} else {
					if *verbose {
						fmt.Printf("%s is vulnerable on port %d to SSH default username/password!\n", ip.String(), p)
					}
					*vulnports = append(*vulnports, dpvulnerability{"ssh", p, creds})
					break
				}
			}
		}
	}
	return 0
}

func logban(ip net.IP, tests map[string][]int, banfiledir string) bool { // true if banned
	banned := false
	var b strings.Builder
	for test, ports := range tests {
		if len(ports) > 0 {
			banned = true
			b.WriteString("." + test + ":" + strconv.Itoa(ports[0]) + ":") // test + where to start from next time
			ports = ports[1:]
			portsleftstr := strings.Trim(strings.Join(strings.Fields(fmt.Sprint(ports)), ","), "[]") // int slice to comma separated string
			b.WriteString(portsleftstr)                                                              // add portlist
		}
	}
	if banned {
		content := []byte(b.String()[1:]) // remove leading "."
		err := ioutil.WriteFile(banfiledir+"banfile-"+ip.String(), content, 0644)
		if err != nil {
			log.Fatalf("Failed to write banfile! %s\n", err)
		}
	}
	return banned
}

func dpProbe(ip net.IP, tests map[string][]int) []dpvulnerability {
	clist, err := fetchdefaults(*dpfile)
	if err != nil {
		log.Fatalf("Error:%s\n", err)
	}
	banfiledir := aputil.ExpandDirPath("/var/spool/defaultpass/")
	if err := os.MkdirAll(banfiledir, 0755); err != nil {
		log.Fatalf("Error creating banfile directory:%s\n", err)
	}
	var vulnports []dpvulnerability

	if *verbose {
		fmt.Printf("Probing %s:\n", ip)
	}
	for test := range tests {
		startfrom := tests[test][0] // first element of portlist is starting index
		if startfrom < 0 || len(clist) <= startfrom {
			log.Printf("Invalid starting index: %d\n", startfrom)
			startfrom = 0 // if invalid, just start from the beginning
		}
		tests[test] = tests[test][1:]
		for _, port := range tests[test] {
			if *verbose {
				fmt.Printf("Testing port %d for %s...\n", port, test)
			}
			if startfrom = testMap[test](clist, startfrom, &vulnports, ip, port); startfrom != 0 {
				tests[test] = append([]int{startfrom}, tests[test]...)
				break // continue with the other tests
			}
			if *verbose {
				if len(vulnports) == 0 {
					fmt.Printf("Port %d is safe against %s.\n", port, test)
				}
			}
			tests[test] = tests[test][1:]
		}
	}
	if !logban(ip, tests, banfiledir) { // write to banfile if banned at any point
		if err := os.RemoveAll(banfiledir + "banfile-" + ip.String()); err != nil { // all tests ran, remove banfile if present
			log.Fatalf("File removal error:%s\n", err)
		}
	}
	return vulnports
}

func main() {
	var ip net.IP
	tests := make(map[string][]int) // map of tests to port lists

	flag.Parse()

	if *dpfile == "" {
		flag.Usage()
		log.Fatalf("Filename required.\n")
	}
	if *ipaddr == "" {
		flag.Usage()
		log.Fatalf("IP address required.\n")
	} else {
		if ip = net.ParseIP(*ipaddr); ip == nil {
			log.Fatalf("'%s' is not a valid IP address\n", *ipaddr)
		}
	}

	testlist := strings.Split(*teststorun, ".")
	for _, t := range testlist {
		testinfo := strings.Split(t, ":")             // 0: service to test, 1: index to start from (optional), 2: portlist
		if len(testinfo) != 2 && len(testinfo) != 3 { // invalid testinfo length, skip entry
			continue
		}
		if _, inmap := testMap[testinfo[0]]; !inmap { // invalid test
			log.Printf("Unable to test unknown service: %s\n", testinfo[0])
			continue
		} else if len(tests[testinfo[0]]) > 0 { // unique tests
			log.Printf("Duplicate test: %s\n", testinfo[0])
			continue
		}

		if len(testinfo) == 3 { // optional starting index was passed in
			startfrom, err := strconv.Atoi(testinfo[1])
			if err != nil {
				log.Printf("Invalid starting index: %s\n", testinfo[1])
				continue
			}
			tests[testinfo[0]] = append(tests[testinfo[0]], startfrom)
		} else {
			tests[testinfo[0]] = append(tests[testinfo[0]], 0) // otherwise, start from the beginning of the list
		}

		portlist := strings.Split(testinfo[len(testinfo)-1], ",")
		for _, p := range portlist {
			portNo, err := strconv.Atoi(p)
			if err != nil {
				log.Printf("Invalid port: %s\n", p)
				continue
			}
			tests[testinfo[0]] = append(tests[testinfo[0]], portNo)
		}
	}

	vulnports := dpProbe(ip, tests) // probe for vulnerable ports
	log.Println("Vulnports list:", vulnports)
	if len(vulnports) > 0 {
		fmt.Printf("%s is vulnerable!\n", ip.String())
	} else {
		fmt.Printf("%s is ok.\n", ip.String())
	}
}
