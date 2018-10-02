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

	"bg/ap_common/apcfg"
	"bg/ap_common/aputil"
	"bg/ap_common/broker"
	"bg/ap_common/mcp"
	"bg/ap_common/network"
	"bg/base_def"
	"bg/base_msg"
	"bg/common/cfgmsg"
	"bg/common/cfgtree"

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
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
	set    func(string, string, *time.Time, bool) error
	delete func(string) error
}

type subtreeMatch struct {
	path *regexp.Regexp
	ops  *subtreeOps
}

var defaultSubtreeOps = subtreeOps{getPropHandler, setPropHandler, delPropHandler}
var devSubtreeOps = subtreeOps{getDevHandler, setDevHandler, delDevHandler}

var subtreeMatchTable = []subtreeMatch{
	{regexp.MustCompile(`^@/devices`), &devSubtreeOps},
}

var updateCheckTable = []struct {
	path  *regexp.Regexp
	check func(*cfgtree.PNode, string) error
}{
	{regexp.MustCompile(`^@/uuid$`), uuidCheck},
	{regexp.MustCompile(`^@/clients/.*/(dns|dhcp)_name$`), dnsCheck},
	{regexp.MustCompile(`^@/clients/.*/ipv4$`), ipv4Check},
	{regexp.MustCompile(`^@/dns/cnames/`), cnameCheck},
}

var (
	verbose = flag.Bool("v", false, "verbose log output")

	propTree *cfgtree.PTree

	mcpd    *mcp.MCP
	brokerd *broker.Broker

	authTypeToDefaultRing map[string]string

	propCallbacks = cfgtree.Callbacks{
		Updated: propUpdated,
		Deleted: propDeleted,
	}
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

func propUpdated(node *cfgtree.PNode) {
	propNotify(node.Path(), node.Value, node.Expires,
		base_msg.EventConfig_CHANGE)
}

func propDeleted(node *cfgtree.PNode) {
	expirationRemove(node)
	propNotify(node.Path(), "-", nil, base_msg.EventConfig_DELETE)
}

func expirationNotify(node *cfgtree.PNode) {
	propNotify(node.Path(), node.Value, nil, base_msg.EventConfig_EXPIRE)
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
	node, _ := propTree.GetNode(path)
	ring := selectRing(hwaddr.String(), node, entity.Authtype)

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

	propTree.ChangesetInit()
	failed := false
	for p, v := range updates {
		if v != nil && *v != "" {
			if err := propTree.Add(p, *v, nil); err != nil {
				log.Printf("failed to insert %s: %v\n", p, err)
				failed = true
			}
		}
	}
	if failed {
		propTree.ChangesetRevert()
	} else {
		if propTree.ChangesetCommit() {
			if rerr := propTreeStore(); rerr != nil {
				log.Printf("failed to write properties: %v",
					rerr)
			}
		}
	}
}

/****************************************************************************
 *
 * Logic for determining the proper ring on which to place a newly discovered
 * device.
 */
func defaultRingInit() {
	authTypeToDefaultRing = make(map[string]string)

	if node, _ := propTree.GetNode("@/network/default_ring"); node != nil {
		for a, n := range node.Children {
			authTypeToDefaultRing[a] = n.Value
			log.Printf("default %s ring: %s\n", a, n.Value)
		}
	}
}

func selectRing(mac string, client *cfgtree.PNode, authp *string) string {
	var ring, auth string

	if client != nil && client.Children != nil {
		// If the client already has a ring set, don't override it
		if n := client.Children["ring"]; n != nil && n.Value != "" {
			return n.Value
		}
		if conn, ok := client.Children["connection"]; ok {
			if a, ok := conn.Children["authtype"]; ok {
				auth = a.Value
			}
		}
	}
	if authp != nil {
		auth = *authp
	}

	if auth == "" {
		log.Printf("Can't select ring for %s: no auth type\n", mac)
	} else if r, ok := authTypeToDefaultRing[auth]; ok {
		log.Printf("Setting initial ring for %s %s to %s\n",
			mac, auth, r)
		ring = r
	} else {
		log.Printf("Can't select ring for %s: unsupported auth: %s\n",
			mac, auth)
	}
	return ring
}

func uuidCheck(node *cfgtree.PNode, uuid string) error {
	const nullUUID = "00000000-0000-0000-0000-000000000000"

	if node != nil && node.Value != nullUUID {
		return fmt.Errorf("cannot change an appliance's UUID")
	}
	return nil
}

//
// Check to see whether the given hostname is already inuse as either a device's
// dns_name or as the left hand side of a cname.  We can optionally indicate a
// device to ignore, allowing us to answer the question "is any other device
// using this hostname?"
//
func dnsNameInuse(ignore *cfgtree.PNode, hostname string) bool {
	lower := strings.ToLower(hostname)

	clients, _ := propTree.GetNode("@/clients")
	if clients == nil {
		return false
	}
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

	if cnames, _ := propTree.GetNode("@/dns/cnames"); cnames != nil {
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

// Validate the hostname that will be used to generate DNS A records
// for this device
func dnsCheck(node *cfgtree.PNode, hostname string) error {
	var parent *cfgtree.PNode
	var err error

	if node != nil {
		parent = node.Parent()
	}

	if !network.ValidDNSLabel(hostname) {
		err = fmt.Errorf("invalid hostname: %s", hostname)
	} else if dnsNameInuse(parent, hostname) {
		err = fmt.Errorf("duplicate hostname")
	}

	return err
}

// Validate both the hostname and the canonical name that will be
// used to generate DNS CNAME records
func cnameCheck(node *cfgtree.PNode, hostname string) error {
	var err error

	name := node.Name()

	if !network.ValidHostname(name) {
		err = fmt.Errorf("invalid hostname: %s", name)
	} else if !network.ValidHostname(hostname) {
		err = fmt.Errorf("invalid canonical name: %s", hostname)
	} else if dnsNameInuse(node, name) {
		err = fmt.Errorf("duplicate hostname")
	}

	return err
}

// Validate an ipv4 assignment for this device
func ipv4Check(node *cfgtree.PNode, addr string) error {
	ipv4 := net.ParseIP(addr)
	if ipv4 == nil {
		return fmt.Errorf("invalid address: %s", addr)
	}

	// Make sure the address isn't already assigned
	clients, _ := propTree.GetNode("@/clients")
	if clients == nil {
		return nil
	}

	for name, device := range clients.Children {
		if node != nil && node.Name() == name {
			// Reassigning the device's address to itself is fine
			continue
		}

		if ipv4Node, ok := device.Children["ipv4"]; ok {
			addr := net.ParseIP(ipv4Node.Value)
			expired := ipv4Node.Expires != nil &&
				ipv4Node.Expires.Before(time.Now())

			if ipv4.Equal(addr) && !expired {
				return fmt.Errorf("%s in use by %s", addr, name)
			}
		}
	}

	return nil
}

/*************************************************************************
 *
 * Handling incoming requests from other daemons
 */
func getPropHandler(prop string) (string, error) {
	prop, err := propTree.Get(prop)
	if err == cfgtree.ErrNoProp {
		err = apcfg.ErrNoProp
	} else if err == cfgtree.ErrExpired {
		err = apcfg.ErrExpired
	}

	return prop, err
}

func setPropHandler(prop string, val string, exp *time.Time, add bool) error {
	var err error

	if val == "" {
		return fmt.Errorf("no value supplied")
	}

	for _, r := range updateCheckTable {
		if r.path.MatchString(prop) {
			node, _ := propTree.GetNode(prop)
			if err = r.check(node, val); err != nil {
				return err
			}
		}
	}

	if add {
		err = propTree.Add(prop, val, exp)
	} else {
		err = propTree.Set(prop, val, exp)
	}

	return err
}

func delPropHandler(prop string) error {
	return propTree.Delete(prop)
}

func executePropOps(query *cfgmsg.ConfigQuery) (string, error) {
	var rval string
	var err error

	level := apcfg.AccessLevel(query.Level)
	if _, ok := apcfg.AccessLevelNames[level]; !ok {
		return "", fmt.Errorf("invalid access level: %d", level)
	}

	paths := make([]string, 0)
	propTree.ChangesetInit()
	for _, op := range query.Ops {
		var expires *time.Time

		val := op.Value
		prop := op.Property
		if prop == "" {
			err = apcfg.ErrNoProp
			break
		}
		if op.Expires != nil {
			expt, terr := ptypes.Timestamp(op.Expires)
			if terr != nil {
				err = apcfg.ErrBadTime
				break
			}
			expires = &expt
		}

		opsVector := &defaultSubtreeOps
		for _, r := range subtreeMatchTable {
			if r.path.MatchString(prop) {
				opsVector = r.ops
				break
			}
		}

		switch op.Operation {
		case cfgmsg.ConfigOp_GET:
			metrics.getCounts.Inc()
			if len(query.Ops) > 1 {
				err = fmt.Errorf("only single-GET " +
					"operations are supported")
			} else {
				if err = validateProp(prop); err == nil {
					rval, err = opsVector.get(prop)
				}
			}

		case cfgmsg.ConfigOp_CREATE:
			metrics.setCounts.Inc()
			err = validatePropVal(prop, val, level)
			if err == nil {
				err = opsVector.set(prop, val, expires, true)
				paths = append(paths, prop)
			}

		case cfgmsg.ConfigOp_SET:
			metrics.setCounts.Inc()
			err = validatePropVal(prop, val, level)
			if err == nil {
				err = opsVector.set(prop, val, expires, false)
				paths = append(paths, prop)
			}

		case cfgmsg.ConfigOp_DELETE:
			metrics.delCounts.Inc()
			err = validatePropDel(prop, level)
			if err == nil {
				err = opsVector.delete(prop)
			}

		case cfgmsg.ConfigOp_PING:
		// no-op

		default:
			err = apcfg.ErrBadOp
		}

		if err != nil {
			break
		}
	}
	if err != nil {
		propTree.ChangesetRevert()
	} else {
		expirationsEval(paths)
		if propTree.ChangesetCommit() {
			if rerr := propTreeStore(); rerr != nil {
				log.Printf("failed to write properties: %v",
					rerr)
			}
		}
	}

	return rval, err
}

func processOneEvent(query *cfgmsg.ConfigQuery) *cfgmsg.ConfigResponse {
	var err error
	var rval string

	if query.Version.Major != apcfg.Version {
		err = apcfg.ErrBadVer
	}
	if err == nil && query.Timestamp != nil {
		_, err = ptypes.Timestamp(query.Timestamp)
		if err != nil {
			err = apcfg.ErrBadTime
		}
	}
	if err == nil {
		rval, err = executePropOps(query)
	}
	rc := cfgmsg.ConfigResponse_OK
	if err != nil {
		switch err {
		case apcfg.ErrBadOp:
			rc = cfgmsg.ConfigResponse_UNSUPPORTED
		case apcfg.ErrNoProp:
			rc = cfgmsg.ConfigResponse_NOPROP
		case apcfg.ErrBadTime:
			rc = cfgmsg.ConfigResponse_BADTIME
		case apcfg.ErrBadVer:
			rc = cfgmsg.ConfigResponse_BADVERSION
		default:
			rc = cfgmsg.ConfigResponse_FAILED
		}

		if *verbose {
			log.Printf("Config operation failed: %v\n", err)
		}
		rval = fmt.Sprintf("%v", err)
	}
	version := cfgmsg.Version{Major: int32(apcfg.Version)}
	response := &cfgmsg.ConfigResponse{
		Timestamp: ptypes.TimestampNow(),
		Sender:    pname + "(" + strconv.Itoa(os.Getpid()) + ")",
		Version:   &version,
		Debug:     "-",
		Response:  rc,
		Value:     rval,
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
		query := &cfgmsg.ConfigQuery{}
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

func fail(format string, a ...interface{}) {
	log.Printf(format, a)
	mcpd.SetState(mcp.BROKEN)
	os.Exit(1)
}

func configInit() {
	defaults, descriptions, err := loadDefaults()
	if err != nil {
		fail("loadDefaults() failed: %v", err)
	}

	if err = validationInit(descriptions); err != nil {
		fail("validationInit() failed: %v\n", err)
	}

	expirationInit()

	if err = propTreeInit(defaults); err != nil {
		fail("propTreeInit() failed: %v\n", err)
	}

	propTree.RegisterCallbacks(propCallbacks)

	defaultRingInit()
}

func main() {
	var err error

	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	flag.Parse()

	if mcpd, err = mcp.New(pname); err != nil {
		log.Printf("Failed to connect to mcp: %v\n", err)
	}

	prometheusInit()
	configInit()

	brokerd = broker.New(pname)
	brokerd.Handle(base_def.TOPIC_ENTITY, entityHandler)
	defer brokerd.Fini()

	if err = deviceDBInit(); err != nil {
		fail("Failed to import devices database: %v\n", err)
	}

	incoming, _ := zmq.NewSocket(zmq.REP)
	port := base_def.INCOMING_ZMQ_URL + base_def.CONFIGD_ZMQ_REP_PORT
	if err = incoming.Bind(port); err != nil {
		fail("Failed to bind incoming port %s: %v\n", port, err)
	}
	log.Printf("Listening on %s\n", port)

	mcpd.SetState(mcp.ONLINE)

	go expirationHandler()
	eventLoop(incoming)
}
