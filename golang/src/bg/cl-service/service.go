/*
 * Copyright 2020 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


// When running on the same node as cl.configd (the likely scenario for
// testing), the following environment variables should be set:
//
//       export B10E_CLCONFIGD_CONNECTION=127.0.0.1:4431
//       export B10E_CLCONFIGD_DISABLE_TLS=true

//
// There are four identities involved with the creation and use of a remote
// service tunnel:
//
//    cloud_host: the cloud system on which the service user is running and to
//                which the appliance will open the tunnel.
//
//    cloud_user: the person attempting to service the appliance from the
//                cloud
//
//    local_host: the appliance which needs to be serviced
//
//    local_user: the account on the appliance which will be accessible through
//                the tunnel
//
// Each of these entities has (or will generate) an ssh keypair.  The public
// halves of each keypair will be stored in the config tree for the site
// to be serviced.
//
// The general flow:
//      1. the cloud_user logs into the cloud system.
//
//      2. the cloud_user initiates the tunnel connection by running:
//         cl-service -u <appliance uuid>
//
//      3. cl-service:
//         3a. generates host and user ssh keypairs to represent the cloud
//             entities for this tunnel's lifespan.
//         3a. spawns an ssh daemon, which will serve as the cloud endpoint for
//             the service tunnel.
//         3b. updates the appliance's config tree with a number of public
//             keys and related settings (itemized below)
//
//      4. ap.rpcd notices the updated config settings and:
//         4a. creates ssh keypairs for the appliance and a user, putting both
//             public keys in the config tree
//         4b. spawns an sshd process to serve as the appliance endpoint for
//             the service tunnel
//         4c. uses the new keys to connect to the cloud sshd
//         4d. opens an ssh tunnel through that connection
//
//      5. the cloud_user logs into the appliance over the tunnel and performs
//      any maintenance/service tasks
//
//      6. the cloud_user stops cl-service, which causes the tunnel to collapse
//
//      Either side may terminate the support session at any time by closing the
//      network connection and destroying/forgetting the other side's public
//      keys.  In addition, each key property will have an associated
//      expiration time.  When any key expires, the tunnel will collapse.
//
// Properties we need to set for remote appliance to open a tunnel:
//         @/cloud/service/cloud_host
//              IP address / hostname the appliance will connect to
//
//         @/cloud/service/cloud_host_key
//              Public key for the cloud host, which allows the appliance to
//              verify that it is connecting to the correct system
//
//         @/cloud/service/cloud_user
//              User name with whch the appliance should log in
//
//         @/cloud/service/cloud_user_key
//              The public key the service person will use when ssh'ing to the
//              appliance through the established tunnel.  This allows the
//              appliance to ensure that only the service tech can come in over
//              the tunnel
//
//         @/cloud/service/tunnel_port
//              Port number to use for cloud side of the tunnel
//
// Properties we need the appliance to set:
//         @/cloud/service/tunnel_user_key
//              The public key the appliance will use when connecting to set up
//              the tunnel.  This ensures that only this appliance can connect
//              to the cloud service
//
//         @/cloud/service/local_user_name
//              User name to use when connecting back through the tunnel
//

package main

import (
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"text/template"
	"time"

	"bg/cl_common/clcfg"
	"bg/cl_common/registry"
	"bg/common/cfgapi"
	"bg/common/network"
	"bg/common/ssh"

	"github.com/tomazk/envcfg"
	"go.uber.org/zap"
)

const pname = "cl-service"

var environ struct {
	// XXX: this will eventually be a postgres connection, and we will look
	// up the per-appliance cl.configd connection via the database
	ConfigdConnection  string `envcfg:"B10E_CLCONFIGD_CONNECTION"`
	DisableTLS         bool   `envcfg:"B10E_CLCONFIGD_DISABLE_TLS"`
	PostgresConnection string `envcfg:"REG_DBURI"`
}

var (
	sshdTemplate = flag.String("sshd_template", "/opt/net.b10e/etc/sshd_config.got",
		"template to use when building the sshd_config file")
	sshTemplate = flag.String("ssh_template", "/opt/net.b10e/etc/ssh_config.got",
		"template to use when building the ssh_config file")
	lifespan = flag.Duration("lifespan", 3*time.Hour,
		"length of time the tunnel should remain open")
	sshAddr = flag.String("ssh_addr", "",
		"Externally routable address for this system")
	sshPort = flag.Int("ssh_port", 0,
		"port to use for incoming ssh tunnel traffic")
	sshUser = flag.String("ssh_user", "",
		"user name appliance should use when connecting")
	tunnelPort = flag.Int("tunnel_port", 0,
		"local port to open for tunnel")
	uuid = flag.String("uuid", "",
		"uuid of appliance to service")
)

var (
	config    *cfgapi.Handle
	daemon    *ssh.Daemon
	userCreds struct {
		tmpDir    string
		name      string
		publicKey string
		sshConfig string
	}
	slog *zap.SugaredLogger
)

// Get the current user's name
func getUsername() string {
	u, err := user.Current()
	if err != nil {
		slog.Fatalf("unable to get username: %v\n", err)
	}
	return u.Username
}

// Try to get the outbound IP address by checking the GCE metadata server.
func getIPAddrGCE() string {
	// This picks the first external IP that might exist.  If it doesn't,
	// then you'll have to pick one yourself.
	url := "http://169.254.169.254/computeMetadata/v1/instance/network-interfaces/0/access-configs/0/external-ip"
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		panic(err) // URL issues
	}

	req.Header.Add("metadata-flavor", "Google")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		slog.Errorf("Failed to retrieve external IP from GCE metadata: %v", err)
		return ""
	}

	conLenStr := resp.Header.Get("content-length")
	conLen, err := strconv.Atoi(conLenStr)
	if err != nil {
		slog.Errorf("Failed to convert content-length header (%q) to integer: %v",
			conLenStr, err)
		return ""
	}
	if conLen == 0 {
		return ""
	}
	buf := make([]byte, conLen)
	resp.Body.Read(buf)
	return string(buf)
}

// Try to get the outbound IP address.  If this system is behind a NAT, then the
// detected address will not be externally routable.
func getIPAddrUnix() string {
	// To determine which address this system uses by default, figure out
	// which it would use when connecting to google DNS.
	conn, err := net.Dial("udp", "8.8.8.8:53")
	if err != nil {
		slog.Fatalf("unable to get my IP address: %v", err)
	}
	myIP := conn.LocalAddr().(*net.UDPAddr).IP
	conn.Close()

	return myIP.String()
}

// Try to get the outbound IP address.  Check the GCE metadata server first, and
// if there are any issues with that (or it doesn't find an external IP), return
// the local address of a connection to the outside world.
func getIPAddr() string {
	addr := getIPAddrGCE()
	if addr != "" {
		return addr
	}
	return getIPAddrUnix()
}

// Remove all the settings we added
func resetConfigTree(props map[string]string) {
	slog.Debugf("resetting config tree")
	var hdl cfgapi.CmdHdl
	var names []string
	for prop := range props {
		pname := "@/cloud/service/" + prop
		ops := []cfgapi.PropertyOp{
			{
				Op:   cfgapi.PropDelete,
				Name: pname,
			},
		}
		names = append(names, pname)
		hdl = config.Execute(nil, ops)
	}

	// Just wait for the final handle
	_, err := hdl.Wait(nil)
	if err != nil {
		slog.Warnf("prop cleanup may have failed: %v", err)
		slog.Warnf("may need to manually cleanup: %v", strings.Join(names, ", "))
	} else {
		slog.Infof("Successfully reset config tree")
	}
}

// Push the public keys, etc. into the config tree
func updateConfigTree(props map[string]string) {
	ops := make([]cfgapi.PropertyOp, 0)

	expiration := time.Now().Add(*lifespan)
	for prop, val := range props {
		op := cfgapi.PropertyOp{
			Op:      cfgapi.PropCreate,
			Name:    "@/cloud/service/" + prop,
			Value:   val,
			Expires: &expiration,
		}
		ops = append(ops, op)
	}

	if _, err := config.Execute(nil, ops).Wait(nil); err != nil {
		// This may not be a fatal error.  In particular, it could just
		// be that the appliance hasn't responded yet.  Show the error
		// to the user and let them decide whether to give up or not.
		slog.Warnf("config update failed: %v", err)
	}
}

// Return a list of all the properties that need to be set to allow the
// appliance to establish an ssh tunnel to us.
func prepProps() map[string]string {
	var host string

	prefix := "provided"
	if host = *sshAddr; host == "" {
		prefix = "detected"
		host = getIPAddr()
	}

	ip := net.ParseIP(host)
	if ip == nil {
		slog.Fatalf("%s host address invalid: %s", prefix, host)
	}
	if network.IsPrivate(ip) {
		slog.Warnf("%s host address %v is non-routable.  "+
			"Use -ssh_addr to provide a routable address.",
			prefix, ip)
	}

	props := map[string]string{
		"cloud_host":     host + ":" + strconv.Itoa(*sshPort),
		"cloud_host_key": daemon.HostPublicKey,
		"cloud_user":     userCreds.name,
		"cloud_user_key": userCreds.publicKey,
		"tunnel_port":    strconv.Itoa(*tunnelPort),
	}

	return props
}

// Establish a connection to the cl.configd instance for the appliance we're
// trying to service
func connectToConfigd() error {
	slog.Debugf("Connecting to configd")
	url := environ.ConfigdConnection
	if url == "" {
		return fmt.Errorf("B10E_CLCONFIGD_CONNECTION must be set")
	}

	u, err := registry.SiteUUIDByNameFuzzy(
		context.Background(), environ.PostgresConnection, *uuid)
	if err != nil {
		if ase, ok := err.(registry.AmbiguousSiteError); ok {
			log.Fatal(ase.Pretty())
		}
		log.Fatal(err)
	}
	if u.Name != "" {
		log.Printf("%q matched more than one site, but %q (%s) seemed the most likely",
			*uuid, u.Name, u.UUID)
	}

	tls := !environ.DisableTLS
	conn, err := clcfg.NewConfigd(pname, u.UUID.String(), url, tls)
	if err != nil {
		return fmt.Errorf("connection failure: %s", err)
	}
	conn.SetTimeout(20 * time.Second)
	conn.SetLevel(cfgapi.AccessInternal)

	config = cfgapi.NewHandle(conn)
	_, err = config.GetProp("@/apversion")
	if err != nil {
		return err
	}
	slog.Debugf("connected to cl.configd")

	keyProp := "@/cloud/service/tunnel_user_key"
	config.HandleChange(keyProp, userKeyChanged)
	config.HandleDelete(keyProp, userKeyDeleted)

	return nil
}

func updateUserAuthKey(newKey string) {
	if err := daemon.SetAuthUserKey(newKey); err != nil {
		slog.Errorf("importing appliance user key: %v", err)
	}
}

func userKeyDeleted(path []string) {
	updateUserAuthKey("")
}

func userKeyChanged(path []string, val string, exp *time.Time) {
	updateUserAuthKey(val)
}

// Wait until the tunnel's lifetime expires, or until the user terminates it.
func wait(d time.Duration) {
	doneAt := time.Now().Add(d)
	slog.Infof("Leaving tunnel open until %s", doneAt.Format(time.Stamp))

	sig := make(chan os.Signal, 2)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-time.After(d):
		slog.Infof("Tunnel lifetime expired")

	case s := <-sig:
		slog.Infof("Signal (%v) received.  Stopping", s)
	}
}

func newLogger(child bool) *zap.SugaredLogger {
	zapConfig := zap.NewDevelopmentConfig()
	zapConfig.Level = zap.NewAtomicLevel()
	zapConfig.DisableStacktrace = true
	if child {
		zapConfig.DisableCaller = true
	}
	logger, _ := zapConfig.Build()
	return logger.Sugar()
}

func choosePorts() error {
	var err error

	var sshPortRange []int
	if *sshPort == 0 {
		sshPortRange = append(sshPortRange, 19000, 19100)
	} else {
		sshPortRange = append(sshPortRange, *sshPort)
	}
	if *sshPort, err = network.ChoosePort(sshPortRange...); err != nil {
		return fmt.Errorf("getting ssh port: %v", err)
	}

	if *tunnelPort == 0 {
		if *tunnelPort, err = network.ChoosePort(); err != nil {
			return fmt.Errorf("getting tunnel port: %v", err)
		}
	}

	return nil
}

func daemonInit() (*ssh.Daemon, error) {
	cfg := &ssh.DaemonConfig{
		Port:        *sshPort,
		Logger:      slog,
		UserName:    userCreds.name,
		ChildLogger: newLogger(true),
		Template:    *sshdTemplate,
	}

	d, err := ssh.NewSshd(cfg)
	if err != nil {
		err = fmt.Errorf("unable to spawn sshd: %v", err)
	}

	return d, err
}

func userInit() error {
	var tmpdir, key string
	var err error

	if tmpdir, err = ioutil.TempDir("/tmp", "user-"); err != nil {
		return fmt.Errorf("unable to create temp dir: %v", err)
	}

	idFile := filepath.Join(tmpdir, "id_rsa")
	if _, key, err = ssh.GenerateSSHKeypair(idFile); err != nil {
		os.RemoveAll(tmpdir)
		return fmt.Errorf("generating ssh keypair: %v", err)
	}

	if *sshUser != "" {
		userCreds.name = *sshUser
	} else {
		userCreds.name = getUsername()
	}
	userCreds.tmpDir = tmpdir
	userCreds.publicKey = key

	userCreds.sshConfig = filepath.Join(tmpdir, "ssh_config")
	tmpl, err := template.ParseFiles(*sshTemplate)
	if err != nil {
		os.RemoveAll(tmpdir)
		return fmt.Errorf("loading template for ssh_config: %v", err)
	}
	config, err := os.OpenFile(userCreds.sshConfig, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		os.RemoveAll(tmpdir)
		return fmt.Errorf("opening ssh_config: %v", err)
	}
	defer config.Close()
	err = tmpl.Execute(config, struct {
		SSHDir string
		Port   int
	}{tmpdir, *tunnelPort})
	if err != nil {
		os.RemoveAll(tmpdir)
		return fmt.Errorf("generating ssh_config: %v", err)
	}
	return nil
}

func userCleanup() {
	if userCreds.tmpDir != "" {
		os.RemoveAll(userCreds.tmpDir)
	}
}

func main() {
	var err error

	if err = envcfg.Unmarshal(&environ); err != nil {
		slog.Fatalf("Environment Error: %s", err)
	}

	flag.Parse()
	slog = newLogger(false)

	if err = choosePorts(); err != nil {
		slog.Fatalf("choosing ports: %v", err)
	}

	if err = userInit(); err != nil {
		slog.Fatalf("generating user credentials: %v", err)
	}

	// Push the sshd settings into the config tree, so the appliance knows
	// how to connect to us
	if err = connectToConfigd(); err != nil {
		slog.Fatalf("connecting to cl.configd: %v", err)
	}

	existing, _ := config.GetProp("@/cloud/service/cloud_host")
	if existing != "" {
		slog.Warnf("service tunnel already configured on: %s", existing)
		os.Exit(1)
	}

	if daemon, err = daemonInit(); err != nil {
		userCleanup()
		slog.Fatalf("spawning sshd daemon: %v", err)
	}
	props := prepProps()
	updateConfigTree(props)

	// Let the user know how to reach the appliance once the tunnel is fully
	// established.
	slog.Infof("\nTo connect to the appliance:\n"+
		"     ssh -F %s root@localhost\n\n"+
		"To copy to the appliance:\n"+
		"     scp -F %s [file] root@localhost:",
		userCreds.sshConfig, userCreds.sshConfig)

	wait(*lifespan)

	slog.Debugf("waiting for sshd loop to exit")
	daemon.Finalize()
	userCleanup()
	resetConfigTree(props)
}

