/*
 * COPYRIGHT 2017 Brightgate Inc.  All rights reserved.
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
 * Each property within the namespace can be backed from a variety of
 * engines.
 *
 * config
 * decision
 * platform
 *
 * Each property within the namespace has a type.
 *
 * For example
 *
 * @/intent/uplink_mode
 *
 * is an enum with values ["GATEWAY", "BRIDGE"], backed by the config engine.
 *
 * @/

 *
 * XXX Handling list values.
 * XXX Handling groups.

 * kinds
 *   name
 *   group
 *   property
 *
 * @/network/wlan0/ssid
 *
 * is
 *
 * anchor(appliance)/group(network)/name(wlan0)/property(ssid)
 *
 * anchor(appliance) must lead to a mix of groups and properties.
 *
 * We had our fuller namespace, @@@/, representing the customer at
 * /customer/customer_id. In this case, we see a couple of new node types
 *
 * @@@/hosts
 *
 * anchor(customer)/summary(hosts)
 *
 * where summary() is a union of all the the hosts across the customer's sites.
 * XXX Do we understand how the cable provider appears in this schema?
 *
 * @@/host/6EB4F934-997D-4D39-88B2-674A49D05F14/
 *
 * anchor(site)/group(host)/name(6EB4F934-997D-4D39-88B2-674A49D05F14)/...
 *
 * If we envision the distributed configuration as a tree, with each
 * node potentially sourced from a different backing store (or layered
 * store), then we might have something like
 *
 * type ConfigNode struct {
 *	kind
 *	backing
 *	childkind
 * }
 */

package main

import (
	"container/heap"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"ap_common/broker"
	"ap_common/mcp"
	"ap_common/network"

	"base_def"
	"base_msg"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/golang/protobuf/proto"
	"github.com/satori/uuid"

	// Ubuntu: requires libzmq3-dev, which is 0MQ 4.2.1.
	zmq "github.com/pebbe/zmq4"
)

const (
	property_filename = "ap_props.json"
	backup_filename   = "ap_props.json.bak"
	default_filename  = "ap_defaults.json"
	pname             = "ap.configd"

	minConfigVersion = 3
	curConfigVersion = 3
)

type property_ops struct {
	get func(*pnode) (string, error)
	set func(*pnode, string, *time.Time) (bool, error)
}

type property_match struct {
	match *regexp.Regexp
	ops   *property_ops
}

var default_ops = property_ops{default_getter, default_setter}
var ssid_ops = property_ops{default_getter, ssid_update}
var uuid_ops = property_ops{default_getter, uuid_update}

var property_match_table = []property_match{
	{regexp.MustCompile(`^@/uuid$`), &uuid_ops},
	{regexp.MustCompile(`^@/network/ssid$`), &ssid_ops},
}

/*
 * All properties are currently represented as strings, but will presumably have
 * more varied types in the future.  Expires contains the time at which a
 * property will expire.  A property with a nil Expires field has no expiraton
 * date.
 */
type pnode struct {
	Name     string
	Value    string     `json:"Value,omitempty"`
	Modified *time.Time `json:"Modified,omitempty"`
	Expires  *time.Time `json:"Expires,omitempty"`
	Children []*pnode   `json:"Children,omitempty"`
	parent   *pnode
	path     string
	ops      *property_ops

	// Used and maintained by the heap interface methods
	index int
}

type pnode_queue []*pnode

var (
	property_root = pnode{Name: "root"}
	addr          = flag.String("listen-address",
		base_def.CONFIGD_PROMETHEUS_PORT,
		"The address to listen on for HTTP requests.")
	brokerd broker.Broker
	propdir = flag.String("propdir", "./",
		"directory in which the property files should be stored")

	ApVersion    string
	upgradeHooks []func() error

	exp_heap  pnode_queue
	exp_timer *time.Timer
	exp_mutex sync.Mutex
	expired   []string
)

/*******************************************************************
 *
 * Implement the functions required by the container/heap interface
 */
func (q pnode_queue) Len() int { return len(q) }

func (q pnode_queue) Less(i, j int) bool {
	return (q[i].Expires).Before(*q[j].Expires)
}

func (q pnode_queue) Swap(i, j int) {
	q[i], q[j] = q[j], q[i]
	q[i].index = i
	q[j].index = j
}

func (q *pnode_queue) Push(x interface{}) {
	n := len(*q)
	prop := x.(*pnode)
	prop.index = n
	*q = append(*q, prop)
}

func (q *pnode_queue) Pop() interface{} {
	old := *q
	n := len(old)
	prop := old[n-1]
	prop.index = -1 // for safety
	*q = old[0 : n-1]
	return prop
}

/*************************************************************************
 *
 * Functions to implement property expiration and maintain the associated
 * datastructures.
 */
func expiration_handler() {
	reset := time.Duration(time.Minute)
	for true {
		<-exp_timer.C
		exp_mutex.Lock()

		for len(exp_heap) > 0 {
			next := exp_heap[0]
			now := time.Now()

			if now.Before(*next.Expires) {
				break
			}

			delay := now.Sub(*next.Expires)
			if delay.Seconds() > 1.0 {
				log.Printf("Missed expiration for %s by %s\n",
					next.Name, delay)
			}
			log.Printf("Expiring: %s at %v\n", next.Name, time.Now())
			heap.Pop(&exp_heap)

			next.index = -1
			next.Expires = nil
			expired = append(expired, next.path)
			expirationNotify(next.path, next.Value)
		}

		if len(exp_heap) > 0 {
			next := exp_heap[0]
			reset = time.Until(*next.Expires)
		}
		exp_timer.Reset(reset)
		exp_mutex.Unlock()
	}
}

func nextExpiration() *pnode {
	if len(exp_heap) == 0 {
		return nil
	}

	return exp_heap[0]
}

/*
 * Update the expiration time of a single property (possibly setting an
 * expiration for the first time).  If this property either starts or ends at
 * the top of the expiration heap, reset the expiration timer accordingly.
 */
func expiration_update(node *pnode) {
	reset := false

	exp_mutex.Lock()

	if node == nextExpiration() {
		reset = true
	}

	if node.Expires == nil {
		// This node doesn't have an expiration.  If it's in the heap,
		// it's probably because we just made the setting permanent.
		// Pull it out of the heap.
		if node.index != -1 {
			heap.Remove(&exp_heap, node.index)
			node.index = -1
		}
	} else {
		if node.index == -1 {
			heap.Push(&exp_heap, node)
		}
		heap.Fix(&exp_heap, node.index)
	}

	if node == nextExpiration() {
		reset = true
	}

	if reset {
		if next := nextExpiration(); next != nil {
			exp_timer.Reset(time.Until(*next.Expires))
		}
	}
	exp_mutex.Unlock()
}

/*
 * Remove a single property from the expiration heap
 */
func expiration_remove(node *pnode) {
	exp_mutex.Lock()
	if node.index != -1 {
		heap.Remove(&exp_heap, node.index)
		node.index = -1
	}
	exp_mutex.Unlock()
}

/*
 * Walk the list of expired properties and remove them from the tree
 */
func expiration_purge() {
	count := 0
	for len(expired) > 0 {
		exp_mutex.Lock()
		copy := expired
		expired = nil
		exp_mutex.Unlock()

		for _, prop := range copy {
			count++
			property_delete(prop)
		}
	}
	if count > 0 {
		prop_tree_store()
	}
}

func expiration_init() {
	exp_heap = make(pnode_queue, 0)
	heap.Init(&exp_heap)

	exp_timer = time.NewTimer(time.Duration(time.Minute))
	go expiration_handler()
}

/*************************************************************************
 *
 * Broker notifications
 */
func prop_notify(prop, val string, action base_msg.EventConfig_Type) {
	t := time.Now()
	entity := &base_msg.EventConfig{
		Timestamp: &base_msg.Timestamp{
			Seconds: proto.Int64(t.Unix()),
			Nanos:   proto.Int32(int32(t.Nanosecond())),
		},
		Sender:   proto.String(brokerd.Name),
		Type:     &action,
		Property: proto.String(prop),
		NewValue: proto.String(val),
	}

	err := brokerd.Publish(entity, base_def.TOPIC_CONFIG)
	if err != nil {
		log.Printf("Failed to propagate config update: %v", err)
	}
}

func updateNotify(prop, val string) {
	prop_notify(prop, val, base_msg.EventConfig_CHANGE)
}

func deleteNotify(prop string) {
	prop_notify(prop, "-", base_msg.EventConfig_DELETE)
}

func expirationNotify(prop, val string) {
	prop_notify(prop, val, base_msg.EventConfig_EXPIRE)
}

func entity_handler(event []byte) {
	updated := false
	entity := &base_msg.EventNetEntity{}
	proto.Unmarshal(event, entity)

	if entity.MacAddress == nil {
		log.Printf("Received a NET.ENTITY event with no MAC: %v\n",
			entity)
		return
	}
	hwaddr := network.Uint64ToHWAddr(*entity.MacAddress)
	path := "@/clients/" + hwaddr.String()
	node := cfg_property_parse(path, true)

	/*
	 * Determine which client properties are already known
	 */
	fields := make(map[string]*pnode)
	for _, c := range node.Children {
		fields[c.Name] = c
	}

	var n *pnode
	var ok bool
	if entity.InterfaceName != nil {
		if n, ok = fields["iface"]; !ok {
			n = property_add(node, "iface")
		}
		if n.Value != *entity.InterfaceName {
			n.Value = *entity.InterfaceName
			updated = true
		}
	}

	if entity.Ipv4Address != nil {
		if n, ok = fields["ipv4_observed"]; !ok {
			n = property_add(node, "ipv4_observed")
		}
		ipv4 := network.Uint32ToIPAddr(*entity.Ipv4Address).String()
		if n.Value != ipv4 {
			n.Value = ipv4
			updated = true
		}
	}

	if n, ok = fields["ring"]; !ok {
		n = property_add(node, "ring")
		n.Value = base_def.RING_UNENROLLED
		updated = true
	}

	if updated {
		prop_tree_store()
	}
}

/*************************************************************************
 *
 * Generic and property-specific setter/getter routines
 */
func default_setter(node *pnode, val string, expires *time.Time) (bool, error) {
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

func default_getter(node *pnode) (string, error) {
	var rval string

	b, err := json.Marshal(node)
	if err == nil {
		rval = string(b)
	}

	return rval, err
}

func uuid_update(node *pnode, uuid string, expires *time.Time) (bool, error) {
	const null_uuid = "00000000-0000-0000-0000-000000000000"

	if node.Value != null_uuid {
		return false, fmt.Errorf("Cannot change an appliance's UUID")
	}
	node.Value = uuid
	return true, nil
}

func ssid_validate(ssid string) error {
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

func ssid_update(node *pnode, ssid string, expires *time.Time) (bool, error) {
	err := ssid_validate(ssid)
	if err == nil && node.Value != ssid {
		node.Value = ssid
		return true, nil
	}
	return false, err
}

/*
 * To determine whether this new property has non-default operations, we walk
 * through the property_match_table, looking for any matching patterns
 */
func property_attach_ops(node *pnode, path string) {
	for _, r := range property_match_table {
		if r.match.MatchString(path) {
			node.ops = r.ops
			return
		}
	}
	node.ops = &default_ops
}

/*************************************************************************
 *
 * Functions to walk and maintain the property tree
 */

/*
 * Updated the modified timestamp for a node and its ancestors
 */
func mark_updated(node *pnode) {
	now := time.Now()

	for node != nil {
		// We want each node in the chain to have the same time, but it
		// can't be a pointer to the same time.
		copy := now
		node.Modified = &copy
		node = node.parent
	}
}

/*
 * Allocate a new property node and insert it into the property tree
 */
func property_add(parent *pnode, property string) *pnode {
	path := parent.path + "/" + property

	n := pnode{Name: property,
		parent: parent,
		path:   path,
		index:  -1}

	parent.Children = append(parent.Children, &n)
	property_attach_ops(&n, path)
	return &n
}

/*
 * Break the property path into its individual components, and use them to
 * navigate the property tree.  Optionally, it will also insert any missing nodes
 * into the tree to instantiate a new property node.
 */
func cfg_property_parse(prop string, insert bool) *pnode {
	/*
	 * Only accept properties that start with exactly one '@', meaning they
	 * are local to this device
	 */
	if len(prop) < 2 || prop[0] != '@' || prop[1] != '/' {
		return nil
	}

	if prop == "@/" {
		return &property_root
	}

	/*
	 * Walk the tree until we run out of path elements or fall off the
	 * bottom of the tree.  If we exhaust the path, we return the current
	 * search node, which may be either internal or a leaf.  Its up to the
	 * caller to determine which of those is considered a successful search.
	 */
	components := strings.Split(prop[2:], "/")
	q := len(components)
	node := &property_root
	path := "@"
	for i := 0; i < q && node != nil; i++ {
		var next *pnode

		name := components[i]
		for _, n := range node.Children {
			if name == n.Name {
				next = n
				break
			}
		}
		if next == nil && insert {
			next = property_add(node, name)
		}
		path += "/" + name
		node = next
	}

	return node
}

func property_delete(property string) error {
	log.Printf("delete property: %s\n", property)
	node := cfg_property_parse(property, false)
	if node == nil {
		return fmt.Errorf("deleting a nonexistent property: %s",
			property)
	}

	siblings := node.parent.Children
	for i, n := range siblings {
		if n == node {
			node.parent.Children =
				append(siblings[:i], siblings[i+1:]...)
			break
		}
	}
	mark_updated(node)
	delete_subtree(node)
	return nil
}

func property_update(property, value string, expires *time.Time,
	insert bool) (bool, error) {
	var err error

	log.Printf("set property %s -> %s\n", property, value)
	updated := false

	node := cfg_property_parse(property, insert)
	if node == nil {
		if insert {
			err = fmt.Errorf("Failed to insert a new property")
		} else {
			err = fmt.Errorf("Updating a nonexistent property: %s",
				property)
		}
	} else if len(node.Children) > 0 {
		err = fmt.Errorf("Can only modify leaf properties")
	} else {
		updated, err = node.ops.set(node, value, expires)
	}

	if err != nil {
		log.Println("property update failed: ", err)
	} else {
		mark_updated(node)
		if node.Expires != nil {
			expiration_update(node)
		}
	}

	return updated, err
}

func property_get(property string) (string, error) {
	var err error
	var rval string

	node := cfg_property_parse(property, false)
	if node == nil {
		err = fmt.Errorf("No such property")
	} else {
		b, err := json.Marshal(node)
		if err == nil {
			rval = string(b)
		}
	}

	if err != nil {
		log.Printf("property get for %s failed: %v\n", property, err)
	}

	return rval, err
}

/*************************************************************************
 *
 * Reading and writing the persistent property file
 */
func file_exists(filename string) bool {
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		return false
	}
	return true
}

func prop_tree_store() error {
	propfile := *propdir + property_filename
	backupfile := *propdir + backup_filename

	node := cfg_property_parse("@/apversion", false)
	if node == nil {
		// This should have been handled by upgradeV1
		log.Printf("Warning: @/apversion property missing\n")
		node = cfg_property_parse("@/apversion", true)
	}
	node.Value = ApVersion

	s, err := json.MarshalIndent(property_root, "", "  ")
	if err != nil {
		log.Fatal("Failed to construct properties JSON: %v\n", err)
	}

	if file_exists(propfile) {
		/*
		 * XXX: could store multiple generations of backup files,
		 * allowing for arbitrary rollback.  Could also take explicit
		 * 'checkpoint' snapshots.
		 */
		os.Rename(propfile, backupfile)
	}

	err = ioutil.WriteFile(propfile, s, 0644)
	if err != nil {
		log.Printf("Failed to write properties file: %v\n", err)
	}

	return err
}

func prop_tree_load(name string) error {
	var file []byte
	var err error

	file, err = ioutil.ReadFile(name)
	if err != nil {
		log.Printf("Failed to load properties file %s: %v\n", name, err)
		return err
	}

	err = json.Unmarshal(file, &property_root)
	if err != nil {
		log.Printf("Failed to import properties from %s: %v\n",
			name, err)
		return err
	}

	return nil
}

func addUpgradeHook(version int, hook func() error) {
	if version > curConfigVersion {
		msg := fmt.Sprintf("Upgrade hook %d > current max of %d\n",
			version, curConfigVersion)
		panic(msg)
	}

	if upgradeHooks == nil {
		upgradeHooks = make([]func() error, curConfigVersion+1)
	}
	upgradeHooks[version] = hook
}

func versionTree() error {
	upgraded := false
	version := 0

	node := cfg_property_parse("@/cfgversion", true)
	if node.Value != "" {
		version, _ = strconv.Atoi(node.Value)
	}
	if version < minConfigVersion {
		return fmt.Errorf("Obsolete properties file.")
	}
	if version > curConfigVersion {
		log.Fatalf("Properties file is newer than the software\n")
	}

	for version < curConfigVersion {
		log.Printf("Upgrading properties from version %d to %d\n",
			version, version+1)
		version++
		if upgradeHooks[version] != nil {
			if err := upgradeHooks[version](); err != nil {
				return fmt.Errorf("upgrade failed: %v", err)
			}
		}
		node.Value = strconv.Itoa(version)
		upgraded = true
	}

	if upgraded {
		if err := prop_tree_store(); err != nil {
			return fmt.Errorf("Failed to write properties: %v", err)
		}
	}
	return nil
}

/*
 * After loading the initial property values, we need to walk the tree to set
 * the parent pointers, attach any non-default operations, and possibly insert
 * into the expiration heap
 */
func patchTree(node *pnode, path string) {
	property_attach_ops(node, path)
	for _, n := range node.Children {
		n.parent = node
		patchTree(n, path+"/"+n.Name)
	}
	node.path = path
	node.index = -1
	if node.Expires != nil {
		expiration_update(node)
	}
}

func dumpTree(node *pnode, level int) {
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
		dumpTree(n, level+1)
	}
}

func delete_subtree(node *pnode) {
	if node.Expires != nil {
		expiration_remove(node)
	}
	for _, n := range node.Children {
		delete_subtree(n)
	}
}

func prop_tree_init() {
	var err error

	propfile := *propdir + property_filename
	backupfile := *propdir + backup_filename
	default_file := *propdir + default_filename

	if file_exists(propfile) {
		err = prop_tree_load(propfile)
	} else {
		err = fmt.Errorf("File missing")
	}

	if err != nil {
		log.Printf("Unable to load properties: %v", err)
		if file_exists(backupfile) {
			err = prop_tree_load(backupfile)
		} else {
			err = fmt.Errorf("File missing")
		}

		if err != nil {
			log.Printf("Unable to load backup properties: %v", err)
		} else {
			log.Printf("Loaded properties from backup file")
		}
	}

	if err != nil {
		log.Printf("No usable properties files.  Loading defaults.\n")
		err := prop_tree_load(default_file)
		if err != nil {
			log.Fatal("Unable to load default properties")
		}
		patchTree(&property_root, "@")
		appliance_uuid := uuid.NewV4().String()
		property_update("@/uuid", appliance_uuid, nil, true)
	}

	if err == nil {
		patchTree(&property_root, "@")
		if err = versionTree(); err != nil {
			log.Printf("Failed version check: %v\n", err)
		}
	}

	dumpTree(&property_root, 0)
}

/*************************************************************************
 *
 * Handling incoming requests from other daemons
 */
func get_handler(q *base_msg.ConfigQuery) (string, error) {
	return (property_get(*q.Property))
}

func set_handler(q *base_msg.ConfigQuery, add bool) error {
	var expires *time.Time

	if q.Expires != nil {
		sec := *q.Expires.Seconds
		nano := int64(*q.Expires.Nanos)
		tmp := time.Unix(sec, nano)
		expires = &tmp
	}

	updated, err := property_update(*q.Property, *q.Value, expires, add)
	if updated {
		prop_tree_store()
		updateNotify(*q.Property, *q.Value)
	}
	return err
}

func delete_handler(q *base_msg.ConfigQuery) error {
	err := property_delete(*q.Property)
	if err == nil {
		prop_tree_store()
		deleteNotify(*q.Property)
	}
	return err
}

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	flag.Parse()
	if !strings.HasSuffix(*propdir, "/") {
		*propdir = *propdir + "/"
	}

	mcpd, err := mcp.New(pname)
	if err != nil {
		log.Printf("Failed to connect to mcp\n")
	}

	if !file_exists(*propdir) {
		log.Fatalf("Properties directory %s doesn't exist", *propdir)
	}

	expiration_init()

	// Prometheus setup
	http.Handle("/metrics", promhttp.Handler())
	go http.ListenAndServe(*addr, nil)

	// zmq setup
	brokerd.Init(pname)
	brokerd.Handle(base_def.TOPIC_ENTITY, entity_handler)
	brokerd.Connect()
	defer brokerd.Disconnect()
	brokerd.Ping()

	prop_tree_init()

	incoming, _ := zmq.NewSocket(zmq.REP)
	incoming.Bind(base_def.CONFIGD_ZMQ_REP_URL)

	if mcpd != nil {
		mcpd.SetState(mcp.ONLINE)
	}
	for {
		val := "-"
		msg, err := incoming.RecvMessageBytes(0)
		if err != nil {
			break // XXX Nope.
		}

		expiration_purge()
		query := &base_msg.ConfigQuery{}
		proto.Unmarshal(msg[0], query)

		rc := base_msg.ConfigResponse_OP_OK
		switch *query.Operation {
		case base_msg.ConfigQuery_GET:
			if val, err = get_handler(query); err != nil {
				rc = base_msg.ConfigResponse_PROP_NOT_FOUND
			}
		case base_msg.ConfigQuery_CREATE:
			if err = set_handler(query, true); err != nil {
				rc = base_msg.ConfigResponse_NO_PERM
			}
		case base_msg.ConfigQuery_SET:
			if err = set_handler(query, false); err != nil {
				rc = base_msg.ConfigResponse_PROP_NOT_FOUND
			}
		case base_msg.ConfigQuery_DELETE:
			if err = delete_handler(query); err != nil {
				rc = base_msg.ConfigResponse_PROP_NOT_FOUND
			}
		default:
			log.Printf("Unrecognized operation")
			rc = base_msg.ConfigResponse_UNSUPPORTED
		}

		t := time.Now()

		response := &base_msg.ConfigResponse{
			Timestamp: &base_msg.Timestamp{
				Seconds: proto.Int64(t.Unix()),
				Nanos:   proto.Int32(int32(t.Nanosecond())),
			},
			Sender:   proto.String(pname + "(" + strconv.Itoa(os.Getpid()) + ")"),
			Debug:    proto.String("-"),
			Response: &rc,
			Property: proto.String("-"),
			Value:    proto.String(val),
		}

		data, err := proto.Marshal(response)

		incoming.SendBytes(data, 0)
	}
}
