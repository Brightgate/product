/*
 * COPYRIGHT 2019 Brightgate Inc. All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package ssh

import (
	"bufio"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"text/template"
	"time"

	"go.uber.org/zap"
)

const (
	sshdCmd        = "/usr/sbin/sshd"
	sshdConfigFile = "sshd_config"
	authKeysFile   = "authorized_key"
	hostKeyFile    = "ssh_host_rsa_key"
)

// DaemonConfig contains the caller-settable parameters for the new sshd daemon
type DaemonConfig struct {
	Template    string
	UserName    string
	Port        int
	Logger      *zap.SugaredLogger
	ChildLogger *zap.SugaredLogger
}

// Daemon represents the high-level configuration for an sshd instance, and
// serves as an opaque handle for the caller to control the lifecycle of an sshd
// daemon process.
type Daemon struct {
	WorkingDir    string
	Port          int
	UserName      string
	HostPublicKey string
	Logger        *zap.SugaredLogger
	ChildLogger   *zap.SugaredLogger

	template   string
	shouldExit chan bool
	alive      bool
	wg         sync.WaitGroup
}

// used to manage a running sshd child process
type sshdInstance struct {
	daemon *Daemon
	args   []string

	pipes      int
	pipeDone   chan bool
	shouldExit chan bool
	didExit    chan bool
	wg         sync.WaitGroup
}

// Wait for stdout/stderr from a process, and print whatever it sends.  When the
// pipe is closed, notify our caller.
func (i *sshdInstance) handlePipe(r io.ReadCloser) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := "[sshd] " + scanner.Text()
		i.daemon.ChildLogger.Infof(line)
	}

	i.pipeDone <- true
}

// Manages the lifecycle of an sshd daemon.  Launch the child process and wait
// for it to exit.  If instructed by our caller, kill the child process.
func (i *sshdInstance) run() {
	defer func() {
		i.didExit <- true
		i.wg.Done()
	}()

	cmd := exec.Command(sshdCmd, i.args...)
	if stdout, err := cmd.StdoutPipe(); err == nil {
		i.pipes++
		go i.handlePipe(stdout)
	}
	if stderr, err := cmd.StderrPipe(); err == nil {
		i.pipes++
		go i.handlePipe(stderr)
	}

	if err := cmd.Start(); err != nil {
		i.daemon.Logger.Errorf("unable to start %s: %v", sshdCmd, err)
		return
	}

	t := time.NewTicker(5 * time.Second)
	defer t.Stop()

	exiting := false
	sig := syscall.SIGTERM
	for i.pipes > 0 {
		select {
		case <-i.pipeDone:
			i.pipes--
			i.daemon.Logger.Debugf("pipe closed")
		case <-i.shouldExit:
			exiting = true
			i.daemon.Logger.Debugf("should exit")
		case <-t.C:
		}

		if exiting {
			i.daemon.Logger.Debugf("sending %v to sshd", sig)
			cmd.Process.Signal(sig)
			sig = syscall.SIGKILL
		}
	}

	i.daemon.Logger.Debugf("Waiting for child process")
	cmd.Wait()
	i.daemon.Logger.Debugf("Child process exited")
}

// Fields that are needed to construct an sshd_config file from a template
type templateData struct {
	Port         int
	WorkingDir   string
	HostKeyFile  string
	AuthKeysFile string
	UserName     string
}

// Create an sshd_config file and return a handle allowing the caller to
// launch/stop a child process using that config.
func (d *Daemon) newSshdInstance() (*sshdInstance, error) {
	td := templateData{
		Port:         d.Port,
		UserName:     d.UserName,
		WorkingDir:   d.WorkingDir,
		HostKeyFile:  hostKeyFile,
		AuthKeysFile: authKeysFile,
	}

	// Create the sshd_config file
	tmpl, err := template.ParseFiles(d.template)
	if err != nil {
		return nil, fmt.Errorf("unable to parse %s: %v", d.template, err)
	}

	configFile := d.WorkingDir + "/" + sshdConfigFile
	cf, err := os.Create(configFile)
	if err != nil {
		return nil, fmt.Errorf("unable to create %s: %v", configFile, err)
	}

	defer cf.Close()
	if err = tmpl.Execute(cf, td); err != nil {
		return nil, fmt.Errorf("unable to populate %s: %v", configFile, err)
	}

	i := &sshdInstance{
		daemon:     d,
		shouldExit: make(chan bool, 1),
		didExit:    make(chan bool, 1),
		pipeDone:   make(chan bool, 2),
		args:       []string{"-D", "-f", configFile},
	}

	return i, nil
}

// Find an unused TCP port
func choosePort() (int, error) {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		return 0, fmt.Errorf("unable to resolve localhost: %v", err)
	}

	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0, fmt.Errorf("unable to open a new port: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()

	return port, nil
}

func (d *Daemon) loop() {
	var err error
	var child *sshdInstance

	d.Logger.Infof("Starting sshd loop")
	done := false
	for !done {
		if child == nil {
			if child, err = d.newSshdInstance(); err != nil {
				d.Logger.Errorf("%v", err)
				break
			}
			child.wg.Add(1)
			go child.run()
		}

		select {
		case done = <-d.shouldExit:
		case <-child.didExit:
			child = nil
		}
	}

	d.Logger.Debugf("Shutting down sshd loop")
	if child != nil {
		child.shouldExit <- true
		child.wg.Wait()
	}
	os.RemoveAll(d.WorkingDir)

	d.Logger.Infof("Finished sshd loop")
	d.alive = false
	d.wg.Done()
}

// SetAuthUserKey is called when the remote side provides us with the public key
// it will be using to connect to this sshd daemon.  If the key is empty, we
// revoke any key we may currently be using.
func (d *Daemon) SetAuthUserKey(key string) error {
	var err error

	file := d.WorkingDir + "/" + authKeysFile
	if key != "" {
		d.Logger.Infof("Received public key for incoming user")
		if err = WriteAuthorizedKey([]byte(key), file); err != nil {
			err = fmt.Errorf("write of %s failed: %v", file, err)
		}
	}

	if err != nil || key == "" {
		os.Remove(file)
	}
	return err
}

// Alive indicates whether the sshd daemon is successfully configured and
// running
func (d *Daemon) Alive() bool {
	return d.alive
}

// Generate a default sugared logger, which is used if the called doesn't
// provide one.
func newLogger() *zap.SugaredLogger {
	zapConfig := zap.NewDevelopmentConfig()
	zapConfig.Level = zap.NewAtomicLevel()
	zapConfig.DisableStacktrace = true
	zapConfig.DisableCaller = true
	logger, _ := zapConfig.Build()
	return logger.Sugar()
}

// Finalize kills any sshd child process and deletes the various files it
// created.
func (d *Daemon) Finalize() {
	d.shouldExit <- true
	d.wg.Wait()
	os.RemoveAll(d.WorkingDir)
}

// NewSshd launches a child sshd daemon.
//
// Before launching the daemon, NewSshd creates an ssh keypair that can be used
// to allow an incoming client to verify that it is connecting to the correct
// sshd daemon.
func NewSshd(config *DaemonConfig) (*Daemon, error) {
	var logger, childLogger *zap.SugaredLogger
	var port int
	var err error

	// Extract parameters from the config struct.  Supply defaults for
	// missing parameters.
	if config.Template == "" {
		return nil, fmt.Errorf("must provide an sshd_config template")
	}
	if port = config.Port; port == 0 {
		if port, err = choosePort(); err != nil {
			return nil, err
		}
	}
	if logger = config.Logger; logger == nil {
		logger = newLogger()
	}
	if childLogger = config.ChildLogger; childLogger == nil {
		childLogger = newLogger()
	}

	// Create a working directory to store our keys and configs
	wdir, err := ioutil.TempDir("/tmp", "sshd-")
	if err != nil {
		return nil, fmt.Errorf("creating working dir: %v", err)
	}

	if err = os.Chmod(wdir, 0755); err != nil {
		return nil, fmt.Errorf("unable to chmod %s: %v", wdir, err)
	}

	_, public, err := GenerateSSHKeypair(wdir + "/" + hostKeyFile)
	if err != nil {
		return nil, fmt.Errorf("generating host keys: %v", err)
	}

	d := &Daemon{
		WorkingDir:    wdir,
		UserName:      config.UserName,
		template:      config.Template,
		Port:          port,
		Logger:        logger,
		ChildLogger:   childLogger,
		shouldExit:    make(chan bool, 1),
		HostPublicKey: public,
		alive:         true,
	}

	d.wg.Add(1)
	go d.loop()

	return d, nil
}
