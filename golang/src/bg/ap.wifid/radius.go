//
// COPYRIGHT 2020 Brightgate Inc.  All rights reserved.
//
// This copyright notice is Copyright Management Information under 17 USC 1202
// and is included to protect this work and deter copyright infringement.
// Removal or alteration of this Copyright Management Information without the
// express written permission of Brightgate Inc is prohibited, and any
// such unauthorized removal or alteration will be a violation of federal law.
//

// user authentication daemon
//
// ## Per-user configuration properties
//
// @/user/[username]:
//     uid:
//     display_name:
//     telephone_number:	international phone number as string
//     user_password: 		hashed, salted password using bcrypt
//     user_md4_password: 	hashed password using MD4 (for RADIUS only)
//     [where possible, use LDAP field names for adding additional fields]
//
// ## RADIUS configuration properties
//
// @/network
//     radius_auth_secret	Password
//
// Secret handling uses Base 64 encoding when stored in the configuration.

// # References
//
// Modern LDAP field names come from RFC 2798, and its successors, RFC
// 4519 and RFC 4524.
//
// M. Smith, "Definition of the inetOrgPerson LDAP Object Class", RFC
// 2798, 2000.
// https://www.ietf.org/rfc/rfc2798.txt
//
// A. Sciberras, Ed., "Lightweight Directory Access Protocol (LDAP):
// Schema for User Applications", RFC 4519, 2006.
// https://tools.ietf.org/html/rfc4519
//
// K. Zeilenga, Ed., " COSINE LDAP/X.500 Schema", RFC 4524, 2006.
// https://tools.ietf.org/html/rfc4524
//
// XXX What is the difference between an 802.11 WPA-EAP request and a
// 802.1X request?

// XXX Exception messages are not displayed.

package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"sync"
	"text/template"

	"bg/ap_common/certificate"
	"bg/base_def"
)

type rConf struct {
	ConfDir string

	ClientFile string
	ConfFile   string
	UserFile   string

	RadiusAuthServer     string // RADIUS authentication server
	RadiusAuthServerPort string // RADIUS authentication server port
	RadiusAuthSecret     string // RADIUS shared secret

	ServerName     string
	PrivateKeyFile string
	CertFile       string
	ChainFile      string

	Status string

	Users map[string]string

	sync.Mutex
}

var (
	radiusConfig *rConf
)

const (
	radiusAuthSecret = "@/network/radius_auth_secret"
)

// Generate the user database needed for hostapd in RADIUS mode.
func generateRadiusHostapdUsers(rc *rConf) error {
	ufile := plat.ExpandDirPath(*templateDir, "hostapd.users.got")
	u, err := template.ParseFiles(ufile)
	if err != nil {
		return fmt.Errorf("parsing users template: %v", err)
	}

	un := rc.ConfDir + "/" + rc.UserFile
	uf, err := os.Create(un)
	if err != nil {
		return fmt.Errorf("creating radius user file %s: %v", un, err)
	}
	defer uf.Close()

	err = u.Execute(uf, rc)
	if err != nil {
		return fmt.Errorf("executing user template: %v", err)
	}

	return nil
}

// Generate the client configuration needed for hostapd in RADIUS mode.
func generateRadiusClientConf(rc *rConf) error {
	var err error
	cfile := plat.ExpandDirPath(*templateDir, "hostapd.radius_clients.got")

	slog.Debugf("radius configuration: %v", rc)

	c, err := template.ParseFiles(cfile)
	if err != nil {
		return fmt.Errorf("parsing client template: %v", err)
	}

	cn := rc.ConfDir + "/" + rc.ClientFile
	cf, err := os.Create(cn)
	if err != nil {
		return fmt.Errorf("creating radius client file %s: %v", cn, err)
	}
	defer cf.Close()

	err = c.Execute(cf, rc)
	if err != nil {
		return fmt.Errorf("executing client template: %v", err)
	}

	return nil
}

// Generate the configuration file needed for hostapd in RADIUS mode.
func generateRadiusHostapdConf(rc *rConf) (string, error) {
	var err error

	tfile := plat.ExpandDirPath(*templateDir, "hostapd.radius.got")

	slog.Debugf("radius configuration: %v", rc)

	// Create hostapd.conf, using the APConfig contents to fill out the .got
	// template.
	t, err := template.ParseFiles(tfile)
	if err != nil {
		return "", fmt.Errorf("parsing radius template: %v", err)
	}

	fn := rc.ConfDir + "/" + rc.ConfFile
	cf, err := os.Create(fn)
	if err != nil {
		return "", fmt.Errorf("creating radius conf file %s: %v",
			fn, err)
	}
	defer cf.Close()

	err = t.Execute(cf, rc)
	if err != nil {
		return "", fmt.Errorf("executing radius template: %v", err)
	}

	return fn, nil
}

func generateRadiusConfig() (string, error) {
	var fn string
	var err error

	if radiusConfig == nil {
		return "", fmt.Errorf("no RADIUS config available")
	}

	radiusConfig.Lock()
	defer radiusConfig.Unlock()

	if err = generateRadiusHostapdUsers(radiusConfig); err != nil {
		err = fmt.Errorf("generating RADIUS user config: %v", err)

	} else if err = generateRadiusClientConf(radiusConfig); err != nil {
		err = fmt.Errorf("generating RADIUS client config: %v", err)

	} else if fn, err = generateRadiusHostapdConf(radiusConfig); err != nil {
		err = fmt.Errorf("generating RADIUS hostapd config: %v", err)
	}

	return fn, err
}

func establishSecret() (string, error) {
	// If @/network/radius_auth_secret is already set, retrieve its value.
	if secret, err := config.GetProp(radiusAuthSecret); err == nil {
		return secret, nil
	}

	// Otherwise generate a new secret and set it.
	s := make([]byte, base_def.RADIUS_SECRET_SIZE)
	n, err := rand.Read(s)
	if err != nil {
		return "", fmt.Errorf("unable to generate random number: %v",
			err)
	}
	if n != base_def.RADIUS_SECRET_SIZE {
		return "", fmt.Errorf("mismatch between requested secret "+
			" size %v and generated %v",
			base_def.RADIUS_SECRET_SIZE, n)
	}

	// base64 encode radius_auth_secret
	secret := base64.StdEncoding.EncodeToString(s)
	err = config.CreateProp(radiusAuthSecret, secret, nil)
	if err != nil {
		return "", fmt.Errorf("could not create '%s': %v",
			radiusAuthSecret, err)
	}

	return secret, nil
}

func radiusUserChange(name, password string) {
	var reset bool

	if radiusConfig == nil {
		return
	}

	radiusConfig.Lock()
	old, ok := radiusConfig.Users[name]
	if password == "" && ok {
		slog.Infof("deleting radius password for %s", name)
		delete(radiusConfig.Users, name)
		reset = true
	} else if old != password {
		slog.Infof("changing radius password for %s", name)
		radiusConfig.Users[name] = password
		reset = true
	}
	radiusConfig.Unlock()

	if reset {
		hostapd.reload()
		hostapd.deauthUser(name)
	}
}

func radiusInit() error {
	var certPaths *certificate.CertPaths

	secret, err := establishSecret()
	if err != nil {
		return fmt.Errorf("cannot establish secret: %v", err)
	}
	slog.Debugf("secret '%v'", secret)

	domainName, err := config.GetDomain()
	if err != nil {
		return fmt.Errorf("failed to fetch gateway domain: %v", err)
	}
	gatewayName := "gateway." + domainName

	certPaths = certificate.GetKeyCertPaths(domainName)
	if certPaths == nil {
		// without a cert, we can't do EAP.  The daemon will restart
		// if/when ap.rpcd retrieves a cert from the cloud.
		return fmt.Errorf("no certs available")
	}

	users := make(map[string]string)
	for name, user := range config.GetUsers() {
		if user.MD4Password == "" {
			slog.Warnf("Skipping user '%s': no password set", name)
		} else {
			users[name] = user.MD4Password
		}
	}

	radiusConfig = &rConf{
		ConfDir:          "/tmp",
		ClientFile:       "hostapd.radius_clients.conf",
		ConfFile:         "hostapd.radius.conf",
		UserFile:         "hostapd.users.conf",
		RadiusAuthSecret: string(secret),
		ServerName:       gatewayName,
		PrivateKeyFile:   certPaths.Key,
		CertFile:         certPaths.Cert,
		ChainFile:        certPaths.Chain,
		Status:           "",
		Users:            users,
	}

	return nil
}
