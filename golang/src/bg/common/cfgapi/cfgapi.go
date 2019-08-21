/*
 * COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
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
	"io"
	"log"
	"math/bits"
	"net"
	"sort"
	"strconv"
	"time"

	"bg/base_def"
	"bg/common/network"

	"github.com/satori/uuid"
)

// Version gets increased each time there is a non-compatible change to the
// config tree format, or configd API.
const Version = int32(23)

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
	ErrNoConfig   = errors.New("no configuration available")
	ErrNoProp     = errors.New("no such property")
	ErrExpired    = errors.New("property expired")
	ErrBadOp      = errors.New("no such operation")
	ErrBadVer     = errors.New("unsupported version")
	ErrBadCmd     = errors.New("no such command")
	ErrBadTime    = errors.New("invalid timestamp")
	ErrQueued     = errors.New("command still queued")
	ErrInProgress = errors.New("command in progress")
	ErrNotSupp    = errors.New("not supported")
	ErrNotEqual   = errors.New("not equal to expected value")
	ErrTimeout    = errors.New("communication timeout")
	ErrBadTree    = errors.New("unable to parse tree")
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

// MaxRings is the largest number of rings we support.  This includes currently
// defined rings and proposed rings, rounded up to the next power of 2.  If we
// reserve 8 bits for each subnet, this allows us to have 15 sites in the
// 192.x.x.x range, 255 in the 172.x.x.x range, or 4095 in the 10.x.x.x range.
// In all cases we have room at the top of the range for Brightgate subnets for
// VPNs, etc.
const MaxRings = 16

var ringToSubnetIdx = map[string]int{
	base_def.RING_INTERNAL:   0,
	base_def.RING_UNENROLLED: 1,
	base_def.RING_CORE:       2,
	base_def.RING_STANDARD:   3,
	base_def.RING_DEVICES:    4,
	base_def.RING_GUEST:      5,
	base_def.RING_QUARANTINE: 6,
}

// RingConfig defines the parameters of a ring's subnet
type RingConfig struct {
	Subnet        string
	IPNet         *net.IPNet
	Bridge        string
	VirtualAP     string
	Vlan          int
	LeaseDuration int
}

// VirtualAP captures the configuration information of a virtual access point
type VirtualAP struct {
	SSID        string   `json:"ssid"`
	Tag5GHz     bool     `json:"tag5GHz"`
	KeyMgmt     string   `json:"keyMgmt"`
	Passphrase  string   `json:"passphrase,omitempty"`
	DefaultRing string   `json:"defaultRing"`
	Rings       []string `json:"rings"`
}

// NicInfo contains all the per-nic state stored in the config file
type NicInfo struct {
	Name    string
	Node    string
	MacAddr string
	Kind    string
	Ring    string
	Pseudo  bool
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
	ConnBand   string     // Connection Radio Band (2.4GHz, 5GHz)
	ConnNode   *uuid.UUID // Connection Node
	ConnVAP    string     // Connection Virtual AP
	Wireless   bool       // Is this a wireless client?
	active     string
}

// VulnInfo represents the detection of a single vulnerability in a single
// client.
type VulnInfo struct {
	FirstDetected  *time.Time // When the vuln was first seen
	LatestDetected *time.Time // When the vuln was most recently seen
	WarnedAt       *time.Time // When the last log message / Event was sent
	ClearedAt      *time.Time // When the vuln was most recently cleared
	RepairedAt     *time.Time // When the vuln was most recently repaired
	Ignore         bool       // If the vuln is seen, take no action
	Active         bool       // vuln was present on last scan
	Details        string     // Additional details from the scanner
	Repair         *bool      // Null: no info T: watcher listen>repair; F: repair failed
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
	PropAdd
	PropTest
	PropTestEq
	TreeReplace
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
	Modified *time.Time `json:"Modified,omitempty"`
	Expires  *time.Time `json:"Expires,omitempty"`
	Children ChildMap   `json:"Children,omitempty"`
}

// GetComm takes a handle to a cfgapi endpoint and returns the handle for
// its communications layer.
func (c *Handle) GetComm() interface{} {
	return c.exec
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

	if err == ErrNoProp || err == ErrNoConfig {
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

// Replace attempts to swap out the entire config tree
func (c *Handle) Replace(newTree []byte) error {
	ops := []PropertyOp{
		{Op: TreeReplace, Name: "@/", Value: string(newTree)},
	}
	_, err := c.Execute(nil, ops).Wait(nil)

	return err
}

// Close closes the underlying connection
func (c *Handle) Close() {
	c.exec.Close()
}

// DisplayName returns the name of the client suitable for primary
// display to the user.
func (c *ClientInfo) DisplayName() string {
	if c.DNSName != "" {
		return c.DNSName
	}
	return c.DHCPName
}

// IsActive returns 'true' if a wireless client is connected to an AP, or if a
// wired client has a valid IP address.
func (c *ClientInfo) IsActive() bool {
	if c == nil {
		return false
	}
	if c.active == "true" {
		return true
	}
	if c.active == "false" {
		return false
	}

	validIP := false
	if c.IPv4 != nil {
		if c.Expires == nil || !c.Expires.Before(time.Now()) {
			validIP = true
		}
	}

	return validIP
}

func dumpSubtree(w io.Writer, name string, node *PropertyNode, indent string) {
	e := ""
	if node.Expires != nil {
		if time.Now().After(*node.Expires) {
			return
		}
		e = node.Expires.Format("2006-01-02T15:04:05")
	}
	fmt.Fprintf(w, "%s%s: %s  %s\n", indent, name, node.Value, e)

	leafChildren := make([]string, 0)
	interiorChildren := make([]string, 0)
	for childName, childNode := range node.Children {
		if len(childNode.Children) == 0 {
			leafChildren = append(leafChildren, childName)
		} else {
			interiorChildren = append(interiorChildren, childName)
		}
	}
	sort.Strings(leafChildren)
	sort.Strings(interiorChildren)
	nextIndent := indent + "  "
	for _, child := range leafChildren {
		dumpSubtree(w, child, node.Children[child], nextIndent)
	}
	for _, child := range interiorChildren {
		dumpSubtree(w, child, node.Children[child], nextIndent)
	}
}

// DumpTree displays the contents of a property tree in a human-legible format
func (n *PropertyNode) DumpTree(w io.Writer, root string) {
	dumpSubtree(w, root, n, "")
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

// Note that unlike other functions in this family, this routine returns
// nil if the time is not present.  Hence the Nil in the name.
func getTimeValNil(root *PropertyNode, name string) (*time.Time, error) {
	var val string
	var rval time.Time
	var err error

	if val, err = getProp(root, name); err == nil {
		if rval, err = time.Parse(time.RFC3339, val); err != nil {
			err = fmt.Errorf("malformed %s property: %s",
				name, val)
		}
	}
	if rval.IsZero() {
		return nil, err
	}
	return &rval, err
}

func getUUIDVal(root *PropertyNode, name string) (*uuid.UUID, error) {
	var val string
	var err error
	var uu uuid.UUID

	if val, err = getProp(root, name); err != nil {
		return nil, err
	}

	if uu, err = uuid.FromString(val); err != nil {
		return nil, fmt.Errorf("malformed %s property: %s", name, val)
	}

	return &uu, nil
}

func getIPv4Val(root *PropertyNode, name string) (*net.IP, error) {
	var val string
	var err error

	if val, err = getProp(root, name); err != nil {
		return nil, err
	}

	if ip := net.ParseIP(val); ip != nil {
		return &ip, nil
	}
	return nil, fmt.Errorf("Invalid ipv4 address")
}

// fetch the various properties we need to calculate the subnet addresses for
// each ring at this site.
func (c *Handle) getSubnetInfo() (string, int, error) {
	var baseProp, siteProp string
	var siteIndex int
	var err error

	if siteProp, err = c.GetProp("@/site_index"); err != nil {
		err = fmt.Errorf("fetching site index: %v", err)

	} else if siteIndex, err = strconv.Atoi(siteProp); err != nil {
		err = fmt.Errorf("parsing site index: %v", err)

	} else if baseProp, err = c.GetProp("@/network/base_address"); err != nil {
		err = fmt.Errorf("fetching base_address: %v", err)

	} else if _, _, err = net.ParseCIDR(baseProp); err != nil {
		err = fmt.Errorf("parsing base address %s: %v", baseProp, err)
	}

	return baseProp, siteIndex, err
}

// GenSubnet calculates the subnet address for a ring.
func GenSubnet(base string, siteIdx, subnetIdx int) (string, error) {
	maxSubnetIdx := MaxRings - 1
	if subnetIdx > maxSubnetIdx {
		return "", fmt.Errorf("subnetIdx must be <= %d", maxSubnetIdx)
	}
	idxBits := uint(bits.Len(uint(maxSubnetIdx)))

	ipaddr, ipnet, err := net.ParseCIDR(base)
	if err != nil {
		return "", fmt.Errorf("parsing base address %s: %v", base, err)
	}
	ones, bits := ipnet.Mask.Size()
	width := uint(bits - ones)

	baseInt := network.IPAddrToUint32(ipaddr)
	subnetInt := baseInt + uint32(((siteIdx<<idxBits)+subnetIdx)<<width)
	subnet := network.Uint32ToIPAddr(subnetInt)

	if !network.IsPrivate(subnet) {
		return "", fmt.Errorf("%s is not a private subnet", subnet)
	}

	cidr := fmt.Sprintf("%v/%d", subnet, ones)
	return cidr, nil
}

// GetRings fetches the Rings subtree from ap.configd, and converts the json
// into a Ring -> RingConfig map
func (c *Handle) GetRings() RingMap {
	base, siteIdx, err := c.getSubnetInfo()
	if err != nil {
		log.Printf("Failed to get subnet info: %v\n", err)
		return nil
	}

	props, err := c.GetProps("@/rings")
	if err != nil {
		log.Printf("Failed to get ring list: %v\n", err)
		return nil
	}

	set := make(map[string]*RingConfig)
	for ringName, ring := range props.Children {
		var subnet, bridge, vap string
		var vlan, duration int
		var ipnet *net.IPNet
		var err error

		if !ValidRings[ringName] {
			err = fmt.Errorf("invalid ring name: %s", ringName)
		}
		if err == nil {
			vlan, err = getIntVal(ring, "vlan")
			if vlan >= 0 {
				bridge = "brvlan" + strconv.Itoa(int(vlan))
			}
		}
		if err == nil {
			vap, err = getStringVal(ring, "vap")
		}

		if err == nil {
			subnet, err = getStringVal(ring, "subnet")
			if err != nil {
				subnetIdx := ringToSubnetIdx[ringName]
				subnet, err = GenSubnet(base, siteIdx, subnetIdx)
			}
		}

		if err == nil {
			duration, err = getIntVal(ring, "lease_duration")
		}

		if err == nil {
			_, ipnet, err = net.ParseCIDR(subnet)
		}

		if err == nil {
			c := RingConfig{
				Vlan:          vlan,
				Subnet:        subnet,
				IPNet:         ipnet,
				Bridge:        bridge,
				VirtualAP:     vap,
				LeaseDuration: duration,
			}
			set[ringName] = &c
		} else {
			log.Printf("Malformed ring %s: %v\n", ringName, err)
		}
	}

	return set
}

func newVAP(name string, root *PropertyNode) *VirtualAP {
	var ssid, keymgmt, pass, defaultRing string
	var tag bool

	if x := root.Children["ssid"]; x != nil {
		ssid = x.Value
	} else {
		log.Printf("vap %s: missing ssid", name)
	}

	if x := root.Children["keymgmt"]; x != nil {
		keymgmt = x.Value
	} else {
		log.Printf("vap %s: missing keymgmt", name)
	}

	if keymgmt == "wpa-psk" {
		if node, ok := root.Children["passphrase"]; ok {
			pass = node.Value
		} else {
			log.Printf("vap %s: missing WPA-PSK passphrase", name)
		}
	}

	if x := root.Children["5ghz"]; x != nil {
		b, err := strconv.ParseBool(x.Value)
		if err != nil {
			log.Printf("vap %s: malformed 5ghz: %s", name, x.Value)
		}
		tag = b
	}

	if x := root.Children["default_ring"]; x != nil {
		defaultRing = x.Value
	} else {
		log.Printf("vap %s: missing default_ring", name)
	}

	return &VirtualAP{
		SSID:        ssid,
		KeyMgmt:     keymgmt,
		Passphrase:  pass,
		Tag5GHz:     tag,
		Rings:       make([]string, 0),
		DefaultRing: defaultRing,
	}
}

// GetVirtualAPs returns a map of all the virtual APs configured for this
// appliances
func (c *Handle) GetVirtualAPs() map[string]*VirtualAP {

	props, err := c.GetProps("@/network/vap")
	if err != nil {
		log.Printf("Failed to get VirtualAP list: %v\n", err)
		return nil
	}

	vaps := make(map[string]*VirtualAP)
	for vapName, conf := range props.Children {
		vaps[vapName] = newVAP(vapName, conf)
	}

	// populate the Rings[] slice of each VirtualAP
	rings := c.GetRings()
	if err != nil {
		log.Printf("Failed to get ring list: %v\n", err)
	}
	for ringName, ringConfig := range rings {
		vap, ok := vaps[ringConfig.VirtualAP]
		if ok && ValidRings[ringName] {
			vap.Rings = append(vap.Rings, ringName)
		}
	}

	return vaps
}

// DNSInfo captures DNS configuration information
type DNSInfo struct {
	Domain  string   `json:"domain"`
	Servers []string `json:"servers"`
}

// GetDNSInfo returns the DNS configuration.
func (c *Handle) GetDNSInfo() *DNSInfo {
	domain, _ := c.GetProp("@/siteid")
	server, _ := c.GetProp("@/network/dnsserver")
	d := &DNSInfo{
		Domain:  domain,
		Servers: make([]string, 0),
	}
	if server != "" {
		d.Servers = append(d.Servers, server)
	}
	return d
}

// WanInfo captures the configuration information of the WAN link
type WanInfo struct {
	CurrentAddress string     `json:"currentAddress,omitempty"`
	StaticAddress  string     `json:"staticAddress,omitempty"`
	StaticRoute    *net.IP    `json:"staticRoute,omitempty"`
	DNSServer      string     `json:"dnsServer,omitempty"`
	DHCPAddress    string     `json:"dhcpAddress,omitempty"`
	DHCPStart      *time.Time `json:"dhcpStart,omitempty"`
	DHCPDuration   int        `json:"dhcpDuration,omitempty"`
	DHCPRoute      *net.IP    `json:"dhcpRoute,omitempty"`
}

// GetWanInfo returns the WAN configuration.
func (c *Handle) GetWanInfo() *WanInfo {
	var w WanInfo

	props, _ := c.GetProps("@/network")
	if props == nil {
		return nil
	}

	wan := props.Children["wan"]
	if wan == nil {
		return nil
	}

	if current := wan.Children["current"]; current != nil {
		w.CurrentAddress, _ = getStringVal(current, "address")
	}
	if static := wan.Children["static"]; static != nil {
		w.StaticAddress, _ = getStringVal(static, "address")
		w.StaticRoute, _ = getIPv4Val(static, "route")
	}
	if dhcp := wan.Children["dhcp"]; dhcp != nil {
		w.DHCPAddress, _ = getStringVal(dhcp, "address")
		w.DHCPRoute, _ = getIPv4Val(dhcp, "route")
		w.DHCPStart, _ = getTimeValNil(dhcp, "start")
		w.DHCPDuration, _ = getIntVal(dhcp, "duration")
	}
	w.DNSServer, _ = getStringVal(props, "dnsserver")
	return &w
}

func getClient(client *PropertyNode) *ClientInfo {
	var ring, dns, dhcp, identity string
	var confidence float64
	var ipv4 net.IP
	var exp *time.Time
	var wireless, private bool
	var connVAP, connBand, active string
	var connNode *uuid.UUID
	var err error

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
	if conn, ok := client.Children["connection"]; ok {
		connVAP, _ = getStringVal(conn, "vap")
		connBand, _ = getStringVal(conn, "band")
		connNode, _ = getUUIDVal(conn, "node")
		active, _ = getStringVal(conn, "active")
		wireless, err = getBoolVal(conn, "wireless")
		// Improve our guess for legacy devices which don't have the
		// 'wireless' boolean.
		if err != nil && connVAP != "" {
			wireless = true
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
		ConnBand:   connBand,
		ConnNode:   connNode,
		ConnVAP:    connVAP,
		Wireless:   wireless,
		active:     active,
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
			v.FirstDetected, _ = getTimeValNil(props, "first")
			v.LatestDetected, _ = getTimeValNil(props, "latest")
			v.WarnedAt, _ = getTimeValNil(props, "warned")
			v.ClearedAt, _ = getTimeValNil(props, "cleared")
			v.RepairedAt, _ = getTimeValNil(props, "repaired")
			v.Ignore, _ = getBoolVal(props, "ignore")
			v.Active, _ = getBoolVal(props, "active")
			v.Details, _ = getStringVal(props, "details")
			// Repair may be absent and the distinction is important
			if val, err := getProp(props, "repair"); err == nil {
				if repair, err := strconv.ParseBool(val); err == nil {
					v.Repair = &repair
				}
			}
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
			s.Start, _ = getTimeValNil(props, "start")
			s.Finish, _ = getTimeValNil(props, "finish")
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
	set := make(map[string]*ClientInfo)

	props, err := c.GetProps("@/clients")
	if err != nil {
		log.Printf("Failed to get clients list: %v\n", err)
	} else {
		for name, client := range props.Children {
			set[name] = getClient(client)
		}
	}

	return set
}

// GetNics returns a slice of mac addresses representing the configured NICs on
// all nodes.
func (c *Handle) GetNics() ([]NicInfo, error) {
	prop, err := c.GetProps("@/nodes")
	if err != nil {
		return nil, fmt.Errorf("property get @/nodes failed: %v", err)
	}

	nics := make([]NicInfo, 0)
	for nodeName, node := range prop.Children {
		nodeNics := node.Children["nics"]
		if nodeNics == nil {
			continue
		}

		for _, nic := range nodeNics.Children {
			n := NicInfo{
				Node: nodeName,
			}

			n.Name, _ = getStringVal(nic, "name")
			n.Ring, _ = getStringVal(nic, "ring")
			n.MacAddr, _ = getStringVal(nic, "mac")
			n.Kind, _ = getStringVal(nic, "kind")
			n.Pseudo, _ = getBoolVal(nic, "pseudo")

			nics = append(nics, n)
		}
	}
	return nics, nil
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
// <integer>.[<jurisdiction>.]brightgate.net.
func (c *Handle) GetDomain() (string, error) {
	const prop = "@/siteid"

	siteid, err := c.GetProp(prop)
	if err != nil {
		return "", fmt.Errorf("property get %s failed: %v", prop, err)
	}
	return siteid, nil
}
