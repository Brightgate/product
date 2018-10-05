/*
 * COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package cfgapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"strconv"
	"time"

	"bg/base_def"
)

// Version gets increased each time there is a non-compatible change to the
// config tree format, or configd API.
const Version = int32(16)

// CmdHdl is returned when one or more operations are submitted to Execute().
// This handle can be used to check on the status of a pending operation, or to
// block until the operation completes or times out.
type CmdHdl interface {
	Status(ctx context.Context) (string, error)
	Wait(ctx context.Context) (string, error)
}

// ConfigExec defines the operations that must be supplied by a
// platform-specific communications layer, in order to support the
// platform-independent cfgapi later.
type ConfigExec interface {
	Ping(ctx context.Context) error
	Execute(ctx context.Context, ops []PropertyOp) CmdHdl
	HandleChange(path string, handler func([]string, string, *time.Time)) error
	HandleDelete(path string, handler func([]string)) error
	HandleExpire(path string, handler func([]string)) error
	Close()
}

// Handle is an opaque handle that encapsulates a connection to *.configd, and
// which allows cfgapi operations to be executed.
type Handle struct {
	exec ConfigExec
}

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
	ErrComm       = errors.New("communication breakdown")
	ErrNoProp     = errors.New("no such property")
	ErrExpired    = errors.New("property expired")
	ErrBadOp      = errors.New("no such operation")
	ErrBadVer     = errors.New("unsupported version")
	ErrBadCmd     = errors.New("no such command")
	ErrBadTime    = errors.New("invalid timestamp")
	ErrQueued     = errors.New("command still queued")
	ErrInProgress = errors.New("command in progress")
	ErrNotSupp    = errors.New("not supported")
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

// NewHandle takes a handle to a communications layer, and returns a handle
// that represents a cfgapi client endpoint.
func NewHandle(exec ConfigExec) *Handle {
	return &Handle{
		exec: exec,
	}
}

// HandleChange allows clients to register a callback that will be invoked when
// a property changes.
func (c *Handle) HandleChange(path string, handler func([]string, string,
	*time.Time)) error {
	return c.exec.HandleChange(path, handler)
}

// HandleDelete allows clients to register a callback that will be invoked when
// a property is deleted
func (c *Handle) HandleDelete(path string, handler func([]string)) error {
	return c.exec.HandleDelete(path, handler)
}

// HandleExpire allows clients to register a callback that will be invoked when
// a property expires
func (c *Handle) HandleExpire(path string, handler func([]string)) error {
	return c.exec.HandleExpire(path, handler)
}

// GetProps retrieves the properties subtree rooted at the given property, and
// returns a PropertyNode representing the root of that subtree
func (c *Handle) GetProps(prop string) (*PropertyNode, error) {
	var root PropertyNode

	ops := []PropertyOp{
		{Op: PropGet, Name: prop},
	}

	tree, err := c.Execute(nil, ops).Wait(nil)

	if err == ErrNoProp {
		return nil, err
	} else if err != nil {
		return nil, fmt.Errorf("Failed to retrieve %s: %v", prop, err)
	} else if err = json.Unmarshal([]byte(tree), &root); err != nil {
		// XXX: this should really be in cfgtree
		return nil, fmt.Errorf("Failed to decode %s: %v", prop, err)
	}

	return &root, err
}

// GetProp retrieves a single property from the tree, returning it as a String
func (c *Handle) GetProp(prop string) (string, error) {
	var rval string

	root, err := c.GetProps(prop)
	if err == nil {
		rval = root.Value
	}

	return rval, err
}

// SetProp updates a single property, taking an optional expiration time.  If
// the property doesn't already exist, an error is returned.
func (c *Handle) SetProp(prop, val string, expires *time.Time) error {
	ops := []PropertyOp{
		{Op: PropSet, Name: prop, Value: val, Expires: expires},
	}
	_, err := c.Execute(nil, ops).Wait(nil)

	return err
}

// CreateProp updates a single property, taking an optional expiration time.  If
// the property doesn't already exist, it is created - as well as any parent
// properties needed to provide a path through the tree.
func (c *Handle) CreateProp(prop, val string, expires *time.Time) error {
	ops := []PropertyOp{
		{Op: PropCreate, Name: prop, Value: val, Expires: expires},
	}
	_, err := c.Execute(nil, ops).Wait(nil)

	return err
}

// DeleteProp will delete a property, or property subtree
func (c *Handle) DeleteProp(prop string) error {
	ops := []PropertyOp{
		{Op: PropDelete, Name: prop},
	}
	_, err := c.Execute(nil, ops).Wait(nil)

	return err
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

// Execute takes a slice of PropertyOp structures and enqueues them for
// submission to a config daemon.  It returns a handle which may be used to
// check the status of the operation.
func (c *Handle) Execute(ctx context.Context, ops []PropertyOp) CmdHdl {
	return c.exec.Execute(ctx, ops)
}

// Ping performs a simple round-trip connectivity test
func (c *Handle) Ping(ctx context.Context) error {
	return c.exec.Ping(ctx)
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
func (c *Handle) GetRings() RingMap {
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
func (c *Handle) GetVulnerabilities(macaddr string) VulnMap {
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
func (c *Handle) GetClientScans(macaddr string) ScanMap {
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
func (c *Handle) GetClient(macaddr string) *ClientInfo {
	client, err := c.GetProps("@/clients/" + macaddr)
	if err != nil {
		log.Printf("Failed to get %s: %v\n", macaddr, err)
		return nil
	}

	return getClient(client)
}

// GetClients the full Clients subtree, and converts the returned json into a
// map of ClientInfo structures, indexed by the client's mac address
func (c *Handle) GetClients() ClientMap {
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

// GetNics returns a slice of mac addresses representing the configured NICs.
// The caller may choose to limit the slice to NICs carrying traffic for a
// single ring and/or NICs that are local to a specific node.
func (c *Handle) GetNics(ring string, limit string) ([]string, error) {
	prop, err := c.GetProps("@/nodes")
	if err != nil {
		return nil, fmt.Errorf("property get %s failed: %v", prop, err)
	}

	s := make([]string, 0)
	for nodeName, node := range prop.Children {
		if limit != "" && limit != nodeName {
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
func (c *Handle) GetActiveBlocks() []string {
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
func (c *Handle) GetDomain() (string, error) {
	const prop = "@/siteid"

	siteid, err := c.GetProp(prop)
	if err != nil {
		return "", fmt.Errorf("property get %s failed: %v", prop, err)
	}
	return siteid + "." + base_def.GATEWAY_CLIENT_DOMAIN, nil
}
