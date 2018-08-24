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

	sshserver "github.com/gliderlabs/ssh"
	ftpdriver "github.com/goftp/file-driver"
	ftpserver "github.com/goftp/server"
)

var clist []credentials
var localhost = net.ParseIP("127.0.0.1")

func runHTTP(listener net.Listener) {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if user, pass, ok := r.BasicAuth(); ok {
			if user == "dptest" && pass == "dptest" {
				return // status 200 (OK) automatically returned
			}
		}
		w.Header().Set("WWW-Authenticate", "Basic realm=dptest") // authenticate response header
		w.WriteHeader(http.StatusUnauthorized)                   // status 401 (unauthorized)
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
			return ctx.User() == "dptest" && pass == "dptest"
		}),
	)
}

func TestHTTP(t *testing.T) {
	listener, err := net.Listen("tcp", ":0") // get an open port
	if err != nil {
		t.Errorf("Error getting open port: %s\n", err)
	}
	go runHTTP(listener) // start the service

	var dpvuln []dpvulnerability
	port := listener.Addr().(*net.TCPAddr).Port

	httpProbe(clist, 0, &dpvuln, localhost, port) // probe for vulnerability

	if len(dpvuln) == 0 {
		t.Errorf("HTTP test failed. Vulnerability not found.\n")
	}
}

func TestFTP(t *testing.T) {
	listener, err := net.Listen("tcp", ":0") // get an open port
	if err != nil {
		t.Errorf("Error getting open port: %s\n", err)
	}
	go runFTP(listener, "dptest", "dptest") // start the service

	var dpvuln []dpvulnerability
	port := listener.Addr().(*net.TCPAddr).Port

	ftpProbe(clist, 0, &dpvuln, localhost, port) // probe for vulnerability

	if len(dpvuln) == 0 {
		t.Errorf("FTP test failed. Vulnerability not found.\n")
	}
}

func TestSSH(t *testing.T) {
	listener, err := net.Listen("tcp", ":0") // get an open port
	if err != nil {
		t.Errorf("Error getting open port: %s\n", err)
	}
	go runSSH(listener) // start the service

	var dpvuln []dpvulnerability
	port := listener.Addr().(*net.TCPAddr).Port

	sshProbe(clist, 0, &dpvuln, localhost, port) // probe for vulnerability

	if len(dpvuln) == 0 {
		t.Errorf("SSH test failed. Vulnerability not found.\n")
	}
}

func TestFalsePositive(t *testing.T) {
	listener, err := net.Listen("tcp", ":0") // get an open port
	if err != nil {
		t.Errorf("Error getting open port: %s\n", err)
	}
	go runFTP(listener, "non-default_username", "non-default_password") // non-default credentials, FTP

	var dpvuln []dpvulnerability
	port := listener.Addr().(*net.TCPAddr).Port

	ftpProbe(clist, 0, &dpvuln, localhost, port) // probe for vulnerability

	if len(dpvuln) != 0 {
		t.Errorf("False Positive test failed. Vulnerability found.\n")
	}
}

func init() {
	var err error
	clist, err = fetchdefaults("vendordefaults.csv") // load credentials from file
	if err != nil {
		log.Fatalf("Error reading defaults list: %s\n", err)
	}
}
