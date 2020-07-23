/*
 * COPYRIGHT 2020 Brightgate Inc.  All rights reserved.
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
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"bg/ap_common/aputil"
	"bg/ap_common/bgmetrics"
	"bg/ap_common/broker"
	"bg/ap_common/comms"
	"bg/ap_common/mcp"
	"bg/ap_common/platform"
	"bg/base_def"
	"bg/base_msg"
	"bg/common"
	"bg/common/cfgapi"
	"bg/common/cfgmsg"
	"bg/common/cfgtree"
	"bg/common/network"

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	"go.uber.org/zap"
)

const (
	pname     = "ap.configd"
	serverURL = base_def.INCOMING_COMM_URL + base_def.CONFIGD_COMM_REP_PORT
)

var (
	bgm *bgmetrics.Metrics

	metrics struct {
		getCounts    *bgmetrics.Counter
		setCounts    *bgmetrics.Counter
		delCounts    *bgmetrics.Counter
		expCounts    *bgmetrics.Counter
		testCounts   *bgmetrics.Counter
		treeSize     *bgmetrics.Gauge
		queueLenAvg  *bgmetrics.Gauge
		queueLenMax  *bgmetrics.Gauge
		queueTimeAvg *bgmetrics.DurationSummary
		queueTimeMax *bgmetrics.DurationSummary
		execTimeAvg  *bgmetrics.DurationSummary
		execTimeMax  *bgmetrics.DurationSummary
		replyTimeAvg *bgmetrics.DurationSummary
		replyTimeMax *bgmetrics.DurationSummary
	}
)

type subtreeOpHandler func(*cfgmsg.ConfigQuery) (*string, error)
type subtreeMatch struct {
	path    *regexp.Regexp
	handler subtreeOpHandler
}

var subtreeMatchTable = []subtreeMatch{
	{regexp.MustCompile(`^@/metrics`), metricsPropHandler},
	{regexp.MustCompile(`^@/`), configPropHandler},
}

var updateCheckTable = []struct {
	path  *regexp.Regexp
	check func(string, string) error
}{
	{regexp.MustCompile(`^@/uuid$`), checkUUID},
	{regexp.MustCompile(`^@/clients/.*/(dns|dhcp)_name$`), checkDNS},
	{regexp.MustCompile(`^@/clients/.*/ipv4$`), checkIPv4},
	{regexp.MustCompile(`^@/network/base_address$`), checkSubnet},
	{regexp.MustCompile(`^@/rings/.*/subnet$`), checkSubnet},
	{regexp.MustCompile(`^@/network/wan/static.*`), checkWan},
	{regexp.MustCompile(`^@/site_index$`), checkSubnet},
	{regexp.MustCompile(`^@/dns/cnames/`), checkCname},
}

var updateHandlers = []struct {
	match   *regexp.Regexp
	handler func(int, string, string)
}{
	{regexp.MustCompile(`^@/network/vap/.*/default_ring$`), updateDefaultRing},
	{regexp.MustCompile(`^@/clients/.*/home$`), updateClientHome},
	{regexp.MustCompile(`^@/settings/ap.configd/.*`), updateSetting},
	{regexp.MustCompile(`^@/site_index`), updateSiteIdx},
	{regexp.MustCompile(`^@/network/base_address`), updateBaseAddress},
	{regexp.MustCompile(`^@/rings/.*/subnet`), updateRingSubnet},
}

var configdSettings = map[string]struct {
	valType    string
	valDefault string
}{
	"verbose":    {"bool", "false"},
	"log_level":  {"string", "info"},
	"downgrade":  {"bool", "false"},
	"store_freq": {"duration", "1s"},
}

var singletonOps = map[cfgmsg.ConfigOp_Operation]bool{
	cfgmsg.ConfigOp_REPLACE: true,
	cfgmsg.ConfigOp_GET:     true,
}

type subnetInfo struct {
	siteIndex   int
	baseAddress string
	perRing     map[string]*net.IPNet
}

var (
	verbose        = flag.Bool("v", false, "verbose log output")
	logLevel       = flag.String("log-level", "info", "zap log level")
	allowDowngrade = flag.Bool("downgrade",
		true, "allow migrations to lower level rings")
	storeFreq = flag.Duration("store-freq", time.Second,
		"tree store frequency")

	propTree *cfgtree.PTree

	mcpd       *mcp.MCP
	brokerd    *broker.Broker
	slog       *zap.SugaredLogger
	plat       *platform.Platform
	serverPort *comms.APComm

	ringSubnets subnetInfo

	virtualAPToDefaultRing map[string]string
	ringOnVirtualAP        map[string]bool
)

/*************************************************************************
 * track subnets
 */
func recalculateRingSubnets(si *subnetInfo) {
	base := si.baseAddress
	idx := si.siteIndex
	si.perRing = make(map[string]*net.IPNet)

	for ring, info := range propTree.GetChildren("@/rings") {
		var subnet string

		if x, ok := info.Children["subnet"]; ok {
			subnet = x.Value
		} else {
			subnet, _ = cfgapi.RingSubnet(ring, base, idx)
		}
		_, si.perRing[ring], _ = net.ParseCIDR(subnet)
	}
}

func ringSubnetInit() {
	x, err := propTree.GetProp("@/site_index")
	if err != nil {
		fail("fetching @/site_index: %v", err)
	}
	ringSubnets.siteIndex, _ = strconv.Atoi(x)

	ringSubnets.baseAddress, err = propTree.GetProp("@/network/base_address")
	if err != nil {
		fail("fetching @/network/base_address: %v", err)
	}
	recalculateRingSubnets(&ringSubnets)
}

func updateSiteIdx(op int, prop, val string) {
	slog.Infof("updated siteIndex to %s", val)
	ringSubnets.siteIndex, _ = strconv.Atoi(val)
	recalculateRingSubnets(&ringSubnets)
}

func updateBaseAddress(op int, prop, val string) {
	slog.Infof("updated baseAddress to %s", val)
	ringSubnets.baseAddress = val
	recalculateRingSubnets(&ringSubnets)
}

func updateRingSubnet(op int, prop, val string) {
	slog.Infof("updated %s to %s", prop, val)
	recalculateRingSubnets(&ringSubnets)
}

/*************************************************************************
 *
 * Broker notifications
 */
const (
	propChange = iota
	propDelete
	propExpire
)

type updateRecord struct {
	kind    int
	path    string
	value   string
	hash    []byte
	expires *time.Time
}

func updateChange(path string, val *string, exp *time.Time) *updateRecord {
	rec := &updateRecord{
		kind:    propChange,
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
		kind: propDelete,
		path: path,
	}
}

func updateExpire(path string) *updateRecord {
	return &updateRecord{
		kind: propExpire,
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
		var kind base_msg.EventConfig_Type
		switch rec.kind {
		case propChange:
			kind = base_msg.EventConfig_CHANGE
		case propDelete:
			kind = base_msg.EventConfig_DELETE
		case propExpire:
			kind = base_msg.EventConfig_EXPIRE
		}

		entity := &base_msg.EventConfig{
			Timestamp: aputil.NowToProtobuf(),
			Sender:    proto.String(pname),
			Type:      &kind,
			Property:  protoPath(rec.path),
			NewValue:  proto.String(rec.value),
			Expires:   aputil.TimeToProtobuf(rec.expires),
			Hash:      rec.hash,
		}

		err := brokerd.Publish(entity, base_def.TOPIC_CONFIG)
		if err != nil {
			slog.Warnf("Failed to propagate config update: %v", err)
		}

		for _, m := range updateHandlers {
			if m.match.MatchString(rec.path) {
				m.handler(rec.kind, rec.path, rec.value)
			}
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
	hwaddr := network.Uint64ToHWAddr(*entity.MacAddress).String()
	path := "@/clients/" + hwaddr + "/"
	client, _ := propTree.GetNode(path)
	updates := make([]*updateRecord, 0)
	active := "unknown"

	// We start by assuming that a client is wired, but any subsequent
	// wireless event will overrule that assumption.
	wireless := "false"
	if w, _ := propTree.GetNode(path + "connection/wireless"); w != nil {
		wireless = w.Value
	}

	if entity.Ring != nil {
		ring = *entity.Ring
	}
	if entity.VirtualAP != nil {
		wireless = "true"
		vap = *entity.VirtualAP

		if *entity.Disconnect {
			active = "false"
		} else {
			active = "true"
		}
		updates = append(updates,
			updateChange(path+"connection/node", entity.Node, nil),
			updateChange(path+"connection/band", entity.Band, nil),
			updateChange(path+"connection/vap", &vap, nil))
	}
	updates = append(updates,
		updateChange(path+"connection/active", &active, nil),
		updateChange(path+"connection/wireless", &wireless, nil))

	if entity.Username != nil {
		updates = append(updates,
			updateChange(path+"connection/username", entity.Username,
				nil))
	}
	if ring = selectRing(hwaddr, client, vap, ring); ring != "" {
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
			slog.Debugf("Added %s -> %s", u.path, u.value)
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
		propTree.ChangesetCommit()
	}
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
	} else if err == cfgtree.ErrNotLeaf {
		err = cfgapi.ErrNotLeaf
	}
	return err
}

func cfgPropGetNode(prop string) (*cfgtree.PNode, error) {
	rval, err := propTree.GetNode(prop)
	err = xlateError(err)

	return rval, err
}

func cfgPropGet(prop string) (*string, error) {
	rval, err := propTree.Get(prop)
	err = xlateError(err)

	return rval, err
}

func cfgPropSet(prop string, val string, exp *time.Time, add bool) error {
	var err error

	if val == "" {
		return fmt.Errorf("no value supplied")
	}

	if old, _ := propTree.GetProp(prop); old != val {
		for _, r := range updateCheckTable {
			if r.path.MatchString(prop) {
				if err = r.check(prop, val); err != nil {
					return xlateError(err)
				}
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

func cfgPropDel(prop string) ([]string, error) {
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

func restart() {
	slog.Infof("exiting %s for a clean restart", pname)
	os.Exit(0)
}

func configPropHandler(query *cfgmsg.ConfigQuery) (*string, error) {
	var rval *string
	var err error
	var persistTree bool

	level := cfgapi.AccessLevel(query.Level)
	updates := make([]*updateRecord, 0)
	propTree.ChangesetInit()
	for _, op := range query.Ops {
		prop, val, expires, gerr := getParams(op)
		if gerr != nil {
			err = gerr
			break
		}

		switch op.Operation {
		case cfgmsg.ConfigOp_GET:
			metrics.getCounts.Inc()
			if err = validateProp(prop); err == nil {
				rval, err = cfgPropGet(prop)
				if prop == "@/" {
					refreshEvent(rval)
				}
			}

		case cfgmsg.ConfigOp_CREATE, cfgmsg.ConfigOp_SET:
			metrics.setCounts.Inc()
			if err = validatePropVal(prop, val, level); err == nil {
				err = cfgPropSet(prop, val, expires,
					(op.Operation == cfgmsg.ConfigOp_CREATE))
			}

			if err == nil {
				slog.Debugf("Set %s -> %s", prop, val)
				update := updateChange(prop, &val, expires)
				update.hash = propTree.Root().Hash()
				updates = append(updates, update)
			}

		case cfgmsg.ConfigOp_DELETE:
			var paths []string

			metrics.delCounts.Inc()
			if err = validatePropDel(prop, level); err == nil {
				paths, err = cfgPropDel(prop)
			}

			for _, path := range paths {
				slog.Debugf("Deleted %s", path)
				update := updateDelete(path)
				if path == prop {
					// If we delete a subtree, we send
					// notifications for each node in that
					// tree.  We only want the hash after
					// the root node is removed, since that
					// subsumes all of the child deletions.
					update.hash = propTree.Root().Hash()
				}

				updates = append(updates, update)
			}

		case cfgmsg.ConfigOp_TEST, cfgmsg.ConfigOp_TESTEQ:
			opEq := (op.Operation == cfgmsg.ConfigOp_TESTEQ)
			metrics.testCounts.Inc()
			if err = validateProp(prop); err == nil {
				var testNode *cfgtree.PNode

				testNode, err = cfgPropGetNode(prop)
				if err == nil && opEq && val != testNode.Value {
					err = cfgapi.ErrNotEqual
				}
				if err != nil {
					slog.Debugf("%v failed: %v", op, err)
				}
			}

		case cfgmsg.ConfigOp_PING:
		// no-op

		case cfgmsg.ConfigOp_ADDVALID:
			if level != cfgapi.AccessInternal {
				err = fmt.Errorf("must be internal to add " +
					"new property types")
			} else {
				slog.Debugf("Adding %s: %s", prop, val)
				err = addSetting(prop, val)
			}

		case cfgmsg.ConfigOp_REPLACE:
			slog.Infof("Replacing config tree")

			newTree := []byte(val)
			if err = propTree.Replace(newTree); err != nil {
				aputil.ReportError("importing replacement tree: %v", err)
				err = cfgapi.ErrBadTree

			} else {
				// By restarting automatically, there is some
				// risk that we will take down ap.rpcd before
				// propagating the command completion.  Because
				// we just received a command, we can be
				// reasonably confident that we have a live
				// connection, so the 30 second delay should be
				// more than enough time to minimize that risk.
				slog.Warnf("Config tree replaced - "+
					"%s will be restarted", pname)
				time.AfterFunc(30*time.Second, restart)
				expirationInit(propTree)
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
			if u.kind == propChange {
				changedPaths = append(changedPaths, u.path)
			}
		}
		expirationsEval(changedPaths)

		updateNotify(updates)
		propTree.ChangesetCommit()
		if persistTree {
			propTreeStoreTrigger <- true
		}
	}

	return rval, err
}

func executePropOps(query *cfgmsg.ConfigQuery) (*string, error) {
	var handler subtreeOpHandler
	var rval *string
	var err error

	level := cfgapi.AccessLevel(query.Level)
	if _, ok := cfgapi.AccessLevelNames[level]; !ok {
		return nil, fmt.Errorf("invalid access level: %d", level)
	}

	// Iterate over all of the operations in the vector to sanity-check the
	// arguments and identify the correct handler for the vector.
	match := -1
	for _, op := range query.Ops {
		var newMatch int
		var opName, prop string

		if opName, err = cfgmsg.OpName(op.Operation); err != nil {
			err = cfgapi.ErrBadOp
			break
		}

		if prop, _, _, err = getParams(op); err != nil {
			break
		}

		for idx, r := range subtreeMatchTable {
			if r.path.MatchString(prop) {
				newMatch = idx
				handler = r.handler
				break
			}
		}
		if match == -1 {
			match = newMatch
		} else if match != newMatch {
			err = fmt.Errorf("operation spans multiple trees")
			break
		}

		if len(query.Ops) > 1 && singletonOps[op.Operation] {
			err = fmt.Errorf("compund %s operations not supported",
				opName)
			break
		}
	}

	if err == nil && handler != nil {
		rval, err = handler(query)
	}

	return rval, err
}

func processOneEvent(query *cfgmsg.ConfigQuery) *cfgmsg.ConfigResponse {
	var err error
	var rval *string

	if query.Version.Major != cfgapi.Version {
		err = cfgapi.ErrBadVer

	} else if query.Timestamp != nil {
		_, err = ptypes.Timestamp(query.Timestamp)
		if err != nil {
			err = cfgapi.ErrBadTime
		}
	}

	start := time.Now()
	if err == nil {
		rval, err = executePropOps(query)
	}
	if t := time.Since(start); t > 100*time.Millisecond {
		sstr := query.GetSender()
		qstr := cfgapi.QueryToString(query)
		if l := len(qstr); l > 200 {
			r := strconv.Itoa(l - 100)
			qstr = qstr[:100] + " ... <" + r + " removed>"
		}
		if t > 500*time.Millisecond {
			slog.Warnf("%s op took %v: %s", sstr, t, qstr)
		} else {
			slog.Debugf("%s op took %v: %s", sstr, t, qstr)
		}
	}

	if err != nil && *verbose {
		slog.Warnf("Config operation failed: %v", err)
	}

	response := cfgapi.GenerateConfigResponse(rval, err)
	response.CmdID = query.CmdID
	response.Sender = pname + "(" + strconv.Itoa(os.Getpid()) + ")"

	return response
}

func msgHandler(msg []byte) []byte {
	var response *cfgmsg.ConfigResponse

	query := &cfgmsg.ConfigQuery{}
	if err := proto.Unmarshal(msg, query); err != nil {
		response = cfgapi.GenerateConfigResponse(nil, err)
	} else {
		response = processOneEvent(query)
	}
	data, err := proto.Marshal(response)
	if err != nil {
		slog.Warnf("Failed to marshal response to %v: %v",
			*query, err)
	}
	return data
}

func statsLoop() {
	t := time.NewTicker(5 * time.Second)
	for {
		<-t.C

		s := serverPort.Stats()
		metrics.queueLenAvg.Set(float64(s.QueueLenAvg))
		metrics.queueLenMax.Set(float64(s.QueueLenMax))
		metrics.queueTimeAvg.Observe(s.QueueTime.Avg)
		metrics.queueTimeMax.Observe(s.QueueTime.Max)
		metrics.execTimeAvg.Observe(s.ExecTime.Avg)
		metrics.execTimeMax.Observe(s.ExecTime.Max)
		metrics.replyTimeAvg.Observe(s.ReplyTime.Avg)
		metrics.replyTimeMax.Observe(s.ReplyTime.Max)
	}
}

func metricsInit() {
	hdl := NewInternalHdl()

	bgm = bgmetrics.NewMetrics(pname, hdl)
	metrics.getCounts = bgm.NewCounter("gets")
	metrics.setCounts = bgm.NewCounter("sets")
	metrics.delCounts = bgm.NewCounter("deletes")
	metrics.expCounts = bgm.NewCounter("expires")
	metrics.testCounts = bgm.NewCounter("tests")
	metrics.treeSize = bgm.NewGauge("tree_size")
	metrics.queueLenAvg = bgm.NewGauge("queue_len_avg")
	metrics.queueLenMax = bgm.NewGauge("queue_len_max")
	metrics.queueTimeAvg = bgm.NewDurationSummary("queue_time_avg")
	metrics.queueTimeMax = bgm.NewDurationSummary("queue_time_max")
	metrics.execTimeAvg = bgm.NewDurationSummary("exec_time_avg")
	metrics.execTimeMax = bgm.NewDurationSummary("exec_time_max")
	metrics.replyTimeAvg = bgm.NewDurationSummary("reply_time_avg")
	metrics.replyTimeMax = bgm.NewDurationSummary("reply_time_max")
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

	if err = propTreeInit(defaults); err != nil {
		fail("propTreeInit() failed: %v", err)
	}
	propTree.SetCacheable()

	initSettings()
	expirationInit(propTree)
	defaultRingInit()
	ringSubnetInit()
}

func signalHandler() {
	var s os.Signal

	sig := make(chan os.Signal, 3)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	done := false
	for !done {
		s = <-sig
		if s == syscall.SIGHUP {
			bgm.Dump()
		} else {
			done = true
		}
	}
	slog.Infof("Signal (%v) received, stopping", s)
	serverPort.Close()
}

func main() {
	var err error
	var wg sync.WaitGroup

	flag.Parse()
	slog = aputil.NewLogger(pname)
	defer slog.Sync()
	slog.Infof("starting")
	aputil.LogSetLevel("", *logLevel)

	aputil.ReportInit(slog, pname)

	if mcpd, err = mcp.New(pname); err != nil {
		slog.Warnf("Failed to connect to mcp: %v", err)
	}

	metricsInit()
	configInit()

	exitSignal := make(chan bool, 1)
	wg.Add(1)
	go propTreeWriter(exitSignal, &wg)

	brokerd, err = broker.NewBroker(slog, pname)
	if err != nil {
		slog.Fatal(err)
	}
	brokerd.Handle(base_def.TOPIC_ENTITY, eventHandler)
	defer brokerd.Fini()

	if serverPort, err = comms.NewAPServer(pname, serverURL); err != nil {
		fail("opening server port: %v", err)
	}
	go serverPort.Serve(msgHandler)
	go statsLoop()

	// for pprof
	go http.ListenAndServe(base_def.CONFIGD_DIAG_PORT, nil)

	slog.Infof("%s online running %s", pname, common.GitVersion)
	mcpd.SetState(mcp.ONLINE)

	go expirationHandler()
	signalHandler()

	propTreeStoreTrigger <- true
	exitSignal <- true
	wg.Wait()
	slog.Infof("stopping")
}

// This is done as an init() function so it is executed during 'go test' as well
// as when running
func init() {
	plat = platform.NewPlatform()
}
