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
	"errors"
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
	"bg/ap_common/platform"
	"bg/base_def"
	"bg/common/cfgmsg"

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/timestamp"

	// Ubuntu: requires libzmq3-dev, which is 0MQ 4.2.1.
	zmq "github.com/pebbe/zmq4"
)

// Version gets increased each time there is a non-compatible change to the
// config tree format, or ap.configd API.
const Version = int32(16)

// AccessLevel represents a level of privilege needed or obtained for configd operations
type AccessLevel int32

// Access levels required to modify/delete configd properties. 'iota' is not
// used because these are wire protocol constants.
const (
	AccessNone      AccessLevel = 0 // All requests denied
	AccessUser      AccessLevel = 10
	AccessAdmin     AccessLevel = 20
	AccessService   AccessLevel = 30
	AccessDeveloper AccessLevel = 40
	AccessInternal  AccessLevel = 50
)

// AccessLevels maps user-friendly access level names to the integer values used
// internally
var AccessLevels = map[string]AccessLevel{
	"none":      AccessNone,
	"user":      AccessUser,
	"admin":     AccessAdmin,
	"service":   AccessService,
	"developer": AccessDeveloper,
	"internal":  AccessInternal,
}

// AccessLevelNames translates numeric access levels to strings
var AccessLevelNames = map[AccessLevel]string{
	AccessNone:      "none",
	AccessUser:      "user",
	AccessAdmin:     "admin",
	AccessService:   "service",
	AccessDeveloper: "developer",
	AccessInternal:  "internal",
}

// Some specific, common ways in which apcfg operations can fail
var (
	ErrComm    = errors.New("communication breakdown")
	ErrNoProp  = errors.New("no such property")
	ErrExpired = errors.New("property expired")
	ErrBadOp   = errors.New("no such operation")
	ErrBadVer  = errors.New("unsupported version")
	ErrBadTime = errors.New("bad timestamp")
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
	base_def.RING_WAN:        true,
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
	Identity   string     // Current best guess at the client type
	Confidence float64    // Confidence for the Identity guess
	DNSPrivate bool       // We don't collect DNS queries
}

// VulnInfo represents the detection of a single vulnerability in a single
// client.
type VulnInfo struct {
	FirstDetected  *time.Time // When the vuln was first seen
	LatestDetected *time.Time // When the vuln was most recently seen
	WarnedAt       *time.Time // When the last log message / Event was sent
	Ignore         bool       // If the vuln is seen, take no action
	Active         bool       // vuln was present on last scan
}

// ScanInfo represents a record of scanning activity for a single client.
type ScanInfo struct {
	Start  *time.Time // When the scan was started
	Finish *time.Time // When the scan completed
}

// RingMap maps ring names to the configuration information
type RingMap map[string]*RingConfig

// ClientMap maps a device's mac address to its configuration information
type ClientMap map[string]*ClientInfo

// ChildMap is a name->structure map of a property's children
type ChildMap map[string]*PropertyNode

// VulnMap is a name->VulnInfo map representing all of the vulnerabilities we
// have discovered on a device
type VulnMap map[string]*VulnInfo

// ScanMap is a name->ScanInfo map representing all of the scans we
// have performed on a device
type ScanMap map[string]*ScanInfo

// List of the supported property operation types
const (
	PropGet = iota
	PropSet
	PropCreate
	PropDelete
)

var opToMsgType = map[int]cfgmsg.ConfigOp_Operation{
	PropGet:    cfgmsg.ConfigOp_GET,
	PropSet:    cfgmsg.ConfigOp_SET,
	PropCreate: cfgmsg.ConfigOp_CREATE,
	PropDelete: cfgmsg.ConfigOp_DELETE,
}

// PropertyOp represents an operation on a single property
type PropertyOp struct {
	Op      int
	Name    string
	Value   string
	Expires *time.Time
}

// PropertyNode is a single node in the property tree
type PropertyNode struct {
	Value    string     `json:"Value,omitempty"`
	Expires  *time.Time `json:"Expires,omitempty"`
	Children ChildMap   `json:"Children,omitempty"`
}

// IsActive returns 'true' if we believe the client is currently connected to
// this AP
func (c *ClientInfo) IsActive() bool {
	if c == nil || c.IPv4 == nil {
		return false
	}

	expired := false
	if c.Expires != nil {
		expired = c.Expires.Before(time.Now())
	}

	return !expired
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

	platform       *platform.Platform
	broker         *broker.Broker
	changeHandlers []changeMatch
	deleteHandlers []delexpMatch
	expireHandlers []delexpMatch
	handling       bool
	level          AccessLevel
}

// NewConfig will connect to ap.configd, and will return a handle used for
// subsequent interactions with the daemon
func NewConfig(b *broker.Broker, name string, level AccessLevel) (*APConfig, error) {
	var host string

	plat := platform.NewPlatform()
	if _, ok := AccessLevelNames[level]; !ok {
		return nil, fmt.Errorf("invalid access level: %d", level)
	}

	sender := fmt.Sprintf("%s(%d)", name, os.Getpid())

	socket, err := zmq.NewSocket(zmq.REQ)
	if err != nil {
		err = fmt.Errorf("failed to create new cfg socket: %v", err)
		return nil, err
	}

	err = socket.SetSndtimeo(time.Duration(base_def.LOCAL_ZMQ_SEND_TIMEOUT * time.Second))
	if err != nil {
		log.Printf("failed to set cfg send timeout: %v\n", err)
		return nil, err
	}

	err = socket.SetRcvtimeo(time.Duration(base_def.LOCAL_ZMQ_RECEIVE_TIMEOUT * time.Second))
	if err != nil {
		log.Printf("failed to set cfg receive timeout: %v\n", err)
		return nil, err
	}

	if aputil.IsSatelliteMode() {
		host = base_def.GATEWAY_ZMQ_URL
	} else {
		host = base_def.LOCAL_ZMQ_URL
	}
	err = socket.Connect(host + base_def.CONFIGD_ZMQ_REP_PORT)
	if err != nil {
		err = fmt.Errorf("failed to connect new cfg socket: %v", err)
		return nil, err
	}

	c := &APConfig{
		sender:         sender,
		socket:         socket,
		broker:         b,
		platform:       plat,
		level:          level,
		changeHandlers: make([]changeMatch, 0),
		deleteHandlers: make([]delexpMatch, 0),
		expireHandlers: make([]delexpMatch, 0),
	}

	err = c.Ping()
	return c, err
}

func (c *APConfig) sendOp(query *cfgmsg.ConfigQuery) (string, error) {

	query.Sender = c.sender
	query.Level = int32(c.level)
	op, err := proto.Marshal(query)
	if err != nil {
		return "", fmt.Errorf("unable to build ping: %v", err)
	}

	rval := ""
	response := &cfgmsg.ConfigResponse{}
	c.mutex.Lock()
	_, err = c.socket.SendBytes(op, 0)
	if err != nil {
		log.Printf("Failed to send config msg: %v\n", err)
		err = ErrComm
	} else {
		reply, rerr := c.socket.RecvMessageBytes(0)
		if rerr != nil {
			log.Printf("Failed to receive config reply: %v\n", err)
			err = ErrComm
		} else if len(reply) > 0 {
			proto.Unmarshal(reply[0], response)
		}
	}
	c.mutex.Unlock()
	if err == nil {
		switch response.Response {
		case cfgmsg.ConfigResponse_OK:
			rval = response.Value
		case cfgmsg.ConfigResponse_UNSUPPORTED:
			err = ErrBadOp
		case cfgmsg.ConfigResponse_NOPROP:
			err = ErrNoProp
		case cfgmsg.ConfigResponse_BADTIME:
			err = ErrBadTime
		case cfgmsg.ConfigResponse_BADVERSION:
			var version string

			if response.MinVersion != nil {
				version = fmt.Sprintf("%d or greater",
					response.MinVersion.Major)
			} else {
				version = fmt.Sprintf("%d",
					response.Version.Major)
			}
			err = fmt.Errorf("ap.configd requires version %s",
				version)
		case cfgmsg.ConfigResponse_FAILED:
			err = fmt.Errorf("Failed: %s", response.Value)
		default:
			err = fmt.Errorf("Unexpected response: %#v", response)
		}
	}

	return rval, err
}

// GeneratePingQuery generates a ping query
func GeneratePingQuery() *cfgmsg.ConfigQuery {
	opType := cfgmsg.ConfigOp_PING

	ops := []*cfgmsg.ConfigOp{
		&cfgmsg.ConfigOp{
			Property:  "ping",
			Operation: opType,
		},
	}
	version := cfgmsg.Version{Major: Version}
	query := cfgmsg.ConfigQuery{
		Timestamp: ptypes.TimestampNow(),
		Debug:     "-",
		Version:   &version,
		Ops:       ops,
	}

	return &query
}

// Ping sends a no-op command to configd to check for liveness and to check for
// version compatibility.
func (c *APConfig) Ping() error {
	query := GeneratePingQuery()
	_, err := c.sendOp(query)
	if err != nil {
		err = fmt.Errorf("ping failed: %v", err)
	}
	return err
}

// GeneratePropQuery takes a slice of PropertyOp structures and creates a
// corresponding ConfigQuery protobuf
func GeneratePropQuery(ops []PropertyOp) (*cfgmsg.ConfigQuery, error) {
	get := false
	msgOps := make([]*cfgmsg.ConfigOp, len(ops))
	for i, op := range ops {
		get = get || (op.Op == PropGet)

		opType, ok := opToMsgType[op.Op]
		if !ok {
			return nil, ErrBadOp
		}

		var tspb *timestamp.Timestamp
		if op.Expires != nil {
			var err error
			tspb, err = ptypes.TimestampProto(*op.Expires)
			if err != nil {
				return nil, ErrBadTime
			}
		}
		msgOps[i] = &cfgmsg.ConfigOp{
			Operation: opType,
			Property:  op.Name,
			Value:     op.Value,
			Expires:   tspb,
		}
	}
	if get && len(ops) > 1 {
		return nil, fmt.Errorf("GET ops must be singletons")
	}

	version := cfgmsg.Version{Major: Version}
	query := cfgmsg.ConfigQuery{
		Timestamp: ptypes.TimestampNow(),
		Debug:     "-",
		Version:   &version,
		Ops:       msgOps,
	}

	return &query, nil
}

// Execute takes a slice of PropertyOp structures, marshals them into a protobuf
// query, and sends that to ap.configd.  It then unmarshals the result from
// ap.configd, and returns that to the caller.
func (c *APConfig) Execute(ops []PropertyOp) (string, error) {
	if len(ops) == 0 {
		return "", nil
	}
	query, err := GeneratePropQuery(ops)
	if query == nil {
		return "", err
	}
	return c.sendOp(query)
}

// GetProps retrieves the properties subtree rooted at the given property, and
// returns a PropertyNode representing the root of that subtree
func (c *APConfig) GetProps(prop string) (*PropertyNode, error) {
	var root PropertyNode

	ops := []PropertyOp{
		{Op: PropGet, Name: prop},
	}
	tree, err := c.Execute(ops)

	if err == ErrNoProp {
		return nil, err
	} else if err != nil {
		return nil, fmt.Errorf("Failed to retrieve %s: %v", prop, err)
	} else if err = json.Unmarshal([]byte(tree), &root); err != nil {
		return nil, fmt.Errorf("Failed to decode %s: %v", prop, err)
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
	ops := []PropertyOp{
		{Op: PropSet, Name: prop, Value: val, Expires: expires},
	}
	_, err := c.Execute(ops)

	return err
}

// CreateProp updates a single property, taking an optional expiration time.  If
// the property doesn't already exist, it is created - as well as any parent
// properties needed to provide a path through the tree.
func (c *APConfig) CreateProp(prop, val string, expires *time.Time) error {
	ops := []PropertyOp{
		{Op: PropCreate, Name: prop, Value: val, Expires: expires},
	}
	_, err := c.Execute(ops)

	return err
}

// DeleteProp will delete a property, or property subtree
func (c *APConfig) DeleteProp(prop string) error {
	ops := []PropertyOp{
		{Op: PropDelete, Name: prop},
	}
	_, err := c.Execute(ops)

	return err
}

//
// Utility functions to fetch specific property subtrees and transform the
// results into typed maps

func getProp(root *PropertyNode, name string) (string, error) {
	child := root.Children[name]
	if child == nil {
		return "", fmt.Errorf("missing %s property", name)
	}

	if child.Expires != nil && child.Expires.Before(time.Now()) {
		return "", fmt.Errorf("expired %s property", name)
	}

	return child.Value, nil
}

func getStringVal(root *PropertyNode, name string) (string, error) {
	return getProp(root, name)
}

func getIntVal(root *PropertyNode, name string) (int, error) {
	var val string
	var rval int
	var err error

	if val, err = getProp(root, name); err == nil {
		if rval, err = strconv.Atoi(val); err != nil {
			err = fmt.Errorf("malformed %s property: %s",
				name, val)
		}
	}

	return rval, err
}

func getFloat64Val(root *PropertyNode, name string) (float64, error) {
	var val string
	var rval float64
	var err error

	if val, err = getProp(root, name); err == nil {
		if rval, err = strconv.ParseFloat(val, 64); err != nil {
			err = fmt.Errorf("malformed %s property: %s",
				name, val)
		}
	}

	return rval, err
}

func getBoolVal(root *PropertyNode, name string) (bool, error) {
	var val string
	var rval bool
	var err error

	if val, err = getProp(root, name); err == nil {
		if rval, err = strconv.ParseBool(val); err != nil {
			err = fmt.Errorf("malformed %s property: %s",
				name, val)
		}
	}
	return rval, err
}

func getTimeVal(root *PropertyNode, name string) (*time.Time, error) {
	var val string
	var rval time.Time
	var err error

	if val, err = getProp(root, name); err == nil {
		if rval, err = time.Parse(time.RFC3339, val); err != nil {
			err = fmt.Errorf("malformed %s property: %s",
				name, val)
		}
	}

	return &rval, err
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
			log.Printf("Malformed ring %s: %v\n", ringName, err)
		}
	}

	return set
}

func getClient(client *PropertyNode) *ClientInfo {
	var ring, dns, dhcp, identity string
	var confidence float64
	var ipv4 net.IP
	var exp *time.Time
	var private bool

	private, _ = getBoolVal(client, "dns_private")
	ring, _ = getStringVal(client, "ring")
	identity, _ = getStringVal(client, "identity")
	confidence, _ = getFloat64Val(client, "confidence")
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

// GetVulnerabilities fetches a map of the vulnerabilities detected on a single
// client
func (c *APConfig) GetVulnerabilities(macaddr string) VulnMap {
	list := make(VulnMap)

	vulns, _ := c.GetProps("@/clients/" + macaddr + "/vulnerabilities")
	if vulns != nil {
		for name, props := range vulns.Children {
			var v VulnInfo
			v.Ignore, _ = getBoolVal(props, "ignore")
			v.Active, _ = getBoolVal(props, "active")
			v.FirstDetected, _ = getTimeVal(props, "first")
			v.LatestDetected, _ = getTimeVal(props, "latest")
			v.WarnedAt, _ = getTimeVal(props, "warned")
			list[name] = &v
		}
	}

	return list
}

// GetClientScans fetches a list of the scans performed on a single client
func (c *APConfig) GetClientScans(macaddr string) ScanMap {
	scanMap := make(ScanMap)

	scans, _ := c.GetProps("@/clients/" + macaddr + "/scans")
	if scans != nil {
		for name, props := range scans.Children {
			var s ScanInfo
			s.Start, _ = getTimeVal(props, "start")
			s.Finish, _ = getTimeVal(props, "finish")
			scanMap[name] = &s
		}
	}

	return scanMap
}

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

	ops := []PropertyOp{
		{Op: PropGet, Name: path},
	}
	tree, err := c.Execute(ops)

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

	localNodeName, err := c.platform.GetNodeID()
	if err != nil {
		return nil, err
	}
	s := make([]string, 0)
	for nodeName, node := range prop.Children {
		if local && nodeName != localNodeName {
			continue
		}

		nics := node.Children["nics"]
		if nics == nil {
			continue
		}

		for nicName, nic := range nics.Children {
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

	if active, _ := c.GetProps("@/firewall/blocked"); active != nil {
		now := time.Now()
		for name, node := range active.Children {
			if node.Expires == nil || now.Before(*node.Expires) {
				list = append(list, name)
			}
		}
	}

	return list
}

// GetDomain returns the default "appliance domainname" -- i.e.
// <siteid>.brightgate.net.
func (c *APConfig) GetDomain() (string, error) {
	const prop = "@/siteid"

	siteid, err := c.GetProp(prop)
	if err != nil {
		return "", fmt.Errorf("property get %s failed: %v", prop, err)
	}
	return siteid + "." + base_def.GATEWAY_CLIENT_DOMAIN, nil
}
