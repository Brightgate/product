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

	"golang.org/x/crypto/ssh"
	"github.com/jlaffaye/ftp"
)

/* 
 *  TODO: 
 *  1. Eventually, the goal is to use a device identity or MAC address to directly lookup the relevant credentials.
 *     For now, we will brute force all known default credentials (around 1000 entries).
 *  2. Add timeout (to prevent entire vuln scan from getting hung up).
 *  3. Use nmap to detect open ports/services being run, then only test for those particular ports/services.
 *  4. Handle the cases which dial attempts get blocked with a message other than connection refused.
 *  5. Potentially change credentials after vulnerability has been discovered (do-able on SSH using "passwd" command, not on FTP)
 *  6. Log vulnports list + vulnerable credentials somewhere.
 *  7. Continue to try different individual usernames upon being blocked, rather than assuming a blanket block occurred.
 *  8. Add automated testing.
 */

type probefunc func([]credentials, *[]dpvulnerability, net.IP, int) (int)

type credentials struct {
	username string
	password string
}
type dpvulnerability struct {
	protocol string
	port int
	info credentials
}

var (
	dpfile     = flag.String("f", "", "file to retrieve credentials from (required)")
	ipaddr     = flag.String("i", "", "ip address to probe (required)")
	portlist   = flag.String("p", "80,21,22", "list of ports to probe (optional)")
	startfrom  = flag.Int("s", 0, "credential index to start from (optional)")
	testfrom   = flag.String("t", "", "protocol test to begin with (optional)")
	verbose    = flag.Bool("v", false, "verbose output (optional)")
)

var testMap = map[string]probefunc {
	"http": httpProbe,
	"ftp": ftpProbe,
	"ssh": sshProbe,
}

func fetchdefaults() ([]credentials, error) {
	var clist []credentials
	defaultslist, err := os.Open(*dpfile)
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
			log.Printf("Warning: line %d in vendor passwords file has unexpected format!", l)
		} else {
			tempcreds := credentials{line[1], line[2]}
			clist = append(clist, tempcreds)
		}
		l++
	}
	if *startfrom < 0 || len(clist) <= *startfrom {
		return clist, fmt.Errorf("invalid password starting index:%d, range: 0-%d", *startfrom, len(clist)-1)
	}
	return clist, nil
}

func httpProbe(clist []credentials, vulnports *[]dpvulnerability, ip net.IP, p int) (int) {
	httpclient := &http.Client {
		Timeout: time.Second,
	}
	if *verbose {
		fmt.Printf("HTTP Basic Auth... ")
	}
	req, err := http.NewRequest("GET", "http://" + fmt.Sprintf("%s:%d", ip.String(), p), nil)
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
				for i, creds := range(clist[*startfrom:]) {
					if *verbose {
						fmt.Printf("HTTP Basic Auth test: [ %d / %d ]\n", i + 1, len(clist))
					}
					req.SetBasicAuth(creds.username, creds.password)
					resp, err := httpclient.Do(req)
					if err != nil {
						if strings.Contains(err.Error(), "connection refused") {
							// note: error may be something other than connection refused. Possibly handle this in the future.
							fmt.Printf("Banned. Will resume probing during the next scan.\n")
							return i
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

func ftpProbe(clist []credentials, vulnports *[]dpvulnerability, ip net.IP, p int) (int) {
	validftpport := false
	if *verbose {
		fmt.Printf("FTP... ")
	}
	for i, creds := range(clist[*startfrom:]) {
		// note: there is an issue where DialTimeout doesn't always time out
		if ftpclient, err := ftp.DialTimeout(fmt.Sprintf("%s:%d", ip.String(), p), time.Second); err != nil { // port is closed
			if validftpport {
				fmt.Printf("Banned. Will resume probing during the next scan.\n")
				return i // banned
			}
			break
		} else { // valid ftp port
			if *verbose {
				if !validftpport && *verbose {
					fmt.Println("FTP detected, probing... ")
				}
				fmt.Printf("FTP test: [ %d / %d ]\n", i + 1, len(clist))
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

func sshProbe(clist []credentials, vulnports *[]dpvulnerability, ip net.IP, p int) (int) {
	validsshport := false
	sshconfig := &ssh.ClientConfig { // safe, since publically available default passwords are being used
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout: time.Second,
	}
	if *verbose {
		fmt.Printf("SSH... ")
	}
	for i, creds := range(clist[*startfrom:]) {
		if *verbose && validsshport {
			fmt.Printf("SSH test: [ %d / %d ]\n", i + 1, len(clist))
		}
		sshconfig.User = creds.username
		sshconfig.Auth = []ssh.AuthMethod{ ssh.Password(creds.password) }
		if sshclient, err := ssh.Dial("tcp", fmt.Sprintf("%s:%d", ip.String(), p), sshconfig); err != nil {
			if strings.Contains(err.Error(), "unable to authenticate") { // note: there may be various other response errors
				if *verbose && !validsshport {
					fmt.Println("SSH detected, probing... ")
					fmt.Printf("SSH test: [ %d / %d ]\n", i + 1, len(clist))
				}
				validsshport = true
				continue
			} else if validsshport {
				fmt.Printf("Banned. Will resume probing during the next scan.\n")
				return i
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

func logban(test string, ip net.IP, portsleft []int, i int, banfiledir string) {
	portsleftstr := strings.Trim(strings.Join(strings.Fields(fmt.Sprint(portsleft)), ","), "[]") // int slice to string
	content := []byte(fmt.Sprintf("%s|%s|%d", portsleftstr, test, i))
	err := ioutil.WriteFile(banfiledir + "banfile-" + ip.String(), content, 0777)
	if err != nil {
		log.Fatalf("Failed to write http ban to file:%s\n", err)
	}
}

func dpProbe(ip net.IP, ports []int) ([]dpvulnerability) {
	clist, err := fetchdefaults()
	if err != nil {
		log.Fatalf("Error:%s\n", err)
	}
	banfiledir := aputil.ExpandDirPath("/var/spool/defaultpass/")
	if err := os.MkdirAll(banfiledir, 0777); err != nil {
		log.Fatalf("Error creating banfile directory:%s\n", err)
	}
	var vulnports []dpvulnerability
	var portsleft []int
	portsleft = append(portsleft, ports...) // make a copy of portlist
	for _, p := range ports {
		if *verbose {
			fmt.Printf("Probing %s, port %d - ", ip, p)
		}
		for test, probe := range testMap {
			if *testfrom == "" || *testfrom == test {
				if i := probe(clist, &vulnports, ip, p); i != 0 {
					logban(test, ip, portsleft, i, banfiledir)
					return vulnports
				}
				*startfrom = 0
				*testfrom = ""
			}
		}
		if *verbose {
			if len(vulnports) == 0 {
				fmt.Printf("port %d is ok.", p)
			}
			fmt.Println()
		}
		portsleft = portsleft[1:]
	}
	// if this was reached without returning, all tests were able to run.
	if err := os.RemoveAll(banfiledir + "banfile-" + ip.String()); err != nil {
		log.Fatalf("File removal error:%s\n", err)
	}
	return vulnports
}

func main() {
	var ports []int
	var ip net.IP
	var vulnports []dpvulnerability

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

	if *portlist != "" {
		list := strings.Split(*portlist, ",")
		for _, p := range list {
			portNo, err := strconv.Atoi(p)
			if err != nil {
				log.Fatalf("Invalid port #:%s\n", p)
			}
			ports = append(ports, portNo)
		}
	}

	vulnports = dpProbe(ip, ports)
	fmt.Println("Vulnports list:", vulnports)
	if len(vulnports) > 0 {
		fmt.Printf("%s is vulnerable!\n", ip.String())
	} else {
		fmt.Printf("%s is ok.\n", ip.String())
	}
}
