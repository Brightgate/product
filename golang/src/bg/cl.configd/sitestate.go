/*
 * COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 *
 */

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"bg/cl_common/daemonutils"
	rpc "bg/cloud_rpc"
	"bg/common/cfgmsg"
	"bg/common/cfgtree"

	"github.com/golang/protobuf/ptypes"
)

// Each subscriber to a tree's changes has its own update queue.  Each update
// may appear on multiple queues.  Rather than adding an explicit reference
// count to each update, we rely on Go's garbage collector to recognize when all
// subscribers have dropped their pointers to an update.
type updateQueue struct {
	id      int64            // used to remove this queue from updateQueues[]
	site    *siteState       // tree being monitored
	updates []*rpc.CfgUpdate // pending updates
	newData chan bool        // tickled when data is added to updates[]

	sync.Mutex
}

type siteMetrics struct {
	tree *cfgtree.PTree // in-core cache of the metrics tree

	refreshedAt      time.Time
	refreshesWanted  int
	refreshScheduled bool
}

type siteState struct {
	siteUUID   string         // site UUID
	cachedTree *cfgtree.PTree // in-core cache of the config tree
	metrics    *siteMetrics

	cmdQueue     cmdQueue
	updateQueues map[int64]*updateQueue
	sync.Mutex   // protects the updateQueues map
}

var (
	metricsRefreshPeriod time.Duration

	state     = make(map[string]*siteState)
	stateLock sync.Mutex
)

func newSiteState(siteUUID string, tree *cfgtree.PTree) *siteState {
	var queue cmdQueue
	var m siteMetrics

	if environ.MemCmdQueue || environ.PostgresConnection == "" {
		queue = newMemCmdQueue(siteUUID, *cqMax)
	} else {
		queue = newDBCmdQueue(environ.PostgresConnection)
	}

	return &siteState{
		siteUUID:     siteUUID,
		cachedTree:   tree,
		metrics:      &m,
		cmdQueue:     queue,
		updateQueues: make(map[int64]*updateQueue),
	}
}

// XXX: this is an interim function.  When we get an update from an unknown
// site, we construct a new in-core cache for it.  Eventually this probably
// needs to be instantiated as part of the device provisioning process.
func initSiteState(siteUUID string) *siteState {
	stateLock.Lock()
	defer stateLock.Unlock()

	s, ok := state[siteUUID]
	if ok {
		return s
	}

	s = newSiteState(siteUUID, nil)
	state[siteUUID] = s
	return s
}

// Get the in-core state for a site; if not present, try to load
// it from the storage backend.
func getSiteState(ctx context.Context, siteUUID string) (*siteState, error) {
	stateLock.Lock()
	defer stateLock.Unlock()

	if siteUUID == "" {
		return nil, fmt.Errorf("No UUID provided")
	}

	s, ok := state[siteUUID]
	if ok {
		return s, nil
	}

	tree, err := store.get(ctx, siteUUID)
	if err == nil {
		s = newSiteState(siteUUID, tree)
		state[siteUUID] = s

		if environ.Emulate {
			slog.Infof("Enabled emulator for site %s", siteUUID)
			go s.emulateAppliance(context.Background())
		}
	}

	return s, err
}

// Issue one GET to refresh the @/metrics subtree, unless we already have a GET
// in-flight.  Schedule the next refresh if we have any more pending.
func (s *siteState) metricsRefresh() {
	m := s.metrics

	slog.Debugf("%s metrics refresh requested", s.siteUUID)
	m.refreshedAt = time.Now()
	refreshConfig(context.TODO(), s, s.siteUUID, metricsPath)

	if m.refreshesWanted--; m.refreshesWanted > 0 {
		slog.Debugf("%s metrics refresh scheduled", s.siteUUID)
		time.AfterFunc(metricsRefreshPeriod, s.metricsRefresh)
		m.refreshScheduled = true
	} else {
		m.refreshScheduled = false
	}
}

func (s *siteState) metricsScheduleRefresh() {
	m := s.metrics

	if m.refreshScheduled {
		return
	}

	nextRefresh := m.refreshedAt.Add(metricsRefreshPeriod)
	now := time.Now()
	if nextRefresh.Before(now) {
		// Current data is stale, so refresh it immediately
		s.metricsRefresh()
	} else {
		// Schedule a refresh request for when the current data becomes
		// stale.
		m.refreshScheduled = true
		until := nextRefresh.Sub(now)
		time.AfterFunc(until, s.metricsRefresh)
	}
}

func (s *siteState) metricsGet(prop string) (string, error) {
	var node *cfgtree.PNode
	var err error
	var payload []byte

	s.Lock()
	m := s.metrics

	if m.tree == nil {
		err = cfgtree.ErrNoProp
	} else {
		node, err = m.tree.GetNode(prop)
	}
	m.refreshesWanted = 2
	s.metricsScheduleRefresh()
	s.Unlock()

	if node != nil {
		payload, err = json.Marshal(node)
	}
	return string(payload), err
}

func (s *siteState) updateCaches(ctx context.Context, prop, rval string) {

	_, slog := daemonutils.EndpointLogger(ctx)

	if prop != rootPath && prop != metricsPath {
		slog.Warnf("asked to cache unsupported tree: %s", prop)
		return
	}

	tree, err := cfgtree.NewPTree(prop, []byte(rval))
	if err != nil {
		slog.Warnf("failed to refresh %s for %s: %v", prop, s.siteUUID, err)

	} else if prop == rootPath {
		slog.Infof("New tree for %s.  hash %x", s.siteUUID,
			tree.Root().Hash())
		s.Lock()
		s.cachedTree = tree
		s.Unlock()
		_ = s.store(ctx)

	} else if prop == metricsPath {
		slog.Debugf("metrics refresh completed")
		s.Lock()
		s.metrics.tree = tree
		s.Unlock()
	}
}

func (s *siteState) store(ctx context.Context) error {
	_, slog := daemonutils.EndpointLogger(ctx)
	if s.cachedTree == nil {
		return nil
	}
	err := store.set(ctx, s.siteUUID, s.cachedTree)
	if err != nil {
		slog.Errorf("Failed to store config for %v: %v", s.siteUUID, err)
		return err
	}
	slog.Debugf("Stored config for %v - %x", s.siteUUID, s.cachedTree.Root().Hash())
	return nil
}

// Execute a single ConfigQuery command, which may include multiple property
// updates.  This mimics work that would really be done by ap.configd on the
// appliance.  The changes made to the in-core tree are not persisted, so we
// will revert to the original tree next time cl.configd launches.
// XXX: is there a need for a Reset() rpc to trigger this cleanup without
// restarting the daemon?
func execute(t *cfgtree.PTree, ops *cfgmsg.ConfigQuery) *cfgmsg.ConfigResponse {
	var err error
	var rval cfgmsg.ConfigResponse

	t.ChangesetInit()

	for _, op := range ops.Ops {
		prop, val, expires, perr := getParams(op)
		if perr != nil {
			err = perr
			break
		}

		switch op.Operation {

		case cfgmsg.ConfigOp_SET:
			err = t.Set(prop, val, expires)

		case cfgmsg.ConfigOp_CREATE:
			err = t.Add(prop, val, expires)

		case cfgmsg.ConfigOp_DELETE:
			_, err = t.Delete(prop)
		}

		if err != nil {
			break
		}
	}

	if err == nil {
		rval.Response = cfgmsg.ConfigResponse_OK
		t.ChangesetCommit()
	} else {
		rval.Errmsg = fmt.Sprintf("%v", err)
		rval.Response = cfgmsg.ConfigResponse_FAILED
		t.ChangesetRevert()
	}

	rval.CmdID = ops.CmdID
	rval.Timestamp = ptypes.TimestampNow()

	return &rval
}

func delay() {
	const maxDelay = 3

	seconds := rand.Int() % maxDelay
	time.Sleep(time.Duration(seconds) * time.Second)
}

// Repeatedly pull commands from the queue, execute them, and post the results.
// Sleep for some number of seconds between iterations to emulate the
// asynchronous nature of interacting with a remote device.
func (s *siteState) emulateAppliance(ctx context.Context) {
	lastCmd := int64(-1)

	for {
		delay()
		ops, err := s.cmdQueue.fetch(ctx, s, lastCmd, 1, true)
		if err != nil {
			slog.Warnf("Emulator: Unexpected failure to fetch: %v", err)
			continue
		}
		if len(ops) > 0 {
			delay()
			for _, o := range ops {
				r := execute(s.cachedTree, o)
				_ = s.cmdQueue.complete(ctx, s, r)
				if o.CmdID > lastCmd {
					lastCmd = o.CmdID
				}
			}
		}
	}
}

// Allocate a new queue to receive updates posted to a site's config tree.
func (s *siteState) newUpdateQueue() *updateQueue {
	q := &updateQueue{
		site:    s,
		id:      time.Now().UnixNano(),
		updates: make([]*rpc.CfgUpdate, 0),
		newData: make(chan bool, 2),
	}

	s.Lock()
	s.updateQueues[q.id] = q
	s.Unlock()
	return q
}

// Remove one queue from a site's list of update queues
func (q *updateQueue) finalize() {
	q.site.Lock()
	delete(q.site.updateQueues, q.id)
	q.site.Unlock()
}

// Block until there are updates in the queue.  Return all posted updates.
func (q *updateQueue) fetch() []*rpc.CfgUpdate {
	<-q.newData
	q.Lock()
	u := q.updates
	q.updates = make([]*rpc.CfgUpdate, 0)
	q.Unlock()

	return u
}

// A site's config tree has been updated.  Post the update to all the queues for
// that site.
func (s *siteState) postUpdate(update *rpc.CfgUpdate) {
	s.Lock()
	for _, q := range s.updateQueues {
		q.Lock()
		q.updates = append(q.updates, update)
		// Only signal on the first update posted to a queue to avoid
		// blocking on a clogged channel.
		if len(q.updates) == 1 {
			q.newData <- true
		}
		q.Unlock()
	}
	s.Unlock()
}
