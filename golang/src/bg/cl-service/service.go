/*
 * COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
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
	"flag"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"os/signal"
	"os/user"
	"strconv"
	"syscall"
	"time"

	"bg/cl_common/clcfg"
	"bg/common/cfgapi"
	"bg/common/ssh"

	"github.com/tomazk/envcfg"
	"go.uber.org/zap"
)

const pname = "cl-service"

var environ struct {
	// XXX: this will eventually be a postgres connection, and we will look
	// up the per-appliance cl.configd connection via the database
	ConfigdConnection string `envcfg:"B10E_CLCONFIGD_CONNECTION"`
	DisableTLS        bool   `envcfg:"B10E_CLCONFIGD_DISABLE_TLS"`
}

var (
	sshdTemplate = flag.String("template", "/opt/net.b10e/etc/sshd_config.got",
		"template to use when building the sshd_config file")
	lifespan = flag.Duration("lifespan", 3*time.Hour,
		"length of time the tunnel should remain open")
	sshAddr = flag.String("ssh_addr", "",
		"Externally routable address for this system")
	sshPort = flag.Int("ssh_port", 19000,
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
	}
	slog *zap.SugaredLogger
)

// Select a random, available port to use for our side of the tunnel
func choosePort() int {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		slog.Fatalf("unable to resolve localhost: %v", err)
	}

	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		slog.Fatalf("unable to open a new port: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()

	return port
}

// Get the current user's name
func getUsername() string {
	u, err := user.Current()
	if err != nil {
		slog.Fatalf("unable to get username: %v\n", err)
	}
	return u.Username
}

func isPrivate(ip net.IP) bool {
	_, a, _ := net.ParseCIDR("10.0.0.0/8")
	_, b, _ := net.ParseCIDR("172.16.0.0/12")
	_, c, _ := net.ParseCIDR("192.168.0.0/16")

	return a.Contains(ip) || b.Contains(ip) || c.Contains(ip)
}

// Try to get the outbound IP address.  If this system is behind a NAT, then the
// detected address will not be externally routable.
func getIPAddr() string {
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

// Remove all the settings we added
func resetConfigTree(props map[string]string) {
	slog.Debugf("resetting config tree")
	for prop := range props {
		config.DeleteProp("@/cloud/service/" + prop)
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

	if *sshPort == 0 {
		*sshPort = choosePort()
	}
	if *tunnelPort == 0 {
		*tunnelPort = choosePort()
	}
	prefix := "provided"
	if host = *sshAddr; host == "" {
		prefix = "detected"
		host = getIPAddr()
	}

	ip := net.ParseIP(host)
	if ip == nil {
		slog.Fatalf("%s host address invalid: %s", prefix, host)
	}
	if isPrivate(ip) {
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
	tls := !environ.DisableTLS
	conn, err := clcfg.NewConfigd(pname, *uuid, url, tls)
	if err != nil {
		return fmt.Errorf("connection failure: %s", err)
	}
	conn.SetTimeout(20 * time.Second)
	conn.SetLevel(cfgapi.AccessInternal)

	config = cfgapi.NewHandle(conn)
	config.Ping(nil)
	slog.Debugf("connected to cl.configd")
	return nil
}

func updateUserAuthKey(oldkey string) (string, error) {
	key, err := config.GetProp("@/cloud/service/tunnel_user_key")
	if err != nil {
		err = fmt.Errorf("fetching appliance user key: %v", err)
		key = oldkey
	} else if key != oldkey {
		if err = daemon.SetAuthUserKey(key); err != nil {
			err = fmt.Errorf("importing appliance user key: %v",
				err)
		}
	}

	return key, err
}

// Wait until the tunnel's lifetime expires, or until the user terminates it.
// Periodically check to see whether the incoming user key has changed.
func loop(d time.Duration) {
	var done, warned bool
	var authKey string
	var err error

	doneAt := time.Now().Add(d)
	slog.Infof("Leaving tunnel open until %s", doneAt.Format(time.Stamp))

	sig := make(chan os.Signal, 2)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	ticker := time.NewTicker(time.Second)

	for !done {
		if authKey, err = updateUserAuthKey(authKey); err == nil {
			warned = false
		} else if !warned {
			slog.Warnf("%v", err)
			warned = true
		}

		select {
		case <-ticker.C:
			if time.Now().After(doneAt) {
				done = true
			}

		case s := <-sig:
			slog.Infof("Signal (%v) received.  Stopping", s)
			done = true
		}
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

	if _, key, err = ssh.GenerateSSHKeypair(tmpdir + "/id_rsa"); err != nil {
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

	if err = userInit(); err != nil {
		slog.Fatalf("generating user credentials: %v", err)
	}

	if daemon, err = daemonInit(); err != nil {
		userCleanup()
		slog.Fatalf("spawning sshd daemon: %v", err)
	}

	// Push the sshd settings into the config tree, so the appliance knows
	// how to connect to us
	props := prepProps()
	if err = connectToConfigd(); err != nil {
		slog.Fatalf("connecting to cl.configd: %v", err)
	}
	updateConfigTree(props)

	// Let the user know how to reach the appliance once the tunnel is fully
	// established.
	slog.Infof("To connect to the appliance:\n"+
		"     /usr/bin/ssh -i %s/id_rsa -p %d root@localhost",
		userCreds.tmpDir, *tunnelPort)

	loop(*lifespan)

	slog.Debugf("waiting for sshd loop to exit")
	daemon.Finalize()
	userCleanup()
	resetConfigTree(props)
}
