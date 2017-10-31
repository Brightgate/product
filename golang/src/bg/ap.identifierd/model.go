/*
 * COPYRIGHT 2017 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package main

import (
	"encoding/csv"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"bg/ap_common/aputil"
	"bg/ap_common/model"
	"bg/ap_common/network"
	"bg/base_msg"

	"github.com/golang/protobuf/proto"
	tf "github.com/tensorflow/tensorflow/tensorflow/go"
)

const collectionDuration = 30 * time.Minute

// saved_model_cli should be able to give us the names below by doing
//
//   $ saved_model_cli show --dir <model-dir> --tag_set serve --signature_def serving_default
//
// but the resulting output doesn't give the feature key or the class ID path.
// Manually inspect the saved model .pbtxt file to fill in what we need.
const tfFeaturesKey = "x"
const tfInput = "input_example_tensor"
const tfClassID = "linear/head/predictions/class_ids"
const tfProb = "linear/head/predictions/probabilities"

// See ap.configd/devices.json. Keep in sync with Python training script
const devIDBase = 2

// entity contains data about a client. The data is sent to the cloud for later
// use as training data. Some data is collected for only 30 minutes after seeing
// a NetEntity message which helps limit the volume of data we collect and
// reduces noisy in the data. The 30 minute timeout is reset if identiferd
// restarts.
//
// Some data we see may be "sensitive." Currently only DNS queries are deemed
// sensitive. A client can opt-out of DNS collection by setting that client's
// dns_private config option:
//
// $ ap-configctl add @/clients/dc:9b:9c:60:b8:6d/dns_private true
type entity struct {
	timeout time.Time
	private bool
	info    *base_msg.DeviceInfo
}

// entities is a vessel to collect data about clients.
type entities struct {
	sync.Mutex
	dataMap map[uint64]*entity
}

// prediction is a struct to communicate a new prediction from the named model.
// See Observations.Predict()
type prediction struct {
	hwaddr      uint64
	devID       string
	probability float32
}

type client struct {
	attrs    []string
	identity *prediction
}

// observations contains a subset of the data we observe from a client.
type observations struct {
	sync.Mutex

	// hwaddr -> client
	clients map[uint64]*client

	// attribute name -> column index
	attrMap map[string]int

	// TensorFlow data
	savedModel  *tf.SavedModel
	inputs      *tf.Operation
	outputProb  *tf.Operation
	outputClass *tf.Operation
}

func (e *entities) getEntityLocked(hwaddr uint64) *entity {
	_, ok := e.dataMap[hwaddr]
	if !ok {
		e.dataMap[hwaddr] = newEntity(hwaddr)
	}
	return e.dataMap[hwaddr]
}

func (e *entities) setPrivacy(mac net.HardwareAddr, private bool) {
	e.Lock()
	defer e.Unlock()

	d := e.getEntityLocked(network.HWAddrToUint64(mac))
	d.private = private
}

func (e *entities) addTimeout(hwaddr uint64) {
	e.Lock()
	defer e.Unlock()

	d := e.getEntityLocked(hwaddr)
	d.timeout = time.Now().Add(collectionDuration)
}

func (e *entities) addDHCPName(hwaddr uint64, name string) {
	e.Lock()
	defer e.Unlock()

	d := e.getEntityLocked(hwaddr)
	if d.info.DhcpName == nil {
		d.info.DhcpName = proto.String(name)
	}
}

func (e *entities) addMsgEntity(hwaddr uint64, msg *base_msg.EventNetEntity) {
	e.Lock()
	defer e.Unlock()

	d := e.getEntityLocked(hwaddr)
	if d.info.Entity == nil {
		d.info.Entity = msg
	}
}

func (e *entities) addMsgOptions(hwaddr uint64, msg *base_msg.DHCPOptions) {
	e.Lock()
	defer e.Unlock()

	d := e.getEntityLocked(hwaddr)
	d.info.Options = append(d.info.Options, msg)
}

// A message is recorded in this entity's DeviceInfo if the timeout is
// uninitialized (during identifierd startup for example) or if the timeout has
// not expired. If the message contains a feature included in
// observations.attrMap then we always record it.
func recordMsg(d *entity) bool {
	return d.timeout.IsZero() || time.Now().Before(d.timeout)
}

func (e *entities) addMsgScan(hwaddr uint64, msg *base_msg.EventNetScan, force bool) {
	e.Lock()
	defer e.Unlock()

	d := e.getEntityLocked(hwaddr)

	// We don't always get a get OS fingerprint on the first scan
	if force || recordMsg(d) {
		d.info.Scan = append(d.info.Scan, msg)
	}
}

func (e *entities) addMsgRequest(hwaddr uint64, msg *base_msg.EventNetRequest, force bool) {
	e.Lock()
	defer e.Unlock()

	d := e.getEntityLocked(hwaddr)
	if force || (recordMsg(d) && !d.private) {
		d.info.Request = append(d.info.Request, msg)
	}
}

func (e *entities) addMsgListen(hwaddr uint64, msg *base_msg.EventListen, force bool) {
	e.Lock()
	defer e.Unlock()

	d := e.getEntityLocked(hwaddr)
	if force || recordMsg(d) {
		d.info.Listen = append(d.info.Listen, msg)
	}
}

func (e *entities) writeInventory(path string) error {
	e.Lock()
	defer e.Unlock()

	tmpPath := path + ".tmp"
	f, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	inventory := &base_msg.DeviceInventory{
		Timestamp: aputil.NowToProtobuf(),
	}

	for _, d := range e.dataMap {
		inventory.Devices = append(inventory.Devices, d.info)
	}

	out, err := proto.Marshal(inventory)
	if err != nil {
		return fmt.Errorf("failed to encode device inventory: %s", err)
	}

	if _, err := f.Write(out); err != nil {
		return fmt.Errorf("failed to write device inventory: %s", err)
	}

	return os.Rename(tmpPath, path)
}

func (o *observations) loadModel(dataPath, modelPath string) error {
	o.Lock()
	defer o.Unlock()

	// Read the header from the training data. There is a very strong assumption
	// here that the file we are reading has the features/attributes in the same
	// order as the file used to train the model
	f, err := os.Open(dataPath)
	if err != nil {
		return fmt.Errorf("failed to open %s: %s", dataPath, err)
	}
	defer f.Close()
	reader := csv.NewReader(f)

	header, err := reader.Read()
	if err != nil {
		return fmt.Errorf("failed to read header from %s: %s", dataPath, err)
	}

	// The last entry is the device ID.
	last := len(header) - 1
	for i, attr := range header[:last] {
		o.attrMap[attr] = i
	}

	linearModel, err := tf.LoadSavedModel(modelPath, []string{"serve"}, nil)
	if err != nil {
		return fmt.Errorf("failed to LoadSavedModel at %s: %s", modelPath, err)
	}
	o.savedModel = linearModel

	// Test that we have the correct paths in the model. Store the ops for later
	// inference
	testForInputs := linearModel.Graph.Operation(tfInput)
	if testForInputs == nil {
		return fmt.Errorf("wrong input path %s", tfInput)
	}
	o.inputs = testForInputs

	testForProbs := linearModel.Graph.Operation(tfProb)
	if testForProbs == nil {
		return fmt.Errorf("wrong output path %s", tfProb)
	}
	o.outputProb = testForProbs

	testForClasses := linearModel.Graph.Operation(tfClassID)
	if testForClasses == nil {
		return fmt.Errorf("wrong output path %s", tfClassID)
	}
	o.outputClass = testForClasses

	return nil
}

func (o *observations) setByName(hwaddr uint64, attr string) bool {
	o.Lock()
	defer o.Unlock()

	if _, ok := o.clients[hwaddr]; !ok {
		c := &client{
			attrs:    make([]string, len(o.attrMap)),
			identity: &prediction{hwaddr, "0", 0.0},
		}

		for i := 0; i < len(c.attrs); i++ {
			c.attrs[i] = "0"
		}
		o.clients[hwaddr] = c
	}

	col, ok := o.attrMap[attr]
	if !ok {
		return false
	}
	o.clients[hwaddr].attrs[col] = "1"
	return true
}

func (o *observations) inference(c *client) (int64, float32, error) {
	var runResult []*tf.Tensor
	var runErr error

	example, err := model.FormatTFExample(tfFeaturesKey, strings.Join(c.attrs, ","))
	if err != nil {
		return 0, 0, fmt.Errorf("failed to make tf.Example: %s", err)
	}

	feeds := make(map[tf.Output]*tf.Tensor)
	feeds[o.inputs.Output(0)] = example[0]

	fetchProb := make([]tf.Output, 1)
	fetchProb[0] = o.outputProb.Output(0)

	fetchClass := make([]tf.Output, 1)
	fetchClass[0] = o.outputClass.Output(0)

	// Fetch the class first. The runResult is the class id which provides
	// an index into the fetched probabilities
	runResult, runErr = o.savedModel.Session.Run(feeds, fetchClass, nil)
	if runErr != nil {
		return 0, 0, fmt.Errorf("fetching class ID failed: %s", runErr)
	}
	predClass := runResult[0].Value().([]int64)[0]

	runResult, runErr = o.savedModel.Session.Run(feeds, fetchProb, nil)
	if runErr != nil {
		return 0, 0, fmt.Errorf("fetching probabilities failed: %s", runErr)
	}
	predProb := runResult[0].Value().([][]float32)[0]
	classProb := predProb[predClass]

	return predClass, classProb, nil
}

func (o *observations) predictClients(ch chan *prediction) {
	for _, c := range o.clients {
		devID, prob, err := o.inference(c)
		if err != nil {
			log.Printf("failed to run inference: %s\n", err)
		}

		// The model returns the most probable identity. If the identity has
		// changed then the old identity is now less probable than (or equal to)
		// the new identity, so send an update. If the identity hasn't changed
		// but the model's confience has, send an update
		newID := strconv.FormatInt(devID+devIDBase, 10)
		if newID != c.identity.devID || prob != c.identity.probability {
			c.identity.devID = newID
			c.identity.probability = prob
			ch <- c.identity
		}
	}
}

// Predict periodically runs predictions over the entire set of Observations.
// When a client's predicted identity changes a new prediction is sent on the
// channel returned to the caller.
func (o *observations) predict() <-chan *prediction {
	predCh := make(chan *prediction)

	go func(ch chan *prediction) {
		tick := time.NewTicker(time.Minute)
		for {
			<-tick.C
			o.Lock()
			o.predictClients(ch)
			o.Unlock()
		}
	}(predCh)
	return predCh
}

func newEntity(hwaddr uint64) *entity {
	ret := &entity{
		private: false,
		info: &base_msg.DeviceInfo{
			Timestamp:  aputil.NowToProtobuf(),
			MacAddress: proto.Uint64(hwaddr),
		},
	}
	return ret
}

// NewEntities creates an empty Entities
func newEntities() *entities {
	ret := &entities{
		dataMap: make(map[uint64]*entity),
	}
	return ret
}

// NewObservations creates an empty Observations
func newObservations() *observations {
	ret := &observations{
		clients: make(map[uint64]*client),
		attrMap: make(map[string]int),
	}
	return ret
}
