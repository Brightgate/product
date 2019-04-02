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
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"bg/ap_common/aputil"
	"bg/ap_common/broker"
	"bg/ap_common/mcp"
	"bg/base_def"
	"bg/base_msg"
	"bg/common/cfgapi"
	"bg/common/cfgmsg"
	"bg/common/cfgtree"
	"bg/common/network"

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	zmq "github.com/pebbe/zmq4"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
)

const pname = "ap.configd"

var metrics struct {
	getCounts  prometheus.Counter
	setCounts  prometheus.Counter
	delCounts  prometheus.Counter
	expCounts  prometheus.Counter
	testCounts prometheus.Counter
	treeSize   prometheus.Gauge
}

// Allow for significant variation in the processing of subtrees
type subtreeOps struct {
	get    func(string) (string, error)
	set    func(string, string, *time.Time, bool) error
	delete func(string) ([]string, error)
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
	check func(string, string) error
}{
	{regexp.MustCompile(`^@/uuid$`), uuidCheck},
	{regexp.MustCompile(`^@/clients/.*/(dns|dhcp)_name$`), dnsCheck},
	{regexp.MustCompile(`^@/clients/.*/ipv4$`), ipv4Check},
	{regexp.MustCompile(`^@/network/base_address$`), subnetCheck},
	{regexp.MustCompile(`^@/site_index$`), subnetCheck},
	{regexp.MustCompile(`^@/dns/cnames/`), cnameCheck},
}

var (
	verbose  = flag.Bool("v", false, "verbose log output")
	logLevel = flag.String("log-level", "info", "zap log level")

	propTree *cfgtree.PTree

	eventSocket *zmq.Socket

	mcpd    *mcp.MCP
	brokerd *broker.Broker
	slog    *zap.SugaredLogger

	virtualAPToDefaultRing map[string]string
	ringToVirtualAP        map[string]string
)

/*************************************************************************
 *
 * Broker notifications
 */
const (
	urChange = base_msg.EventConfig_CHANGE
	urDelete = base_msg.EventConfig_DELETE
	urExpire = base_msg.EventConfig_EXPIRE
)

type updateRecord struct {
	kind    base_msg.EventConfig_Type
	path    string
	value   string
	hash    []byte
	expires *time.Time
}

func updateChange(path string, val *string, exp *time.Time) *updateRecord {
	rec := &updateRecord{
		kind:    urChange,
		path:    path,
		expires: exp,
	}
	if val != nil {
		rec.value = *val
	}

	return rec
}

func updateDelete(path string) *updateRecord {
	return &updateRecord{
		kind: urDelete,
		path: path,
	}
}

func updateExpire(path string) *updateRecord {
	return &updateRecord{
		kind: urExpire,
		path: path,
	}
}

func protoPath(path string) *string {
	fields := make([]string, 0)

	for _, field := range strings.Split(path, "/") {
		if len(field) > 0 {
			fields = append(fields, field)
		}
	}

	return proto.String(strings.Join(fields, "/"))
}

// convert one or more internal updateRecord structures into EventConfig
// protobufs, and send them to ap.brokerd.
func updateNotify(records []*updateRecord) {
	for _, rec := range records {
		entity := &base_msg.EventConfig{
			Timestamp: aputil.NowToProtobuf(),
			Sender:    proto.String(pname),
			Type:      &rec.kind,
			Property:  protoPath(rec.path),
			NewValue:  proto.String(rec.value),
			Expires:   aputil.TimeToProtobuf(rec.expires),
			Hash:      rec.hash,
		}

		err := brokerd.Publish(entity, base_def.TOPIC_CONFIG)
		if err != nil {
			slog.Warnf("Failed to propagate config update: %v", err)
		}
	}
}

func eventHandler(event []byte) {
	var vap, ring string

	entity := &base_msg.EventNetEntity{}
	proto.Unmarshal(event, entity)

	if entity.MacAddress == nil {
		slog.Warnf("Received a NET.ENTITY event with no MAC: %v",
			entity)
		return
	}
	hwaddr := network.Uint64ToHWAddr(*entity.MacAddress)
	path := "@/clients/" + hwaddr.String() + "/"
	node, _ := propTree.GetNode(path)

	if entity.Ring != nil {
		ring = *entity.Ring
	}
	if entity.VirtualAP != nil {
		vap = *entity.VirtualAP
	}

	updates := []*updateRecord{
		updateChange(path+"connection/node", entity.Node, nil),
		updateChange(path+"connection/band", entity.Band, nil),
		updateChange(path+"connection/vap", &vap, nil),
	}

	if ring = selectRing(hwaddr.String(), node, vap, ring); ring != "" {
		updates = append(updates,
			updateChange(path+"ring", &ring, nil))
	}

	// Ipv4Address is an 'optional' field in the protobuf, but we will
	// also allow a value of 0.0.0.0 to indicate that the field should be
	// ignored.
	if entity.Ipv4Address != nil && *entity.Ipv4Address != 0 {
		ipv4 := network.Uint32ToIPAddr(*entity.Ipv4Address).String()
		updates = append(updates,
			updateChange(path+"ipv4_observed", &ipv4, nil))
	}

	propTree.ChangesetInit()
	failed := false
	for _, u := range updates {
		if len(u.value) > 0 {
			err := propTree.Add(u.path, u.value, nil)
			if err != nil {
				slog.Warnf("failed to insert %s: %v", u.path, err)
				failed = true
			} else {
				u.hash = propTree.Root().Hash()
			}
		}
	}

	if failed {
		propTree.ChangesetRevert()
	} else {
		updateNotify(updates)
		if propTree.ChangesetCommit() {
			if rerr := propTreeStore(); rerr != nil {
				slog.Warnf("failed to write properties: %v",
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
	virtualAPToDefaultRing = make(map[string]string)
	ringToVirtualAP = make(map[string]string)

	// For each virtual AP, find @/network/vap/<id>/default_ring
	if node, _ := propTree.GetNode("@/network/vap"); node != nil {
		for id, vap := range node.Children {
			if ring, ok := vap.Children["default_ring"]; ok {
				virtualAPToDefaultRing[id] = ring.Value
			}
		}
	}

	// For each virtual ring, find @/rings/<ring>/vap
	if node, _ := propTree.GetNode("@/rings"); node != nil {
		for ring, config := range node.Children {
			if vap, ok := config.Children["vap"]; ok {
				ringToVirtualAP[ring] = vap.Value
			}
		}
	}
}

// These are the ring transitions we will impose automatically when we find a
// client on a new VAP.  Essentially, we will let a device transition from a PSK
// ring to an EAP ring, but not from EAP to PSK.
var validRingUpgrades = map[string]bool{
	base_def.RING_UNENROLLED + ":" + base_def.RING_STANDARD: true,
	base_def.RING_GUEST + ":" + base_def.RING_STANDARD:      true,
	base_def.RING_DEVICES + ":" + base_def.RING_STANDARD:    true,
}

func selectRing(mac string, client *cfgtree.PNode, vap, ring string) string {
	var oldVAP, oldRing, newRing string

	if client != nil && client.Children != nil {
		if n := client.Children["ring"]; n != nil {
			if vap, ok := ringToVirtualAP[n.Value]; ok {
				oldRing = n.Value
				oldVAP = vap
			}
		}
	}

	if ring != "" && !cfgapi.ValidRings[ring] {
		slog.Warnf("invalid ring for %s: %s", mac, ring)
	} else {
		newRing = ring
	}

	if vap != "" {
		// if we're already assigned to a ring on this vap, keep it
		if vap == oldVAP {
			return oldRing
		}
		if vapRing, ok := virtualAPToDefaultRing[vap]; ok {
			newRing = vapRing
		} else {
			slog.Warnf("invalid virtualAP for %s: %s", mac, vap)
		}
	}

	if oldRing == "" {
		if newRing == "" {
			slog.Warnf("%s: no ring assignment available", mac)
		} else {
			slog.Infof("%s: assigned to %s ring", mac, newRing)
		}
	} else if validRingUpgrades[oldRing+":"+newRing] {
		slog.Infof("%s: upgrading from '%s' to '%s' ring", mac,
			oldRing, newRing)
	} else {
		slog.Infof("%s: declining to move from '%s' to '%s' ring",
			mac, oldRing, newRing)
		newRing = oldRing
	}
	return newRing
}

func uuidCheck(prop, uuid string) error {
	const nullUUID = "00000000-0000-0000-0000-000000000000"

	node, _ := propTree.GetNode(prop)
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
func dnsCheck(prop, hostname string) error {
	var parent *cfgtree.PNode
	var err error

	if node, _ := propTree.GetNode(prop); node != nil {
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
func cnameCheck(prop, hostname string) error {
	var err error
	var cname string

	// The validation code and the regexp that got us here should guarantee
	// that the structure of the path is @/dns/cnames/<hostname>
	path := strings.Split(prop, "/")
	if len(path) != 4 {
		err = fmt.Errorf("invalid property path: %s", prop)
	} else {
		cname = path[3]

		if !network.ValidHostname(cname) {
			err = fmt.Errorf("invalid hostname: %s", cname)
		} else if !network.ValidHostname(hostname) {
			err = fmt.Errorf("invalid canonical name: %s", hostname)
		} else if dnsNameInuse(nil, cname) {
			err = fmt.Errorf("duplicate hostname")
		}
	}

	return err
}

// Validate that a given site_index and base_address will allow us to generate
// legal subnet addresses
func subnetCheck(prop, val string) error {
	const basePath = "@/network/base_address"
	const sitePath = "@/site_index"
	var baseProp, siteProp string

	if prop == basePath {
		baseProp = val
	} else if p, err := propTree.GetProp(basePath); err == nil {
		baseProp = p
	} else {
		baseProp = "192.168.0.2/24"
	}

	if prop == sitePath {
		siteProp = val
	} else if p, err := propTree.GetProp(sitePath); err == nil {
		siteProp = p
	} else {
		siteProp = "0"
	}
	siteIdx, err := strconv.Atoi(siteProp)
	if err != nil {
		return fmt.Errorf("invalid %s: %v", sitePath, err)
	}

	// Make sure the base network address generates a valid subnet for both
	// the lowest and highest subnet indices.
	_, err = cfgapi.GenSubnet(baseProp, siteIdx, 0)
	if err != nil {
		err = fmt.Errorf("invalid %s: %v", prop, err)
	} else {
		_, err = cfgapi.GenSubnet(baseProp, siteIdx, cfgapi.MaxRings-1)
		if err != nil {
			err = fmt.Errorf("invalid %s for max subnet: %v",
				prop, err)
		}
	}

	return err
}

// Validate an ipv4 assignment for this device
func ipv4Check(prop, addr string) error {
	var updating string

	ipv4 := net.ParseIP(addr)
	if ipv4 == nil {
		return fmt.Errorf("invalid address: %s", addr)
	}

	// Make sure the address isn't already assigned
	clients, _ := propTree.GetNode("@/clients")
	if clients == nil {
		return nil
	}

	if path := strings.Split(prop, "/"); len(path) > 3 {
		updating = path[2]
	}

	for name, device := range clients.Children {
		if updating == name {
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
func xlateError(err error) error {
	if err == cfgtree.ErrNoProp {
		err = cfgapi.ErrNoProp
	} else if err == cfgtree.ErrExpired {
		err = cfgapi.ErrExpired
	}
	return err
}

func getPropHandler(prop string) (string, error) {
	rval, err := propTree.Get(prop)
	err = xlateError(err)

	return rval, err
}

func setPropHandler(prop string, val string, exp *time.Time, add bool) error {
	var err error

	if val == "" {
		return fmt.Errorf("no value supplied")
	}

	for _, r := range updateCheckTable {
		if r.path.MatchString(prop) {
			if err = r.check(prop, val); err != nil {
				return xlateError(err)
			}
		}
	}

	if add {
		err = propTree.Add(prop, val, exp)
	} else {
		err = propTree.Set(prop, val, exp)
	}

	return xlateError(err)
}

func delPropHandler(prop string) ([]string, error) {
	rval, err := propTree.Delete(prop)
	return rval, xlateError(err)
}

// utility function to extract the property parameters from a ConfigOp struct
func getParams(op *cfgmsg.ConfigOp) (string, string, *time.Time, error) {
	var expires *time.Time
	var err error

	prop := op.Property
	if prop == "" {
		err = cfgapi.ErrNoProp
	}
	val := op.Value
	if op.Expires != nil {
		expt, terr := ptypes.Timestamp(op.Expires)
		if terr != nil {
			err = cfgapi.ErrBadTime
		}
		expires = &expt
	}

	return prop, val, expires, err
}

func executePropOps(query *cfgmsg.ConfigQuery) (string, error) {
	var prop, val, rval string
	var expires *time.Time
	var err error
	var persistTree bool

	level := cfgapi.AccessLevel(query.Level)
	if _, ok := cfgapi.AccessLevelNames[level]; !ok {
		return "", fmt.Errorf("invalid access level: %d", level)
	}

	updates := make([]*updateRecord, 0)

	propTree.ChangesetInit()
	for _, op := range query.Ops {
		if prop, val, expires, err = getParams(op); err != nil {
			break
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
			} else if err = validateProp(prop); err == nil {
				rval, err = opsVector.get(prop)
			}

		case cfgmsg.ConfigOp_CREATE, cfgmsg.ConfigOp_SET:
			metrics.setCounts.Inc()
			if err = validatePropVal(prop, val, level); err == nil {
				err = opsVector.set(prop, val, expires,
					(op.Operation == cfgmsg.ConfigOp_CREATE))
			}
			if err == nil {
				update := updateChange(prop, &val, expires)
				update.hash = propTree.Root().Hash()
				updates = append(updates, update)
			}

		case cfgmsg.ConfigOp_DELETE:
			var paths []string

			metrics.delCounts.Inc()
			if err = validatePropDel(prop, level); err == nil {
				paths, err = opsVector.delete(prop)
			}

			for _, path := range paths {
				update := updateDelete(path)
				if path == prop {
					// If we delete a subtree, we send
					// notifications for each node in that
					// tree.  We only want the hash
					// after the root node is removed, since
					// that subsumes all of the child
					// deletions.
					update.hash = propTree.Root().Hash()
				}

				updates = append(updates, update)
			}

		case cfgmsg.ConfigOp_TEST:
			metrics.testCounts.Inc()
			if err = validateProp(prop); err == nil {
				_, err = opsVector.get(prop)
			}

		case cfgmsg.ConfigOp_TESTEQ:
			var testVal string
			metrics.testCounts.Inc()
			if err = validateProp(prop); err != nil {
				break
			}
			if testVal, err = opsVector.get(prop); err != nil {
				break
			}
			var testNode cfgapi.PropertyNode
			err = json.Unmarshal([]byte(testVal), &testNode)
			if err != nil {
				// will become ConfigResponse_FAILED
				break
			}
			if val != testNode.Value {
				err = cfgapi.ErrNotEqual
			}

		case cfgmsg.ConfigOp_PING:
		// no-op

		case cfgmsg.ConfigOp_ADDPROP:
			if level != cfgapi.AccessInternal {
				err = fmt.Errorf("must be internal to add " +
					"new settings")
			}
			slog.Debugf("Adding %s: %s", prop, val)
			err = addSetting(prop, val)

		case cfgmsg.ConfigOp_REPLACE:
			slog.Infof("Replacing config tree")

			newTree := []byte(val)
			if len(query.Ops) > 1 {
				err = fmt.Errorf("compound REPLACE op")

			} else if err = propTree.Replace(newTree); err != nil {
				slog.Warnf("importing replacement tree: %v", err)
				err = cfgapi.ErrBadTree

			} else {
				// Ideally we would restart automatically here,
				// but we run the risk of taking down ap.rpcd
				// before the completion has propagated back to
				// the cloud - which would cause us to refetch
				// and repeat the command.
				slog.Warnf("Config tree replaced - "+
					"%s should be restarted", pname)
				expirationInit()
				defaultRingInit()
				persistTree = true
			}

		default:
			err = cfgapi.ErrBadOp
		}

		if err != nil {
			break
		}
	}
	if err != nil {
		propTree.ChangesetRevert()
	} else {
		changedPaths := make([]string, 0)
		for _, u := range updates {
			if u.kind == urChange {
				changedPaths = append(changedPaths, u.path)
			}
		}
		expirationsEval(changedPaths)

		updateNotify(updates)
		if propTree.ChangesetCommit() || persistTree {
			if rerr := propTreeStore(); rerr != nil {
				slog.Warnf("failed to write properties: %v",
					rerr)
			}
		}
	}

	return rval, err
}

func processOneEvent(query *cfgmsg.ConfigQuery) *cfgmsg.ConfigResponse {
	var err error
	var rval string

	if query.Version.Major != cfgapi.Version {
		err = cfgapi.ErrBadVer

	} else if query.Timestamp != nil {
		_, err = ptypes.Timestamp(query.Timestamp)
		if err != nil {
			err = cfgapi.ErrBadTime
		}
	}

	if err == nil {
		rval, err = executePropOps(query)
	}

	if err == nil && *verbose {
		slog.Warnf("Config operation failed: %v", err)
	}

	response := cfgapi.GenerateConfigResponse(rval, err)
	response.Sender = pname + "(" + strconv.Itoa(os.Getpid()) + ")"
	return response
}

func eventLoop() {
	errs := 0

	for {
		msg, err := eventSocket.RecvMessageBytes(0)
		if err != nil {
			if err == zmq.ErrorSocketClosed {
				return
			}

			slog.Warnf("Error receiving message: %v", err)
			if errs++; errs > 10 {
				slog.Errorf("too many errors - giving up")
				eventSocket.Close()
				return
			}
			continue
		}

		errs = 0
		query := &cfgmsg.ConfigQuery{}
		proto.Unmarshal(msg[0], query)

		response := processOneEvent(query)
		data, err := proto.Marshal(response)
		if err != nil {
			slog.Warnf("Failed to marshal response to %v: %v",
				*query, err)
		} else {
			eventSocket.SendBytes(data, 0)
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
	metrics.testCounts = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "configd_tests",
		Help: "test operations",
	})
	metrics.treeSize = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "configd_tree_size",
		Help: "size of config tree",
	})

	prometheus.MustRegister(metrics.getCounts)
	prometheus.MustRegister(metrics.setCounts)
	prometheus.MustRegister(metrics.delCounts)
	prometheus.MustRegister(metrics.testCounts)
	prometheus.MustRegister(metrics.expCounts)

	prometheus.MustRegister(metrics.treeSize)
	http.Handle("/metrics", promhttp.Handler())
	go http.ListenAndServe(base_def.CONFIGD_DIAG_PORT, nil)
}

func fail(format string, a ...interface{}) {
	slog.Warnf(format, a...)
	mcpd.SetState(mcp.BROKEN)
	os.Exit(1)
}

func configInit() {
	defaults, descriptions, err := loadDefaults()
	if err != nil {
		fail("loadDefaults() failed: %v", err)
	}

	if err = validationInit(descriptions); err != nil {
		fail("validationInit() failed: %v", err)
	}

	expirationInit()

	if err = propTreeInit(defaults); err != nil {
		fail("propTreeInit() failed: %v", err)
	}

	defaultRingInit()
}

func zmqInit() {
	var err error

	eventSocket, err = zmq.NewSocket(zmq.REP)
	if err != nil {
		slog.Fatalf("creating zmq socket: %v", err)
	}

	port := base_def.INCOMING_ZMQ_URL + base_def.CONFIGD_ZMQ_REP_PORT
	if err = eventSocket.Bind(port); err != nil {
		fail("Failed to bind incoming port %s: %v", port, err)
	}

	slog.Debugf("Listening on %s", port)
}

func signalHandler() {
	sig := make(chan os.Signal, 2)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	s := <-sig
	slog.Infof("Signal (%v) received, stopping", s)
	eventSocket.Close()
}

func main() {
	var err error

	flag.Parse()
	slog = aputil.NewLogger(pname)
	defer slog.Sync()
	slog.Infof("starting")
	aputil.LogSetLevel("", *logLevel)

	if mcpd, err = mcp.New(pname); err != nil {
		slog.Warnf("Failed to connect to mcp: %v", err)
	}

	prometheusInit()
	configInit()

	brokerd = broker.New(pname)
	brokerd.Handle(base_def.TOPIC_ENTITY, eventHandler)
	defer brokerd.Fini()

	if err = deviceDBInit(); err != nil {
		fail("Failed to import devices database: %v", err)
	}

	zmqInit()

	mcpd.SetState(mcp.ONLINE)

	go expirationHandler()
	go signalHandler()

	eventLoop()

	slog.Infof("stopping")
}
