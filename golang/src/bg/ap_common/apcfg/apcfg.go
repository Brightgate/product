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

// RingMap maps ring names to the configuration information
type RingMap map[string]*RingConfig

// ClientMap maps a device's mac address to its configuration information
type ClientMap map[string]*ClientInfo

// ChildMap is a name->structure map of a property's children
type ChildMap map[string]*PropertyNode

// PropertyNode is a single node in the property tree
type PropertyNode struct {
	Value    string     `json:"Value,omitempty"`
	Expires  *time.Time `json:"Expires,omitempty"`
	Children ChildMap   `json:"Children,omitempty"`
}

// IsActive returns 'true' if we believe the client is currently connected to
// this AP
func (c *ClientInfo) IsActive() bool {
	return c != nil && c.IPv4 != nil
}

func dumpSubtree(name string, node *PropertyNode, level int) {
	indent := ""
	for i := 0; i < level; i++ {
		indent += "  "
	}
	e := ""
	if node.Expires != nil {
		e = node.Expires.Format("2006-01-02T15:04:05")
	}
	fmt.Printf("%s%s: %s  %s\n", indent, name, node.Value, e)
	for childName, child := range node.Children {
		dumpSubtree(childName, child, level+1)
	}
}

// DumpTree displays the contents of a property tree in a human-legible format
func (n *PropertyNode) DumpTree(root string) {
	dumpSubtree(root, n, 0)
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

func getProp(root *PropertyNode, name string) (string, error) {
	if child, ok := root.Children[name]; ok {
		return child.Value, nil
	}
	return "", fmt.Errorf("missing %s property", name)
}

func getStringVal(root *PropertyNode, name string) (string, error) {
	return getProp(root, name)
}

func getIntVal(root *PropertyNode, name string) (int, error) {
	var rval int
	var err error

	if val, err := getProp(root, name); err == nil {
		if rval, err = strconv.Atoi(val); err != nil {
			err = fmt.Errorf("malformed %s property: %s",
				name, val)
		}
	}

	return rval, err
}

func getBoolVal(root *PropertyNode, name string) (bool, error) {
	var rval bool
	var err error

	if val, err := getProp(root, name); err == nil {
		if rval, err = strconv.ParseBool(val); err != nil {
			err = fmt.Errorf("malformed %s property: %s",
				name, val)
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
	for ringName, ring := range props.Children {
		var auth, subnet, bridge string
		var vlan, duration int
		var err error

		if !ValidRings[ringName] {
			err = fmt.Errorf("invalid ring name: %s", ringName)
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
			set[ringName] = &c
		} else {
			fmt.Printf("Malformed ring %s: %v\n", ringName, err)
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
	if addr, ok := client.Children["ipv4"]; ok {
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
	for name, client := range props.Children {
		set[name] = getClient(client)
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

// GetNics returns a slice of mac addresses representing the configured NICs.
// The caller may choose to limit the slice to NICs carrying traffic for a
// single ring and/or NICs that are local to this node.
func (c *APConfig) GetNics(ring string, local bool) ([]string, error) {
	prop, err := c.GetProps("@/nodes")
	if err != nil {
		return nil, fmt.Errorf("property get %s failed: %v", prop, err)
	}

	localNodeName := aputil.GetNodeID().String()
	s := make([]string, 0)
	for nodeName, node := range prop.Children {
		if local && nodeName != localNodeName {
			continue
		}

		for nicName, nic := range node.Children {
			var nicRing string
			if x, ok := nic.Children["ring"]; ok {
				nicRing = x.Value
			}

			if ring == "" || ring == nicRing {
				s = append(s, nicName)
			}
		}
	}
	return s, nil
}

// GetActiveBlocks builds a slice of all the IP addresses that were being
// actively blocked at the time of the call.
func (c *APConfig) GetActiveBlocks() []string {
	list := make([]string, 0)

	active, _ := c.GetProps("@/firewall/blocked")
	now := time.Now()
	for name, node := range active.Children {
		if node.Expires == nil || now.Before(*node.Expires) {
			list = append(list, name)
		}
	}

	return list
}
