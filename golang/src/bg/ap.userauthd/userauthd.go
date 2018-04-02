//
// COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
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
//     displayName:
//     telephoneNumber:		international phone number as string
//     preferredLanguage:
//     userPassword: 		hashed, salted password using bcrypt
//     userMD4Password: 	hashed password using MD4 (for RADIUS only)
//     [where possible, use LDAP field names for adding additional fields]
//     TOTP:
//
// ## RADIUS configuration properties
//
// @/network
//     radiusAuthServer		IP address
//     radiusAuthServerPort	Port
//     radiusAuthSecret		Password
//
// These properties are established so that we could redefine them to
// point to an external server.  Secret handling, which uses Base 64
// encoding when stored in the configuration, may have to be adjusted
// for use with particular external server implementations.

// RFC 6238 5.1 suggests that we place `TOTP` in a secure area.
//
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
// One-time passwords were defined in RFCxxxx.  The time-based variant
// is TOTP, described in RFC 6238.
//
// D. M'Raihi, S. Machani, M. Pei, and J.Rydell, "TOTP: Time-Based
// One-Time Password Algorithm", RFC 6238. 2011.
// https://tools.ietf.org/html/rfc6238
//
// https://en.wikipedia.org/wiki/Time-based_One-time_Password_Algorithm
//
// https://github.com/google/google-authenticator/wiki/Key-Uri-Format
//
// EAP references are given in userauthd_eap.go.

// XXX What is the difference between an 802.11 WPA-EAP request and a
// 802.1X request?

// XXX Exception messages are not displayed.

package main

import (
	"crypto/rand"
	"encoding/base64"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"text/template"
	"time"

	"bg/ap_common/apcfg"
	"bg/ap_common/aputil"
	"bg/ap_common/broker"
	"bg/ap_common/certificate"
	"bg/ap_common/mcp"
	"bg/base_def"
	"bg/base_msg"

	"github.com/golang/protobuf/proto"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
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

	Users apcfg.UserMap
}

var (
	addr = flag.String("listen-address", base_def.USERAUTHD_PROMETHEUS_PORT,
		"The address to listen on for HTTP requests.")
	templateDir = flag.String("template_dir", "golang/src/ap.userauthd",
		"location of userauthd templates")

	hostapdProcess *aputil.Child // track the hostapd proc

	configd    *apcfg.APConfig
	mcpd       *mcp.MCP
	running    bool
	secret     []byte
	rc         *rConf
	domainname string

	authRequests = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "userauthd_radius_auth_requests",
			Help: "Number of RADIUS authentication requests",
		})
	authFailures = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "userauthd_radius_auth_failures",
			Help: "Number of RADIUS authentication failures",
		})
)

const (
	pname            = "ap.userauthd"
	radiusAuthSecret = "@/network/radiusAuthSecret"
	hostapdPath      = "/usr/sbin/hostapd"
	failuresAllowed  = 4
	period           = time.Duration(time.Minute)
)

func configNetworkRadiusChanged(path []string, val string, expires *time.Time) {
	var resetFunc func(*rConf) string

	// Watch for changes to the network configuration.
	switch path[1] {
	case "radiusAuthServer":
		resetFunc = generateRadiusHostapdConf
	case "radiusAuthServerPort":
		resetFunc = generateRadiusHostapdConf
	case "radiusAuthSecret":
		resetFunc = generateRadiusClientConf
		log.Printf("surprising change to network/radiusAuthSecret\n")
	default:
		log.Printf("ignoring change to %v\n", path)
	}

	if resetFunc != nil {
		resetFunc(rc)
		hostapdProcess.Signal(syscall.SIGHUP)
	}
}

func configUserChanged(path []string, val string, expires *time.Time) {
	generateRadiusHostapdUsers(rc)
	hostapdProcess.Signal(syscall.SIGHUP)
}

// If a new certificate has been generated, we need to restart.
func sysErrorCertificate(event []byte) {
	syserror := &base_msg.EventSysError{}
	proto.Unmarshal(event, syserror)

	log.Printf("sys.error received by handler: %v", *syserror)

	// Check if event is a certificate error
	if *syserror.Reason == base_msg.EventSysError_RENEWED_SSL_CERTIFICATE {
		log.Printf("exiting due to renewed certificate")
		hostapdProcess.Stop()
		os.Exit(0)
	}
}

// Generate the user database needed for hostapd in RADIUS mode.
func generateRadiusHostapdUsers(rc *rConf) string {
	// Get current users.
	rc.Users = configd.GetUsers()

	// Incomplete users should not be included in the config file
	for u, i := range rc.Users {
		if i.MD4Password == "" {
			log.Printf("Skipping user '%s': no password set\n", u)
			delete(rc.Users, u)
		}
	}

	log.Printf("user configuration: %v\n", rc)

	// var err error
	ufile := *templateDir + "/hostapd.users.got"

	u, err := template.ParseFiles(ufile)
	if err != nil {
		log.Fatalf("users template parse failed: %v\n", err)
	}

	un := rc.ConfDir + "/" + rc.UserFile
	uf, _ := os.Create(un)
	defer uf.Close()

	err = u.Execute(uf, rc)
	if err != nil {
		log.Fatalf("users template execution failed: %v\n", err)
	}

	return ufile
}

// Generate the configuration file needed for hostapd in RADIUS mode.
func generateRadiusHostapdConf(rc *rConf) string {
	var err error
	tfile := *templateDir + "/hostapd.radius.got"

	log.Printf("radius configuration: %v\n", rc)

	// Create hostapd.conf, using the APConfig contents to fill out the .got
	// template
	t, err := template.ParseFiles(tfile)
	if err != nil {
		log.Fatalf("radius template parse failed: %v\n", err)
	}

	fn := rc.ConfDir + "/" + rc.ConfFile
	cf, _ := os.Create(fn)
	defer cf.Close()

	err = t.Execute(cf, rc)
	if err != nil {
		log.Fatalf("radius template execution failed: %v\n", err)
	}

	return fn
}

// Generate the client configuration needed for hostapd in RADIUS mode.
// XXX Maybe we can share this between the RADIUS hostapd (server) and the
// WPA-EAP hostapd (client).
func generateRadiusClientConf(rc *rConf) string {
	var err error
	cfile := *templateDir + "/hostapd.radius_clients.got"

	log.Printf("radius configuration: %v\n", rc)

	// Create hostapd.radius_client.conf, using the rConf contents
	// to fill out the template.
	c, err := template.ParseFiles(cfile)
	if err != nil {
		log.Fatalf("client template parse failed: %v\n", err)
	}

	fn := rc.ConfDir + "/" + rc.ClientFile
	cf, _ := os.Create(fn)
	defer cf.Close()

	err = c.Execute(cf, rc)
	if err != nil {
		log.Fatalf("client template execution failed: %v\n", err)
	}

	return fn
}

//
// When we get a signal, set the 'running' flag to false and signal any hostapd
// process we're monitoring.  We want to be sure the wireless interface has been
// released before we give mcp a chance to restart the whole stack.
//
func signalHandler() {
	sig := make(chan os.Signal)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	s := <-sig

	log.Printf("Received signal %v\n", s)
	running = false
	hostapdProcess.Stop()
}

//
// Launch, monitor, and maintain the hostapd process for a single interface
//
func runOne(rc *rConf) {
	log.Printf("runOne entry\n")
	generateRadiusHostapdUsers(rc)
	generateRadiusClientConf(rc)
	fn := generateRadiusHostapdConf(rc)
	log.Printf("runOne configuration %v\n", fn)

	startTimes := make([]time.Time, failuresAllowed)
	for running {
		hostapdProcess = aputil.NewChild(hostapdPath, "-d", fn)
		hostapdProcess.LogOutputTo("radius: ",
			log.Ldate|log.Ltime, os.Stderr)

		startTime := time.Now()
		startTimes = append(startTimes[1:failuresAllowed], startTime)

		log.Printf("Starting RADIUS hostapd\n")

		if err := hostapdProcess.Start(); err != nil {
			rc.Status = fmt.Sprintf("RADIUS hostapd failed to launch: %v", err)
			break
		}

		hostapdProcess.Wait()

		log.Printf("RADIUS hostapd exited after %s\n",
			time.Since(startTime))

		if !running {
			break
		}
		if time.Since(startTimes[0]) < period {
			rc.Status = fmt.Sprintf("Dying too quickly")
			break
		}

		// Give everything a chance to settle before we attempt to
		// restart the daemon.
		time.Sleep(time.Second)
	}
	log.Printf("runOne exit\n")
}

func establishSecret() ([]byte, error) {
	// If @/network/radiusAuthSecret is already set, retrieve its value.
	sp, err := configd.GetProp(radiusAuthSecret)
	if err == nil {
		return []byte(sp), nil
	}

	// Otherwise generate a new secret and set it.
	s := make([]byte, base_def.RADIUS_SECRET_SIZE)
	n, err := rand.Read(s)
	if n != base_def.RADIUS_SECRET_SIZE {
		return nil, fmt.Errorf("mismatch between requested secret size %v and generated %v",
			base_def.RADIUS_SECRET_SIZE, n)
	}

	// base64 encode radiusAuthSecret
	s64 := base64.StdEncoding.EncodeToString(s)
	// XXX Handle staleness by expiration?
	err = configd.CreateProp(radiusAuthSecret, (s64), nil)
	if err != nil {
		return nil, fmt.Errorf("could not create '%s': %v", radiusAuthSecret, err)
	}

	return []byte(s64), nil
}

func main() {
	var err error

	flag.Parse()

	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	mcpd, err := mcp.New(pname)
	if err != nil {
		log.Println("Failed to connect to mcp")
	}

	http.Handle("/metrics", promhttp.Handler())
	go http.ListenAndServe(*addr, nil)

	log.Println("prometheus client launched")

	brokerd := broker.New(pname)
	brokerd.Handle(base_def.TOPIC_ERROR, sysErrorCertificate)
	defer brokerd.Fini()

	configd, err = apcfg.NewConfig(brokerd, pname)
	if err != nil {
		log.Fatalf("cannot connect to configd: %v\n", err)
	}

	configd.HandleChange(`^@/users/.*$`, configUserChanged)
	configd.HandleChange(`^@/network/radius.*$`, configNetworkRadiusChanged)

	domainName, err := configd.GetDomain()
	if err != nil {
		log.Fatalf("failed to fetch gateway domain: %v\n", err)
	}
	gatewayName := "gateway." + domainName
	keyfn, certfn, chainfn, _, err := certificate.GetKeyCertPaths(brokerd,
		gatewayName, time.Now(), false)
	if err != nil {
		log.Fatalf("Cannot get any SSL key/certificate/chain: %v", err)
	}

	secret, err = establishSecret()
	if err != nil {
		log.Fatalf("Cannot establish secret: %v", err)
	}

	if mcpd != nil {
		mcpd.SetState(mcp.ONLINE)
	}

	log.Printf("secret '%v'\n", secret)

	rc = &rConf{
		ConfDir:          "/tmp",
		ClientFile:       "hostapd.radius_clients.conf",
		ConfFile:         "hostapd.radius.conf",
		UserFile:         "hostapd.users.conf",
		RadiusAuthSecret: string(secret),
		ServerName:       gatewayName,
		PrivateKeyFile:   keyfn,
		CertFile:         certfn,
		ChainFile:        chainfn,
		Status:           "",
		Users:            nil,
	}

	running = true
	go signalHandler()

	runOne(rc)
}
