/*
 * COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

/*
 * Appliance configuration server.
 *
 *
 * Property namespace.
 *
 * All configuration is accessible via a unified namespace, which is
 * filesystem-like.
 *
 * /customer/customer_id/site/site_id/appliance/appliance_id/ is the
 * full path to a per-appliance configuration space.  A shorthand for
 * each of these is defined:
 *
 * @@@/ is equivalent to /customer/customer_id for this appliance's
 * customer.
 * @@/ is equivalent to /customer/customer_id/site/site_id for this
 * appliance's site.
 * @/ is equivalent to
 * /customer/customer_id/site/site_id/appliance/appliance_id/ for this
 *  appliance.
 *
 */

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"bg/ap_common/apcfg"
	"bg/ap_common/aputil"
	"bg/ap_common/broker"
	"bg/ap_common/mcp"
	"bg/ap_common/network"
	"bg/base_def"
	"bg/base_msg"

	"github.com/golang/protobuf/proto"
	zmq "github.com/pebbe/zmq4"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const pname = "ap.configd"

var metrics struct {
	getCounts prometheus.Counter
	setCounts prometheus.Counter
	delCounts prometheus.Counter
	expCounts prometheus.Counter
	treeSize  prometheus.Gauge
}

// Allow for significant variation in the processing of subtrees
type subtreeOps struct {
	get    func(string) (string, error)
	set    func(string, *string, *time.Time, bool) error
	delete func(string) error
}

type subtreeMatch struct {
	match *regexp.Regexp
	ops   *subtreeOps
}

var defaultSubtreeOps = subtreeOps{getPropHandler, setPropHandler, delPropHandler}
var devSubtreeOps = subtreeOps{getDevHandler, setDevHandler, delDevHandler}

var subtreeMatchTable = []subtreeMatch{
	{regexp.MustCompile(`^@/devices`), &devSubtreeOps},
}

// Allow for specific properties to have their own handlers as well
type propertyOps struct {
	get    func(*pnode) (string, error)
	set    func(*pnode, string, *time.Time) (bool, error)
	expire func(*pnode)
}

type propertyMatch struct {
	match *regexp.Regexp
	ops   *propertyOps
}

var defaultPropOps = propertyOps{defaultGetter, defaultSetter, defaultExpire}
var ssidPropOps = propertyOps{defaultGetter, ssidUpdate, defaultExpire}
var passphrasePropOps = propertyOps{defaultGetter, passphraseUpdate, defaultExpire}
var authPropOps = propertyOps{defaultGetter, authUpdate, defaultExpire}
var uuidPropOps = propertyOps{defaultGetter, uuidUpdate, defaultExpire}
var ringPropOps = propertyOps{defaultGetter, defaultSetter, ringExpire}
var ipv4PropOps = propertyOps{defaultGetter, ipv4Setter, defaultExpire}
var dnsPropOps = propertyOps{defaultGetter, dnsSetter, defaultExpire}
var cnamePropOps = propertyOps{defaultGetter, cnameSetter, defaultExpire}

var propertyMatchTable = []propertyMatch{
	{regexp.MustCompile(`^@/uuid$`), &uuidPropOps},
	{regexp.MustCompile(`^@/network/ssid$`), &ssidPropOps},
	{regexp.MustCompile(`^@/network/passphrase$`), &passphrasePropOps},
	{regexp.MustCompile(`^@/rings/.*/auth$`), &authPropOps},
	{regexp.MustCompile(`^@/clients/.*/ring$`), &ringPropOps},
	{regexp.MustCompile(`^@/clients/.*/dns_name$`), &dnsPropOps},
	{regexp.MustCompile(`^@/clients/.*/dhcp_name$`), &dnsPropOps},
	{regexp.MustCompile(`^@/clients/.*/ipv4$`), &ipv4PropOps},
	{regexp.MustCompile(`^@/dns/cnames/`), &cnamePropOps},
}

/*
 * All properties are currently represented as strings, but will presumably have
 * more varied types in the future.  Expires contains the time at which a
 * property will expire.  A property with a nil Expires field has no expiraton
 * date.
 */
type pnode struct {
	Value    string            `json:"Value,omitempty"`
	Modified *time.Time        `json:"Modified,omitempty"`
	Expires  *time.Time        `json:"Expires,omitempty"`
	Children map[string]*pnode `json:"Children,omitempty"`
	parent   *pnode
	name     string
	path     string
	ops      *propertyOps

	// Used and maintained by the heap interface methods
	index int
}

var (
	propTreeRoot = pnode{name: "root"}
	verbose      = flag.Bool("v", false, "verbose log output")

	brokerd *broker.Broker

	authTypeToDefaultRing map[string]string
)

/*************************************************************************
 *
 * Broker notifications
 */
func propNotify(prop, val string, expires *time.Time,
	action base_msg.EventConfig_Type) {

	entity := &base_msg.EventConfig{
		Timestamp: aputil.NowToProtobuf(),
		Sender:    proto.String(pname),
		Type:      &action,
		Property:  proto.String(prop),
		NewValue:  proto.String(val),
		Expires:   aputil.TimeToProtobuf(expires),
	}

	err := brokerd.Publish(entity, base_def.TOPIC_CONFIG)
	if err != nil {
		log.Printf("Failed to propagate config update: %v", err)
	}
}

func updateNotify(prop, val string, expires *time.Time) {
	propNotify(prop, val, expires, base_msg.EventConfig_CHANGE)
}

func deleteNotify(prop string) {
	propNotify(prop, "-", nil, base_msg.EventConfig_DELETE)
}

func expirationNotify(prop, val string) {
	propNotify(prop, val, nil, base_msg.EventConfig_EXPIRE)
}

func entityHandler(event []byte) {
	entity := &base_msg.EventNetEntity{}
	proto.Unmarshal(event, entity)

	if entity.MacAddress == nil {
		log.Printf("Received a NET.ENTITY event with no MAC: %v\n",
			entity)
		return
	}
	hwaddr := network.Uint64ToHWAddr(*entity.MacAddress)
	path := "@/clients/" + hwaddr.String() + "/"
	node, err := propertyInsert(path)
	if node == nil {
		log.Printf("Unable to create client record for %s: %v\n",
			hwaddr.String(), err)
		return
	}
	ring := selectRing(node, entity.Authtype)

	updates := make(map[string]*string)
	updates[path+"ring"] = &ring
	updates[path+"connection/node"] = entity.Node
	updates[path+"connection/mode"] = entity.Mode
	updates[path+"connection/authtype"] = entity.Authtype
	// Ipv4Address is an 'optional' field in the protobuf, but we will
	// also allow a value of 0.0.0.0 to indicate that the field should be
	// ignored.
	if entity.Ipv4Address != nil && *entity.Ipv4Address != 0 {
		ipv4 := network.Uint32ToIPAddr(*entity.Ipv4Address).String()
		updates[path+"ipv4_observed"] = &ipv4
	}

	updated := make(map[string]*string)
	for p, v := range updates {
		if v != nil && *v != "" {
			if node, _ := propertyInsert(p); node != nil {
				if u, _ := propertyUpdate(node, *v, nil); u {
					updated[p] = v
				}
			}
		}
	}

	if len(updated) > 0 {
		for p, v := range updated {
			updateNotify(p, *v, nil)
		}
		propTreeStore()
	}
}

/****************************************************************************
 *
 * Logic for determining the proper ring on which to place a newly discovered
 * device.
 */
func defaultRingInit() {
	authTypeToDefaultRing = make(map[string]string)

	if node := propertySearch("@/network/default_ring"); node != nil {
		for a, n := range node.Children {
			authTypeToDefaultRing[a] = n.Value
			log.Printf("default %s ring: %s\n", a, n.Value)
		}
	}
}

func selectRing(client *pnode, authp *string) string {
	var ring, auth string
	var ok bool

	if n, ok := client.Children["ring"]; ok {
		// If the client already has a ring set, don't override it
		return n.Value
	}

	if authp != nil {
		auth = *authp
	} else if n, ok := client.Children["authtype"]; ok {
		auth = n.Value
	}

	if auth == "" {
		log.Printf("Can't select ring for %s: no auth type\n",
			client.name)
	} else if ring, ok = authTypeToDefaultRing[auth]; !ok {
		log.Printf("Can't select ring for %s: unsupported auth: %s\n",
			client.name, auth)
	} else {
		log.Printf("Setting initial ring for %s to %s\n",
			client.name, ring)
	}
	return ring
}

/*************************************************************************
 *
 * Generic and property-specific setter/getter routines
 */
func defaultSetter(node *pnode, val string, expires *time.Time) (bool, error) {
	updated := false

	if node.Value != val {
		node.Value = val
		updated = true
	}

	if node.Expires != nil || expires != nil {
		node.Expires = expires
		updated = true
	}
	return updated, nil
}

func defaultGetter(node *pnode) (string, error) {
	var rval string

	b, err := json.Marshal(node)
	if err == nil {
		rval = string(b)
	}

	return rval, err
}

func defaultExpire(node *pnode) {
	expirationNotify(node.path, node.Value)
	expired = append(expired, node.path)

	node.Value = ""
}

func uuidUpdate(node *pnode, uuid string, expires *time.Time) (bool, error) {
	const nullUUID = "00000000-0000-0000-0000-000000000000"

	if node.Value != nullUUID {
		return false, fmt.Errorf("Cannot change an appliance's UUID")
	}
	node.Value = uuid
	return true, nil
}

func authValidate(auth string) error {
	if auth != "wpa-psk" && auth != "wpa-eap" {
		return fmt.Errorf("Only wpa-psk and wpa-eap are supported")
	}

	return nil
}

func authUpdate(node *pnode, auth string, expires *time.Time) (bool, error) {
	auth = strings.ToLower(auth)
	err := authValidate(auth)
	if err == nil && node.Value != auth {
		node.Value = auth
		return true, nil
	}
	return false, err
}

func ssidValidate(ssid string) error {
	if len(ssid) == 0 || len(ssid) > 32 {
		return fmt.Errorf("SSID must be between 1 and 32 characters")
	}

	for _, c := range ssid {
		// XXX: this is overly strict, but safe.  We'll need to support
		// a broader range eventually.
		if c > unicode.MaxASCII || !unicode.IsPrint(c) {
			return fmt.Errorf("Invalid characters in SSID name")
		}
	}

	return nil
}

func ssidUpdate(node *pnode, ssid string, expires *time.Time) (bool, error) {
	err := ssidValidate(ssid)
	if err == nil && node.Value != ssid {
		node.Value = ssid
		return true, nil
	}
	return false, err
}

func passphraseUpdate(node *pnode, pass string, expires *time.Time) (bool, error) {
	var err error

	changed := false
	if len(pass) == 64 {
		re := regexp.MustCompile(`^[a-fA-F0-9]+$`)
		if !re.Match([]byte(pass)) {
			err = fmt.Errorf("64-character passphrases must be" +
				" hex strings")
		}
	} else if len(pass) < 8 || len(pass) > 63 {
		err = fmt.Errorf("passphrase must be between 8 and 63 characters")
	} else {
		for _, c := range pass {
			if c > unicode.MaxASCII || !unicode.IsPrint(c) {
				err = fmt.Errorf("Invalid characters in passphrase")
				break
			}
		}
	}

	if err == nil && node.Value != pass {
		node.Value = pass
		changed = true
	}

	return changed, err
}

//
// Check to see whether the given hostname is already inuse as either a device's
// dns_name or as the left hand side of a cname.  We can optionally indicate a
// device to ignore, allowing us to answer the question "is any other device
// using this hostname?"
//
func dnsNameInuse(ignore *pnode, hostname string) bool {
	lower := strings.ToLower(hostname)

	clients := propertySearch("@/clients")
	for _, device := range clients.Children {
		if device == ignore {
			continue
		}
		if prop, ok := device.Children["dns_name"]; ok {
			if strings.ToLower(prop.Value) == lower {
				return true
			}
		}
		if prop, ok := device.Children["dhcp_name"]; ok {
			if strings.ToLower(prop.Value) == lower {
				return true
			}
		}

	}

	if cnames := propertySearch("@/dns/cnames"); cnames != nil {
		for name, record := range cnames.Children {
			if record == ignore {
				continue
			}
			if strings.ToLower(name) == lower {
				return true
			}
		}
	}

	return false
}

// Validate and record the hostname that will be used to generate DNS A records
// for this device
func dnsSetter(node *pnode, hostname string, expires *time.Time) (bool, error) {
	if !network.ValidDNSName(hostname) {
		return false, fmt.Errorf("invalid hostname: %s", hostname)
	}

	if dnsNameInuse(node.parent, hostname) {
		return false, fmt.Errorf("duplicate hostname")
	}

	return defaultSetter(node, hostname, expires)
}

// Validate and record both the hostname and the canonical name that will be
// used to generate DNS CNAME records
func cnameSetter(node *pnode, hostname string, expires *time.Time) (bool, error) {
	if !network.ValidHostname(node.name) {
		return false, fmt.Errorf("invalid hostname: %s", node.name)
	}

	if !network.ValidHostname(hostname) {
		return false, fmt.Errorf("invalid canonical name: %s", hostname)
	}

	if dnsNameInuse(node, node.name) {
		return false, fmt.Errorf("duplicate hostname")
	}

	return defaultSetter(node, hostname, expires)
}

// Validate and record an ipv4 assignment for this device
func ipv4Setter(node *pnode, addr string, expires *time.Time) (bool, error) {
	ipv4 := net.ParseIP(addr)
	if ipv4 == nil {
		return false, fmt.Errorf("invalid address: %s", addr)
	}

	// Make sure the address isn't already assigned
	clients := propertySearch("@/clients")
	for _, device := range clients.Children {
		if device == node.parent {
			// Reassigning the device's address to itself is fine
			continue
		}

		if ipv4Node, ok := device.Children["ipv4"]; ok {
			if ipv4.Equal(net.ParseIP(ipv4Node.Value)) {
				return false, fmt.Errorf("%s in use by %s",
					addr, device.name)
			}
		}
	}

	return defaultSetter(node, addr, expires)
}

//
// When a client's ring assignment expires, it returns to the Unenrolled ring
//
func ringExpire(node *pnode) {
	node.Value = base_def.RING_UNENROLLED
	updateNotify(node.path, node.Value, nil)
}

/*
 * To determine whether this new property has non-default operations, we walk
 * through the property_match_table, looking for any matching patterns
 */
func propAttachOps(node *pnode) {
	for _, r := range propertyMatchTable {
		if r.match.MatchString(node.path) {
			node.ops = r.ops
			return
		}
	}
	node.ops = &defaultPropOps
}

/*************************************************************************
 *
 * Functions to walk and maintain the property tree
 */

/*
 * Updated the modified timestamp for a node and its ancestors
 */
func markUpdated(node *pnode) {
	now := time.Now()

	for node != nil {
		// We want each node in the chain to have the same time, but it
		// can't be a pointer to the same time.
		copy := now
		node.Modified = &copy
		node = node.parent
	}
}

func propertyParse(prop string) []string {
	prop = strings.TrimSuffix(prop, "/")
	if prop == "@" {
		return make([]string, 0)
	}

	/*
	 * Only accept properties that start with exactly one '@', meaning they
	 * are local to this device
	 */
	if len(prop) < 2 || prop[0] != '@' || prop[1] != '/' {
		return nil
	}

	return strings.Split(prop[2:], "/")
}

/*
 * Insert an empty property into the tree, returning the leaf node.  If the
 * property already exists, the tree is left unchanged.  If the node exists, but
 * is not a leaf, return an error.
 */
func propertyInsert(prop string) (*pnode, error) {
	var err error

	components := propertyParse(prop)
	if components == nil || len(components) < 1 {
		return nil, fmt.Errorf("invalid property path: %s", prop)
	}

	node := &propTreeRoot
	path := "@"
	for _, name := range components {
		if node.Children == nil {
			node.Children = make(map[string]*pnode)
		}
		path += "/" + name
		next := node.Children[name]
		if next == nil {
			next = &pnode{
				name:   name,
				parent: node,
				path:   path,
				index:  -1}

			propAttachOps(next)
			node.Children[name] = next
		}
		node = next
	}

	if node != nil && len(node.Children) > 0 {
		err = fmt.Errorf("inserting an internal node: %s", prop)
	}

	return node, err
}

/*
 * Walk the tree looking for the given property.
 */
func propertySearch(prop string) *pnode {
	components := propertyParse(prop)
	if components == nil {
		return nil
	}

	node := &propTreeRoot
	ok := false
	for _, name := range components {
		if node, ok = node.Children[name]; !ok {
			break
		}
	}

	// If the caller explicitly asked for an internal node and we found a
	// leaf, don't operate on it.
	if node != nil && len(node.Children) == 0 && strings.HasSuffix(prop, "/") {
		node = nil
	}

	return node
}

func deleteSubtree(node *pnode) {
	if node.Expires != nil {
		expirationRemove(node)
	}
	for _, n := range node.Children {
		deleteSubtree(n)
	}
}

func propertyDelete(property string) error {
	if *verbose {
		log.Printf("delete property: %s\n", property)
	}
	node := propertySearch(property)
	if node == nil {
		return fmt.Errorf("deleting a nonexistent property: %s",
			property)
	}

	if parent := node.parent; parent != nil {
		delete(parent.Children, node.name)
		markUpdated(parent)
	}
	deleteSubtree(node)

	return nil
}

func propertyUpdate(node *pnode, value string, exp *time.Time) (bool, error) {
	var err error
	var updated bool
	var oldExpiration *time.Time

	if *verbose {
		log.Printf("set property %s -> %s\n", node.path, value)
	}
	if len(node.Children) > 0 {
		err = fmt.Errorf("can only modify leaf properties")
	} else {
		oldExpiration = node.Expires
		updated, err = node.ops.set(node, value, exp)
	}

	if err != nil {
		log.Printf("property update failed: %v\n", err)
	} else {
		markUpdated(node)
		if oldExpiration != nil || exp != nil {
			expirationUpdate(node)
		}
	}

	return updated, err
}

func propertyGet(property string) (string, error) {
	var err error
	var rval string

	if node := propertySearch(property); node != nil {
		b, err := json.Marshal(node)
		if err == nil {
			rval = string(b)
		}
	} else {
		err = apcfg.ErrNoProp
	}

	if err != nil && *verbose {
		log.Printf("property get for %s failed: %v\n", property, err)
	}

	return rval, err
}

/*************************************************************************
 *
 * Handling incoming requests from other daemons
 */
func getPropHandler(prop string) (string, error) {
	return propertyGet(prop)
}

func setPropHandler(prop string, val *string, exp *time.Time, add bool) error {
	var node *pnode
	var err error

	if val == nil {
		err = fmt.Errorf("no value supplied")
	} else if add {
		if node, err = propertyInsert(prop); err != nil {
			node = nil
		}
	} else {
		if node = propertySearch(prop); node == nil {
			err = fmt.Errorf("no such property: %s", prop)
		}
	}
	if node != nil {
		updated, rerr := propertyUpdate(node, *val, exp)
		if updated {
			propTreeStore()
			updateNotify(prop, *val, exp)
		} else {
			err = rerr
		}
	}
	return err

}

func delPropHandler(prop string) error {
	err := propertyDelete(prop)
	if err == nil {
		propTreeStore()
		deleteNotify(prop)
	}
	return err
}

func executePropOps(ops []*base_msg.ConfigQuery_ConfigOp) (string, error) {
	var rval string
	var err error

	for _, op := range ops {
		prop := *op.Property
		val := op.Value
		expires := aputil.ProtobufToTime(op.Expires)

		opsVector := &defaultSubtreeOps
		for _, r := range subtreeMatchTable {
			if r.match.MatchString(prop) {
				opsVector = r.ops
				break
			}
		}

		switch *op.Operation {
		case base_msg.ConfigQuery_ConfigOp_GET:
			metrics.getCounts.Inc()
			if len(ops) > 1 {
				err = fmt.Errorf("only single-GET " +
					"operations are supported")
			} else {
				rval, err = opsVector.get(prop)
			}

		case base_msg.ConfigQuery_ConfigOp_CREATE:
			metrics.setCounts.Inc()
			err = opsVector.set(prop, val, expires, true)

		case base_msg.ConfigQuery_ConfigOp_SET:
			metrics.setCounts.Inc()
			err = opsVector.set(prop, val, expires, false)

		case base_msg.ConfigQuery_ConfigOp_DELETE:
			metrics.delCounts.Inc()
			err = opsVector.delete(prop)

		case base_msg.ConfigQuery_ConfigOp_PING:
		// no-op

		default:
			err = apcfg.ErrBadOp
		}

		if err != nil {
			break
		}
	}

	return rval, err
}

func processOneEvent(query *base_msg.ConfigQuery) *base_msg.ConfigResponse {
	var err error
	var rval string

	if *query.Version.Major != apcfg.Version {
		err = apcfg.ErrBadVer
	} else {
		rval, err = executePropOps(query.Ops)
	}

	rc := base_msg.ConfigResponse_OK
	if err != nil {
		switch err {
		case apcfg.ErrNoProp:
			rc = base_msg.ConfigResponse_NOPROP
		case apcfg.ErrBadOp:
			rc = base_msg.ConfigResponse_UNSUPPORTED
		case apcfg.ErrBadVer:
			rc = base_msg.ConfigResponse_BADVERSION
		default:
			rc = base_msg.ConfigResponse_FAILED
		}

		if *verbose {
			log.Printf("Config operation failed: %v\n", err)
		}
		rval = fmt.Sprintf("%v", err)
	}
	version := base_msg.Version{Major: proto.Int32(int32(apcfg.Version))}
	response := &base_msg.ConfigResponse{
		Timestamp: aputil.NowToProtobuf(),
		Sender:    proto.String(pname + "(" + strconv.Itoa(os.Getpid()) + ")"),
		Version:   &version,
		Debug:     proto.String("-"),
		Response:  &rc,
		Value:     proto.String(rval),
	}

	return response
}

func eventLoop(incoming *zmq.Socket) {
	errs := 0
	for {
		msg, err := incoming.RecvMessageBytes(0)
		if err != nil {
			log.Printf("Error receiving message: %v\n", err)
			errs++
			if errs > 10 {
				log.Fatalf("Too many errors - giving up\n")
			}
			continue
		}

		errs = 0
		expirationPurge()
		query := &base_msg.ConfigQuery{}
		proto.Unmarshal(msg[0], query)

		response := processOneEvent(query)
		data, err := proto.Marshal(response)
		if err != nil {
			log.Printf("Failed to marshall response to %v: %v\n",
				*query, err)
		} else {
			incoming.SendBytes(data, 0)
		}
	}
}

func prometheusInit() {
	metrics.getCounts = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "configd_gets",
		Help: "get operations",
	})
	metrics.setCounts = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "configd_sets",
		Help: "set operations",
	})
	metrics.delCounts = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "configd_deletes",
		Help: "delete operations",
	})
	metrics.expCounts = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "configd_expires",
		Help: "property expirations",
	})
	metrics.treeSize = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "configd_tree_size",
		Help: "size of config tree",
	})

	prometheus.MustRegister(metrics.getCounts)
	prometheus.MustRegister(metrics.setCounts)
	prometheus.MustRegister(metrics.delCounts)
	prometheus.MustRegister(metrics.expCounts)

	prometheus.MustRegister(metrics.treeSize)
	http.Handle("/metrics", promhttp.Handler())
	go http.ListenAndServe(base_def.CONFIGD_PROMETHEUS_PORT, nil)
}

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	flag.Parse()

	mcpd, err := mcp.New(pname)
	if err != nil {
		log.Printf("Failed to connect to mcp\n")
	}

	if !strings.HasSuffix(*propdir, "/") {
		*propdir = *propdir + "/"
	}
	*propdir = aputil.ExpandDirPath(*propdir)
	if !aputil.FileExists(*propdir) {
		log.Printf("Properties directory %s doesn't exist", *propdir)
		mcpd.SetState(mcp.BROKEN)
		os.Exit(1)
	}

	prometheusInit()
	expirationInit()

	brokerd = broker.New(pname)
	brokerd.Handle(base_def.TOPIC_ENTITY, entityHandler)
	defer brokerd.Fini()

	if err = deviceDBInit(); err != nil {
		log.Printf("Failed to import devices database: %v\n", err)
		mcpd.SetState(mcp.BROKEN)
		os.Exit(1)
	}

	propTreeInit()
	defaultRingInit()

	incoming, _ := zmq.NewSocket(zmq.REP)
	port := base_def.INCOMING_ZMQ_URL + base_def.CONFIGD_ZMQ_REP_PORT
	if err = incoming.Bind(port); err != nil {
		log.Printf("Failed to bind incoming port %s: %v\n", port, err)
		mcpd.SetState(mcp.BROKEN)
		os.Exit(1)
	}
	log.Printf("Listening on %s\n", port)

	mcpd.SetState(mcp.ONLINE)
	eventLoop(incoming)
}
