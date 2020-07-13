/*
 * COPYRIGHT 2020 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package wgconf

import (
	"fmt"
	"net"
	"strconv"
	"sync"

	"bg/base_def"
)

// UserConf contains the information needed for a WireGuard server to
// authenticate an incoming client and its traffic.
type UserConf struct {
	ID    int
	Mac   string // Artificial MAC address used for accounting
	User  string // owner of the key
	Label string // User-defined value to distinguish between configs

	Endpoint
}

// Server contains the information needed to instantiate a WireGuard server and
// to populate the server-related fields in a client configuration file.
type Server struct {
	Address    string // Publicly reachable hostname or IP address
	Keyfile    string // Location of the private key
	ListenPort int    // Port for incoming connections

	UserKeys map[string]*UserConf // Authorized user keys

	Endpoint // WireGuard device details

	sync.Mutex
}

func (s *Server) validateLocked() (bool, error) {
	var changed bool
	var err error

	addr := s.Address
	ip := net.ParseIP(addr)
	if ip == nil {
		addrs, err := net.LookupHost(addr)
		if err == nil && len(addrs) > 0 {
			ip = net.ParseIP(addrs[0])
		}
	}

	if ip == nil {
		err = fmt.Errorf("bad server address: '%s'", addr)
	} else {
		var old string

		if s.IPAddress != nil {
			old = s.IPAddress.String()
		}

		s.setIPAddressLocked(ip)
		changed = (old != s.IPAddress.String())
	}

	return changed, err
}

// ValidateRemoteAddress verifies that a server's hostname can be resolved to an
// IP address.  If so, and if that address doesn't match the current address,
// the IP address is updated.  The call returns 'true' if the address has
// changed, 'false' if it hasn't, and an error if the hostname cannot be
// resolved.  If the server is identified by IP address, this operation is a
// no-op, and will return 'false'.
func (s *Server) ValidateRemoteAddress() (bool, error) {
	s.Lock()
	defer s.Unlock()

	return s.validateLocked()
}

// SetRemoteAddress verifies that the string represents a valid IP address or
// hostname, and uses it to update the Address and AssignedIP fields.
func (s *Server) SetRemoteAddress(addr string) error {
	s.Lock()
	defer s.Unlock()

	s.Address = addr
	_, err := s.validateLocked()

	return err
}

// SetListenPort verifies that the string represents a valid port number and
// uses it to update the ListenPort field.
func (s *Server) SetListenPort(port string) error {
	var err error

	s.Lock()
	defer s.Unlock()

	s.ListenPort = 0
	p, err := strconv.Atoi(port)
	if err == nil && p > 0 && p < 65536 {
		s.ListenPort = p
	} else {
		err = fmt.Errorf("invalid port number: '%s'", port)
	}

	fmt.Printf("Set port to %d\n", s.ListenPort)
	return err
}

// AddClientKey adds a single client key to this servers set of allowed keys
func (s *Server) AddClientKey(key *UserConf) error {
	s.UserKeys[key.Mac] = key
	return nil
}

func (s *Server) getKey(mac string) *UserConf {
	key := s.UserKeys[mac]
	if key == nil {
		key = &UserConf{Mac: mac}
		s.UserKeys[mac] = key
	}
	return key
}

// SetClientKeyIP updates the allowed IP address for a given client key
func (s *Server) SetClientKeyIP(mac, ip string) error {
	s.Lock()
	defer s.Unlock()

	return s.getKey(mac).SetIPAddress(ip)
}

// SetClientKeySubnets updates the routable subnets for a given client key
func (s *Server) SetClientKeySubnets(mac, subnets string) error {
	s.Lock()
	defer s.Unlock()

	return s.getKey(mac).SetIPAddress(subnets)
}

// SetClientKeyPublic updates public key for a given client config
func (s *Server) SetClientKeyPublic(mac, public string) error {
	s.Lock()
	defer s.Unlock()

	return s.getKey(mac).SetKey(public)
}

// DeleteClientKey removes a key from the list of allowed clients fopr this
// server
func (s *Server) DeleteClientKey(mac string) {
	s.Lock()
	defer s.Unlock()

	delete(s.UserKeys, mac)
}

// SetServerPublic is a no-op.  It exists for symmetry with the client-side
// operations.
func (s *Server) SetServerPublic(mac, public string) {
	// No-op
}

// Delete depopulates a Server structure.
func (s *Server) Delete() {
	s.Lock()
	defer s.Unlock()

	s.Keyfile = ""
	s.Devname = ""
	s.UserKeys = nil
	s.ListenPort = 0
}

// NewServer returns a Server structure with the basic server-side fields
// populated, and an empty alllowed users list.
func NewServer(keyfile, device string) *Server {
	s := &Server{
		Keyfile:    keyfile,
		UserKeys:   make(map[string]*UserConf),
		ListenPort: base_def.WIREGUARD_PORT,
	}
	s.Devname = device
	return s
}

// ToEndpoint extracts the local endpoint portion of a Server structure
func (s *Server) ToEndpoint() *Endpoint {
	return &s.Endpoint
}
