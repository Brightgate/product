/*
 * COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
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
	"bytes"
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"bg/ap_common/aputil"
	"bg/ap_common/apvuln"
	"bg/ap_common/platform"
	"bg/common/passwordgen"

	"github.com/jlaffaye/ftp"
	"golang.org/x/crypto/ssh"
)

/*	TODO:
	1. Eventually, the goal is to use a device identity or MAC address to directly lookup the relevant credentials.
	   For now, we will brute force all known default credentials (around 1000 entries).
	2. Handle the cases which dial attempts get blocked with a message other than connection refused.
	3. Change credentials after vulnerability has been discovered (doable through SSH using "passwd", but not through FTP)
*/

type probefunc func([]apvuln.DPcredentials, int, *apvuln.Vulnerabilities, net.IP, int) int

var (
	dpPath      = flag.String("f", "", "credentials path (required except in reset mode)")
	ipAddr      = flag.String("i", "", "target ip address (required)")
	verbose     = flag.Bool("v", false, "verbose output (optional)")
	testsToRun  = flag.String("t", "http:80.ftp:21.ssh:22", "format, dot-separated = test:(starting index:)port(,more,ports)")
	reset       = flag.String("r", "", "reset mode, service:port:user:password, e.g. ssh:22:admin:password (optional)")
	newUsername = flag.String("u", "", "new username (reset mode only)")
	humanPass   = flag.Bool("human-password", false, "generate human-friendly password (reset mode only)")

	plat *platform.Platform
)

var testMap = map[string]probefunc{
	"http": httpProbe,
	"ftp":  ftpProbe,
	"ssh":  sshProbe,
}

func fetchDefaults(defaultsPath string) ([]apvuln.DPcredentials, error) {
	var clist []apvuln.DPcredentials
	defaultsList, err := os.Open(defaultsPath)
	if err != nil {
		return nil, err
	}
	defer defaultsList.Close()
	defaultsReader := csv.NewReader(bufio.NewReader(defaultsList))
	defaultsReader.FieldsPerRecord = 3
	defaultsReader.Comment = '#'
	for {
		line, err := defaultsReader.Read()
		if err == io.EOF {
			break
		} else if err != nil {
			return nil, err
		}
		clist = append(clist, apvuln.DPcredentials{
			Username: line[1],
			Password: line[2],
		})
	}
	return clist, nil
}

func httpProbe(clist []apvuln.DPcredentials, startfrom int, vulnports *apvuln.Vulnerabilities, ip net.IP, p int) int {
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
					fmt.Printf("HTTP Basic Auth detected, probing...\n")
				}
				for i, creds := range clist[startfrom:] {
					if *verbose {
						fmt.Printf("HTTP Basic Auth test: [ %d / %d ]\n", i+startfrom+1, len(clist))
					}
					req.SetBasicAuth(creds.Username, creds.Password)
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
							fmt.Printf("%s is vulnerable on port %d to HTTP Basic Auth default username/password\n", ip.String(), p)
						}
						*vulnports = append(*vulnports,
							apvuln.DPvulnerability{
								IP:          ip.String(),
								Protocol:    "tcp",
								Service:     "http",
								Port:        strconv.Itoa(p),
								Credentials: creds})
						break
					}
				}
			}
		}
	}
	return 0
}

func ftpProbe(clist []apvuln.DPcredentials, startfrom int, vulnports *apvuln.Vulnerabilities, ip net.IP, p int) int {
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
			if err := ftpclient.Login(creds.Username, creds.Password); err != nil { // incorrect login
				continue
			} else {
				if *verbose { // vulnerable
					fmt.Printf("%s is vulnerable on port %d to FTP default username/password\n", ip.String(), p)
				}
				*vulnports = append(*vulnports,
					apvuln.DPvulnerability{
						IP:          ip.String(),
						Protocol:    "tcp",
						Service:     "ftp",
						Port:        strconv.Itoa(p),
						Credentials: creds})
				break
			}
		}
	}
	return 0
}

func sshProbe(clist []apvuln.DPcredentials, startfrom int, vulnports *apvuln.Vulnerabilities, ip net.IP, p int) int {
	validsshport := false
	sshconfig := &ssh.ClientConfig{ // safe, since publically available default passwords are being used
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         time.Second,
	}
	for i, creds := range clist[startfrom:] {
		if *verbose && validsshport {
			fmt.Printf("SSH test: [ %d / %d ]\n", i+startfrom+1, len(clist))
		}
		sshconfig.User = creds.Username
		sshconfig.Auth = []ssh.AuthMethod{ssh.Password(creds.Password)}
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
						fmt.Printf("%s is vulnerable on port %d to SSH default username/password\n", ip.String(), p)
					}
					*vulnports = append(*vulnports,
						apvuln.DPvulnerability{
							IP:          ip.String(),
							Protocol:    "tcp",
							Service:     "ssh",
							Port:        strconv.Itoa(p),
							Credentials: creds})
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
			aputil.Fatalf("Failed to write banfile %s\n", err)
		}
	}
	return banned
}

func dpProbe(ip net.IP, tests map[string][]int) apvuln.Vulnerabilities {
	clist, err := fetchDefaults(*dpPath)
	if err != nil {
		aputil.Fatalf("dpProbe: error fetching defaults: %s\n", err)
	}
	banfiledir := plat.ExpandDirPath("__APDATA__", "defaultpass")
	if err := os.MkdirAll(banfiledir, 0755); err != nil {
		aputil.Fatalf("dpProbe: error creating banfile directory: %s\n", err)
	}
	var vulnports apvuln.Vulnerabilities

	if *verbose {
		fmt.Printf("Probing %s:\n", ip)
	}
	for test := range tests {
		startfrom := tests[test][0] // first element of portlist is starting index
		if startfrom < 0 || len(clist) <= startfrom {
			aputil.Errorf("dpProbe: invalid starting index: %d\n", startfrom)
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
			aputil.Fatalf("dpProbe: file removal error: %s\n", err)
		}
	}
	return vulnports
}

// Reset an SSH password
func resetSSH() {
	resetData := strings.SplitN(*reset, ":", 4)
	if len(resetData) < 4 {
		aputil.Fatalf("-r for ssh requires ssh:<port>:<user>:<pass>\n")
	}
	if strings.ToLower(resetData[0]) != "ssh" {
		aputil.Fatalf("resetSSH() called with service %s\n", resetData[0])
	}
	address := fmt.Sprintf("%s:%s", *ipAddr, resetData[1])
	username := resetData[2]
	oldPass := resetData[3]
	if *newUsername != "" {
		aputil.Errorf("-u not supported for ssh; using %s\n", username)
	}

	var newPass string
	var err error
	if *humanPass {
		if newPass, err = passwordgen.HumanPassword(passwordgen.HumanPasswordSpec); err != nil {
			aputil.Fatalf("HumanPassword() failed: %v\n", err)
		}
	} else {
		var newPassCheck string
		if newPass, err = getPassword("New Password: "); err != nil {
			aputil.Fatalf("failed to get password from stdin: %v\n", err)
		}
		if newPassCheck, err = getPassword("Retype New Password: "); err != nil {
			aputil.Fatalf("failed to get password from stdin: %v\n", err)
		}
		if newPass != newPassCheck {
			aputil.Fatalf("New passwords do not match\n")
		}
	}

	err = SSHResetPassword(address, username, oldPass, newPass)
	if err != nil {
		aputil.Fatalf("SSHResetPassword() failed: %v\n", err)
	}
	fmt.Printf("success %s:%s\n", username, newPass)
}

// resetMode handles, well, the default password reset mode
// Broke this out of main() for readability
// Uses same global flags *ipAddr and *reset as main
func resetMode() {
	resetData := strings.SplitN(*reset, ":", 2)
	if len(resetData) < 2 {
		aputil.Fatalf("-r requires <service>:...\n")
	}

	service := resetData[0]
	switch service {
	case "ssh":
		resetSSH()
	// additional cases could be resetTelnet, resetHTTP, etc.
	default:
		aputil.Fatalf("-r: unsupported service %v\n", service)
	}
}

func usagef(format string, v ...interface{}) {
	flag.Usage()
	aputil.Fatalf(format, v...)
}

func main() {
	var ip net.IP
	tests := make(map[string][]int) // map of tests to port lists

	flag.Parse()

	if *ipAddr == "" {
		usagef("IP address required\n")
	} else {
		if ip = net.ParseIP(*ipAddr); ip == nil {
			aputil.Fatalf("'%s' is not a valid IP address\n", *ipAddr)
		}
	}

	if *reset != "" {
		resetMode()
		os.Exit(0)
	}
	// Otherwise we are in probe mode

	if *dpPath == "" {
		usagef("Filename required\n")
	}

	plat = platform.NewPlatform()

	testlist := strings.Split(*testsToRun, ".")
	for _, t := range testlist {
		testinfo := strings.Split(t, ":")             // 0: service to test, 1: index to start from (optional), 2: portlist
		if len(testinfo) != 2 && len(testinfo) != 3 { // invalid testinfo length, skip entry
			continue
		}
		if _, inmap := testMap[testinfo[0]]; !inmap { // invalid test
			aputil.Errorf("Unable to test unknown service: %s\n", testinfo[0])
			continue
		} else if len(tests[testinfo[0]]) > 0 { // unique tests
			aputil.Errorf("Duplicate test: %s\n", testinfo[0])
			continue
		}

		if len(testinfo) == 3 { // optional starting index was passed in
			startfrom, err := strconv.Atoi(testinfo[1])
			if err != nil {
				aputil.Errorf("Invalid starting index: %s\n", testinfo[1])
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
				aputil.Errorf("Invalid port: %s\n", p)
				continue
			}
			tests[testinfo[0]] = append(tests[testinfo[0]], portNo)
		}
	}

	vulnports := dpProbe(ip, tests) // probe for vulnerable ports
	aputil.Errorf("Vulnports list: %v\n", vulnports)
	if len(vulnports) > 0 {
		jsonVulns, err := apvuln.MarshalVulns(vulnports)
		if err != nil {
			aputil.Fatalf("Error marshaling vulnports: %s\n", err)
		}
		fmt.Printf("%s is vulnerable\n%s\n", ip.String(), jsonVulns)
	} else {
		fmt.Printf("%s is ok.\n", ip.String())
	}
}
