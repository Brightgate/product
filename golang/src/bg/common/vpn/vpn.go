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

	"bg/base_def"
	"bg/common/cfgapi"

	dhcp "github.com/krolaw/dhcp4"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

const lastMacProp = "@/network/vpn/last_mac"

var (
	config *cfgapi.Handle

	rings cfgapi.RingMap

	vpnStart  net.IP
	vpnRouter net.IP
	vpnSpan   int
)

type keyConfig struct {
	// Used to help match an issued key with its associated config data
	User  string
	ID    string
	Label string

	ClientAddr       string // IP address assigned to client device
	ClientPrivateKey string // Client's private key

	ServerAddress   string // Internet facing DNS or IP address
	ServerPublicKey string // Server's public key
	ServerPort      int    // Port open on the internet

	DNSAddress string // Address of DNS server (= VPN ring router)
	AllowedIPs string // Ring subnets that should be routed over VPN
}

const confTemplate = `
# Client key {{.User}} {{.ID}} {{.Label}}

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

func getServerConfig(conf *keyConfig) error {
	var port string

	props, err := config.GetProps("@/network/vpn")
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

	conf.DNSAddress = vpnRouter.String()

	return nil
}

// Choose an address in the VPN subnet that isn't already in use by some other
// client.
func chooseIPAddr(users cfgapi.UserMap) (string, error) {
	// Build a map of all possible addresses in the VPN ring's subnet
	available := make(map[string]bool)
	for i := 0; i < vpnSpan; i++ {
		str := dhcp.IPAdd(vpnStart, i).String()
		available[str] = true
	}

	// Remove all in-use addresses from the available list
	for _, u := range config.GetUsers() {
		for _, key := range u.WGConfig {
			delete(available, key.WGAssignedIP)
		}
	}
	// The router address isn't available either
	delete(available, vpnRouter.String())

	for addr := range available {
		return addr, nil
	}

	return "", fmt.Errorf("no addresses available")
}

// Given a list of rings we want the client to access, return a list of the
// subnets to include in its route table.
func chooseRoutedSubnets(routedRings string) (string, error) {
	subnets := make([]string, 0)      // list of subnets to include
	included := make(map[string]bool) // used to avoid duplicates

	ringList := strings.Split(routedRings, ",")
	ringList = append(ringList, "vpn")

	for _, ring := range ringList {
		if c, ok := rings[ring]; ok {
			if !included[ring] {
				subnets = append(subnets, c.Subnet)
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

func updateConfig(lastMac string, props map[string]string) error {
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

	_, err := config.Execute(context.Background(), ops).Wait(nil)
	return err
}

// AddKey generates a new client wireguard key, inserts the related properties
// into the config tree, and returns the contents of a wireguard config file to
// the caller.
//
// The caller can optionally identify the rings that should be routed over the
// VPN, a label that should be associated with the key, and the IP address the
// connecting client should be assigned,
func AddKey(name, rings, label, ipaddr string) ([]byte, error) {
	var rval []byte
	var err error

	retries := 0
Retry:
	users := config.GetUsers()
	if ipaddr == "" {
		if ipaddr, err = chooseIPAddr(users); err != nil {
			return nil, err
		}
	} else if ip := net.ParseIP(ipaddr); ip == nil {
		return nil, fmt.Errorf("bad client address: %s", ipaddr)
	}

	lastMac, newMac, err := chooseMacAddress(users)
	if err != nil {
		return nil, fmt.Errorf("choosing a new mac address: %v", err)
	}

	user := users[name]
	if user == nil {
		return nil, fmt.Errorf("no such user")
	}

	subnets, err := chooseRoutedSubnets(rings)
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
		AllowedIPs:       subnets,
	}
	if label != "" {
		conf.Label = "(" + label + ")"
	}

	if err = getServerConfig(&conf); err != nil {
		return nil, err
	}

	err = updateConfig(lastMac, props)
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
func RemoveKey(name, mac string) error {
	prop := "@/users/" + name + "/vpn/" + mac

	err := config.DeleteProp(prop)
	if err != nil && err != cfgapi.ErrNoProp {
		return fmt.Errorf("deleting %s: %v", prop, err)
	}

	return nil
}

// Init must be called before any other vpn function.
func Init(c *cfgapi.Handle) error {
	var err error
	var vpnRing *cfgapi.RingConfig

	config = c
	if rings = config.GetRings(); rings == nil {
		err = fmt.Errorf("unable to fetch ring configs")

	} else if vpnRing = rings[base_def.RING_VPN]; vpnRing == nil {
		err = fmt.Errorf("VPN ring is unconfigured")

	} else {
		start, ipnet, _ := net.ParseCIDR(vpnRing.Subnet)
		ones, bits := ipnet.Mask.Size()

		vpnStart = start
		vpnSpan = (1<<uint32(bits-ones) - 3)
		vpnRouter = dhcp.IPAdd(vpnStart, 1)
	}

	return err
}
