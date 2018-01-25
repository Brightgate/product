/*
 * COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package apcfg

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/crypto/md4"

	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"

	"bg/ap_common/aputil"
	"bg/ap_common/broker"
	"bg/ap_common/device"
	"bg/base_def"
	"bg/base_msg"

	"github.com/golang/protobuf/proto"

	// Ubuntu: requires libzmq3-dev, which is 0MQ 4.2.1.
	zmq "github.com/pebbe/zmq4"
)

// ValidRings is a map containing all of the known ring names.  Checking for map
// membership is a simple way to whether a given name is valid.
var ValidRings = map[string]bool{
	base_def.RING_INTERNAL:   true,
	base_def.RING_UNENROLLED: true,
	base_def.RING_SETUP:      true,
	base_def.RING_CORE:       true,
	base_def.RING_STANDARD:   true,
	base_def.RING_DEVICES:    true,
	base_def.RING_GUEST:      true,
	base_def.RING_QUARANTINE: true,
}

// RingConfig defines the parameters of a ring's subnet
type RingConfig struct {
	Auth          string
	Subnet        string
	Bridge        string
	Vlan          int
	LeaseDuration int
}

// ClientInfo contains all of the configuration information for a client device
type ClientInfo struct {
	Ring       string     // Assigned security ring
	DNSName    string     // Assigned hostname
	IPv4       net.IP     // Network address
	Expires    *time.Time // DHCP lease expiration time
	DHCPName   string     // Requested hostname
	Identity   string     // Our current best guess at the client type
	Confidence string     // Our confidence for the Identity guess
	DNSPrivate bool       // We don't collect DNS queries
}

// UserInfo contains all of the configuration information for an appliance user
// account.  Expected roles are: "SITE_ADMIN", "SITE_USER",
// "SITE_GUEST", "CUST_ADMIN", "CUST_USER", "CUST_GUEST".
type UserInfo struct {
	UID               string // Username
	UUID              string
	Role              string // User role
	DisplayName       string // User's friendly name
	Email             string // User email
	PreferredLanguage string
	TelephoneNumber   string // User telephone number
	TOTP              string // Time-based One Time Password URL
	Password          string // bcrypt Password
	MD4Password       string // MD4 Password for WPA-EAP/MSCHAPv2
}

// RingMap maps ring names to the configuration information
type RingMap map[string]*RingConfig

// ClientMap maps a device's mac address to its configuration information
type ClientMap map[string]*ClientInfo

// UserMap maps an account's username to its configuration information
type UserMap map[string]*UserInfo

// PropertyNode is a single node in the property tree
type PropertyNode struct {
	Name     string
	Value    string          `json:"Value,omitempty"`
	Expires  *time.Time      `json:"Expires,omitempty"`
	Children []*PropertyNode `json:"Children,omitempty"`
}

func dumpSubtree(node *PropertyNode, level int) {
	indent := ""
	for i := 0; i < level; i++ {
		indent += "  "
	}
	e := ""
	if node.Expires != nil {
		e = node.Expires.Format("2006-02-01T15:04:05")
	}
	fmt.Printf("%s%s: %s  %s\n", indent, node.Name, node.Value, e)
	for _, n := range node.Children {
		dumpSubtree(n, level+1)
	}
}

// DumpTree displays the contents of a property tree in a human-legible format
func (n *PropertyNode) DumpTree() {
	dumpSubtree(n, 0)
}

// GetChild searches through a node's list of children, looking for one with a
// name matching the provided key.  Returns a pointer the child node if it finds
// a match, nil if it doesn't.
func (n *PropertyNode) GetChild(key string) *PropertyNode {
	for _, s := range n.Children {
		if s.Name == key {
			return s
		}
	}
	return nil
}

// GetChildByValue searches through a node's list of childrenn, looking for one
// with a value matching the provided key.  Returns a pointer the child node if
// it finds a match, nil if it doesn't.  If multiple children have the same
// value, this will return only the first one found.
func (n *PropertyNode) GetChildByValue(value string) *PropertyNode {
	for _, s := range n.Children {
		if s.Value == value {
			return s
		}
	}
	return nil
}

// GetName returns the name field of a property node
func (n *PropertyNode) GetName() string {
	return n.Name
}

// GetValue returns the value field of a property node
func (n *PropertyNode) GetValue() string {
	return n.Value
}

// GetExpiry returns the expiration time of a property.  Properties that don't
// expire will return nil.
func (n *PropertyNode) GetExpiry() *time.Time {
	return n.Expires
}

// APConfig is an opaque type representing a connection to ap.configd
type APConfig struct {
	mutex  sync.Mutex
	socket *zmq.Socket
	sender string

	broker         *broker.Broker
	changeHandlers []changeMatch
	deleteHandlers []delexpMatch
	expireHandlers []delexpMatch
	handling       bool
}

// NewConfig will connect to ap.configd, and will return a handle used for
// subsequent interactions with the daemon
func NewConfig(b *broker.Broker, name string) (*APConfig, error) {
	var host string

	sender := fmt.Sprintf("%s(%d)", name, os.Getpid())

	socket, err := zmq.NewSocket(zmq.REQ)
	if err != nil {
		err = fmt.Errorf("Failed to create new cfg socket: %v", err)
		return nil, err
	}

	err = socket.SetSndtimeo(time.Duration(base_def.LOCAL_ZMQ_SEND_TIMEOUT * time.Second))
	if err != nil {
		fmt.Printf("Failed to set cfg send timeout: %v\n", err)
		return nil, err
	}

	err = socket.SetRcvtimeo(time.Duration(base_def.LOCAL_ZMQ_RECEIVE_TIMEOUT * time.Second))
	if err != nil {
		fmt.Printf("Failed to set cfg receive timeout: %v\n", err)
		return nil, err
	}

	if aputil.IsSatelliteMode() {
		host = base_def.GATEWAY_ZMQ_URL
	} else {
		host = base_def.LOCAL_ZMQ_URL
	}
	err = socket.Connect(host + base_def.CONFIGD_ZMQ_REP_PORT)
	if err != nil {
		err = fmt.Errorf("Failed to connect new cfg socket: %v", err)
		return nil, err
	}

	c := APConfig{
		sender:         sender,
		socket:         socket,
		broker:         b,
		changeHandlers: make([]changeMatch, 0),
		deleteHandlers: make([]delexpMatch, 0),
		expireHandlers: make([]delexpMatch, 0),
	}
	return &c, nil
}

func (c *APConfig) msg(oc base_msg.ConfigQuery_Operation, prop, val string,
	expires *time.Time) (string, error) {

	response := &base_msg.ConfigResponse{}
	query := &base_msg.ConfigQuery{
		Timestamp: aputil.NowToProtobuf(),
		Sender:    proto.String(c.sender),
		Debug:     proto.String("-"),
		Operation: &oc,
		Property:  proto.String(prop),
		Value:     proto.String(val),
		Expires:   aputil.TimeToProtobuf(expires),
	}

	data, err := proto.Marshal(query)
	if err != nil {
		fmt.Printf("Failed to marshal config arguments: %v\n", err)
		return "", err
	}

	c.mutex.Lock()
	_, err = c.socket.SendBytes(data, 0)
	rval := ""
	if err != nil {
		fmt.Printf("Failed to send config msg: %v\n", err)
	} else {
		var reply [][]byte

		reply, err = c.socket.RecvMessageBytes(0)
		if len(reply) > 0 {
			proto.Unmarshal(reply[0], response)
		}
	}
	c.mutex.Unlock()
	if err == nil {
		switch *response.Response {
		case base_msg.ConfigResponse_OK:
			if oc == base_msg.ConfigQuery_GET {
				rval = *response.Value
			}
		default:
			err = fmt.Errorf("%s", *response.Value)
		}
	}

	return rval, err
}

// GetProps retrieves the properties subtree rooted at the given property, and
// returns a PropertyNode representing the root of that subtree
func (c *APConfig) GetProps(prop string) (*PropertyNode, error) {
	var root PropertyNode
	var err error

	tree, err := c.msg(base_msg.ConfigQuery_GET, prop, "-", nil)
	if err != nil {
		return &root, fmt.Errorf("Failed to retrieve %s: %v", prop, err)
	}

	if err = json.Unmarshal([]byte(tree), &root); err != nil {
		return &root, fmt.Errorf("Failed to decode %s: %v", prop, err)
	}

	return &root, err
}

// GetProp retrieves a single property from the tree, returning it as a String
func (c *APConfig) GetProp(prop string) (string, error) {
	var rval string

	root, err := c.GetProps(prop)
	if err == nil {
		rval = root.Value
	}

	return rval, err
}

// SetProp updates a single property, taking an optional expiration time.  If
// the property doesn't already exist, an error is returned.
func (c *APConfig) SetProp(prop, val string, expires *time.Time) error {
	_, err := c.msg(base_msg.ConfigQuery_SET, prop, val, expires)

	return err
}

// CreateProp updates a single property, taking an optional expiration time.  If
// the property doesn't already exist, it is created - as well as any parent
// properties needed to provide a path through the tree.
func (c *APConfig) CreateProp(prop, val string, expires *time.Time) error {
	_, err := c.msg(base_msg.ConfigQuery_CREATE, prop, val, expires)

	return err
}

// DeleteProp will delete a property, or property subtree
func (c *APConfig) DeleteProp(prop string) error {
	_, err := c.msg(base_msg.ConfigQuery_DELETE, prop, "-", nil)

	return err
}

//
// Utility functions to fetch specific property subtrees and transform the
// results into typed maps

func getStringVal(root *PropertyNode, name string) (string, error) {
	var err error
	var rval string

	node := root.GetChild(name)
	if node == nil {
		err = fmt.Errorf("%s is missing a %s property",
			root.Name, name)
	} else {
		rval = node.Value
	}

	return rval, err
}

func getIntVal(root *PropertyNode, name string) (int, error) {
	var err error
	var rval int

	node := root.GetChild(name)
	if node == nil {
		err = fmt.Errorf("%s is missing a %s property",
			root.Name, name)
	} else {
		if rval, err = strconv.Atoi(node.Value); err != nil {
			err = fmt.Errorf("%s has malformed %s property",
				root.Name, name)
		}
	}
	return rval, err
}

func getBoolVal(root *PropertyNode, name string) (bool, error) {
	var err error
	var rval bool

	node := root.GetChild(name)
	if node == nil {
		err = fmt.Errorf("%s is missing a %s property", root.Name, name)
	} else {
		if rval, err = strconv.ParseBool(node.Value); err != nil {
			err = fmt.Errorf("%s has malformed %s property", root.Name, name)
		}
	}
	return rval, err
}

// GetRings fetches the Rings subtree from ap.configd, and converts the json
// into a Ring -> RingConfig map
func (c *APConfig) GetRings() RingMap {
	props, err := c.GetProps("@/rings")
	if err != nil {
		log.Printf("Failed to get ring list: %v\n", err)
		return nil
	}

	set := make(map[string]*RingConfig)
	for _, ring := range props.Children {
		var auth, subnet, bridge string
		var vlan, duration int
		var err error

		if !ValidRings[ring.Name] {
			err = fmt.Errorf("invalid ring name: %s", ring.Name)
		}
		if err == nil {
			vlan, err = getIntVal(ring, "vlan")
			if vlan >= 0 {
				bridge = "brvlan" + strconv.Itoa(vlan)
			}
		}
		if err == nil {
			subnet, err = getStringVal(ring, "subnet")
		}
		if err == nil {
			duration, err = getIntVal(ring, "lease_duration")
		}
		if err == nil {
			auth, err = getStringVal(ring, "auth")
		}
		if err == nil {
			c := RingConfig{
				Auth:          auth,
				Vlan:          vlan,
				Subnet:        subnet,
				Bridge:        bridge,
				LeaseDuration: duration}
			set[ring.Name] = &c
		} else {
			fmt.Printf("Malformed ring %s: %v\n", ring.Name, err)
		}
	}

	return set
}

func getClient(client *PropertyNode) *ClientInfo {
	var ring, dns, dhcp, identity, confidence string
	var ipv4 net.IP
	var exp *time.Time
	var private bool

	private, _ = getBoolVal(client, "dns_private")
	ring, _ = getStringVal(client, "ring")
	identity, _ = getStringVal(client, "identity")
	confidence, _ = getStringVal(client, "confidence")
	dhcp, _ = getStringVal(client, "dhcp_name")
	dns, _ = getStringVal(client, "dns_name")
	if addr := client.GetChild("ipv4"); addr != nil {
		if ip := net.ParseIP(addr.Value); ip != nil {
			ipv4 = ip.To4()
			exp = addr.Expires
		}
	}

	c := ClientInfo{
		Ring:       ring,
		DHCPName:   dhcp,
		DNSName:    dns,
		IPv4:       ipv4,
		Expires:    exp,
		Identity:   identity,
		Confidence: confidence,
		DNSPrivate: private,
	}
	return &c
}

//
// GetClient fetches a single client from ap.configd and converts the json
// result into a ClientInfo structure
func (c *APConfig) GetClient(macaddr string) *ClientInfo {
	client, err := c.GetProps("@/clients/" + macaddr)
	if err != nil {
		log.Printf("Failed to get %s: %v\n", macaddr, err)
		return nil
	}

	return getClient(client)
}

// GetClients the full Clients subtree, and converts the returned json into a
// map of ClientInfo structures, indexed by the client's mac address
func (c *APConfig) GetClients() ClientMap {
	props, err := c.GetProps("@/clients")
	if err != nil {
		log.Printf("Failed to get clients list: %v\n", err)
		return nil
	}

	set := make(map[string]*ClientInfo)
	for _, client := range props.Children {
		set[client.Name] = getClient(client)
	}

	return set
}

// GetDevicePath fetches a single device by its path
func (c *APConfig) GetDevicePath(path string) (*device.Device, error) {
	var dev device.Device

	tree, err := c.msg(base_msg.ConfigQuery_GET, path, "-", nil)
	if err != nil {
		err = fmt.Errorf("failed to retrieve %s: %v", path, err)
	} else if err = json.Unmarshal([]byte(tree), &dev); err != nil {
		err = fmt.Errorf("failed to decode %s: %v", tree, err)
	}

	return &dev, err
}

// GetDevice fetches a single device by its ID #
func (c *APConfig) GetDevice(devid int) (*device.Device, error) {
	path := fmt.Sprintf("@/devices/%d", devid)
	return c.GetDevicePath(path)
}

// GetNics returns a slice of mac addresses, representing the NICs that match
// the filter parameters
func (c *APConfig) GetNics(ring string, local bool) ([]string, error) {
	var nodes []*PropertyNode

	s := make([]string, 0)
	prop := "@/nodes"
	if local {
		prop += "/" + aputil.GetNodeID().String()
	}
	props, err := c.GetProps(prop)
	if err != nil {
		return nil, fmt.Errorf("property get %s failed: %v", prop, err)
	}

	if local {
		nodes = []*PropertyNode{props}
	} else {
		nodes = props.Children
	}
	for _, node := range nodes {
		for _, nic := range node.Children {
			var nicRing string
			if x := nic.GetChild("ring"); x != nil {
				nicRing = x.Value
			}

			if ring == "" || ring == nicRing {
				s = append(s, nic.Name)
			}
		}
	}
	return s, nil
}

func getUser(user *PropertyNode) (*UserInfo, error) {
	uid, err := getStringVal(user, "uid")
	if err != nil {
		// Most likely manual creation of the @/users/[uid] node.
		log.Printf("incomplete user property node: %v", err)
		return nil, err
	}

	password, _ := getStringVal(user, "userPassword")
	md4password, _ := getStringVal(user, "userMD4Password")
	uuid, _ := getStringVal(user, "uuid")
	email, _ := getStringVal(user, "email")
	telephoneNumber, _ := getStringVal(user, "telephoneNumber")
	preferredLanguage, _ := getStringVal(user, "preferredLanguage")
	displayName, _ := getStringVal(user, "displayName")
	totp, _ := getStringVal(user, "totp")

	u := UserInfo{
		UID:               uid,
		UUID:              uuid,
		Email:             email,
		TelephoneNumber:   telephoneNumber,
		PreferredLanguage: preferredLanguage,
		DisplayName:       displayName,
		TOTP:              totp,
		Password:          password,
		MD4Password:       md4password,
	}

	return &u, nil
}

// GetUser fetches the UserInfo structure for a given user
func (c *APConfig) GetUser(uid string) (*UserInfo, error) {
	user, err := c.GetProps("@/users/" + uid)
	if err != nil {
		log.Printf("Failed to get %s: %v\n", uid, err)
		return nil, err
	}

	return getUser(user)
}

// GetUsers fetches the Users subtree, in the form of a map of UID to UserInfo
// structures.
func (c *APConfig) GetUsers() UserMap {
	props, err := c.GetProps("@/users")
	if err != nil {
		log.Printf("Failed to get users list: %v\n", err)
		return nil
	}

	log.Printf("users as props: %v\n", props)

	set := make(map[string]*UserInfo)
	for _, user := range props.Children {
		log.Printf("userinfoing %v\n", user.Name)
		if us, err := getUser(user); err == nil {
			set[user.Name] = us
		} else {
			log.Printf("couldn't userinfo %v: %v\n", user.Name, err)
		}
	}

	return set
}

// SetUserPassword assigns all appropriate password hash properties for the given user.
func (c *APConfig) SetUserPassword(user string, passwd string) error {
	// Test if user exists.
	_, err := c.GetUser(user)
	if err != nil {
		return fmt.Errorf("no such user '%v'", user)
	}

	// Generate bcrypt password property.
	hps, err := bcrypt.GenerateFromPassword([]byte(passwd), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("could not encrypt password: %v", err)
	}

	pp := fmt.Sprintf("@/users/%s/userPassword", user)
	err = c.CreateProp(pp, string(hps), nil)
	if err != nil {
		return fmt.Errorf(
			"could not create userPassword property '%v': %v",
			pp, err)
	}

	// Generate MD4 password property.
	enc := unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM).NewEncoder()
	md4ps := md4.New()
	t := transform.NewWriter(md4ps, enc)
	t.Write([]byte(passwd))
	md4s := fmt.Sprintf("%x", md4ps.Sum(nil))

	pp = fmt.Sprintf("@/users/%s/userMD4Password", user)
	err = c.CreateProp(pp, md4s, nil)
	if err != nil {
		return fmt.Errorf(
			"could not create userMD4Password property '%v': %v",
			pp, err)
	}

	return nil
}

// GetActiveBlocks builds a slice of all the IP addresses that were being
// actively blocked at the time of the call.
func (c *APConfig) GetActiveBlocks() []string {
	list := make([]string, 0)

	active, _ := c.GetProps("@/firewall/active")
	now := time.Now()
	for _, node := range active.Children {
		if node.Expires == nil || now.Before(*node.Expires) {
			list = append(list, node.Name)
		}
	}

	return list
}
