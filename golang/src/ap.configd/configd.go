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
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
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

	"database/sql"
	_ "github.com/mattn/go-sqlite3"
)

type prop_setter func(*property_node, string) error
type prop_getter func(*property_node) (string, error)

// All properties are currently represented as strings, but will presumably have
// more varied types in the future
type property_node struct {
	name     string
	value    string
	lifetime int

	parent   *property_node   // allows a child to learn about itself
	children []*property_node // internal nodes have one or more children
	set      prop_setter      // leaf properties may have setter
	get      prop_getter      //    and getter functions
}

var (
	db_hdl        *sql.DB = nil
	property_root         = property_node{"root", "", -1, nil, nil, nil, nil}
	addr                  = flag.String("listen-address",
		base_def.CONFIGD_PROMETHEUS_PORT,
		"The address to listen on for HTTP requests.")
	broker ap_common.Broker
)

func dump_tree(node *property_node, level int) {
	indent := ""
	for i := 0; i < level; i++ {
		indent += "  "
	}
	fmt.Printf("%s%s: %s\n", indent, node.name, node.value)
	for _, n := range node.children {
		dump_tree(n, level+1)
	}
}

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

func uuid_update(node *property_node, uuid string) error {
	if len(node.value) > 0 {
		return fmt.Errorf("Cannot change an appliance's UUID")
	}
	node.value = uuid
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

func ssid_update(node *property_node, ssid string) error {
	err := ssid_validate(ssid)
	if err == nil {
		log.Println("publish new ssid: ", ssid)
		node.value = ssid
	}
	return err
}

func ssid_get(node *property_node) (string, error) {
	return node.value, nil
}

func property_add(parent *property_node, name string, set prop_setter,
	get prop_getter) *property_node {

	n := property_node{name, "", -1, parent, nil, set, get}
	parent.children = append(parent.children, &n)
	return &n
}

func cfg_property_full_name(node *property_node) string {
	if node == &property_root {
		return "@"
	} else {
		return cfg_property_full_name(node.parent) + "/" + node.name
	}
}

func cfg_property_parse(prop string) *property_node {
	// Only accept properties that start with exactly one '@', meaning they
	// are local to this device
	if len(prop) < 2 || prop[0] != '@' || prop[1] != '/' {
		return nil
	}

	// Walk the tree until we run out of path elements or fall off the
	// bottom of the tree.  If we exhaust the path, we return the current
	// search node, which may be either internal or a leaf.  Its up to the
	// caller to determine which of those is considered a successful search.
	path := strings.Split(prop[2:], "/")
	q := len(path)
	node := &property_root
	for i := 0; i < q && node != nil; i++ {
		var next *property_node

		name := path[i]
		for _, n := range node.children {
			if name == n.name {
				next = n
				break
			}
		}
		node = next
	}

	return node
}

// properties may have lifetimes.  Currently ignoring that
func property_update(property, value string) error {
	var err error

	log.Println("set property " + property + " -> " + value)
	node := cfg_property_parse(property)
	if node == nil {
		err = fmt.Errorf("No such property")
	} else if len(node.children) > 0 {
		err = fmt.Errorf("Can only modify leaf properties")
	} else if node.set == nil {
		// If the property doesn't have a setter, we simply cache the
		// value in-core and store it in the database
		node.value = value
		property_db_store(node, value)
	} else {
		err = node.set(node, value)
	}
	if err == nil {
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
	node := cfg_property_parse(property)
	if node == nil {
		err = fmt.Errorf("No such property")
	} else if len(node.children) != 0 {
		// XXX Should eventually support returning a subtree rather
		// than just leaf properties
		err = fmt.Errorf("Can only read leaf properties")
	} else if node.get != nil {
		rval, err = node.get(node)
	} else {
		// If the property doesn't provide a getter, we will first look
		// for a value in-core.  Failing that, we look in the database
		rval = node.value
		if rval == "" {
			rval, err = property_db_lookup(node)
		}
	}

	if err != nil {
		log.Println("property get failed: ", err)
	}

	return rval, err
}

func property_db_store(node *property_node, value string) {
	/* SET is an INSERT or an UPDATE. */
	/* INSERT INTO properties (NULL, %s, %s, %s, %s) */
	/* UPDATE properties SET value = %, modify_dt = %, lifetime = % WHERE name = %
	 */
	property := cfg_property_full_name(node)
	log.Printf("update property %s\n", property)

	rows, err := db_hdl.Query("SELECT COUNT(*) FROM properties WHERE name = ?",
		property)
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	log.Printf("completed query\n")

	count := 0
	for rows.Next() {
		log.Printf("count(*), row = %d\n", count)

		var c int
		err := rows.Scan(&c)
		if err != nil {
			log.Fatal(err)
		}
		log.Println(c)
	}

	log.Println(count)
}

func property_db_lookup(node *property_node) (string, error) {
	rval := ""
	property := cfg_property_full_name(node)
	qs := fmt.Sprintf("select * from properties where name = '%s'",
		property)
	log.Printf("requesting (%s)\n", qs)
	rows, err := db_hdl.Query(qs)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	nrows := 0
	for rows.Next() {
		var id int
		var name string
		var value string
		var modify_dt time.Time
		var lifetime int
		err = rows.Scan(&id, &name, &value, &modify_dt, &lifetime)
		if err != nil {
			return "", err
		}
		fmt.Println(id, name, value, modify_dt, lifetime)
		rval = value
		nrows++
	}
	err = rows.Err()
	if err != nil {
		return "", nil
	}

	log.Printf("%d rows\n", nrows)
	if nrows == 0 {
		return "", fmt.Errorf("Invalid property")
	} else {
		return rval, nil
	}
}

func db_init() (*sql.DB, error) {
	log.Println("build minimal config database")

	db, err := sql.Open("sqlite3", "./config.db")
	if err != nil {
		return nil, err
	}

	qs := "SELECT name" +
		" FROM sqlite_master" +
		" WHERE type='table' AND name='properties';"
	log.Println("query: " + qs)
	rows, err := db.Query(qs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	found_properties := false
	for rows.Next() {
		var name string
		err = rows.Scan(&name)
		if err != nil {
			log.Fatal(err)
		}

		if name == "properties" {
			found_properties = true
		}
	}

	/* Does the table we want exist? */
	if !found_properties {
		sqlStmt := `
		create table properties (
			id integer not null primary key,
			name text,
			value text,
			modify_dt datetime,
			lifetime integer
		);
		`
		log.Println("need to execute " + sqlStmt)
		_, err = db.Exec(sqlStmt)
		if err != nil {
			log.Printf("%q: %s\n", err, sqlStmt)
			return nil, err
		}
	}
	return db, nil
}

// Build a minimal property tree for testing:
//    @/uuid = "random uuid"
//    @/network
//       @/network/wlan0
//          @/network/wlan0/ssid = "test<pid>"
func prop_tree_init() {
	property_add(&property_root, "uuid", uuid_update, nil)

	network := property_add(&property_root, "network", nil, nil)
	wlan0 := property_add(network, "wlan0", nil, nil)
	property_add(wlan0, "ssid", ssid_update, ssid_get)

	appliance_uuid := uuid.NewV4().String()
	initial_ssid := fmt.Sprintf("test%d", os.Getpid())

	property_update("@/uuid", appliance_uuid)
	property_update("@/network/wlan0/ssid", initial_ssid)

	dump_tree(&property_root, 0)
}

func main() {
	var err error

	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	log.Println("start")

	flag.Parse()

	log.Println("cli flags parsed")

	// XXX Ping!

	// Prometheus setup
	http.Handle("/metrics", promhttp.Handler())
	go http.ListenAndServe(*addr, nil)
	log.Println("prometheus client launched")

	// database setup
	db_hdl, err = db_init()
	if err != nil {
		log.Fatal(err)
	}
	defer db_hdl.Close()

	// zmq setup
	broker.Init("ap.configd")
	broker.Connect()
	defer broker.Disconnect()

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
		if *query.Operation == base_msg.ConfigQuery_GET {
			val, err = property_get(*query.Property)
			if err != nil {
				rc = base_msg.ConfigResponse_GET_PROP_NOT_FOUND
			}
		} else if *query.Operation == base_msg.ConfigQuery_SET {
			log.Printf("set op\n")
			err = property_update(*query.Property, *query.Value)
			if err != nil {
				// XXX - not the only possible failure
				rc = base_msg.ConfigResponse_SET_PROP_NOT_FOUND
			}

		} else {
			// XXX Must be a delete if operation was a DELETE.
			rc = base_msg.ConfigResponse_DELETE_PROP_NO_PERM
			log.Printf("not set or get\n")
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
