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
	"time"
	"unicode"

	"ap_common"
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
)

type property_ops struct {
	get    func(*pnode) (string, error)
	set    func(*pnode, string, *time.Time) error
	expire func(*pnode) error
}

type property_match struct {
	match *regexp.Regexp
	ops   *property_ops
}

var ssid_ops = property_ops{default_getter, ssid_update, default_expirer}
var uuid_ops = property_ops{default_getter, uuid_update, default_expirer}

var property_match_table = []property_match{
	{regexp.MustCompile(`^@/uuid$`), &uuid_ops},
	{regexp.MustCompile(`^@/network/wlan[0-9]+/ssid$`), &ssid_ops},
}

// All properties are currently represented as strings, but will presumably have
// more varied types in the future.  Expires contains the time at which a
// property will expire.  A property with a nil Expires field has no expiraton
// date.
type pnode struct {
	Name     string
	Value    string     `json:"Value,omitempty"`
	Expires  *time.Time `json:"Expires,omitempty"`
	Children []*pnode   `json:"Children,omitempty"`
	parent   *pnode
	ops      *property_ops
}

var (
	property_root = pnode{Name: "root"}
	addr          = flag.String("listen-address",
		base_def.CONFIGD_PROMETHEUS_PORT,
		"The address to listen on for HTTP requests.")
	broker  ap_common.Broker
	propdir = flag.String("propdir", "./",
		"directory in which the property files should be stored")
)

// Broadcast a notification of a property change
func update_notify(prop, val string) {
	t := time.Now()
	entity := &base_msg.EventConfig{
		Timestamp: &base_msg.Timestamp{
			Seconds: proto.Int64(t.Unix()),
			Nanos:   proto.Int32(int32(t.Nanosecond())),
		},
		Sender:   proto.String(fmt.Sprintf("ap.configd(%d)", os.Getpid())),
		Property: proto.String(prop),
		NewValue: proto.String(val),
	}

	data, err := proto.Marshal(entity)
	err = broker.Publish(base_def.TOPIC_CONFIG, data)
	if err != nil {
		log.Printf("Failed to propagate config update: %v", err)
	}
}

func default_setter(node *pnode, val string, expires *time.Time) error {
	node.Value = val
	node.Expires = expires
	return nil
}

func default_getter(node *pnode) (string, error) {
	return node.Value, nil
}

func default_expirer(node *pnode) error {
	return nil
}

func uuid_update(node *pnode, uuid string, expires *time.Time) error {
	const null_uuid = "00000000-0000-0000-0000-000000000000"

	if node.Value != null_uuid {
		return fmt.Errorf("Cannot change an appliance's UUID")
	}
	node.Value = uuid
	return nil
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

func ssid_update(node *pnode, ssid string, expires *time.Time) error {
	err := ssid_validate(ssid)
	if err == nil {
		node.Value = ssid
	}
	return err
}

// To determine whether this new property has non-default operations, we walk
// through the property_match_table, looking for any matching patterns
func property_attach_ops(node *pnode, path string) {
	for _, r := range property_match_table {
		if r.match.MatchString(path) {
			node.ops = r.ops
			return
		}
	}
}

// Allocate a new property node and insert it into the property tree
func property_add(parent *pnode, name string, path string) *pnode {
	n := pnode{Name: name, parent: parent}
	parent.Children = append(parent.Children, &n)
	property_attach_ops(&n, path)
	return &n
}

// Break the property path into its individual components, and use them to
// navigate the property tree.  Optionally, it will also insert any missing nodes
// into the tree to instantiate a new property node.
func cfg_property_parse(prop string, insert bool) *pnode {
	// Only accept properties that start with exactly one '@', meaning they
	// are local to this device
	if len(prop) < 2 || prop[0] != '@' || prop[1] != '/' {
		return nil
	}

	// Walk the tree until we run out of path elements or fall off the
	// bottom of the tree.  If we exhaust the path, we return the current
	// search node, which may be either internal or a leaf.  Its up to the
	// caller to determine which of those is considered a successful search.
	components := strings.Split(prop[2:], "/")
	q := len(components)
	node := &property_root
	path := "@"
	for i := 0; i < q && node != nil; i++ {
		var next *pnode

		name := components[i]
		path += "/" + name
		for _, n := range node.Children {
			if name == n.Name {
				next = n
				break
			}
		}
		if next == nil && insert {
			next = property_add(node, name, path)
		}
		node = next
	}

	return node
}

func property_delete(property string) error {
	log.Println("delete property " + property)
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
	delete_subtree(node)
	prop_tree_store()

	return nil
}

func property_update(property, value string, expires *time.Time, insert bool) error {
	var err error

	log.Println("set property " + property + " -> " + value)
	node := cfg_property_parse(property, insert)
	if node == nil {
		if !insert {
			log.Fatal("Failed to insert a new propery node")
		}
		err = fmt.Errorf("Updating a nonexistent property: %s",
			property)
	} else if len(node.Children) > 0 {
		err = fmt.Errorf("Can only modify leaf properties")
	} else if node.ops == nil {
		node.Value = value
		node.Expires = expires
	} else {
		err = node.ops.set(node, value, expires)
	}
	if err == nil {
		prop_tree_store()
		update_notify(property, value)
	} else {
		log.Println("property update failed: ", err)
	}

	return err
}

func property_get(property string) (string, error) {
	var err error
	var rval string

	log.Println("get property: " + property)
	node := cfg_property_parse(property, false)
	if node == nil {
		err = fmt.Errorf("No such property")
	} else if node.ops != nil {
		rval, err = node.ops.get(node)
	} else if len(node.Children) != 0 {
		b, err := json.Marshal(node)
		if err == nil {
			rval = string(b)
		}
	} else {
		rval = node.Value
	}

	if err != nil {
		log.Println("property get failed: ", err)
	}

	return rval, err
}

func file_exists(filename string) bool {
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		return false
	}
	return true
}

func prop_tree_store() error {
	propfile := *propdir + property_filename
	backupfile := *propdir + backup_filename

	s, err := json.MarshalIndent(property_root, "", "    ")
	if err != nil {
		log.Fatal("Failed to construct properties JSON: %v\n", err)
	}

	if file_exists(propfile) {
		// XXX: could store multiple generations of backup files,
		// allowing for arbitrary rollback.  Could also take explicit
		// 'checkpoint' snapshots.
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

// After loading the initial property values, we need to walk the tree to set
// the parent pointers, attach any non-default operations, and possibly insert
// into the expiration heap
func patch_tree(node *pnode, path string) {
	property_attach_ops(node, path)
	for _, n := range node.Children {
		n.parent = node
		patch_tree(n, path+"/"+n.Name)
	}
}

func dump_tree(node *pnode, level int) {
	indent := ""
	for i := 0; i < level; i++ {
		indent += "  "
	}
	fmt.Printf("%s%s: %s\n", indent, node.Name, node.Value)
	for _, n := range node.Children {
		dump_tree(n, level+1)
	}
}
func delete_subtree(node *pnode) {
	fmt.Printf("Deleting %s\n", node.Name)
	for _, n := range node.Children {
		delete_subtree(n)
	}
}

func prop_tree_init() {
	propfile := *propdir + property_filename
	backupfile := *propdir + backup_filename
	default_file := *propdir + default_filename

	if file_exists(propfile) || file_exists(backupfile) {
		// Load primary properties file
		err := prop_tree_load(propfile)
		if err != nil {
			// Attempt to recover from backup
			err = prop_tree_load(backupfile)
			if err != nil {
				log.Fatal("Unable to load properties")
			}
			log.Printf("Loaded properties from backup file")
		}
	} else {
		err := prop_tree_load(default_file)
		if err != nil {
			log.Fatal("Unable to load default properties")
		}
		appliance_uuid := uuid.NewV4().String()
		property_update("@/uuid", appliance_uuid, nil, false)

		if err = prop_tree_store(); err != nil {
			log.Fatalf("Failed to create initial properties: %v",
				err)
		}
	}

	patch_tree(&property_root, "@")
}

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

	err := property_update(*q.Property, *q.Value, expires, add)
	if add {
		dump_tree(&property_root, 0)
	}
	return err
}

func delete_handler(q *base_msg.ConfigQuery) error {
	err := property_delete(*q.Property)
	dump_tree(&property_root, 0)
	return err
}

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	flag.Parse()
	if !strings.HasSuffix(*propdir, "/") {
		*propdir = *propdir + "/"
	}
	if !file_exists(*propdir) {
		log.Fatalf("Properties directory %s doesn't exist", *propdir)
	}

	// Prometheus setup
	http.Handle("/metrics", promhttp.Handler())
	go http.ListenAndServe(*addr, nil)
	log.Println("prometheus client launched")

	// zmq setup
	broker.Init("ap.configd")
	broker.Connect()
	defer broker.Disconnect()
	broker.Ping()

	prop_tree_init()

	log.Println("Set up listening socket.")
	incoming, _ := zmq.NewSocket(zmq.REP)
	incoming.Bind(base_def.CONFIGD_ZMQ_REP_URL)

	for {
		val := "-"
		msg, err := incoming.RecvMessageBytes(0)
		if err != nil {
			break // XXX Nope.
		}

		query := &base_msg.ConfigQuery{}
		proto.Unmarshal(msg[0], query)

		// XXX Query by property or by value?
		log.Println(query)

		rc := base_msg.ConfigResponse_OP_OK
		switch *query.Operation {
		case base_msg.ConfigQuery_GET:
			val, err = get_handler(query)
			if err != nil {
				rc = base_msg.ConfigResponse_GET_PROP_NOT_FOUND
			}
		case base_msg.ConfigQuery_CREATE:
			err = set_handler(query, true)
			if err != nil {
				rc = base_msg.ConfigResponse_SET_PROP_NO_PERM
			}
		case base_msg.ConfigQuery_SET:
			err = set_handler(query, false)
			if err != nil {
				rc = base_msg.ConfigResponse_SET_PROP_NOT_FOUND
			}
		case base_msg.ConfigQuery_DELETE:
			err = delete_handler(query)
			if err != nil {
				rc = base_msg.ConfigResponse_DELETE_PROP_NOT_FOUND
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
			Sender:   proto.String("ap.configd(" + strconv.Itoa(os.Getpid()) + ")"),
			Debug:    proto.String("-"),
			Response: &rc,
			Property: proto.String("-"),
			Value:    proto.String(val),
		}

		log.Println(response)
		data, err := proto.Marshal(response)

		incoming.SendBytes(data, 0)
	}
}
