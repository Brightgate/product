/*
 * Copyright 2019 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package ssh

import (
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"golang.org/x/crypto/ssh"
)

// TunnelConfig contains all the settings necessary to construct an ssh tunnel
type TunnelConfig struct {
	LocalHost     string // hostname/address[:port] for the local endpoint
	TargetHost    string // hostname/address[:port] for the remote endpoint
	TargetHostKey string // remote host's public key
	TargetUser    string // username to use when establishing the tunnel
	TunnelPort    int    // port # to open on the remote side
	Logger        *zap.SugaredLogger
}

// Tunnel represents an opaque handle to a configured ssh tunnel
type Tunnel struct {
	localHost      string        // ssh endpoint on the side of the tunnel
	targetHost     string        // ssh endpoint on the remote side
	targetUser     string        // username to use when connecting
	tunnelPort     string        // port # to open on remote host
	publicUserKey  string        // public key we'll use when connecting
	privateUserKey ssh.Signer    // private key we'll use
	publicHostKey  ssh.PublicKey // the remote host's public ssh keys

	server   *ssh.Client
	listener net.Listener
	slog     *zap.SugaredLogger

	sync.Mutex
}

// a tunnel may have multiple channels, each representing an independent stream
// of tunneled data
type channel struct {
	id     int
	tunnel *Tunnel
	remote net.Conn
	local  net.Conn

	close chan bool
	wg    sync.WaitGroup
}

func (c *channel) forwardPort(wg *sync.WaitGroup, a, b net.Conn) {
	const ErrNetClosing = "use of closed network connection"

	_, err := io.Copy(a, b)

	// because Go's error handling is garbage...
	if err != nil && !strings.Contains(err.Error(), ErrNetClosing) {
		c.tunnel.slog.Warnf("tunnel error: %s", err)
	}
	c.close <- true
	wg.Done()
}

// Given two open network connections A and B, all traffic arriving on A should
// be written out to B and vice versa.
func (c *channel) forwardTraffic() {
	var forwardWg sync.WaitGroup

	forwardWg.Add(2)
	go c.forwardPort(&forwardWg, c.local, c.remote)
	go c.forwardPort(&forwardWg, c.remote, c.local)

	// Keep forwarding traffic until we're told not to, or until one or more
	// of the forwarding ports close.
	<-c.close

	// close the forwarding ports, which will kill the goroutines as a side
	// effect
	c.local.Close()
	c.remote.Close()

	// wait for the goroutines to exit
	forwardWg.Wait()
	c.tunnel.slog.Infof("closed tunneled channel %d", c.id)

	c.wg.Done()
}

// Wait for connections on the remote tunnel port.  For each new connection,
// spawn a goroutine to forward all traffic to/from that port.
func (t *Tunnel) handleConnections() {
	var local, remote net.Conn
	var err error

	channels := make([]*channel, 0)
	listener := t.listener
	laddr := t.localHost

	for {
		if remote, err = listener.Accept(); err != nil {
			break
		}

		local, err = net.Dial("tcp", laddr)
		if err != nil {
			t.slog.Errorf("connecting to local sshd: %v", err)
			remote.Close()
		} else {
			c := &channel{
				id:     len(channels),
				tunnel: t,
				remote: remote,
				local:  local,
				close:  make(chan bool, 3),
			}
			c.wg.Add(1)
			channels = append(channels, c)
			t.slog.Infof("opened tunneled channel %d", c.id)
			go c.forwardTraffic()
		}
	}

	if err != io.EOF {
		t.slog.Warnf("tunnel failed: %v", err)
	}
	t.slog.Debugf("closing tunneled connections")
	for _, c := range channels {
		c.close <- true
		c.wg.Wait()
	}
	t.slog.Infof("ssh tunnel is closed")
}

// Close closes the incoming tunnel port, and shuts down any existing tunneled
// connections.
func (t *Tunnel) Close() {
	t.Lock()
	defer t.Unlock()

	if t.server != nil {
		t.server.Close()
		t.server = nil
	}
	if t.listener != nil {
		// Closing the listener port will cause the Accept() in
		// handleConnections() to fail, which will result in all open
		// connections being closed.
		t.listener.Close()
		t.listener = nil
	}
}

// Open triggers the tunnel creation, first by establishing an ssh connection to
// the remote endpoint, then requesting that the endpoint open a port for
// forwarding, and finally by launching a goroutine that will handle connections
// arriving over that port.
func (t *Tunnel) Open() error {
	t.Lock()
	defer t.Unlock()

	if t.IsOpen() {
		return fmt.Errorf("tunnel is already open")
	}

	config := &ssh.ClientConfig{
		User: t.targetUser,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(t.privateUserKey),
		},
		HostKeyCallback: ssh.FixedHostKey(t.publicHostKey),
		Timeout:         15 * time.Second,
	}

	t.slog.Infof("opening ssh connection to %s", t.targetHost)
	server, err := ssh.Dial("tcp", t.targetHost, config)
	if err != nil {
		return fmt.Errorf("dialing to remote tunnel: %v", err)
	}

	// Open a port on the remote side for tunnelers to connect to
	t.slog.Debugf("opening remote port %s", t.tunnelPort)
	tunnelAddr := "localhost:" + t.tunnelPort
	listener, err := server.Listen("tcp", tunnelAddr)
	if err != nil {
		server.Close()
		return fmt.Errorf("opening tunnel port on server: %v", err)
	}
	t.slog.Infof("ssh tunnel is open to %s", t.targetHost)

	t.server = server
	t.listener = listener
	go t.handleConnections()

	return nil
}

// IsOpen indicates whether the tunnel has an established connection to the
// remote endpoint.
func (t *Tunnel) IsOpen() bool {
	return (t.server != nil && t.listener != nil)
}

// GetUserPublicKey returns a string containing the public key that will be used
// when connecting to the remote ssh endpoint.
func (t *Tunnel) GetUserPublicKey() string {
	return t.publicUserKey
}

// NewTunnel prepares to construct a tunnel to the endpoint described by the
// provided TunnelConfig.  It will also generate an ssh keypair that will be
// used to connect to that endpoint.
func NewTunnel(config *TunnelConfig) (*Tunnel, error) {
	switch {
	case config.LocalHost == "":
		return nil, fmt.Errorf("missing LocalHost")
	case config.TargetHost == "":
		return nil, fmt.Errorf("missing TargetHost")
	case config.TargetUser == "":
		return nil, fmt.Errorf("missing TargetUser")
	case config.TargetHostKey == "":
		return nil, fmt.Errorf("missing TargetHostKey")
	case config.TunnelPort == 0:
		return nil, fmt.Errorf("missing TunnelPort")
	}

	parsedHostKey, err := ParsePublicKey([]byte(config.TargetHostKey))
	if err != nil {
		return nil, fmt.Errorf("bad host key: %v", err)
	}

	userPrivate, userPublic, err := generateRSAKeypair()
	if err != nil {
		return nil, fmt.Errorf("generating user keys: %v", err)
	}
	parsedUserKey, err := ssh.ParsePrivateKey(userPrivate)
	if err != nil {
		return nil, fmt.Errorf("parsing user private key: %v", err)
	}

	t := &Tunnel{
		localHost:      config.LocalHost,
		targetHost:     config.TargetHost,
		targetUser:     config.TargetUser,
		tunnelPort:     strconv.Itoa(config.TunnelPort),
		publicUserKey:  string(userPublic),
		privateUserKey: parsedUserKey,
		publicHostKey:  parsedHostKey,
		slog:           config.Logger,
	}

	// If the caller didn't specify which port(s) to use for the ssh
	// connections, default to the standard port 22.
	if strings.Index(t.targetHost, ":") == -1 {
		t.targetHost += ":22"
	}
	if strings.Index(t.localHost, ":") == -1 {
		t.localHost += ":22"
	}

	if t.slog == nil {
		t.slog = newLogger()
	}

	return t, nil
}

