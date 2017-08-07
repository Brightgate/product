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
	"strconv"
	"sync"
	"time"

	"base_def"
	"base_msg"

	"github.com/golang/protobuf/proto"

	// Ubuntu: requires libzmq3-dev, which is 0MQ 4.2.1.
	zmq "github.com/pebbe/zmq4"
)

const (
	N_WAN = iota
	N_CONNECT
	N_WIRED
	N_WIFI
	N_MAX
)

type Nic struct {
	Logical string
	Iface   string
	Mac     string
}

type ClassConfig struct {
	Interface     string
	LeaseDuration int
}

type ClientInfo struct {
	Class    string     // Assigned class
	DNSName  string     // Assigned hostname
	IPv4     net.IP     // Network address
	Expires  *time.Time // DHCP lease expiration time
	DHCPName string     // Requested hostname
	Identity string     // Our current best guess at the client type
}

type ClassMap map[string]*ClassConfig
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

// Opaque type representing a connection to ap.configd
type APConfig struct {
	mutex  sync.Mutex
	socket *zmq.Socket
	sender string
}

// Connect to ap.configd.  Return a handle used for subsequent interactions with
// the daemon
func NewConfig(name string) *APConfig {
	sender := fmt.Sprintf("%s(%d)", name, os.Getpid())
	socket, _ := zmq.NewSocket(zmq.REQ)
	socket.Connect(base_def.CONFIGD_ZMQ_REP_URL)

	return &APConfig{sender: sender, socket: socket}
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
		case base_msg.ConfigResponse_OP_OK:
			if oc == base_msg.ConfigQuery_GET {
				rval = *response.Value
			}
		case base_msg.ConfigResponse_PROP_NOT_FOUND:
			err = fmt.Errorf("Property not found")
		case base_msg.ConfigResponse_VALUE_NOT_FOUND:
			err = fmt.Errorf("Value not found")
		case base_msg.ConfigResponse_NO_PERM:
			err = fmt.Errorf("Permission denied")
		case base_msg.ConfigResponse_UNSUPPORTED:
			err = fmt.Errorf("Operation not supported")
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
		fmt.Printf("%s %s -> %d\n", name, node.Value, rval)
	}
	return rval, err
}

//
// Fetch the Classes subtree and return a Class -> ClassConfig map
func (c APConfig) GetClasses() ClassMap {
	props, err := c.GetProps("@/classes")
	if err != nil {
		log.Printf("Failed to get class list: %v\n", err)
		return nil
	}

	set := make(map[string]*ClassConfig)
	for _, class := range props.Children {
		var iface string
		var duration int

		if iface, err = getStringVal(class, "interface"); err == nil {
			duration, err = getIntVal(class, "lease_duration")
		}
		if err == nil {
			c := ClassConfig{Interface: iface, LeaseDuration: duration}
			set[class.Name] = &c
		} else {
			fmt.Printf("Malformed class %s: %v\n", class.Name, err)
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
	var class, dns, dhcp, identity string
	var ipv4 net.IP
	var exp *time.Time
	var err error

	if class, err = getStringVal(client, "class"); err != nil {
		class = "unclassified"
	}

	identity, _ = getStringVal(client, "identity")
	dhcp, _ = getStringVal(client, "dhcp_name")
	dns, _ = getStringVal(client, "dns_name")
	if addr := client.GetChild("ipv4"); addr != nil {
		if ip := net.ParseIP(addr.Value); ip != nil {
			ipv4 = ip.To4()
			exp = addr.Expires
		}
	}

	c := ClientInfo{
		Class:    class,
		DHCPName: dhcp,
		DNSName:  dns,
		IPv4:     ipv4,
		Expires:  exp,
		Identity: identity,
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
		log.Printf("%s -> %s / %s\n", name, nic, mac)
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
		nics[N_CONNECT] = getNic(props, "connect_nic")
	} else {
		err = fmt.Errorf("Failed to get network config: %v", err)
	}

	return nics, err
}
