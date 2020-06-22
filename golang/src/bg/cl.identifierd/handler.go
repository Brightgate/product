/*
 * COPYRIGHT 2020 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package main

import (
	"context"
	"fmt"
	"net"
	"regexp"
	"sort"
	"sync"
	"time"

	"bg/base_msg"
	"bg/cl-obs/classifier"
	"bg/cl-obs/extract"
	"bg/cl-obs/sentence"
	"bg/cl_common/clcfg"
	"bg/cl_common/deviceinfo"
	"bg/cloud_models/appliancedb"
	"bg/cloud_rpc"
	"bg/common/cfgapi"
	"bg/common/network"

	"cloud.google.com/go/pubsub"
	"cloud.google.com/go/storage"
	"github.com/gogo/protobuf/proto"
	"github.com/klauspost/oui"
	"github.com/pkg/errors"
	"github.com/satori/uuid"
	"go.uber.org/zap"
	"golang.org/x/sync/semaphore"
	"google.golang.org/api/iterator"
)

const (
	objectPattern = `obs/(?P<mac>[a-f0-9:]*)/device_info.(?P<ts>[0-9]*).pb`
)

var (
	objectRE = regexp.MustCompile(objectPattern)
)

// ProtobufToTime converts a Protobuf timestamp into the equivalent Go version
// XXX common utils?  Move to base_msg?
func ProtobufToTime(ptime *base_msg.Timestamp) *time.Time {
	if ptime == nil {
		return nil
	}

	sec := *ptime.Seconds
	nano := int64(*ptime.Nanos)
	tmp := time.Unix(sec, nano)
	return &tmp
}

// Client represents a client device at a customer site
type Client struct {
	sync.Mutex
	HWAddr         net.HardwareAddr
	SentenceSeries *SentenceSeries
	Results        map[string]classifier.ClassifyResult
	Backfilled     bool
}

func newClient(mac uint64) *Client {
	hwaddr := network.Uint64ToHWAddr(mac)
	return &Client{
		HWAddr:         hwaddr,
		SentenceSeries: newSentenceSeries(hwaddr),
		Results:        make(map[string]classifier.ClassifyResult),
		Backfilled:     false,
	}
}

// Site represents a customer site; it holds a set of Clients
type Site struct {
	Clients map[uint64]*Client
}

func newSite() *Site {
	return &Site{
		Clients: make(map[uint64]*Client),
	}
}

// Client returns the client structure named by the mac address.
func (s *Site) Client(mac uint64) *Client {
	if _, ok := s.Clients[mac]; !ok {
		s.Clients[mac] = newClient(mac)
	}
	return s.Clients[mac]
}

// InventoryHandler represents a subsystem which can take observations
// received and do work based on them.
type InventoryHandler struct {
	sync.Mutex
	Sites               map[uuid.UUID]*Site
	ApplianceDB         appliancedb.DataStore
	StorageClient       *storage.Client
	Store               deviceinfo.Store
	OuiDB               oui.OuiDB
	BayesClassifiers    []*classifier.BayesClassifier
	MfgLookupClassifier *classifier.MfgLookupClassifier
	clientIngestSem     *semaphore.Weighted
}

const maxPerClientWorkers = int64(20)
const maxClientWorkers = int64(200)

func newInventoryHandler(db appliancedb.DataStore, storageClient *storage.Client,
	store deviceinfo.Store, ouiDB oui.OuiDB,
	classifiers []*classifier.BayesClassifier,
	mfgLookupClassifier *classifier.MfgLookupClassifier) *InventoryHandler {

	h := &InventoryHandler{
		Sites:               make(map[uuid.UUID]*Site),
		ApplianceDB:         db,
		Store:               store,
		StorageClient:       storageClient,
		BayesClassifiers:    classifiers,
		OuiDB:               ouiDB,
		MfgLookupClassifier: mfgLookupClassifier,
		clientIngestSem:     semaphore.NewWeighted(maxClientWorkers),
	}

	return h
}

// Site returns the Site structure named by the uuid
func (h *InventoryHandler) Site(uu uuid.UUID) *Site {
	if _, ok := h.Sites[uu]; !ok {
		h.Sites[uu] = newSite()
	}
	return h.Sites[uu]
}

// BackfillClient fills in deviceinfo records for the client from cloud
// storage.  This ensures that we have the best aggregate sentence for
// a client.
func (h *InventoryHandler) BackfillClient(ctx context.Context, slog *zap.SugaredLogger, siteUUID uuid.UUID, client *Client) error {
	if client.Backfilled {
		panic("client already backfilled")
	}

	res, err := h.ApplianceDB.CloudStorageByUUID(ctx, siteUUID)
	if err != nil {
		return err
	}
	bucket := h.StorageClient.Bucket(res.Bucket)
	prefix := fmt.Sprintf("obs/%s", client.HWAddr)
	slog.Infof("backfill: starting. source: %s/%s", res.Bucket, prefix)

	q := storage.Query{Prefix: prefix}
	if err := q.SetAttrSelection([]string{"Name", "Updated"}); err != nil {
		return errors.Wrap(err, "setting up GCS query")
	}
	objs := bucket.Objects(ctx, &q)

	// controls the worker goroutines for this backfill operation; this is
	// further governed by h.clientIngestSem
	clientIngestSem := semaphore.NewWeighted(maxPerClientWorkers)

	ingest := 0
	tuples := make([]deviceinfo.Tuple, 0)
	for {
		oattrs, err := objs.Next()
		if err != nil {
			if err == iterator.Done {
				break
			} else {
				slog.Warnf("failed to get next object: %s", err)
				continue
			}
		}
		om := objectRE.FindAllStringSubmatch(oattrs.Name, -1)
		if om == nil {
			slog.Warnf("object '%s' doesn't match pattern", oattrs.Name)
			continue
		}

		tuple, err := deviceinfo.NewTupleFromStrings(siteUUID.String(), om[0][1], om[0][2])
		if err != nil {
			slog.Fatalf("error building tuple: %v", err)
		}

		tuples = append(tuples, tuple)
	}
	nObjs := len(tuples)

	// Sort by tuple TS
	sort.Slice(tuples, func(i, j int) bool { return tuples[i].TS.Before(tuples[j].TS) })

	// Add all of the records from the cutoffTime forwards.  If that totals
	// less than minRecords, add older records until there are at least
	// minRecords.
	cutoffTime, minRecords := client.SentenceSeries.Bounds()

	// Work backwards
	addRecords := 0
	var i int
	for i = len(tuples) - 1; i >= 0; i-- {
		if addRecords < minRecords {
			addRecords++
			continue
		}
		if tuples[i].TS.After(cutoffTime) {
			addRecords++
			continue
		}
		// We're full and now too old
		i--
		break
	}
	slog.Debugf("backfill: going to add %d records of total %d tuples; i = %d", addRecords, len(tuples), i)
	// Lop off the too-old records
	i++
	skipped := i
	tuples = tuples[i:]

	if len(tuples) > 0 {
		oldest := tuples[0].TS
		newest := tuples[len(tuples)-1].TS
		slog.Debugf("backfill: oldest tuple: %s; newest tuple: %s", oldest.Format(time.RFC3339), newest.Format(time.RFC3339))
	}

	// Now work forwards from the oldest to newest
	for _, tuple := range tuples {
		if err := clientIngestSem.Acquire(ctx, 1); err != nil {
			slog.Fatalf("error getting objectIngest semaphore: %v", err)
		}
		if err := h.clientIngestSem.Acquire(ctx, 1); err != nil {
			slog.Fatalf("error getting objectIngest semaphore: %v", err)
		}
		ingest++

		go func(tuple deviceinfo.Tuple) {
			defer clientIngestSem.Release(1)
			defer h.clientIngestSem.Release(1)
			slog.Debugf("backfill: starting DeviceInfo %s", tuple)

			di, err := h.Store.ReadTuple(ctx, tuple)
			if err != nil {
				slog.Errorf("couldn't get DeviceInfo %s: %v", tuple, err)
				return
			}

			sent := extract.BayesSentenceFromDeviceInfo(h.OuiDB, di)
			_ = client.SentenceSeries.Add(tuple.TS, sent)
			slog.Debugf("backfill: finished DeviceInfo %s", tuple)
		}(tuple)
	}

	// Wait for all workers to finish
	_ = clientIngestSem.Acquire(context.TODO(), maxPerClientWorkers)
	slog.Infof("backfill: done; ingested %d of %d examined objects (%d skips)", ingest, nObjs, skipped)

	// So we don't do it again
	client.Backfilled = true
	return nil
}

func getConfig(ctx context.Context, siteUUID string) (*cfgapi.Handle, error) {
	url := environ.ConfigdConnection
	tls := !environ.DisableTLS
	conn, err := clcfg.NewConfigd(pname, siteUUID, url, tls)
	if err != nil {
		return nil, err
	}
	err = conn.Ping(ctx)
	if err != nil {
		return nil, err
	}
	cfg := cfgapi.NewHandle(conn)
	return cfg, nil
}

var model2prop = map[string]string{
	"bayes-os-4":     "classification/os_genus",
	"bayes-device-3": "classification/device_genus",
	"lookup-mfg":     "classification/oui_mfg",
}

var errNothingToPush = fmt.Errorf("nothing to push")

// PushToConfigTree compares the classification results for a client to the
// values stored in the corresponding config tree.  If they are different,
// it attempts to push those results to the site's config.
func (h *InventoryHandler) PushToConfigTree(ctx context.Context, slog *zap.SugaredLogger, siteUUID uuid.UUID, client *Client) error {
	cfg, err := getConfig(ctx, siteUUID.String())
	if err != nil {
		slog.Warnf("failed syncing %s: config handle: %s\n", err)
		return err
	}
	defer cfg.Close()

	clientPath := fmt.Sprintf("@/clients/%s", client.HWAddr.String())
	_, err = cfg.GetProps(clientPath)
	if err != nil {
		if err == cfgapi.ErrNoProp {
			slog.Infof("skipping client %s; not in tree", client.HWAddr)
			return nil
		}
		slog.Warnf("failed syncing: get @/clients : %s\n", err)
		return err
	}

	// For every client classification property we know about, generate
	// propop groups which clear out the classification info.
	propOps := make(map[string]cfgapi.PropertyOp)
	for model, prop := range model2prop {
		propPath := clientPath + "/" + prop
		oldValue, err := cfg.GetProp(propPath)
		if err == nil && oldValue != "" {
			propOps[prop] = cfgapi.PropertyOp{
				Op:   cfgapi.PropDelete,
				Name: propPath,
			}
		}
		if client.Results[model].Region != classifier.ClassifyCertain {
			// If we're not certain, then the PropDelete will be
			// executed.
			continue
		}
		newClassification := client.Results[model].Classification
		if oldValue == newClassification {
			delete(propOps, prop)
			continue
		}
		propOps[prop] = cfgapi.PropertyOp{
			Op:    cfgapi.PropCreate,
			Name:  propPath,
			Value: newClassification,
		}
	}

	prefix := ""
	if environ.DisablePush {
		prefix = "[push-disabled]: "
	}

	if len(propOps) == 0 {
		return errNothingToPush
	}

	// Work through the propOps.  Generate a PropOp array along with a
	// guarding PropTest (so we don't accidentally recreate a deleted
	// client).  Then execute.
	clientOps := []cfgapi.PropertyOp{
		{
			Op:   cfgapi.PropTest,
			Name: fmt.Sprintf("@/clients/%s", client.HWAddr),
		},
	}
	for _, pOp := range propOps {
		clientOps = append(clientOps, pOp)
	}

	slog.Infow(prefix+"sending ops", "ops", clientOps)
	if !environ.DisablePush {
		cmdHdl := cfg.Execute(ctx, clientOps)
		_, err := cmdHdl.Wait(ctx)
		if err != nil {
			slog.Infof("error on cfg execute/wait: %s", err)
			err := cmdHdl.Cancel(ctx)
			if err != nil {
				slog.Infof("tried to cancel operation, but cancelation failed: %s", err)
			} else {
				slog.Infof("cancelled config operation; site was not responsive")
			}
		}
	}
	return nil
}

// InventoryMessage accepts a new inventory message.
func (h *InventoryHandler) InventoryMessage(ctx context.Context, siteUUID uuid.UUID, m *pubsub.Message) {
	var err error

	inv := &cloud_rpc.InventoryReport{}

	slog := slog.With("site_uuid", m.Attributes["site_uuid"])
	site, err := h.ApplianceDB.CustomerSiteByUUID(ctx, siteUUID)
	if err == nil {
		// Makes it easier to see what is going on
		slog = slog.With("site_name", site.Name)
	}

	err = proto.Unmarshal(m.Data, inv)
	if err != nil {
		slog.Errorw("failed to unmarshal", "message", m, "error", err)
		return
	}

	inventory := inv.GetInventory()
	devices := inventory.GetDevices()
	slog.Infow("incoming inventory", "ts", inventory.Timestamp, "devices", len(devices))
	updatesPushed := 0

	for _, device := range devices {
		mac := device.GetMacAddress()
		hwaddr := network.Uint64ToHWAddr(mac)
		sent := extract.BayesSentenceFromDeviceInfo(h.OuiDB, device)
		slog := slog.With("hwaddr", hwaddr.String())

		var newS sentence.Sentence
		h.Lock()
		client := h.Site(siteUUID).Client(mac)
		h.Unlock()

		didBackfill := false
		client.Lock()
		if !client.Backfilled {
			didBackfill = true
			err := h.BackfillClient(ctx, slog, siteUUID, client)
			if err != nil {
				// Conservative approach: don't proceed for a
				// client we can't backfill.
				slog.Errorf("failed to backfill: %s", err)
				client.Unlock()
				continue
			}
		}
		client.Unlock()

		redundant := client.SentenceSeries.Add(*ProtobufToTime(device.GetUpdated()), sent)
		if !redundant || didBackfill {
			newS = client.SentenceSeries.Sentence
			slog.Infof("sentence now %v", newS)

			changed := 0
			// For presentation
			resultStrs := make([]string, 0)
			for _, c := range h.BayesClassifiers {
				res := c.Classify(newS)
				if !client.Results[c.ModelName].Equal(res) {
					changed++
					client.Results[res.ModelName] = res
					resultStrs = append(resultStrs, "!"+res.String())
				} else {
					resultStrs = append(resultStrs, res.String())
				}
			}
			lookupRes := h.MfgLookupClassifier.Classify(hwaddr)
			if !client.Results[lookupRes.ModelName].Equal(lookupRes) {
				changed++
				resultStrs = append(resultStrs, "!"+lookupRes.String())
				client.Results[lookupRes.ModelName] = lookupRes
			} else {
				resultStrs = append(resultStrs, lookupRes.String())
			}

			// Log results if there was a change; we restate all of
			// the results, with ! marking those which changed.
			if changed > 0 {
				slog.Infof("changed %d in-memory results: %v", changed, resultStrs)
				slog.Debugf("trying to push to config tree")
				err := h.PushToConfigTree(ctx, slog, siteUUID, client)
				if err == errNothingToPush {
					slog.Debugf("nothing to push to tree")
				} else if err != nil {
					slog.Errorf("error pushing to config tree: %v", err)
				} else {
					slog.Debugf("pushed to config tree")
					updatesPushed++
				}
			}
		}
	}
	slog.Infow("finished incoming inventory", "updatesPushed", updatesPushed)
}
