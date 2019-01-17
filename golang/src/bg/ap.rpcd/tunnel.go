/*
 * COPYRIGHT 2019 Brightgate Inc. All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package main

import (
	"fmt"
	"strconv"
	"sync"
	"time"

	"bg/ap_common/aputil"
	"bg/common/ssh"
)

// The following code creates and maintains an ssh reverse tunnel allowing a
// cloud-based user to connect to the appliance for service and/or
// troubleshooting.  See cl-service.go for a more detailed description.
//
// Todo:
//   - Add a 'service user' account and disable login-as-root

type propChange struct {
	key   string
	value string
}

var (
	sshKeyLifetime = 3 * time.Hour

	tstate struct {
		daemon *ssh.Daemon
		tunnel *ssh.Tunnel

		properties     map[string]string
		pendingChanges []*propChange

		nextDaemonAttempt time.Time // when to try launching sshd
		nextTunnelAttempt time.Time // when to try opening tunnel
		wantOpen          bool      // do we want the tunnel to be open?
		active            sync.WaitGroup

		sync.Mutex
	}

	// We want to open a tunnel iff all four of the required settings have
	// been pushed into the config tree.
	neededProps = []string{
		"cloud_host", "cloud_user", "cloud_host_key", "tunnel_port",
	}
)

// launch an ssh daemon to handle this side of the service tunnel
func sshDaemonInit() (*ssh.Daemon, error) {
	template := aputil.ExpandDirPath(*templateDir) + "/sshd_config.got"
	clogger, err := aputil.NewChildLogger()
	if err != nil {
		clogger = slog
	}

	cfg := &ssh.DaemonConfig{
		Port:        0,
		Logger:      slog,
		ChildLogger: clogger,
		Template:    template,
	}
	d, err := ssh.NewSshd(cfg)
	if err != nil {
		return nil, fmt.Errorf("unable to spawn sshd child: %v", err)
	}
	// let the daemon know which key the incoming user will provide
	d.SetAuthUserKey(tstate.properties["cloud_user_key"])

	return d, nil
}

// The keypair we use for connecting to the cloud system has changed.  Sanity
// check the new public key and push it into the config tree.
func updateLocalUserKey(key string) error {
	var err error

	prop := "tunnel_user_key"
	path := "@/cloud/service/" + prop

	if key == "" {
		if err = config.DeleteProp(path); err != nil {
			err = fmt.Errorf("failed to delete %s: %v", path, err)
		}
	} else {
		exp := time.Now().Add(sshKeyLifetime)
		if _, err = ssh.ParsePublicKey([]byte(key)); err != nil {
			err = fmt.Errorf("bad public key: %v", err)

		} else if err = config.CreateProp(path, key, &exp); err != nil {
			err = fmt.Errorf("failed to update %s: %v", path, err)
		}
	}

	tstate.properties[prop] = key
	return err
}

func processConfigChanges() {
	var authUserKey *string
	var changed bool

	tstate.Lock()
	changes := tstate.pendingChanges
	tstate.pendingChanges = make([]*propChange, 0)
	tstate.Unlock()

	for _, c := range changes {
		// Ignore any updates that affect unknown properties, or which don't
		// actually change anything.
		if old, ok := tstate.properties[c.key]; ok && old != c.value {
			slog.Debugf("changing %s from '%s' to '%s'", c.key,
				old, c.value)
			changed = true
			tstate.properties[c.key] = c.value

			// If the cloud_user_key changes, we need to notify the
			// sshd daemon that relies on it.
			if c.key == "cloud_user_key" {
				authUserKey = &c.value
			}
		}
	}

	if changed {
		if authUserKey != nil && tstate.daemon != nil {
			tstate.daemon.SetAuthUserKey(*authUserKey)
		}
		tunnelClose()
	}

	// See if we still have all the properties we need to maintain a tunnel
	tstate.wantOpen = true
	for _, prop := range neededProps {
		if tstate.properties[prop] == "" {
			tstate.wantOpen = false
		}
	}
}

func queueConfigChange(key, value string) {
	c := &propChange{
		key:   key,
		value: value,
	}

	tstate.Lock()
	tstate.pendingChanges = append(tstate.pendingChanges, c)
	tstate.Unlock()
}

func handleUpdateEvent(path []string, val string, expires *time.Time) {
	if len(path) == 3 {
		queueConfigChange(path[2], val)
	}
}

func handleDeleteEvent(path []string) {
	if len(path) == 2 {
		// If the whole subtree is deleted, clean up each of the cached
		// values individually.
		for prop := range tstate.properties {
			queueConfigChange(prop, "")
		}
	} else if len(path) == 3 {
		queueConfigChange(path[2], "")
	}
}

// If we want a service tunnel and the sshd daemon isn't running, start it.
func daemonUpdate() {
	if !tstate.wantOpen {
		return
	}

	// If the daemon crashed, clean up any lingering state and prepare to
	// relaunch
	if d := tstate.daemon; d != nil && !d.Alive() {
		daemonCleanup()
		tstate.nextDaemonAttempt = time.Unix(0, 0)
	}

	// If we have no sshd daemon running, launch one.
	now := time.Now()
	if tstate.daemon == nil && now.After(tstate.nextDaemonAttempt) {
		var err error

		if tstate.daemon, err = sshDaemonInit(); err != nil {
			slog.Errorf("initting sshd daemon: %v", err)
			// If NewSshd() fails, there is no reason to believe
			// that retrying will help.
			tstate.nextDaemonAttempt = now.Add(24 * time.Hour)
		} else {
			tstate.nextTunnelAttempt = now
		}
	}
}

func daemonCleanup() {
	if d := tstate.daemon; d != nil {
		d.Finalize()
		tstate.daemon = nil
	}
}

// shut down any active tunnel to the cloud
func tunnelClose() {
	if t := tstate.tunnel; t != nil {
		t.Close()
	}
	tstate.nextDaemonAttempt = time.Now()
	tstate.nextTunnelAttempt = time.Now()
}

func newTunnel() (*ssh.Tunnel, error) {
	port := tstate.properties["tunnel_port"]
	portNo, err := strconv.Atoi(port)
	if err != nil {
		return nil, fmt.Errorf("bad port '%s': %v", port, err)
	}

	localPort := strconv.Itoa(tstate.daemon.Port)
	config := ssh.TunnelConfig{
		LocalHost:     "127.0.0.1:" + localPort,
		TargetUser:    tstate.properties["cloud_user"],
		TargetHost:    tstate.properties["cloud_host"],
		TargetHostKey: tstate.properties["cloud_host_key"],
		TunnelPort:    portNo,
		Logger:        slog,
	}

	return ssh.NewTunnel(&config)
}

// If we want a service tunnel and the ssh connection isn't up, bring it up.
func tunnelUpdate() {
	var err error

	// Don't open the tunnel until we have all the necessary parameters, and
	// until we've successfully spawned an ssh daemon to serve as the local
	// endpoint for the tunnel.
	if !tstate.wantOpen || tstate.daemon == nil {
		if tstate.tunnel != nil {
			tstate.tunnel.Close()
			tstate.tunnel = nil
			updateLocalUserKey("")
		}
		return
	}

	now := time.Now()
	if now.Before(tstate.nextTunnelAttempt) {
		return
	}

	// Create a new Tunnel
	if t := tstate.tunnel; t == nil {
		if t, err = newTunnel(); err == nil {
			// We generated a fresh userkey as a side effect of
			// preparing the tunnel.  Push it into the config tree.
			err = updateLocalUserKey(t.GetUserPublicKey())
		}
		if err != nil {
			slog.Errorf("preparing tunnel: %v", err)
			tstate.nextTunnelAttempt = now.Add(24 * time.Hour)
			return
		}
		tstate.tunnel = t
	}

	// Open the tunnel
	if t := tstate.tunnel; !t.IsOpen() {
		if err = t.Open(); err != nil {
			slog.Errorf("opening tunnel: %v", err)
			tstate.nextTunnelAttempt = now.Add(5 * time.Second)
		} else {
			slog.Infof("remote tunnel opened")
		}
	}
}

func tunnelConfigInit() {
	tstate.properties = map[string]string{
		"cloud_user":      "",
		"cloud_user_key":  "",
		"cloud_host":      "",
		"cloud_host_key":  "",
		"tunnel_port":     "",
		"tunnel_user_key": "",
	}

	if props, err := config.GetProps("@/cloud/service"); err == nil {
		for key, n := range props.Children {
			if n.Expires == nil || n.Expires.After(time.Now()) {
				queueConfigChange(key, n.Value)
			}
		}
	}
	processConfigChanges()

	config.HandleChange(`^@/cloud/service/.*$`, handleUpdateEvent)
	config.HandleDelete(`^@/cloud/service.*$`, handleDeleteEvent)
	config.HandleExpire(`^@/cloud/service/.*$`, handleDeleteEvent)
}

// Open and maintain an ssh tunnel connection to a cloud endpoint.  Launch (and
// keep launched) an ssh daemon that will serve as the endpoint for connections
// coming in over that established tunnel.
func tunnelLoop(wg *sync.WaitGroup, doneChan chan bool) {
	slog.Infof("tunnel loop starting")

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	tunnelConfigInit()
	done := false
	for !done {
		processConfigChanges()
		daemonUpdate()
		tunnelUpdate()

		select {
		case <-ticker.C:
		case <-doneChan:
			done = true
		}
	}

	tunnelClose()
	daemonCleanup()

	slog.Infof("tunnel loop done")
	wg.Done()
}
