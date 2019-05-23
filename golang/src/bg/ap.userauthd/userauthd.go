//
// COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
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
// EAP references are given in userauthd_eap.go.

// XXX What is the difference between an 802.11 WPA-EAP request and a
// 802.1X request?

// XXX Exception messages are not displayed.

package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
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
	"bg/ap_common/platform"
	"bg/base_def"
	"bg/common/cfgapi"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

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

	Users cfgapi.UserMap
}

var (
	templateDir = apcfg.String("template_dir", "__APPACKAGE__/etc/templates/ap.userauthd",
		false, nil)
	hostapdDebug   = apcfg.Bool("hostapd_debug", false, true, hostapdReset)
	hostapdVerbose = apcfg.Bool("hostapd_verbose", false, true, hostapdReset)
	_              = apcfg.String("log_level", "info", true, aputil.LogSetLevel)

	hostapdProcess *aputil.Child // track the hostapd proc

	plat    *platform.Platform
	configd *cfgapi.Handle
	mcpd    *mcp.MCP
	slog    *zap.SugaredLogger
	running bool
	secret  []byte
	rc      *rConf

	// XXX: these metrics are currently just aspirational, since hostapd
	// does all of the authentication work internally.
	metrics struct {
		authRequests prometheus.Counter
		authFailures prometheus.Counter
	}
)

const (
	pname            = "ap.userauthd"
	radiusAuthSecret = "@/network/radius_auth_secret"
	failuresAllowed  = 4
	period           = time.Duration(time.Minute)
)

func configNetworkRadiusSecretChanged(path []string, val string, expires *time.Time) {
	slog.Infof("surprising change to network/radius_auth_secret")

	generateRadiusClientConf(rc)
	hostapdProcess.Signal(syscall.SIGHUP)
}

func configUserChanged(path []string, val string, expires *time.Time) {
	generateRadiusHostapdUsers(rc)
	hostapdProcess.Signal(syscall.SIGHUP)
}

func certStateChange(path []string, val string, expires *time.Time) {
	if val == "installed" {
		slog.Infof("exiting due to renewed certificate")
		hostapdProcess.Stop()
		os.Exit(0)
	}
}

// Generate the user database needed for hostapd in RADIUS mode.
func generateRadiusHostapdUsers(rc *rConf) string {
	// Get current users.
	rc.Users = configd.GetUsers()

	// Incomplete users should not be included in the config file.
	for u, i := range rc.Users {
		if i.MD4Password == "" {
			slog.Warnf("Skipping user '%s': no password set", u)
			delete(rc.Users, u)
		}
	}

	slog.Debugf("user configuration: %v", rc)

	ufile := plat.ExpandDirPath(*templateDir, "hostapd.users.got")

	u, err := template.ParseFiles(ufile)
	if err != nil {
		slog.Fatalf("users template parse failed: %v", err)
	}

	un := rc.ConfDir + "/" + rc.UserFile
	uf, _ := os.Create(un)
	defer uf.Close()

	err = u.Execute(uf, rc)
	if err != nil {
		slog.Fatalf("users template execution failed: %v", err)
	}

	return ufile
}

// Generate the configuration file needed for hostapd in RADIUS mode.
func generateRadiusHostapdConf(rc *rConf) string {
	var err error
	tfile := plat.ExpandDirPath(*templateDir, "hostapd.radius.got")

	slog.Debugf("radius configuration: %v", rc)

	// Create hostapd.conf, using the APConfig contents to fill out the .got
	// template.
	t, err := template.ParseFiles(tfile)
	if err != nil {
		slog.Fatalf("radius template parse failed: %v", err)
	}

	fn := rc.ConfDir + "/" + rc.ConfFile
	cf, _ := os.Create(fn)
	defer cf.Close()

	err = t.Execute(cf, rc)
	if err != nil {
		slog.Fatalf("radius template execution failed: %v", err)
	}

	return fn
}

// Generate the client configuration needed for hostapd in RADIUS mode.
// XXX Maybe we can share this between the RADIUS hostapd (server) and the
// WPA-EAP hostapd (client).
func generateRadiusClientConf(rc *rConf) string {
	var err error
	cfile := plat.ExpandDirPath(*templateDir, "hostapd.radius_clients.got")

	slog.Debugf("radius configuration: %v", rc)

	// Create hostapd.radius_client.conf, using the rConf contents
	// to fill out the template.
	c, err := template.ParseFiles(cfile)
	if err != nil {
		slog.Fatalf("client template parse failed: %v", err)
	}

	fn := rc.ConfDir + "/" + rc.ClientFile
	cf, _ := os.Create(fn)
	defer cf.Close()

	err = c.Execute(cf, rc)
	if err != nil {
		slog.Fatalf("client template execution failed: %v", err)
	}

	return fn
}

//
// When we get a signal, set the 'running' flag to false and signal any hostapd
// process we're monitoring.  We want to be sure the wireless interface has been
// released before we give mcp a chance to restart the whole stack.
//
func signalHandler() {
	sig := make(chan os.Signal, 2)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	s := <-sig

	slog.Infof("Received signal %v", s)
	running = false
	hostapdProcess.Stop()
}

func hostapdReset(name, val string) error {
	if hostapdProcess != nil {
		hostapdProcess.Stop()
	}
	return nil
}

//
// Launch, monitor, and maintain the hostapd process for a single interface
//
func runOne(rc *rConf) {
	slog.Debugf("runOne entry")
	generateRadiusHostapdUsers(rc)
	generateRadiusClientConf(rc)
	fn := generateRadiusHostapdConf(rc)
	slog.Debugf("runOne configuration %v", fn)

	startTimes := make([]time.Time, failuresAllowed)
	for running {
		args := make([]string, 0)
		if *hostapdVerbose {
			args = append(args, "-dd")
		} else if *hostapdDebug {
			args = append(args, "-d")
		}
		args = append(args, fn)

		hostapdProcess = aputil.NewChild(plat.HostapdCmd, args...)
		hostapdProcess.UseZapLog("radius: ", slog, zapcore.InfoLevel)

		startTime := time.Now()
		startTimes = append(startTimes[1:failuresAllowed], startTime)

		slog.Infof("Starting RADIUS hostapd")

		if err := hostapdProcess.Start(); err != nil {
			rc.Status = fmt.Sprintf("RADIUS hostapd failed to launch: %v", err)
			break
		}

		hostapdProcess.Wait()

		slog.Infof("RADIUS hostapd exited after %s",
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
	slog.Infof("runOne exit")
}

func establishSecret() ([]byte, error) {
	// If @/network/radius_auth_secret is already set, retrieve its value.
	sp, err := configd.GetProp(radiusAuthSecret)
	if err == nil {
		return []byte(sp), nil
	}

	// Otherwise generate a new secret and set it.
	s := make([]byte, base_def.RADIUS_SECRET_SIZE)
	n, err := rand.Read(s)
	if err != nil {
		return nil, fmt.Errorf("unable to generate random number: %v",
			err)
	}
	if n != base_def.RADIUS_SECRET_SIZE {
		return nil, fmt.Errorf("mismatch between requested secret size %v and generated %v",
			base_def.RADIUS_SECRET_SIZE, n)
	}

	// base64 encode radius_auth_secret
	s64 := base64.StdEncoding.EncodeToString(s)
	// XXX Handle staleness by expiration?
	err = configd.CreateProp(radiusAuthSecret, (s64), nil)
	if err != nil {
		return nil, fmt.Errorf("could not create '%s': %v", radiusAuthSecret, err)
	}

	return []byte(s64), nil
}

func prometheusInit() {
	metrics.authRequests = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "userauthd_radius_auth_requests",
		Help: "Number of RADIUS authentication requests",
	})
	metrics.authFailures = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "userauthd_radius_auth_failures",
		Help: "Number of RADIUS authentication failures",
	})
	prometheus.MustRegister(metrics.authRequests)
	prometheus.MustRegister(metrics.authFailures)

	http.Handle("/metrics", promhttp.Handler())
	go http.ListenAndServe(base_def.USERAUTHD_DIAG_PORT, nil)
}

func siteIDChange(path []string, val string, expires *time.Time) {
	slog.Info("restarting due to changed domain")
	os.Exit(0)
}

func main() {
	var err error

	slog = aputil.NewLogger(pname)
	defer slog.Sync()

	mcpd, err := mcp.New(pname)
	if err != nil {
		slog.Warnf("Failed to connect to mcp")
	}

	plat = platform.NewPlatform()
	prometheusInit()
	brokerd := broker.NewBroker(slog, pname)
	defer brokerd.Fini()

	configd, err = apcfg.NewConfigd(brokerd, pname, cfgapi.AccessInternal)
	if err != nil {
		slog.Fatalf("cannot connect to configd: %v", err)
	}

	domainName, err := configd.GetDomain()
	if err != nil {
		slog.Fatalf("failed to fetch gateway domain: %v", err)
	}
	gatewayName := "gateway." + domainName

	// Find the TLS key and certificate we should be using.  If there isn't
	// one, sleep until ap.rpcd notifies us one is available, and then
	// restart.
	configd.HandleChange(`^@/certs/.*/state`, certStateChange)
	certPaths := certificate.GetKeyCertPaths(domainName)
	if certPaths == nil {
		slog.Warn("Sleeping until a cert is presented")
		select {}
	}

	secret, err = establishSecret()
	if err != nil {
		slog.Fatalf("Cannot establish secret: %v", err)
	}

	configd.HandleChange(`^@/users/.*$`, configUserChanged)
	configd.HandleChange(`^@/network/radius_auth_secret`, configNetworkRadiusSecretChanged)
	configd.HandleChange(`^@/siteid`, siteIDChange)

	mcpd.SetState(mcp.ONLINE)

	slog.Debugf("secret '%v'", secret)

	rc = &rConf{
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
		Users:            nil,
	}

	running = true
	go signalHandler()

	runOne(rc)
}
