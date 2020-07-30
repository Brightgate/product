/*
 * COPYRIGHT 2020 Brightgate Inc.  All rights reserved.
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
	"strings"
	"time"

	"bg/base_def"
	"bg/common/network"
	"bg/common/wifi"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

// Version gets increased each time there is a non-compatible change to the
// config tree format, or configd API.
const Version = int32(34)

// CmdHdl is returned when one or more operations are submitted to Execute().
// This handle can be used to check on the status of a pending operation, or to
// block until the operation completes or times out.
type CmdHdl interface {
	Status(ctx context.Context) (string, error)
	Wait(ctx context.Context) (string, error)
	Cancel(ctx context.Context) error
}

// ConfigExec defines the operations that must be supplied by a
// platform-specific communications layer, in order to support the
// platform-independent cfgapi later.
type ConfigExec interface {
	Ping(ctx context.Context) error
	Execute(ctx context.Context, ops []PropertyOp) CmdHdl
	ExecuteAt(ctx context.Context, ops []PropertyOp, level AccessLevel) CmdHdl
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
	ErrNotLeaf    = errors.New("not a leaf property")
	ErrBadType    = errors.New("property type mismatch")
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
	base_def.RING_VPN:        true,
}

// SystemRings is a map containing special rings that are exempted from many
// standard functions.
var SystemRings = map[string]bool{
	base_def.RING_INTERNAL: true,
	base_def.RING_VPN:      true,
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
	base_def.RING_VPN:        7,
}

// RingConfig defines the parameters of a ring's subnet
type RingConfig struct {
	Subnet        string
	IPNet         *net.IPNet
	Bridge        string
	VirtualAPs    []string
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
	Disabled    bool     `json:"disabled"`
}

// WifiInfo contains both the configured and actual band, channel, and channel
// width parameters for a wireless device.
type WifiInfo struct {
	ConfigBand    string `json:"configBand"`    // "" -> not configured
	ConfigChannel int    `json:"configChannel"` // 0 -> not configured
	ConfigWidth   string `json:"configWidth"`   // "" -> not configured

	ActiveMode    string `json:"activeMode"`
	ActiveBand    string `json:"activeBand"`
	ActiveChannel int    `json:"activeChannel"`
	ActiveWidth   string `json:"activeWidth"`

	ValidBands      []string `json:"validBands"`
	ValidModes      []string `json:"validModes"`
	ValidLoChannels []int    `json:"validLoChannels"`
	ValidHiChannels []int    `json:"validHiChannels"`
}

// NicInfo contains all the per-nic state stored in the config file
type NicInfo struct {
	Name     string
	MacAddr  string
	Kind     string
	Ring     string
	WifiInfo *WifiInfo
	State    string // Only valid for real nics - not pseudo
	Pseudo   bool
}

// NodeInfo contains information about a single gateway or satellite node
type NodeInfo struct {
	ID       string
	Platform string
	Name     string
	Role     string
	BootTime *time.Time
	Alive    *time.Time
	Addr     net.IP
	Nics     []NicInfo
}

// DevIDInfo contains classification information for a client device
type DevIDInfo struct {
	OUIMfg      string `json:"ouiMfg"`      // Based on lookup of MAC in OUI database, e.g. "Apple"
	DeviceGenus string `json:"deviceGenus"` // e.g. "Apple Watch"
	OSGenus     string `json:"osGenus"`     // e.g. "watchOS"
}

// ClientInfo contains all of the configuration information for a client device
type ClientInfo struct {
	Ring         string     // Current/latest security ring
	Home         string     // Intended security ring
	FriendlyName string     // Assigned friendly
	FriendlyDNS  string     // Hostname derived from FriendlyName
	DNSName      string     // Assigned hostname
	IPv4         net.IP     // Network address
	Expires      *time.Time // DHCP lease expiration time
	DHCPName     string     // Requested hostname
	DNSPrivate   bool       // We don't collect DNS queries
	Username     string     // Name used for EAP authentication
	ConnBand     string     // Connection Radio Band (2.4GHz, 5GHz)
	ConnNode     string     // Connection Node
	ConnVAP      string     // Connection Virtual AP
	DevID        *DevIDInfo // Device identification information
	Wireless     bool       // Is this a wireless client?
	active       string
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
	PropTest
	PropTestEq
	AddPropValidation
	TreeReplace
)

// PropertyOp represents an operation on a single property
type PropertyOp struct {
	Op      int
	Name    string
	Value   string
	Expires *time.Time
}

var opName = map[int]string{
	PropGet:           "PropGet",
	PropSet:           "PropSet",
	PropCreate:        "PropCreate",
	PropDelete:        "PropDelete",
	PropTest:          "PropTest",
	PropTestEq:        "PropTestEq",
	AddPropValidation: "AddPropValidation",
	TreeReplace:       "TreeReplace",
}

func (p PropertyOp) String() string {
	s := fmt.Sprintf("<%s", opName[p.Op])
	if p.Name != "" {
		s += " " + p.Name
	}
	if p.Value != "" {
		if p.Op == PropSet || p.Op == PropCreate {
			s += fmt.Sprintf("=%q", p.Value)
		} else if p.Op == PropTestEq {
			s += fmt.Sprintf("==%q", p.Value)
		} else {
			s += fmt.Sprintf(" %q", p.Value)
		}
	}
	if p.Expires != nil {
		s += " (" + p.Expires.Format(time.Stamp) + ")"
	}
	s += ">"
	return s
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

// HandleDelExp allows clients to register a callback that will be invoked when
// a property is deleted or expires
func (c *Handle) HandleDelExp(path string, handler func([]string)) error {
	err := c.exec.HandleDelete(path, handler)
	if err == nil {
		err = c.exec.HandleExpire(path, handler)
	}
	return err
}

// AddPropValidation adds a new property and value type to ap.configd's syntax
// validation table.
func (c *Handle) AddPropValidation(path, proptype string) error {
	ops := []PropertyOp{
		{
			Op:    AddPropValidation,
			Name:  path,
			Value: proptype,
		},
	}

	_, err := c.exec.ExecuteAt(nil, ops, AccessInternal).Wait(nil)

	return err
}

// GetChildren retrieves the properties subtree rooted at the given property,
// and returns a map representing the immediate children, if any, of that
// property.  It is not considered an error if the property is missing or
// has no children.
func (c *Handle) GetChildren(prop string) ChildMap {
	var rval ChildMap

	if node, err := c.GetProps(prop); err == nil {
		rval = node.Children
	}

	return rval
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
		if len(root.Children) > 0 {
			err = ErrNotLeaf
		} else {
			rval = root.Value
		}
	}

	return rval, err
}

// GetPropInt retrieves a single property, returning it as an integer.
func (c *Handle) GetPropInt(prop string) (int, error) {
	var rval int

	v, err := c.GetProp(prop)
	if err == nil {
		if rval, err = strconv.Atoi(v); err != nil {
			err = ErrBadType
		}
	}

	return rval, err
}

// GetPropBool retrieves a single property, returning it as a boolean.
func (c *Handle) GetPropBool(prop string) (bool, error) {
	var rval bool

	v, err := c.GetProp(prop)
	if err == nil {
		if strings.EqualFold(v, "true") {
			rval = true
		} else if strings.EqualFold(v, "false") {
			rval = false
		} else {
			err = ErrBadType
		}
	}

	return rval, err
}

// GetPropDuration retrieves a single property from the tree, returning it as a
// time.Duration
func (c *Handle) GetPropDuration(prop string) (time.Duration, error) {
	var rval time.Duration

	val, err := c.GetProp(prop)
	if err == nil {
		rval, err = time.ParseDuration(val)
		if err != nil {
			err = ErrBadType
		}
	}

	return rval, err
}

// SetProp updates a single property, taking an optional expiration time.  If
// the property doesn't already exist, an error is returned.
func (c *Handle) SetProp(prop, val string, expires *time.Time) error {
	if expires != nil && expires.IsZero() {
		expires = nil
	}

	ops := []PropertyOp{
		{Op: PropSet, Name: prop, Value: val, Expires: expires},
	}
	_, err := c.Execute(nil, ops).Wait(nil)

	return err
}

// SetProps updates multiple properties, taking an optional expiration time.  If
// any property doesn't already exist, an error is returned.
func (c *Handle) SetProps(all map[string]string, expires *time.Time) error {
	if expires != nil && expires.IsZero() {
		expires = nil
	}

	if len(all) == 0 {
		return nil
	}

	ops := make([]PropertyOp, 0)
	for prop, val := range all {
		op := PropertyOp{
			Op:      PropSet,
			Name:    prop,
			Value:   val,
			Expires: expires}
		ops = append(ops, op)
	}

	_, err := c.Execute(nil, ops).Wait(nil)

	return err
}

// CreateProp updates a single property, taking an optional expiration time.  If
// the property doesn't already exist, it is created - as well as any parent
// properties needed to provide a path through the tree.
func (c *Handle) CreateProp(prop, val string, expires *time.Time) error {
	if expires != nil && expires.IsZero() {
		expires = nil
	}

	ops := []PropertyOp{
		{Op: PropCreate, Name: prop, Value: val, Expires: expires},
	}
	_, err := c.Execute(nil, ops).Wait(nil)

	return err
}

// CreateProps creates multiple properties, taking an optional expiration time.
func (c *Handle) CreateProps(all map[string]string, expires *time.Time) error {
	if expires != nil && expires.IsZero() {
		expires = nil
	}

	if len(all) == 0 {
		return nil
	}

	ops := make([]PropertyOp, 0)
	for prop, val := range all {
		op := PropertyOp{
			Op:      PropCreate,
			Name:    prop,
			Value:   val,
			Expires: expires}
		ops = append(ops, op)
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

// CfgFeature describes a configuration feature
type CfgFeature string

// FeatureClientFriendlyName indicates that the site is running software new
// enough to accept friendly_name property settings.  This functionality was
// introduced with cfgversion 25.
const FeatureClientFriendlyName CfgFeature = "clientFriendlyName"

// FeatureVPNConfig indicates that the site is running software new
// enough to accept VPN related user and site level property settings.  This
// functionality was introduced with cfgversion 28.
const FeatureVPNConfig CfgFeature = "vpnConfig"

// FeatureUserServerKey indicates that the site is capable of recording the
// current VPN server public key in a user's client config record.  Storing the
// server key in the client config lets us identify client VPN keys that have
// been made stale by the generation of a new server key.
const FeatureUserServerKey CfgFeature = "vpnUserServerKey"

// CfgFeatures captures information about config-tree related features which
// may be present, which are not obviously discoverable simply by inspecting
// the tree.
type CfgFeatures map[CfgFeature]bool

// GetFeatures returns the Features information for the tree being inspected;
// this allows clients to determine whether certain functionality is supported.
func (c *Handle) GetFeatures() (CfgFeatures, error) {
	val, err := c.GetProp("@/cfgversion")
	if err != nil {
		return nil, err
	}
	var rval int
	if rval, err = strconv.Atoi(val); err != nil {
		return nil, fmt.Errorf("malformed cfgversion: %s", val)
	}
	features := make(CfgFeatures)
	if rval >= 25 {
		features[FeatureClientFriendlyName] = true
	}
	if rval >= 28 {
		features[FeatureVPNConfig] = true
	}
	if rval >= 34 {
		features[FeatureUserServerKey] = true
	}
	return features, nil
}

// DisplayName returns the name of the client suitable for primary
// display to the user.
func (c *ClientInfo) DisplayName() string {

	if c.FriendlyName != "" {
		return c.FriendlyName
	} else if c.DNSName != "" {
		return c.DNSName
	} else if c.DHCPName != "" {
		return c.DHCPName
	}
	return ""
}

// HasIP returns true if the client has a static IP address or a DHCP lease
// which hasn't expired.
func (c *ClientInfo) HasIP() bool {
	if c.IPv4 == nil {
		return false
	}
	if c.Expires != nil && c.Expires.Before(time.Now()) {
		return false
	}

	return true
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

	return c.HasIP()
}

func dumpSubtree(w io.Writer, name string, node *PropertyNode, indent string) {
	if node.Expired() {
		return
	}

	e := ""
	if node.Expires != nil {
		e = node.Expires.Format("2006-01-02T15:04:05")
		fmt.Fprintf(w, "%s%s: %s  %s\n", indent, name, node.Value, e)
	} else {
		fmt.Fprintf(w, "%s%s: %s\n", indent, name, node.Value)
	}

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

// GenSubnet calculates the subnet address for a given subnet index.
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

// RingSubnet returns the calculated subnet for a given ring
func RingSubnet(ring, base string, siteIdx int) (string, error) {
	subnetIdx, ok := ringToSubnetIdx[ring]
	if !ok {
		return "", fmt.Errorf("no such ring")
	}

	return GenSubnet(base, siteIdx, subnetIdx)
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
		var subnet, bridge string
		var vap []string
		var vlan, duration int
		var ipnet *net.IPNet
		var err error

		if !ValidRings[ringName] {
			err = fmt.Errorf("invalid ring name: %s", ringName)
		}
		if err == nil {
			vlan, err = ring.GetChildInt("vlan")
			if vlan >= 0 {
				bridge = "brvlan" + strconv.Itoa(int(vlan))
			}
		}
		if err == nil {
			vap, err = ring.GetChildStringSlice("vap")
		}

		if err == nil {
			subnet, err = ring.GetChildString("subnet")
			if err != nil {
				subnetIdx := ringToSubnetIdx[ringName]
				subnet, err = GenSubnet(base, siteIdx, subnetIdx)
			}
		}

		if err == nil {
			duration, err = ring.GetChildInt("lease_duration")
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
				VirtualAPs:    vap,
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

	tag, err := root.GetChildBool("5ghz")
	if err != nil && err != ErrNoProp {
		log.Printf("vap %s: %v", name, err)
	}

	disabled, err := root.GetChildBool("disabled")
	if err != nil && err != ErrNoProp {
		log.Printf("vap %s: %v", name, err)
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
		Disabled:    disabled,
	}
}

func ringsPerVap(allRings RingMap, vap string) []string {
	vapRings := make([]string, 0)

	for ringName, ringConfig := range allRings {
		for _, v := range ringConfig.VirtualAPs {
			if v == vap {
				vapRings = append(vapRings, ringName)
			}
		}
	}

	return vapRings
}

// GetClientRings returns a slice of all the rings available to this client,
// based on the type of connection (wired/wireless) and the VAP it last
// connected to.  If it has never connected to a VAP (i.e., if it's a new
// client), also offer a wide selection of valid rings.
func (c *Handle) GetClientRings(client *ClientInfo, allRings RingMap) []string {
	if !client.Wireless || client.ConnVAP == "" {
		// Return GetRings() - SystemRings
		ringList := make([]string, 0)
		for name := range allRings {
			if SystemRings[name] {
				continue
			}
			ringList = append(ringList, name)
		}
		return ringList
	}

	return ringsPerVap(c.GetRings(), client.ConnVAP)
}

// GetVirtualAPs returns a map of all the virtual APs configured for this
// appliance
func (c *Handle) GetVirtualAPs() map[string]*VirtualAP {

	props, err := c.GetProps("@/network/vap")
	if err != nil {
		log.Printf("Failed to get VirtualAP list: %v\n", err)
		return nil
	}

	rings := c.GetRings()

	vaps := make(map[string]*VirtualAP)
	for vapName, conf := range props.Children {
		vaps[vapName] = newVAP(vapName, conf)
		vaps[vapName].Rings = ringsPerVap(rings, vapName)
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
		w.CurrentAddress, _ = current.GetChildString("address")
	}

	if static := wan.Children["static"]; static != nil {
		w.StaticAddress, _ = static.GetChildString("address")
		w.StaticRoute, _ = static.GetChildIPv4("route")
	}
	if dhcp := wan.Children["dhcp"]; dhcp != nil {
		w.DHCPAddress, _ = dhcp.GetChildString("address")
		w.DHCPRoute, _ = dhcp.GetChildIPv4("route")
		w.DHCPStart, _ = dhcp.GetChildTime("start")
		w.DHCPDuration, _ = dhcp.GetChildInt("duration")
	}
	w.DNSServer, _ = props.GetChildString("dnsserver")
	return &w
}

func getClient(client *PropertyNode) *ClientInfo {
	var ipv4 net.IP
	var exp *time.Time
	var wireless bool
	var username, connVAP, connBand, connNode, active string
	var devID *DevIDInfo
	var err error

	private, _ := client.GetChildBool("dns_private")
	ring, _ := client.GetChildString("ring")
	home, _ := client.GetChildString("home")
	dhcp, _ := client.GetChildString("dhcp_name")
	dns, _ := client.GetChildString("dns_name")
	friendly, _ := client.GetChildString("friendly_name")
	friendlyDNS, _ := client.GetChildString("friendly_dns")
	if node, err := client.GetChild("ipv4"); err == nil {
		if ip, err := node.GetIPv4(); err == nil {
			ipv4 = ip.To4()
			exp = node.Expires
		}
	}
	if conn, ok := client.Children["connection"]; ok {
		username, _ = conn.GetChildString("username")
		connVAP, _ = conn.GetChildString("vap")
		connBand, _ = conn.GetChildString("band")
		connNode, _ = conn.GetChildString("node")
		active, _ = conn.GetChildString("active")
		wireless, err = conn.GetChildBool("wireless")
		// Improve our guess for legacy devices which don't have the
		// 'wireless' boolean.
		if err != nil && connVAP != "" {
			wireless = true
		}
	}
	if dev, ok := client.Children["classification"]; ok {
		ouiMfg, _ := dev.GetChildString("oui_mfg")
		devGenus, _ := dev.GetChildString("device_genus")
		osGenus, _ := dev.GetChildString("os_genus")
		devID = &DevIDInfo{
			OUIMfg:      ouiMfg,
			DeviceGenus: devGenus,
			OSGenus:     osGenus,
		}
	}

	c := ClientInfo{
		Ring:         ring,
		Home:         home,
		DHCPName:     dhcp,
		FriendlyName: friendly,
		FriendlyDNS:  friendlyDNS,
		DNSName:      dns,
		IPv4:         ipv4,
		Expires:      exp,
		DNSPrivate:   private,
		Username:     username,
		ConnBand:     connBand,
		ConnNode:     connNode,
		ConnVAP:      connVAP,
		Wireless:     wireless,
		DevID:        devID,
		active:       active,
	}
	return &c
}

// GetVulnerabilities fetches a map of the vulnerabilities detected on a single
// client
func (c *Handle) GetVulnerabilities(macaddr string) VulnMap {
	list := make(VulnMap)

	vulnProp := "@/clients/" + macaddr + "/vulnerabilities"
	for name, props := range c.GetChildren(vulnProp) {
		var v VulnInfo
		v.FirstDetected, _ = props.GetChildTime("first")
		v.LatestDetected, _ = props.GetChildTime("latest")
		v.WarnedAt, _ = props.GetChildTime("warned")
		v.ClearedAt, _ = props.GetChildTime("cleared")
		v.RepairedAt, _ = props.GetChildTime("repaired")
		v.Ignore, _ = props.GetChildBool("ignore")
		v.Active, _ = props.GetChildBool("active")
		v.Details, _ = props.GetChildString("details")
		// Repair may be absent and the distinction is important
		if val, err := props.GetChildBool("repair"); err == nil {
			v.Repair = &val
		}
		list[name] = &v
	}

	return list
}

// GetClientScans fetches a list of the scans performed on a single client
func (c *Handle) GetClientScans(macaddr string) ScanMap {
	scanMap := make(ScanMap)

	scanProp := "@/clients/" + macaddr + "/scans"
	for name, props := range c.GetChildren(scanProp) {
		var s ScanInfo
		s.Start, _ = props.GetChildTime("start")
		s.Finish, _ = props.GetChildTime("finish")
		scanMap[name] = &s
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

// ClientMetrics captures metrics about a specifc client device.
// The array format is chosen to provide a reasonably compact JSON
// encoding.
// [0, 1, 2, 3]: [second, minute, hour, day]
type ClientMetrics struct {
	LastActivity   *time.Time `json:"lastActivity"`
	SignalStrength int        `json:"signalStrength"`
	BytesRcvd      [4]uint64  `json:"bytesRcvd"`
	BytesSent      [4]uint64  `json:"bytesSent"`
	PktsRcvd       [4]uint64  `json:"pktsRcvd"`
	PktsSent       [4]uint64  `json:"pktsSent"`
}

// GetClientMetricsFromNode extracts client metrics from the given
// client metrics node.
func (c *Handle) GetClientMetricsFromNode(clientMetrics *PropertyNode) *ClientMetrics {
	var cm ClientMetrics
	// The order matches the slot order for the ClientMetrics struct
	cm.LastActivity, _ = clientMetrics.GetChildTime("last_activity")
	cm.SignalStrength, _ = clientMetrics.GetChildInt("signal_str")
	subs := []string{"second", "minute", "hour", "day"}
	for i, sub := range subs {
		m := clientMetrics.Children[sub]
		if m != nil {
			cm.BytesRcvd[i], _ = m.GetChildUint("bytes_rcvd")
			cm.BytesSent[i], _ = m.GetChildUint("bytes_sent")
			cm.PktsRcvd[i], _ = m.GetChildUint("pkts_rcvd")
			cm.PktsSent[i], _ = m.GetChildUint("pkts_sent")
		}
	}
	return &cm
}

// GetClientMetrics extracts client metrics from the client
// named by the mac parameter.
func (c *Handle) GetClientMetrics(mac string) *ClientMetrics {
	path := fmt.Sprintf("@/metrics/clients/%s", mac)
	props, err := c.GetProps(path)
	if err != nil {
		return nil
	}
	return c.GetClientMetricsFromNode(props)
}

func getNic(nic *PropertyNode) NicInfo {
	n := NicInfo{}

	n.Name, _ = nic.GetChildString("name")
	n.Ring, _ = nic.GetChildString("ring")
	n.MacAddr, _ = nic.GetChildString("mac")
	n.Kind, _ = nic.GetChildString("kind")
	n.Pseudo, _ = nic.GetChildBool("pseudo")
	n.State, _ = nic.GetChildString("state")

	if n.Kind == "wireless" && !n.Pseudo {
		w := WifiInfo{}
		w.ConfigBand, _ = nic.GetChildString("cfg_band")
		w.ConfigChannel, _ = nic.GetChildInt("cfg_channel")
		w.ConfigWidth, _ = nic.GetChildString("cfg_width")
		w.ActiveMode, _ = nic.GetChildString("active_mode")
		w.ActiveBand, _ = nic.GetChildString("active_band")
		w.ActiveChannel, _ = nic.GetChildInt("active_channel")
		w.ActiveWidth, _ = nic.GetChildString("active_width")
		w.ValidBands, _ = nic.GetChildStringSlice("bands")
		w.ValidModes, _ = nic.GetChildStringSlice("modes")

		supported, _ := nic.GetChildIntSet("channels")
		w.ValidLoChannels = make([]int, 0)
		for _, c := range wifi.Channels[wifi.LoBand] {
			if supported[c] {
				w.ValidLoChannels = append(w.ValidLoChannels, c)
			}
		}
		w.ValidHiChannels = make([]int, 0)
		for _, c := range wifi.Channels[wifi.HiBand] {
			if supported[c] {
				w.ValidHiChannels = append(w.ValidHiChannels, c)
			}
		}
		// Older cfgtrees, as may exist for lagging appliances seen
		// by the cloud, don't have this information.
		// If there's really no wifi info here, set it to nil
		if cmp.Equal(w, WifiInfo{}, cmpopts.EquateEmpty()) {
			n.WifiInfo = nil
		} else {
			n.WifiInfo = &w
		}
	}

	return n
}

// Return a slice of either all NICs attached to the specified node,
// or all NICs in the cluster if the node parameter is empty.
func getNics(prop *PropertyNode, node string) ([]NicInfo, error) {
	nics := make([]NicInfo, 0)
	for name, info := range prop.Children {
		nodeNics := info.Children["nics"]

		if (node == "" || node == name) && nodeNics != nil {
			for _, nic := range nodeNics.Children {
				nics = append(nics, getNic(nic))
			}
		}
	}

	sort.Slice(nics,
		func(i, j int) bool { return nics[i].Name < nics[j].Name },
	)

	return nics, nil
}

// ValidChannel tests if the given channel could be valid for the
// Wifi Radio, regardless of Band or Channel.
func (w *WifiInfo) ValidChannel(channel int) bool {
	// 0: Automatic mode
	if channel == 0 {
		return true
	}
	for _, v := range w.ValidLoChannels {
		if channel == v {
			return true
		}
	}
	for _, v := range w.ValidHiChannels {
		if channel == v {
			return true
		}
	}
	return false
}

// GetNics returns a slice of mac addresses representing the configured NICs on
// all nodes.
func (c *Handle) GetNics() ([]NicInfo, error) {
	prop, err := c.GetProps("@/nodes")
	if err != nil {
		return nil, fmt.Errorf("property get @/nodes failed: %v", err)
	}

	return getNics(prop, "")
}

// GetNic returns a NicInfo representing the named nic for the named node.
func (c *Handle) GetNic(node, nic string) (*NicInfo, error) {
	path := fmt.Sprintf("@/nodes/%s/nics/%s", node, nic)
	prop, err := c.GetProps(path)
	if err != nil {
		return nil, fmt.Errorf("GetNic: property get %s failed: %v", path, err)
	}
	n := getNic(prop)
	return &n, nil
}

// Build a mac->ip map of all the NICs on the internal ring
func (c *Handle) getInternalAddrs() map[string]string {
	addrs := make(map[string]string)

	for mac, client := range c.GetChildren("@/clients") {
		var ring, addr string

		ring, _ = client.GetChildString("ring")
		addr, _ = client.GetChildString("ipv4")

		if ring == base_def.RING_INTERNAL {
			addrs[mac] = addr
		}
	}

	return addrs
}

// GetNodes returns a slice of all nodes
func (c *Handle) GetNodes() ([]NodeInfo, error) {
	var metrics ChildMap

	prop, err := c.GetProps("@/nodes")
	if err != nil {
		return nil, fmt.Errorf("property get @/nodes failed: %v", err)
	}
	if x, _ := c.GetProps("@/metrics/health/"); x != nil {
		metrics = x.Children
	}

	internal := c.getInternalAddrs()

	nodes := make([]NodeInfo, 0)
	for nodeName, node := range prop.Children {
		ni := NodeInfo{
			ID: nodeName,
		}
		ni.Platform, _ = node.GetChildString("platform")
		ni.Name, _ = node.GetChildString("name")
		ni.Nics, _ = getNics(prop, nodeName)

		if m, ok := metrics[nodeName]; ok {
			ni.Alive, _ = m.GetChildTime("alive")
			ni.BootTime, _ = m.GetChildTime("boot_time")
			ni.Role, _ = m.GetChildString("role")
			if ni.Role == "gateway" {
				a, _ := c.GetProp("@/network/wan/current/address")
				ni.Addr, _, _ = net.ParseCIDR(a)
			} else {
				for _, nic := range ni.Nics {
					if a, ok := internal[nic.MacAddr]; ok {
						ni.Addr = net.ParseIP(a)
						break
					}
				}
			}
		}

		nodes = append(nodes, ni)
	}

	sort.Slice(nodes,
		func(i, j int) bool {
			if nodes[i].Role == nodes[j].Role {
				return nodes[i].ID < nodes[j].ID
			}
			// gateway first, then satellites
			return nodes[i].Role == "gateway"
		},
	)

	return nodes, nil
}

// GetActiveBlocks builds a slice of all the IP addresses that were being
// actively blocked at the time of the call.
func (c *Handle) GetActiveBlocks() []string {
	list := make([]string, 0)

	for name, node := range c.GetChildren("@/firewall/blocked") {
		if !node.Expired() {
			list = append(list, name)
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
