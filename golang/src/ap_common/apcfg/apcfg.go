/*
 * COPYRIGHT 2017 Brightgate Inc.  All rights reserved.
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
	"regexp"
	"strconv"
	"sync"
	"time"

	"ap_common/broker"
	"ap_common/device"
	"base_def"
	"base_msg"

	"github.com/golang/protobuf/proto"

	// Ubuntu: requires libzmq3-dev, which is 0MQ 4.2.1.
	zmq "github.com/pebbe/zmq4"
)

const (
	N_WAN = iota
	N_SETUP
	N_WIRED
	N_WIFI
	N_MAX
)

var ValidRings = map[string]bool{
	base_def.RING_UNENROLLED: true,
	base_def.RING_SETUP:      true,
	base_def.RING_CORE:       true,
	base_def.RING_STANDARD:   true,
	base_def.RING_DEVICES:    true,
	base_def.RING_GUEST:      true,
	base_def.RING_QUARANTINE: true,
	base_def.RING_WIRED:      true,
}

type Nic struct {
	Logical string
	Iface   string
	Mac     string
}

type RingConfig struct {
	Interface     string
	LeaseDuration int
}

type ClientInfo struct {
	Ring       string     // Assigned security ring
	DNSName    string     // Assigned hostname
	IPv4       net.IP     // Network address
	Expires    *time.Time // DHCP lease expiration time
	DHCPName   string     // Requested hostname
	Identity   string     // Our current best guess at the client type
	Confidence string     // Our confidence for the Identity guess
}

type RingMap map[string]*RingConfig
type ClientMap map[string]*ClientInfo
type SubnetMap map[string]string
type NicMap map[string]string

//
// A node in the property tree.
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

// Dump the contents of a property tree in a human-legible format
func (n *PropertyNode) DumpTree() {
	dumpSubtree(n, 0)
}

// Search the node's children, looking for one with a name matching the provided
// key.  Returns a pointer the child node if it finds a match, nil if it
// doesn't.
func (n *PropertyNode) GetChild(key string) *PropertyNode {
	for _, s := range n.Children {
		if s.Name == key {
			return s
		}
	}
	return nil
}

// Search the node's children, looking for one with a value matching the
// provided key.  Returns a pointer the child node if it finds a match, nil if
// it doesn't.  If multiple children have the same value, this will return only
// the first one found.
func (n *PropertyNode) GetChildByValue(value string) *PropertyNode {
	for _, s := range n.Children {
		if s.Value == value {
			return s
		}
	}
	return nil
}

// Returns the property name
func (n *PropertyNode) GetName() string {
	return n.Name
}

// Returns the property value
func (n *PropertyNode) GetValue() string {
	return n.Value
}

// Returns the property expiration time
func (n *PropertyNode) GetExpiry() *time.Time {
	return n.Expires
}

type changeMatch struct {
	match   *regexp.Regexp
	handler func([]string, string)
}

type delexpMatch struct {
	match   *regexp.Regexp
	handler func([]string)
}

// Opaque type representing a connection to ap.configd
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

// Connect to ap.configd.  Return a handle used for subsequent interactions with
// the daemon
func NewConfig(b *broker.Broker, name string) (*APConfig, error) {
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

	err = socket.Connect(base_def.CONFIGD_ZMQ_REP_URL)
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
	t := time.Now()
	query := &base_msg.ConfigQuery{
		Timestamp: &base_msg.Timestamp{
			Seconds: proto.Int64(t.Unix()),
			Nanos:   proto.Int32(int32(t.Nanosecond())),
		},
		Sender:    proto.String(c.sender),
		Debug:     proto.String("-"),
		Operation: &oc,
		Property:  proto.String(prop),
		Value:     proto.String(val),
	}
	if expires != nil {
		query.Expires = &base_msg.Timestamp{
			Seconds: proto.Int64(expires.Unix()),
			Nanos:   proto.Int32(int32(expires.Nanosecond())),
		}
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

// Retrieves the properties subtree rooted at the given property, and returns a
// PropertyNode representing the root of that subtree
func (c APConfig) GetProps(prop string) (*PropertyNode, error) {
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

// Retrieves a single property from the tree, returning it as a String
func (c APConfig) GetProp(prop string) (string, error) {
	var rval string

	root, err := c.GetProps(prop)
	if err == nil {
		rval = root.Value
	}

	return rval, err
}

// Updates a single property, taking an optional expiration time.  If the
// property doesn't already exist, an error is returned.
func (c APConfig) SetProp(prop, val string, expires *time.Time) error {
	_, err := c.msg(base_msg.ConfigQuery_SET, prop, val, expires)

	return err
}

// Updates a single property, taking an optional expiration time.  If the
// property doesn't already exist, it is created - as well as any parent
// properties needed to provide a path through the tree.
func (c APConfig) CreateProp(prop, val string, expires *time.Time) error {
	_, err := c.msg(base_msg.ConfigQuery_CREATE, prop, val, expires)

	return err
}

// Deletes a property, or property subtree
func (c APConfig) DeleteProp(prop string) error {
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
				node.Name)
		}
	}
	return rval, err
}

//
// Fetch the Rings subtree and return a Ring -> RingConfig map
func (c APConfig) GetRings() RingMap {
	props, err := c.GetProps("@/rings")
	if err != nil {
		log.Printf("Failed to get ring list: %v\n", err)
		return nil
	}

	set := make(map[string]*RingConfig)
	for _, ring := range props.Children {
		var iface string
		var duration int

		if !ValidRings[ring.Name] {
			log.Printf("Invalid ring name: %s\n", ring.Name)
			continue
		}
		if iface, err = getStringVal(ring, "interface"); err == nil {
			duration, err = getIntVal(ring, "lease_duration")
		}
		if err == nil {
			c := RingConfig{Interface: iface, LeaseDuration: duration}
			set[ring.Name] = &c
		} else {
			fmt.Printf("Malformed ring %s: %v\n", ring.Name, err)
		}
	}

	return set
}

//
// Fetch the interfaces subtree, and return a map of Interface -> subnet
func (c APConfig) GetSubnets() SubnetMap {
	props, err := c.GetProps("@/interfaces")
	if err != nil {
		log.Printf("Failed to get interfaces list: %v\n", err)
		return nil
	}

	set := make(map[string]string)
	for _, iface := range props.Children {
		subnet, err := getStringVal(iface, "subnet")
		if err != nil {
			fmt.Printf("Malformed subnet %s: %v\n", iface.Name, err)
		} else {
			set[iface.Name] = subnet
		}
	}

	return set
}

func getClient(client *PropertyNode) *ClientInfo {
	var ring, dns, dhcp, identity, confidence string
	var ipv4 net.IP
	var exp *time.Time

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
	}
	return &c
}

//
// Fetch a single client and return a ClientInfo structure
func (c APConfig) GetClient(macaddr string) *ClientInfo {
	client, err := c.GetProps("@/clients/" + macaddr)
	if err != nil {
		log.Printf("Failed to get %s: %v\n", macaddr, err)
		return nil
	}

	return getClient(client)
}

//
// Fetch the Clients subtree, and return a map of macaddr -> ClientInfo
func (c APConfig) GetClients() ClientMap {
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

func getNic(props *PropertyNode, name string) *Nic {
	var nic, mac string

	if node := props.GetChild(name); node != nil {
		nic = node.GetValue()
		if i, err := net.InterfaceByName(nic); err == nil {
			mac = i.HardwareAddr.String()
		}
	}

	if nic == "" {
		log.Printf("Logical interface %s not configured\n", name)
	} else if mac == "" {
		log.Printf("%s mapped to missing nic %s\n", name, nic)
	} else {
		n := Nic{Logical: name, Iface: nic, Mac: mac}
		return &n
	}
	return nil
}

// GetLogicalNics returns a map of logical->physical nics currently configured
func (c APConfig) GetLogicalNics() ([]*Nic, error) {
	var nics []*Nic

	props, err := c.GetProps("@/network")

	if err == nil {
		nics = make([]*Nic, N_MAX)
		nics[N_WAN] = getNic(props, "wan_nic")
		nics[N_WIFI] = getNic(props, "wifi_nic")
		nics[N_WIRED] = getNic(props, "wired_nic")
		nics[N_SETUP] = getNic(props, "setup_nic")
	} else {
		err = fmt.Errorf("Failed to get network config: %v", err)
	}

	return nics, err
}

// Fetch a single device by its path
func (c APConfig) GetDevicePath(path string) (*device.Device, error) {
	var dev device.Device

	tree, err := c.msg(base_msg.ConfigQuery_GET, path, "-", nil)
	if err != nil {
		err = fmt.Errorf("failed to retrieve %s: %v", path, err)
	} else if err = json.Unmarshal([]byte(tree), &dev); err != nil {
		err = fmt.Errorf("failed to decode %s: %v", tree, err)
	}

	return &dev, err
}

// Fetch a single device by its ID #
func (c APConfig) GetDevice(devid int) (*device.Device, error) {
	path := fmt.Sprintf("@/devices/%d", devid)
	return c.GetDevicePath(path)
}
