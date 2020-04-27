/*
 * COPYRIGHT 2020 Brightgate Inc. All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or  alteration will be a violation of federal law.
 */

package vpn

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"text/template"
	"time"

	"bg/base_def"
	"bg/common/cfgapi"

	dhcp "github.com/krolaw/dhcp4"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

// File locations and vpn-related properties
const (
	SecretDir   = "__APSECRET__/vpn"
	PrivateFile = "private_key"
	EnabledProp = "@/policy/site/vpn/enabled"
	RingsProp   = "@/policy/site/vpn/rings"

	PublicProp   = "@/network/vpn/public_key"
	EscrowedProp = "@/network/vpn/escrowed_key"
	PortProp     = "@/network/vpn/port"

	lastMacProp = "@/network/vpn/last_mac"
)

// Vpn is an opaque handle which is used to perform vpn-related config
// operations.
type Vpn struct {
	config *cfgapi.Handle

	updateCallback func(net.HardwareAddr, net.IP)

	subnets   map[string]string
	vpnStart  net.IP
	vpnRouter net.IP
	vpnSpan   int
}

type keyConfig struct {
	// Used to help match an issued key with its associated config data
	User  string
	ID    string
	Label string

	ClientAddr       string // IP address assigned to client device
	ClientPrivateKey string // Client's private key
	ClientPublicKey  string // Client's public key

	ServerAddress   string // Internet facing DNS or IP address
	ServerPublicKey string // Server's public key
	ServerPort      int    // Port open on the internet

	DNSAddress string // Address of DNS server (= VPN ring router)
	AllowedIPs string // Ring subnets that should be routed over VPN
}

const confTemplate = `
# Client key {{.User}} {{.ID}} {{.Label}} {{.ClientPublicKey}}

[Interface]
Address = {{.ClientAddr}}/32
PrivateKey = {{.ClientPrivateKey}}
DNS = {{.DNSAddress}}

[Peer]
PublicKey = {{.ServerPublicKey}}
Endpoint = {{.ServerAddress}}:{{.ServerPort}}
AllowedIPs = {{.AllowedIPs}}

PersistentKeepalive = 25
`

func getVal(tree *cfgapi.PropertyNode, prop string) (string, error) {
	node, ok := tree.Children[prop]
	if ok {
		return node.Value, nil
	}

	return "", fmt.Errorf("missing @/network/vpn/%s", prop)
}

// Generate a wireguard config file to be deployed by the client
func genConfig(conf keyConfig) ([]byte, error) {
	tmpl, err := template.New("wireguard").Parse(confTemplate)
	if err != nil {
		return nil, fmt.Errorf("parsing template: %v", err)
	}
	b := new(bytes.Buffer)
	if err := tmpl.Execute(b, conf); err != nil {
		return nil, fmt.Errorf("generating config file: %v", err)
	}

	return b.Bytes(), nil

}

func (v *Vpn) getServerConfig(conf *keyConfig) error {
	var port string

	props, err := v.config.GetProps("@/network/vpn")
	if err != nil {
		return fmt.Errorf("fetching server vpn config: %v", err)
	}

	if conf.ServerPublicKey, err = getVal(props, "public_key"); err != nil {
		return err
	}

	if conf.ServerAddress, err = getVal(props, "address"); err != nil {
		return err
	}

	if port, err = getVal(props, "port"); err != nil {
		return err
	}

	if conf.ServerPort, err = strconv.Atoi(port); err != nil {
		return fmt.Errorf("bad port number %s: %v", port, err)
	}

	conf.DNSAddress = v.vpnRouter.String()

	return nil
}

// Choose an address in the VPN subnet that isn't already in use by some other
// client.
func (v *Vpn) chooseIPAddr(users cfgapi.UserMap) (string, error) {
	// Build a map of all possible addresses in the VPN ring's subnet
	available := make(map[string]bool)
	for i := 0; i < v.vpnSpan; i++ {
		str := dhcp.IPAdd(v.vpnStart, i).String()
		available[str] = true
	}

	// Remove all in-use addresses from the available list
	for _, u := range v.config.GetUsers() {
		for _, key := range u.WGConfig {
			delete(available, key.WGAssignedIP)
		}
	}
	// The router address isn't available either
	delete(available, v.vpnRouter.String())

	for addr := range available {
		return addr, nil
	}

	return "", fmt.Errorf("no addresses available")
}

// Given a list of rings we want the client to access, return a list of the
// subnets to include in its route table.
func (v *Vpn) chooseRoutedSubnets(routedRings string) (string, error) {
	subnets := make([]string, 0)      // list of subnets to include
	included := make(map[string]bool) // used to avoid duplicates

	ringList := strings.Split(routedRings, ",")
	ringList = append(ringList, "vpn")

	for _, ring := range ringList {
		if subnet, ok := v.subnets[ring]; ok {
			if !included[ring] {
				subnets = append(subnets, subnet)
				included[ring] = true
			}
		} else {
			return "", fmt.Errorf("no such ring: %s", ring)
		}
	}

	return strings.Join(subnets, ","), nil
}

// Scan @/users/<user>/vpn/* to find the next available index
func chooseIndex(user *cfgapi.UserInfo) string {
	next := 1
	for _, key := range user.WGConfig {
		if key.ID >= next {
			next = key.ID + 1
		}
	}

	return strconv.Itoa(next)
}

func (v *Vpn) updateConfig(lastMac string, props map[string]string) error {

	ops := make([]cfgapi.PropertyOp, 0)

	if lastMac != "" {
		// This op ensures that the pool of available mac addresses
		// hasn't changed since we chose one for this key.
		op := cfgapi.PropertyOp{
			Op:    cfgapi.PropTestEq,
			Name:  lastMacProp,
			Value: lastMac,
		}
		ops = append(ops, op)
	}
	for prop, val := range props {
		op := cfgapi.PropertyOp{
			Op:    cfgapi.PropCreate,
			Name:  prop,
			Value: val,
		}
		ops = append(ops, op)
	}

	_, err := v.config.Execute(context.Background(), ops).Wait(nil)
	return err
}

// AddKey generates a new client wireguard key, inserts the related properties
// into the config tree, and returns the contents of a wireguard config file to
// the caller.
//
// The caller can optionally identify a label that should be associated with the
// key, and the IP address the connecting client should be assigned,
func (v *Vpn) AddKey(name, label, ipaddr string) ([]byte, error) {
	var rval []byte
	var err error

	retries := 0
Retry:
	users := v.config.GetUsers()
	if ipaddr == "" {
		if ipaddr, err = v.chooseIPAddr(users); err != nil {
			return nil, err
		}
	} else if ip := net.ParseIP(ipaddr); ip == nil {
		return nil, fmt.Errorf("bad client address: %s", ipaddr)
	}

	lastMac, newMac, err := v.chooseMacAddress(users)
	if err != nil {
		return nil, fmt.Errorf("choosing a new mac address: %v", err)
	}

	user := users[name]
	if user == nil {
		return nil, fmt.Errorf("no such user")
	}

	rings, _ := v.config.GetProp(RingsProp)
	subnets, err := v.chooseRoutedSubnets(rings)
	if err != nil {
		return nil, err
	}

	private, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		return nil, fmt.Errorf("generating wireguard private key: %v", err)
	}
	public := private.PublicKey()

	id := chooseIndex(user)
	base := "@/users/" + name + "/vpn/" + newMac + "/"
	props := map[string]string{
		base + "public_key":  public.String(),
		base + "assigned_ip": ipaddr,
		base + "id":          id,
		lastMacProp:          newMac,
	}
	if label != "" {
		props[base+"label"] = label
	}

	conf := keyConfig{
		User:             name,
		ID:               id,
		ClientAddr:       ipaddr,
		ClientPrivateKey: private.String(),
		ClientPublicKey:  public.String(),
		AllowedIPs:       subnets,
	}
	if label != "" {
		conf.Label = "(" + label + ")"
	}

	if err = v.getServerConfig(&conf); err != nil {
		return nil, err
	}

	err = v.updateConfig(lastMac, props)
	if err == nil {
		rval, err = genConfig(conf)
	} else if err == cfgapi.ErrNotEqual {
		if retries++; retries < 5 {
			goto Retry
		}
		err = fmt.Errorf("excess mac collisions")
	}

	return rval, err
}

// RemoveKey removes the config properties associated with a single wireguard
// key.
func (v *Vpn) RemoveKey(name, mac string) error {
	prop := "@/users/" + name + "/vpn/" + mac

	err := v.config.DeleteProp(prop)
	if err != nil && err != cfgapi.ErrNoProp {
		return fmt.Errorf("deleting %s: %v", prop, err)
	}

	return nil
}

// IsEnabled checks whether the VPN functionality has been enabled for this site
func (v *Vpn) IsEnabled() bool {
	enabled, _ := v.config.GetPropBool("@/policy/site/vpn/enabled")
	return enabled
}

// GetKeys returns a mac->WireguardConfig map containing all of the keys
// configured for the given user.  If the user parameter is the empty string,
// the call will return all keys in the system.
func (v *Vpn) GetKeys(name string) (map[string]*cfgapi.WireguardConf, error) {
	var users cfgapi.UserMap

	rval := make(map[string]*cfgapi.WireguardConf)

	if name != "" {
		u, err := v.config.GetUser(name)
		if err != nil {
			return nil, err
		}
		users = cfgapi.UserMap{name: u}
	} else {
		users = v.config.GetUsers()
	}

	for _, conf := range users {
		if conf.WGConfig != nil {
			for _, key := range conf.WGConfig {
				rval[key.GetMac()] = key
			}
		}
	}

	return rval, nil
}

func (v *Vpn) userUpdateEvent(path []string, val string, expires *time.Time) {
	if len(path) == 5 && path[4] == "assigned_ip" {
		if ip := net.ParseIP(val); ip != nil {
			if mac, err := net.ParseMAC(path[3]); err == nil {
				v.updateCallback(mac, ip)
			}
		}
	}
}

func (v *Vpn) userDeleteEvent(path []string) {
	if len(path) == 5 && path[4] == "assigned_ip" {
		if mac, err := net.ParseMAC(path[3]); err == nil {
			v.updateCallback(mac, nil)
		}
	}
}

// RegisterMacIPHandler indicates that the caller wants to be notified of any
// changes to the mac->ip mappings maintained for vpn clients.
func (v *Vpn) RegisterMacIPHandler(cb func(net.HardwareAddr, net.IP)) error {
	if v.updateCallback != nil {
		return fmt.Errorf("vpn update callback already registered")
	}

	v.updateCallback = cb
	v.config.HandleChange(`^@/users/.*/vpn/.*`, v.userUpdateEvent)
	v.config.HandleDelete(`^@/users/.*/vpn/.*`, v.userDeleteEvent)
	v.config.HandleExpire(`^@/users/.*/vpn/.*`, v.userDeleteEvent)
	return nil
}

// NewVpn returns a Vpn handle associated with the provided configd handle.
func NewVpn(config *cfgapi.Handle) (*Vpn, error) {
	var vpnRing *cfgapi.RingConfig

	rings := config.GetRings()
	if rings == nil {
		return nil, fmt.Errorf("unable to fetch ring configs")
	}

	subnets := make(map[string]string)
	for name, ring := range rings {
		subnets[name] = ring.IPNet.String()
	}
	subnets[base_def.RING_WAN] = "0.0.0.0/0"

	if vpnRing = rings[base_def.RING_VPN]; vpnRing == nil {
		return nil, fmt.Errorf("VPN ring is unconfigured")
	}

	start, ipnet, _ := net.ParseCIDR(vpnRing.Subnet)
	ones, bits := ipnet.Mask.Size()

	v := Vpn{
		config:    config,
		subnets:   subnets,
		vpnStart:  start,
		vpnSpan:   (1<<uint32(bits-ones) - 3),
		vpnRouter: dhcp.IPAdd(start, 1),
	}

	return &v, nil
}
