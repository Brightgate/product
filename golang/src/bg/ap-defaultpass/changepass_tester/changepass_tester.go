/*
 * Copyright 2018 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


//
// This file is used to test the changepass functions as a standalone
// program in another directory. It hasn't been fully updated to work
// with `go test` because stubbing the other side of SSH passwd is
// a lot of effort. It has been tested on GNU/(Raspbian, Debian Linux)
// and MacOS (High Sierra).
//
// This file currently is not part of `go test`, but allows you to
// test changepass if you have valid UNIX user credentials on a
// routable SSH server.
//
// It relies on a symbolic link from ../changepass.go into this directory
//
// Example
// GOPATH=~/Product/golang go run changepass.go changepass_tester.go -v -i 192.168.134.52 -u admin -oldpw password
// pi@bg-ea-pi:~/Product/golang/src/bg/ap-defaultpass/changepass_tester $ GOPATH=~/Product/golang go run changepass.go changepass_tester.go -i 192.168.134.52 -u admin -oldpw password
// 2018/11/06 20:49:27 [DANGER-PLAINTEXT PW] Changing password on 192.168.134.52:22 for user admin from password to surgery-natural-racism-gonad-rockslide-0
// 2018/11/06 20:49:27 sshResetPassword 192.168.134.52:22 admin <oldpw>
// 2018/11/06 20:49:34 Resetting password for GNU/Linux
// 2018/11/06 20:49:34 Set to new password surgery-natural-racism-gonad-rockslide-0
// 2018/11/06 20:49:34 [DANGER-PLAINTEXT PW] Changing password on 192.168.134.52:22 for user admin from surgery-natural-racism-gonad-rockslide-0 to password
// 2018/11/06 20:49:34 sshResetPassword 192.168.134.52:22 admin <oldpw>
// 2018/11/06 20:49:41 Resetting password for GNU/Linux
//
// TODO:
// - Stub a ssh server that can receive these commands
// - Rewrite with the `testing` library framework
//
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"strconv"

	"bg/ap_common/apvuln"
)

var (
	user    = flag.String("u", "", "file to retrieve credentials from (required)")
	oldpw   = flag.String("oldpw", "", "old (current) password (required)")
	newpw   = flag.String("newpw", "", "new password (optional)")
	entropy = flag.Bool("entropy", false, "entropy password (optional)")
	host    = flag.String("i", "", "ip address or hostname (required)")
	port    = flag.String("p", "22", "port, default 22 (optional)")
	verbose = flag.Bool("v", false, "verbose output (optional)")
)

// For main()
// changePass
func changePass(ip net.IP, p int, creds apvuln.DPcredentials, newPass string) (err error) {
	address := fmt.Sprintf("%s:%d", ip.String(), p)
	log.Printf("[DANGER-PLAINTEXT PW] Changing password on %s for user %s from %s to %s",
		address, creds.Username, creds.Password, newPass)
	return SSHResetPassword(address, creds.Username, creds.Password, newPass)
}

func usage() {
	log.Fatalf(`
usage:
	<cmd> [-v] -i <host> [-p port] -u <user> -oldpw <old_password> [-newpw <new_password>] [-entropy] [-human]
`)
}

func main() {
	flag.Parse()

	if *user == "" || *oldpw == "" || *host == "" {
		usage()
	}
	if *port == "" {
		*port = "22"
	}
	var portInt int
	if p, err := strconv.Atoi(*port); err == nil {
		portInt = p
	} else {
		log.Printf("Unparseable port %s, using 22", *port)
		portInt = 22
	}
	if *newpw == "" {
		var err error
		if *entropy {
			*newpw, err = EntropyPassword(
				SecurityTheaterPasswordSpec)
			if err != nil {
				log.Fatalf("EntropyPassword() failed: %s %v", *newpw, err)
			}
		} else {
			*newpw, err = HumanPassword(HumanPasswordSpec)
			if err != nil {
				log.Fatalf("HumanPassword() failed: %s %v", *newpw, err)
			}
		}
	}
	err := changePass(net.ParseIP(*host), portInt,
		apvuln.DPcredentials{*user, *oldpw}, *newpw)
	if err != nil {
		log.Fatalf("%v", err)
	}
	log.Printf("Set to new password %s", *newpw)
	err = changePass(net.ParseIP(*host), portInt,
		apvuln.DPcredentials{*user, *newpw}, *oldpw)
	if err != nil {
		log.Fatalf("%v", err)
	}
}

