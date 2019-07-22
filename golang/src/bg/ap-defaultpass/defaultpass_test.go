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
	"log"
	"net"
	"net/http"
	"testing"

	"bg/ap_common/apvuln"

	sshserver "github.com/gliderlabs/ssh"
	ftpdriver "github.com/goftp/file-driver"
	ftpserver "github.com/goftp/server"
)

var clist []apvuln.DPcredentials
var localhost = net.ParseIP("127.0.0.1")

func runHTTP(listener net.Listener) {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if user, pass, ok := r.BasicAuth(); ok {
			if user == "testuser" && pass == "testpass" {
				return // status 200 (OK) automatically returned
			}
		}
		w.Header().Set("WWW-Authenticate", "Basic realm=TEST") // authenticate response header
		w.WriteHeader(http.StatusUnauthorized)                 // status 401 (unauthorized)
	})
	http.Serve(listener, nil)
}

func runFTP(listener net.Listener, user string, pass string) {
	opts := &ftpserver.ServerOpts{
		Factory:  &ftpdriver.FileDriverFactory{},
		Hostname: localhost.String(),
		Auth: &ftpserver.SimpleAuth{
			Name:     user,
			Password: pass,
		},
		Logger: new(ftpserver.DiscardLogger), // suppress server output
	}
	ftps := ftpserver.NewServer(opts)
	ftps.Serve(listener)
}

func runSSH(listener net.Listener) {
	sshserver.Handle(func(s sshserver.Session) {})
	sshserver.Serve(listener, nil,
		sshserver.PasswordAuth(func(ctx sshserver.Context, pass string) bool {
			return ctx.User() == "testuser" && pass == "testpass"
		}),
	)
}

func TestHTTP(t *testing.T) {
	listener, err := net.Listen("tcp", ":0") // get an open port
	if err != nil {
		t.Errorf("Error getting open port: %s\n", err)
	}
	go runHTTP(listener) // start the service

	var dpvuln apvuln.Vulnerabilities
	port := listener.Addr().(*net.TCPAddr).Port

	httpProbe(clist, 0, &dpvuln, localhost, port) // probe for vulnerability

	if len(dpvuln) != 1 {
		t.Errorf("HTTP test failed. Single vulnerability not found.\n")
	}
	if v, ok := dpvuln[0].(apvuln.DPvulnerability); ok {
		if v.Credentials.Username != "testuser" ||
			v.Credentials.Password != "testpass" {
			t.Errorf("HTTP test failed. Credentials not found.\n%v\n", v)
		}
	} else {
		t.Errorf("HTTP test failed; returned wrong type.\n")
	}
}

func TestFTP(t *testing.T) {
	listener, err := net.Listen("tcp", ":0") // get an open port
	if err != nil {
		t.Errorf("Error getting open port: %s\n", err)
	}
	go runFTP(listener, "testuser", "testpass") // start the service

	var dpvuln apvuln.Vulnerabilities
	port := listener.Addr().(*net.TCPAddr).Port

	ftpProbe(clist, 0, &dpvuln, localhost, port) // probe for vulnerability

	if len(dpvuln) != 1 {
		t.Errorf("FTP test failed. Single vulnerability not found.\n")
	}
	if v, ok := dpvuln[0].(apvuln.DPvulnerability); ok {
		if v.Credentials.Username != "testuser" ||
			v.Credentials.Password != "testpass" {
			t.Errorf("FTP test failed. Credentials not found.\n%v\n", v)
		}
	} else {
		t.Errorf("FTP test failed; returned wrong type.\n")
	}
}

func TestSSH(t *testing.T) {
	listener, err := net.Listen("tcp", ":0") // get an open port
	if err != nil {
		t.Errorf("Error getting open port: %s\n", err)
	}
	go runSSH(listener) // start the service

	var dpvuln apvuln.Vulnerabilities
	port := listener.Addr().(*net.TCPAddr).Port

	sshProbe(clist, 0, &dpvuln, localhost, port) // probe for vulnerability

	if len(dpvuln) != 1 {
		t.Errorf("SSH test failed. Single vulnerability not found.\n")
	}
	if v, ok := dpvuln[0].(apvuln.DPvulnerability); ok {
		if v.Credentials.Username != "testuser" ||
			v.Credentials.Password != "testpass" {
			t.Errorf("SSH test failed. Credentials not found.\n%v\n", v)
		}
	} else {
		t.Errorf("SSH test failed; returned wrong type.\n")
	}
}

func TestFalsePositive(t *testing.T) {
	listener, err := net.Listen("tcp", ":0") // get an open port
	if err != nil {
		t.Errorf("Error getting open port: %s\n", err)
	}
	go runFTP(listener, "non-default_username", "non-default_password") // non-default credentials, FTP

	var dpvuln apvuln.Vulnerabilities
	port := listener.Addr().(*net.TCPAddr).Port

	ftpProbe(clist, 0, &dpvuln, localhost, port) // probe for vulnerability

	for _, vuln := range dpvuln {
		if v, ok := vuln.(apvuln.DPvulnerability); ok {
			t.Errorf("False Positive test failed. Vulnerability found:\n%v\n", v)
		} else {
			t.Errorf("False Positive test failed. Unexpected %T returned:\n%v\n", vuln, vuln)
		}
	}
}

// TestDefaultsFile tests successful and unsuccessful parsing of the
// test fixtures files.
func TestDefaultsFile(t *testing.T) {
	defs, err := fetchDefaults("testdefaults.csv")
	if err != nil {
		t.Errorf("Error reading testdefaults.csv: %s\n", err)
	}
	if len(defs) != 2 {
		t.Errorf("Unexpected defs length %d", len(defs))
	}
	_, err = fetchDefaults("testdefaults_bad.csv")
	if err == nil {
		t.Errorf("Expected error reading testdefaults_bad.csv: %s\n", err)
	}
}

func init() {
	var err error
	clist, err = fetchDefaults("testdefaults.csv") // load credentials from file
	if err != nil {
		log.Fatalf("Error reading testdefaults.csv: %s\n", err)
	}
}
